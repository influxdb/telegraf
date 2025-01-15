//go:generate ../../../tools/readme_config_includer/generator
package sql

import (
	"context"
	dbsql "database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var disconnectedServersBehavior = []string{"error", "ignore"}

const magicIdleCount = -int(^uint(0) >> 1)

type SQL struct {
	Driver                      string          `toml:"driver"`
	Dsn                         config.Secret   `toml:"dsn"`
	Timeout                     config.Duration `toml:"timeout"`
	MaxIdleTime                 config.Duration `toml:"connection_max_idle_time"`
	MaxLifetime                 config.Duration `toml:"connection_max_life_time"`
	MaxOpenConnections          int             `toml:"connection_max_open"`
	MaxIdleConnections          int             `toml:"connection_max_idle"`
	Queries                     []query         `toml:"query"`
	Log                         telegraf.Logger `toml:"-"`
	DisconnectedServersBehavior string          `toml:"disconnected_servers_behavior"`

	driverName      string
	db              *dbsql.DB
	serverConnected bool
}

type query struct {
	Query               string   `toml:"query"`
	Script              string   `toml:"query_script"`
	Measurement         string   `toml:"measurement"`
	MeasurementColumn   string   `toml:"measurement_column"`
	TimeColumn          string   `toml:"time_column"`
	TimeFormat          string   `toml:"time_format"`
	TagColumnsInclude   []string `toml:"tag_columns_include"`
	TagColumnsExclude   []string `toml:"tag_columns_exclude"`
	FieldColumnsInclude []string `toml:"field_columns_include"`
	FieldColumnsExclude []string `toml:"field_columns_exclude"`
	FieldColumnsFloat   []string `toml:"field_columns_float"`
	FieldColumnsInt     []string `toml:"field_columns_int"`
	FieldColumnsUint    []string `toml:"field_columns_uint"`
	FieldColumnsBool    []string `toml:"field_columns_bool"`
	FieldColumnsString  []string `toml:"field_columns_string"`

	statement         *dbsql.Stmt
	tagFilter         filter.Filter
	fieldFilter       filter.Filter
	fieldFilterFloat  filter.Filter
	fieldFilterInt    filter.Filter
	fieldFilterUint   filter.Filter
	fieldFilterBool   filter.Filter
	fieldFilterString filter.Filter
}

func (*SQL) SampleConfig() string {
	return sampleConfig
}

func (s *SQL) Init() error {
	// Option handling
	if s.Driver == "" {
		return errors.New("missing SQL driver option")
	}

	if err := s.checkDSN(); err != nil {
		return err
	}

	if s.Timeout <= 0 {
		s.Timeout = config.Duration(5 * time.Second)
	}

	if s.MaxIdleConnections == magicIdleCount {
		// Determine the number by the number of queries + the golang default value
		s.MaxIdleConnections = len(s.Queries) + 2
	}

	for i, q := range s.Queries {
		if q.Query == "" && q.Script == "" {
			return errors.New("neither 'query' nor 'query_script' specified")
		}

		if q.Query != "" && q.Script != "" {
			return errors.New("only one of 'query' and 'query_script' can be specified")
		}

		// In case we got a script, we should read the query now.
		if q.Script != "" {
			query, err := os.ReadFile(q.Script)
			if err != nil {
				return fmt.Errorf("reading script %q failed: %w", q.Script, err)
			}
			s.Queries[i].Query = string(query)
		}

		// Time format
		if q.TimeFormat == "" {
			s.Queries[i].TimeFormat = "unix"
		}

		// Compile the tag-filter
		tagfilter, err := filter.NewIncludeExcludeFilterDefaults(q.TagColumnsInclude, q.TagColumnsExclude, false, false)
		if err != nil {
			return fmt.Errorf("creating tag filter failed: %w", err)
		}
		s.Queries[i].tagFilter = tagfilter

		// Compile the explicit type field-filter
		fieldfilterFloat, err := filter.NewIncludeExcludeFilterDefaults(q.FieldColumnsFloat, nil, false, false)
		if err != nil {
			return fmt.Errorf("creating field filter for float failed: %w", err)
		}
		s.Queries[i].fieldFilterFloat = fieldfilterFloat

		fieldfilterInt, err := filter.NewIncludeExcludeFilterDefaults(q.FieldColumnsInt, nil, false, false)
		if err != nil {
			return fmt.Errorf("creating field filter for int failed: %w", err)
		}
		s.Queries[i].fieldFilterInt = fieldfilterInt

		fieldfilterUint, err := filter.NewIncludeExcludeFilterDefaults(q.FieldColumnsUint, nil, false, false)
		if err != nil {
			return fmt.Errorf("creating field filter for uint failed: %w", err)
		}
		s.Queries[i].fieldFilterUint = fieldfilterUint

		fieldfilterBool, err := filter.NewIncludeExcludeFilterDefaults(q.FieldColumnsBool, nil, false, false)
		if err != nil {
			return fmt.Errorf("creating field filter for bool failed: %w", err)
		}
		s.Queries[i].fieldFilterBool = fieldfilterBool

		fieldfilterString, err := filter.NewIncludeExcludeFilterDefaults(q.FieldColumnsString, nil, false, false)
		if err != nil {
			return fmt.Errorf("creating field filter for string failed: %w", err)
		}
		s.Queries[i].fieldFilterString = fieldfilterString

		// Compile the field-filter
		fieldfilter, err := filter.NewIncludeExcludeFilter(q.FieldColumnsInclude, q.FieldColumnsExclude)
		if err != nil {
			return fmt.Errorf("creating field filter failed: %w", err)
		}
		s.Queries[i].fieldFilter = fieldfilter

		if q.Measurement == "" {
			s.Queries[i].Measurement = "sql"
		}
	}

	// Derive the sql-framework driver name from our config name. This abstracts the actual driver
	// from the database-type the user wants.
	aliases := map[string]string{
		"cockroach": "pgx",
		"tidb":      "mysql",
		"mssql":     "sqlserver",
		"maria":     "mysql",
		"postgres":  "pgx",
		"oracle":    "oracle",
	}
	s.driverName = s.Driver
	if driver, ok := aliases[s.Driver]; ok {
		s.driverName = driver
	}

	availDrivers := dbsql.Drivers()
	if !choice.Contains(s.driverName, availDrivers) {
		for d, r := range aliases {
			if choice.Contains(r, availDrivers) {
				availDrivers = append(availDrivers, d)
			}
		}

		// Sort the list of drivers and make them unique
		sort.Strings(availDrivers)
		last := 0
		for _, d := range availDrivers {
			if d != availDrivers[last] {
				last++
				availDrivers[last] = d
			}
		}
		availDrivers = availDrivers[:last+1]

		return fmt.Errorf("driver %q not supported use one of %v", s.Driver, availDrivers)
	}

	if s.DisconnectedServersBehavior == "" {
		s.DisconnectedServersBehavior = "error"
	}

	if !choice.Contains(s.DisconnectedServersBehavior, disconnectedServersBehavior) {
		return fmt.Errorf("%q is not a valid value for disconnected_servers_behavior", s.DisconnectedServersBehavior)
	}

	return nil
}

func (s *SQL) Start(telegraf.Accumulator) error {
	if err := s.setupConnection(); err != nil {
		return err
	}

	if err := s.ping(); err != nil {
		if s.DisconnectedServersBehavior == "error" {
			return err
		}
		s.Log.Errorf("unable to connect to database: %s", err)
	}
	if s.serverConnected {
		s.prepareStatements()
	}

	return nil
}

func (s *SQL) Gather(acc telegraf.Accumulator) error {
	// during plugin startup, it is possible that the server was not reachable.
	// we try pinging the server in this collection cycle.
	// we are only concerned with `prepareStatements` function to complete(return true), just once.
	if !s.serverConnected {
		if err := s.ping(); err != nil {
			return err
		}
		s.prepareStatements()
	}

	var wg sync.WaitGroup
	tstart := time.Now()
	for _, q := range s.Queries {
		wg.Add(1)
		go func(q query) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.Timeout))
			defer cancel()
			if err := s.executeQuery(ctx, acc, q, tstart); err != nil {
				acc.AddError(err)
			}
		}(q)
	}
	wg.Wait()
	s.Log.Debugf("Executed %d queries in %s", len(s.Queries), time.Since(tstart).String())

	return nil
}

