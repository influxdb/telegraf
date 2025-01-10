//go:generate ../../../tools/readme_config_includer/generator
package postgresql_extensible

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	// Required for SQL framework driver
	_ "github.com/jackc/pgx/v4/stdlib"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/common/postgresql"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var ignoredColumns = map[string]bool{"stats_reset": true}

type Postgresql struct {
	Databases          []string        `deprecated:"1.22.4;use the sqlquery option to specify database to use"`
	Query              []query         `toml:"query"`
	PreparedStatements bool            `toml:"prepared_statements"`
	Log                telegraf.Logger `toml:"-"`
	postgresql.Config

	service *postgresql.Service
}

type query struct {
	Sqlquery    string `toml:"sqlquery"`
	Script      string `toml:"script"`
	Version     int    `deprecated:"1.28.0;use minVersion to specify minimal DB version this query supports"`
	MinVersion  int    `toml:"min_version"`
	MaxVersion  int    `toml:"max_version"`
	Withdbname  bool   `deprecated:"1.22.4;use the sqlquery option to specify database to use"`
	Tagvalue    string `toml:"tagvalue"`
	Measurement string `toml:"measurement"`
	Timestamp   string `toml:"timestamp"`

	additionalTags map[string]bool
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func (*Postgresql) SampleConfig() string {
	return sampleConfig
}

func (p *Postgresql) Init() error {
	// Set defaults for the queries
	for i, q := range p.Query {
		if q.Sqlquery == "" {
			query, err := os.ReadFile(q.Script)
			if err != nil {
				return err
			}
			q.Sqlquery = string(query)
		}
		if q.MinVersion == 0 {
			q.MinVersion = q.Version
		}
		if q.Measurement == "" {
			q.Measurement = "postgresql"
		}

		var queryAddon string
		if q.Withdbname {
			if len(p.Databases) != 0 {
				queryAddon = fmt.Sprintf(` IN ('%s')`, strings.Join(p.Databases, "','"))
			} else {
				queryAddon = " is not null"
			}
		}
		q.Sqlquery += queryAddon

		q.additionalTags = make(map[string]bool)
		if q.Tagvalue != "" {
			for _, tag := range strings.Split(q.Tagvalue, ",") {
				q.additionalTags[tag] = true
			}
		}
		p.Query[i] = q
	}
	p.Config.IsPgBouncer = !p.PreparedStatements

	// Create a service to access the PostgreSQL server
	service, err := p.Config.CreateService()
	if err != nil {
		return err
	}
	p.service = service

	return nil
}

func (p *Postgresql) Start(_ telegraf.Accumulator) error {
	return p.service.Start()
}

func (p *Postgresql) Gather(acc telegraf.Accumulator) error {
	// Retrieving the database version
	query := `SELECT setting::integer / 100 AS version FROM pg_settings WHERE name = 'server_version_num'`
	var dbVersion int
	if err := p.service.DB.QueryRow(query).Scan(&dbVersion); err != nil {
		dbVersion = 0
	}

	// set default timestamp to Now and use for all generated metrics during
	// the same Gather call
	timestamp := time.Now()

	// We loop in order to process each query
	// Query is not run if Database version does not match the query version.
	for _, q := range p.Query {
		if q.MinVersion <= dbVersion && (q.MaxVersion == 0 || q.MaxVersion > dbVersion) {
			acc.AddError(p.gatherMetricsFromQuery(acc, q, timestamp))
		}
	}
	return nil
}

func (p *Postgresql) Stop() {
	p.service.Stop()
}

func (p *Postgresql) gatherMetricsFromQuery(acc telegraf.Accumulator, q query, timestamp time.Time) error {
	rows, err := p.service.DB.Query(q.Sqlquery)
	if err != nil {
		return err
	}

	defer rows.Close()

	// grab the column information from the result
	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	for rows.Next() {
		if err := p.accRow(acc, rows, columns, q, timestamp); err != nil {
			return err
		}
	}
	return nil
}

func (p *Postgresql) accRow(acc telegraf.Accumulator, row scanner, columns []string, q query, timestamp time.Time) error {
	// this is where we'll store the column name with its *interface{}
	columnMap := make(map[string]*interface{})

	for _, column := range columns {
		columnMap[column] = new(interface{})
	}

	columnVars := make([]interface{}, 0, len(columnMap))
	// populate the array of interface{} with the pointers in the right order
	for i := 0; i < len(columnMap); i++ {
		columnVars = append(columnVars, columnMap[columns[i]])
	}

	// deconstruct array of variables and send to Scan
	if err := row.Scan(columnVars...); err != nil {
		return err
	}

	var dbname bytes.Buffer
	if c, ok := columnMap["datname"]; ok && *c != nil {
		// extract the database name from the column map
		switch datname := (*c).(type) {
		case string:
			dbname.WriteString(datname)
		default:
			dbname.WriteString(p.service.ConnectionDatabase)
		}
	} else {
		dbname.WriteString(p.service.ConnectionDatabase)
	}

	// Process the additional tags
	tags := map[string]string{
		"server": p.service.SanitizedAddress,
		"db":     dbname.String(),
	}

	fields := make(map[string]interface{})
	for col, val := range columnMap {
		p.Log.Debugf("Column: %s = %T: %v\n", col, *val, *val)
		_, ignore := ignoredColumns[col]
		if ignore || *val == nil {
			continue
		}

		if col == q.Timestamp {
			if v, ok := (*val).(time.Time); ok {
				timestamp = v
			}
			continue
		}

		if q.additionalTags[col] {
			v, err := internal.ToString(*val)
			if err != nil {
				p.Log.Debugf("Failed to add %q as additional tag: %v", col, err)
			} else {
				tags[col] = v
			}
			continue
		}

		if v, ok := (*val).([]byte); ok {
			fields[col] = string(v)
		} else {
			fields[col] = *val
		}
	}
	acc.AddFields(q.Measurement, fields, tags, timestamp)
	return nil
}

func init() {
	inputs.Add("postgresql_extensible", func() telegraf.Input {
		return &Postgresql{
			Config: postgresql.Config{
				MaxIdle: 1,
				MaxOpen: 1,
			},
			PreparedStatements: true,
		}
	})
}
