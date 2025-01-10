//go:generate ../../../tools/readme_config_includer/generator
package pgbouncer

import (
	"bytes"
	"database/sql"
	_ "embed"
	"fmt"
	"strconv"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/common/postgresql"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var ignoredColumns = map[string]bool{"user": true, "database": true, "pool_mode": true,
	"avg_req": true, "avg_recv": true, "avg_sent": true, "avg_query": true,
	"force_user": true, "host": true, "port": true, "name": true,
}

type PgBouncer struct {
	ShowCommands []string `toml:"show_commands"`
	postgresql.Config

	service *postgresql.Service
}

func (*PgBouncer) SampleConfig() string {
	return sampleConfig
}

func (p *PgBouncer) Init() error {
	// Set defaults and check settings
	if len(p.ShowCommands) == 0 {
		p.ShowCommands = []string{"stats", "pools"}
	}
	for _, cmd := range p.ShowCommands {
		switch cmd {
		case "stats", "pools", "lists", "databases":
		default:
			return fmt.Errorf("invalid setting %q for 'show_command'", cmd)
		}
	}

	// Create a postgres service for the queries
	service, err := p.Config.CreateService()
	if err != nil {
		return err
	}
	p.service = service
	return nil
}

func (p *PgBouncer) Start(_ telegraf.Accumulator) error {
	return p.service.Start()
}

func (p *PgBouncer) Gather(acc telegraf.Accumulator) error {
	for _, cmd := range p.ShowCommands {
		switch cmd {
		case "stats":
			if err := p.showStats(acc); err != nil {
				return err
			}
		case "pools":
			if err := p.showPools(acc); err != nil {
				return err
			}
		case "lists":
			if err := p.showLists(acc); err != nil {
				return err
			}
		case "databases":
			if err := p.showDatabase(acc); err != nil {
				return err
			}
		}
	}

	return nil
}

func (p *PgBouncer) Stop() {
	p.service.Stop()
}

func (p *PgBouncer) accRow(row *sql.Rows, columns []string) (map[string]string, map[string]*interface{}, error) {
	var dbname bytes.Buffer

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
	err := row.Scan(columnVars...)
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't copy the data: %w", err)
	}
	if columnMap["database"] != nil {
		// extract the database name from the column map
		name, ok := (*columnMap["database"]).(string)
		if !ok {
			return nil, nil, fmt.Errorf("database not a string, but %T", *columnMap["database"])
		}
		dbname.WriteString(name)
	} else {
		dbname.WriteString("pgbouncer")
	}

	// Return basic tags and the mapped columns
	return map[string]string{"server": p.service.SanitizedAddress, "db": dbname.String()}, columnMap, nil
}

func (p *PgBouncer) showStats(acc telegraf.Accumulator) error {
	// STATS
	rows, err := p.service.DB.Query(`SHOW STATS`)
	if err != nil {
		return fmt.Errorf("execution error 'show stats': %w", err)
	}

	defer rows.Close()

	// grab the column information from the result
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("don't get column names 'show stats': %w", err)
	}

	for rows.Next() {
		tags, columnMap, err := p.accRow(rows, columns)
		if err != nil {
			return err
		}

		fields := make(map[string]interface{})
		for col, val := range columnMap {
			_, ignore := ignoredColumns[col]
			if ignore {
				continue
			}

			switch v := (*val).(type) {
			case int64:
				// Integer fields are returned in pgbouncer 1.5 through 1.9
				fields[col] = v
			case string:
				// Integer fields are returned in pgbouncer 1.12
				integer, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return fmt.Errorf("couldn't convert metrics 'show stats': %w", err)
				}

				fields[col] = integer
			}
		}
		acc.AddFields("pgbouncer", fields, tags)
	}

	return rows.Err()
}

func (p *PgBouncer) showPools(acc telegraf.Accumulator) error {
	// POOLS
	poolRows, err := p.service.DB.Query(`SHOW POOLS`)
	if err != nil {
		return fmt.Errorf("execution error 'show pools': %w", err)
	}

	defer poolRows.Close()

	// grab the column information from the result
	columns, err := poolRows.Columns()
	if err != nil {
		return fmt.Errorf("don't get column names 'show pools': %w", err)
	}

	for poolRows.Next() {
		tags, columnMap, err := p.accRow(poolRows, columns)
		if err != nil {
			return err
		}

		if user, ok := columnMap["user"]; ok {
			if s, ok := (*user).(string); ok && s != "" {
				tags["user"] = s
			}
		}

		if poolMode, ok := columnMap["pool_mode"]; ok {
			if s, ok := (*poolMode).(string); ok && s != "" {
				tags["pool_mode"] = s
			}
		}

		fields := make(map[string]interface{})
		for col, val := range columnMap {
			_, ignore := ignoredColumns[col]
			if !ignore {
				fields[col] = *val
			}
		}
		acc.AddFields("pgbouncer_pools", fields, tags)
	}

	return poolRows.Err()
}

func (p *PgBouncer) showLists(acc telegraf.Accumulator) error {
	// LISTS
	rows, err := p.service.DB.Query(`SHOW LISTS`)
	if err != nil {
		return fmt.Errorf("execution error 'show lists': %w", err)
	}

	defer rows.Close()

	// grab the column information from the result
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("don't get column names 'show lists': %w", err)
	}

	fields := make(map[string]interface{})
	tags := make(map[string]string)
	for rows.Next() {
		tag, columnMap, err := p.accRow(rows, columns)
		if err != nil {
			return err
		}

		name, ok := (*columnMap["list"]).(string)
		if !ok {
			return fmt.Errorf("metric name(show lists) not a string, but %T", *columnMap["list"])
		}
		if name != "dns_pending" {
			value, ok := (*columnMap["items"]).(int64)
			if !ok {
				return fmt.Errorf("metric value(show lists) not a int64, but %T", *columnMap["items"])
			}
			fields[name] = value
			tags = tag
		}
	}
	acc.AddFields("pgbouncer_lists", fields, tags)

	return rows.Err()
}

func (p *PgBouncer) showDatabase(acc telegraf.Accumulator) error {
	// DATABASES
	rows, err := p.service.DB.Query(`SHOW DATABASES`)
	if err != nil {
		return fmt.Errorf("execution error 'show database': %w", err)
	}
	defer rows.Close()

	// grab the column information from the result
	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("don't get column names 'show database': %w", err)
	}

	for rows.Next() {
		tags, columnMap, err := p.accRow(rows, columns)
		if err != nil {
			return err
		}

		// SHOW DATABASES displays pgbouncer database name under name column,
		// while using database column to store Postgres database name.
		if database, ok := columnMap["database"]; ok {
			if s, ok := (*database).(string); ok && s != "" {
				tags["pg_dbname"] = s
			}
		}

		// pass it under db tag to be compatible with the rest of the measurements
		if name, ok := columnMap["name"]; ok {
			if s, ok := (*name).(string); ok && s != "" {
				tags["db"] = s
			}
		}

		fields := make(map[string]interface{})
		for col, val := range columnMap {
			_, ignore := ignoredColumns[col]
			if !ignore {
				fields[col] = *val
			}
		}
		acc.AddFields("pgbouncer_databases", fields, tags)
	}
	return rows.Err()
}

func init() {
	inputs.Add("pgbouncer", func() telegraf.Input {
		return &PgBouncer{
			Config: postgresql.Config{
				MaxIdle:     1,
				MaxOpen:     1,
				IsPgBouncer: true,
			},
		}
	})
}