func (s *SQL) Stop() {
	// Free the statements
	for _, q := range s.Queries {
		if q.statement != nil {
			if err := q.statement.Close(); err != nil {
				s.Log.Errorf("closing statement for query %q failed: %v", q.Query, err)
			}
		}
	}

	// Close the connection to the server
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			s.Log.Errorf("closing database connection failed: %v", err)
		}
	}
}

func (s *SQL) setupConnection() error {
	// Connect to the database server
	dsnSecret, err := s.Dsn.Get()
	if err != nil {
		return fmt.Errorf("getting DSN failed: %w", err)
	}
	dsn := dsnSecret.String()
	dsnSecret.Destroy()

	s.Log.Debug("Connecting...")
	s.db, err = dbsql.Open(s.driverName, dsn)
	if err != nil {
		// should return since the error is most likely with invalid DSN string format
		return err
	}

	// Set the connection limits
	// s.db.SetConnMaxIdleTime(time.Duration(s.MaxIdleTime)) // Requires go >= 1.15
	s.db.SetConnMaxLifetime(time.Duration(s.MaxLifetime))
	s.db.SetMaxOpenConns(s.MaxOpenConnections)
	s.db.SetMaxIdleConns(s.MaxIdleConnections)
	return nil
}

func (s *SQL) ping() error {
	// Test if the connection can be established
	s.Log.Debug("Testing connectivity...")
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.Timeout))
	err := s.db.PingContext(ctx)
	cancel()
	if err != nil {
		return fmt.Errorf("unable to connect to database: %w", err)
	}
	s.serverConnected = true
	return nil
}

