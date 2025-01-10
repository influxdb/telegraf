//go:generate ../../../tools/readme_config_includer/generator
package postgresql

import (
	"bytes"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/common/postgresql"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var ignoredColumns = map[string]bool{"stats_reset": true}

type Postgresql struct {
	Databases          []string `toml:"databases"`
	IgnoredDatabases   []string `toml:"ignored_databases"`
	PreparedStatements bool     `toml:"prepared_statements"`
	postgresql.Config

	service *postgresql.Service
}

func (*Postgresql) SampleConfig() string {
	return sampleConfig
}

func (p *Postgresql) Init() error {
	p.IsPgBouncer = !p.PreparedStatements

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
	var query string
	if len(p.Databases) == 0 && len(p.IgnoredDatabases) == 0 {
		query = `SELECT * FROM pg_stat_database`
	} else if len(p.IgnoredDatabases) != 0 {
		query = fmt.Sprintf(`SELECT * FROM pg_stat_database WHERE datname NOT IN ('%s')`,
			strings.Join(p.IgnoredDatabases, "','"))
	} else {
		query = fmt.Sprintf(`SELECT * FROM pg_stat_database WHERE datname IN ('%s')`,
			strings.Join(p.Databases, "','"))
	}

	rows, err := p.service.DB.Query(query)
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
		err = p.accRow(rows, acc, columns)
		if err != nil {
			return err
		}
	}

	query = `SELECT * FROM pg_stat_bgwriter`

	bgWriterRow, err := p.service.DB.Query(query)
	if err != nil {
		return err
	}

	defer bgWriterRow.Close()

	// grab the column information from the result
	if columns, err = bgWriterRow.Columns(); err != nil {
		return err
	}

	for bgWriterRow.Next() {
		if err := p.accRow(bgWriterRow, acc, columns); err != nil {
			return err
		}
	}

	return bgWriterRow.Err()
}

func (p *Postgresql) Stop() {
	p.service.Stop()
}

func (p *Postgresql) accRow(row *sql.Rows, acc telegraf.Accumulator, columns []string) error {
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
	if err := row.Scan(columnVars...); err != nil {
		return err
	}
	if columnMap["datname"] != nil {
		// extract the database name from the column map
		if dbNameStr, ok := (*columnMap["datname"]).(string); ok {
			dbname.WriteString(dbNameStr)
		} else {
			// PG 12 adds tracking of global objects to pg_stat_database
			dbname.WriteString("postgres_global")
		}
	} else {
		dbname.WriteString(p.service.ConnectionDatabase)
	}

	tagAddress := p.service.SanitizedAddress
	tags := map[string]string{"server": tagAddress, "db": dbname.String()}

	fields := make(map[string]interface{})
	for col, val := range columnMap {
		_, ignore := ignoredColumns[col]
		if !ignore {
			fields[col] = *val
		}
	}
	acc.AddFields("postgresql", fields, tags)

	return nil
}

func init() {
	inputs.Add("postgresql", func() telegraf.Input {
		return &Postgresql{
			Config: postgresql.Config{
				MaxIdle: 1,
				MaxOpen: 1,
			},
			PreparedStatements: true,
		}
	})
}