func (s *SQL) prepareStatements() {
	// Prepare the statements
	for i, q := range s.Queries {
		s.Log.Debugf("Preparing statement %q...", q.Query)
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.Timeout))
		stmt, err := s.db.PrepareContext(ctx, q.Query)
		cancel()
		if err != nil {
			// Some database drivers or databases do not support prepare
			// statements and report an error here. However, we can still
			// execute unprepared queries for those setups so do not bail-out
			// here but simply do leave the `statement` with a `nil` value
			// indicating no prepared statement.
			s.Log.Warnf("preparing query %q failed: %s; falling back to unprepared query", q.Query, err)
			continue
		}
		s.Queries[i].statement = stmt
	}
}

func (s *SQL) executeQuery(ctx context.Context, acc telegraf.Accumulator, q query, tquery time.Time) error {
	// Execute the query either prepared or unprepared
	var rows *dbsql.Rows
	if q.statement != nil {
		// Use the previously prepared query
		var err error
		rows, err = q.statement.QueryContext(ctx)
		if err != nil {
			return err
		}
	} else {
		// Fallback to unprepared query
		var err error
		rows, err = s.db.Query(q.Query)
		if err != nil {
			return err
		}
	}
	defer rows.Close()

	// Handle the rows
	columnNames, err := rows.Columns()
	if err != nil {
		return err
	}
	rowCount, err := q.parse(acc, rows, tquery, s.Log)
	s.Log.Debugf("Received %d rows and %d columns for query %q", rowCount, len(columnNames), q.Query)

	return err
}

func (s *SQL) checkDSN() error {
	if s.Dsn.Empty() {
		return errors.New("missing data source name (DSN) option")
	}
	return nil
}

func (q *query) parse(acc telegraf.Accumulator, rows *dbsql.Rows, t time.Time, logger telegraf.Logger) (int, error) {
	columnNames, err := rows.Columns()
	if err != nil {
		return 0, err
	}

	// Prepare the list of datapoints according to the received row
	columnData := make([]interface{}, len(columnNames))
	columnDataPtr := make([]interface{}, len(columnNames))

	for i := range columnData {
		columnDataPtr[i] = &columnData[i]
	}

	rowCount := 0
	for rows.Next() {
		measurement := q.Measurement
		timestamp := t
		tags := make(map[string]string)
		fields := make(map[string]interface{}, len(columnNames))

		// Do the parsing with (hopefully) automatic type conversion
		if err := rows.Scan(columnDataPtr...); err != nil {
			return 0, err
		}

		for i, name := range columnNames {
			if q.MeasurementColumn != "" && name == q.MeasurementColumn {
				switch raw := columnData[i].(type) {
				case string:
					measurement = raw
				case []byte:
					measurement = string(raw)
				default:
					return 0, fmt.Errorf("measurement column type \"%T\" unsupported", columnData[i])
				}
			}

			if q.TimeColumn != "" && name == q.TimeColumn {
				var fieldvalue interface{}
				var skipParsing bool

				switch v := columnData[i].(type) {
				case string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
					fieldvalue = v
				case []byte:
					fieldvalue = string(v)
				case time.Time:
					timestamp = v
					skipParsing = true
				case fmt.Stringer:
					fieldvalue = v.String()
				default:
					return 0, fmt.Errorf("time column %q of type \"%T\" unsupported", name, columnData[i])
				}
				if !skipParsing {
					if timestamp, err = internal.ParseTimestamp(q.TimeFormat, fieldvalue, nil); err != nil {
						return 0, fmt.Errorf("parsing time failed: %w", err)
					}
				}
			}

			if q.tagFilter.Match(name) {
				tagvalue, err := internal.ToString(columnData[i])
				if err != nil {
					return 0, fmt.Errorf("converting tag column %q failed: %w", name, err)
				}
				if v := strings.TrimSpace(tagvalue); v != "" {
					tags[name] = v
				}
			}

			// Explicit type conversions take precedence
			if q.fieldFilterFloat.Match(name) {
				v, err := internal.ToFloat64(columnData[i])
				if err != nil {
					return 0, fmt.Errorf("converting field column %q to float failed: %w", name, err)
				}
				fields[name] = v
				continue
			}

			if q.fieldFilterInt.Match(name) {
				v, err := internal.ToInt64(columnData[i])
				if err != nil {
					if err != nil {
						if !errors.Is(err, internal.ErrOutOfRange) {
							return 0, fmt.Errorf("converting field column %q to int failed: %w", name, err)
						}
						logger.Warnf("field column %q: %v", name, err)
					}
				}
				fields[name] = v
				continue
			}

			if q.fieldFilterUint.Match(name) {
				v, err := internal.ToUint64(columnData[i])
				if err != nil {
					if !errors.Is(err, internal.ErrOutOfRange) {
						return 0, fmt.Errorf("converting field column %q to uint failed: %w", name, err)
					}
					logger.Warnf("field column %q: %v", name, err)
				}
				fields[name] = v
				continue
			}

			if q.fieldFilterBool.Match(name) {
				v, err := internal.ToBool(columnData[i])
				if err != nil {
					return 0, fmt.Errorf("converting field column %q to bool failed: %w", name, err)
				}
				fields[name] = v
				continue
			}

			if q.fieldFilterString.Match(name) {
				v, err := internal.ToString(columnData[i])
				if err != nil {
					return 0, fmt.Errorf("converting field column %q to string failed: %w", name, err)
				}
				fields[name] = v
				continue
			}

			// Try automatic conversion for all remaining fields
			if q.fieldFilter.Match(name) {
				var fieldvalue interface{}
				switch v := columnData[i].(type) {
				case string, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
					fieldvalue = v
				case []byte:
					fieldvalue = string(v)
				case time.Time:
					fieldvalue = v.UnixNano()
				case nil:
					fieldvalue = nil
				case fmt.Stringer:
					fieldvalue = v.String()
				default:
					return 0, fmt.Errorf("field column %q of type \"%T\" unsupported", name, columnData[i])
				}
				if fieldvalue != nil {
					fields[name] = fieldvalue
				}
			}
		}
		acc.AddFields(measurement, fields, tags, timestamp)
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return rowCount, err
	}

	return rowCount, nil
}

func init() {
	inputs.Add("sql", func() telegraf.Input {
		return &SQL{
			MaxIdleTime:        config.Duration(0), // unlimited
			MaxLifetime:        config.Duration(0), // unlimited
			MaxOpenConnections: 0,                  // unlimited
			MaxIdleConnections: magicIdleCount,     // will trigger auto calculation
		}
	})
}
