package postgresql_extensible

import (
	"bytes"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"

	"github.com/jackc/pgx"
	"github.com/jackc/pgx/stdlib"
)

type Postgresql struct {
	Address          string
	Outputaddress    string
	Databases        []string
	OrderedColumns   []string
	AllColumns       []string
	AdditionalTags   []string
	sanitizedAddress string
	Query            []struct {
		Sqlquery    string
		Version     int
		Withdbname  bool
		Tagvalue    string
		Measurement string
	}
}

type query []struct {
	Sqlquery    string
	Version     int
	Withdbname  bool
	Tagvalue    string
	Measurement string
}

var ignoredColumns = map[string]bool{"stats_reset": true}

var sampleConfig = `
  ## specify address via a url matching:
  ##   postgres://[pqgotest[:password]]@localhost[/dbname]\
  ##       ?sslmode=[disable|verify-ca|verify-full]
  ## or a simple string:
  ##   host=localhost user=pqotest password=... sslmode=... dbname=app_production
  #
  ## All connection parameters are optional.  #
  ## Without the dbname parameter, the driver will default to a database
  ## with the same name as the user. This dbname is just for instantiating a
  ## connection with the server and doesn't restrict the databases we are trying
  ## to grab metrics for.
  #
  address = "host=localhost user=postgres sslmode=disable"
  ## A list of databases to pull metrics about. If not specified, metrics for all
  ## databases are gathered.
  ## databases = ["app_production", "testing"]
  #
  # outputaddress = "db01"
  ## A custom name for the database that will be used as the "server" tag in the
  ## measurement output. If not specified, a default one generated from
  ## the connection address is used.
  #
  ## Define the toml config where the sql queries are stored
  ## New queries can be added, if the withdbname is set to true and there is no
  ## databases defined in the 'databases field', the sql query is ended by a
  ## 'is not null' in order to make the query succeed.
  ## Example :
  ## The sqlquery : "SELECT * FROM pg_stat_database where datname" become
  ## "SELECT * FROM pg_stat_database where datname IN ('postgres', 'pgbench')"
  ## because the databases variable was set to ['postgres', 'pgbench' ] and the
  ## withdbname was true. Be careful that if the withdbname is set to false you
  ## don't have to define the where clause (aka with the dbname) the tagvalue
  ## field is used to define custom tags (separated by commas)
  ## The optional "measurement" value can be used to override the default
  ## output measurement name ("postgresql").
  #
  ## Structure :
  ## [[inputs.postgresql_extensible.query]]
  ##   sqlquery string
  ##   version string
  ##   withdbname boolean
  ##   tagvalue string (comma separated)
  ##   measurement string
  [[inputs.postgresql_extensible.query]]
    sqlquery="SELECT * FROM pg_stat_database"
    version=901
    withdbname=false
    tagvalue=""
    measurement=""
  [[inputs.postgresql_extensible.query]]
    sqlquery="SELECT * FROM pg_stat_bgwriter"
    version=901
    withdbname=false
    tagvalue="postgresql.stats"
`

func (p *Postgresql) SampleConfig() string {
	return sampleConfig
}

func (p *Postgresql) Description() string {
	return "Read metrics from one or many postgresql servers"
}

func (p *Postgresql) IgnoredColumns() map[string]bool {
	return ignoredColumns
}

var localhost = "host=localhost sslmode=disable"

func (p *Postgresql) Gather(acc telegraf.Accumulator) error {

	var sql_query string
	var query_addon string
	var db_version int
	var query string
	var tag_value string
	var meas_name string

	if p.Address == "" || p.Address == "localhost" {
		p.Address = localhost
	}

	db, err := connect(p.Address)
	if err != nil {
		return err
	}

	defer db.Close()

	// Retreiving the database version

	query = `select substring(setting from 1 for 3) as version from pg_settings where name='server_version_num'`
	err = db.QueryRow(query).Scan(&db_version)
	if err != nil {
		return err
	}
	// We loop in order to process each query
	// Query is not run if Database version does not match the query version.

	for i := range p.Query {
		sql_query = p.Query[i].Sqlquery
		tag_value = p.Query[i].Tagvalue
		if p.Query[i].Measurement != "" {
			meas_name = p.Query[i].Measurement
		} else {
			meas_name = "postgresql"
		}

		if p.Query[i].Withdbname {
			if len(p.Databases) != 0 {
				query_addon = fmt.Sprintf(` IN ('%s')`,
					strings.Join(p.Databases, "','"))
			} else {
				query_addon = " is not null"
			}
		} else {
			query_addon = ""
		}
		sql_query += query_addon

		if p.Query[i].Version <= db_version {
			rows, err := db.Query(sql_query)
			if err != nil {
				return err
			}

			defer rows.Close()

			// grab the column information from the result
			p.OrderedColumns, err = rows.Columns()
			if err != nil {
				return err
			} else {
				for _, v := range p.OrderedColumns {
					p.AllColumns = append(p.AllColumns, v)
				}
			}
			p.AdditionalTags = nil
			if tag_value != "" {
				tag_list := strings.Split(tag_value, ",")
				for t := range tag_list {
					p.AdditionalTags = append(p.AdditionalTags, tag_list[t])
				}
			}

			for rows.Next() {
				err = p.accRow(meas_name, rows, acc)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type scanner interface {
	Scan(dest ...interface{}) error
}

var KVMatcher, _ = regexp.Compile("(password|sslcert|sslkey|sslmode|sslrootcert)=\\S+ ?")

func (p *Postgresql) SanitizedAddress() (_ string, err error) {
	if p.Outputaddress != "" {
		return p.Outputaddress, nil
	}
	var canonicalizedAddress string
	if strings.HasPrefix(p.Address, "postgres://") || strings.HasPrefix(p.Address, "postgresql://") {
		canonicalizedAddress, err = parseURL(p.Address)
		if err != nil {
			return p.sanitizedAddress, err
		}
	} else {
		canonicalizedAddress = p.Address
	}
	p.sanitizedAddress = KVMatcher.ReplaceAllString(canonicalizedAddress, "")

	return p.sanitizedAddress, err
}

func (p *Postgresql) accRow(meas_name string, row scanner, acc telegraf.Accumulator) error {
	var columnVars []interface{}
	var dbname bytes.Buffer

	// this is where we'll store the column name with its *interface{}
	columnMap := make(map[string]*interface{})

	for _, column := range p.OrderedColumns {
		columnMap[column] = new(interface{})
	}

	// populate the array of interface{} with the pointers in the right order
	for i := 0; i < len(columnMap); i++ {
		columnVars = append(columnVars, columnMap[p.OrderedColumns[i]])
	}

	// deconstruct array of variables and send to Scan
	err := row.Scan(columnVars...)

	if err != nil {
		return err
	}
	if columnMap["datname"] != nil {
		// extract the database name from the column map
		dbname.WriteString((*columnMap["datname"]).(string))
	} else {
		dbname.WriteString("postgres")
	}

	var tagAddress string
	tagAddress, err = p.SanitizedAddress()
	if err != nil {
		return err
	}

	// Process the additional tags

	tags := map[string]string{}
	tags["server"] = tagAddress
	tags["db"] = dbname.String()
	fields := make(map[string]interface{})
COLUMN:
	for col, val := range columnMap {
		if acc.Debug() {
			log.Printf("postgresql_extensible: column: %s = %T: %s\n", col, *val, *val)
		}
		_, ignore := ignoredColumns[col]
		if ignore || *val == nil {
			continue
		}

		for _, tag := range p.AdditionalTags {
			if col != tag {
				continue
			}
			switch v := (*val).(type) {
			case string:
				tags[col] = v
			case []byte:
				tags[col] = string(v)
			case int64, int32, int:
				tags[col] = fmt.Sprintf("%d", v)
			default:
				log.Println("failed to add additional tag", col)
			}
			continue COLUMN
		}
		if v, ok := (*val).([]byte); ok {
			fields[col] = string(v)
		} else {
			fields[col] = *val
		}
	}
	acc.AddFields(meas_name, fields, tags)
	return nil
}

func init() {
	inputs.Add("postgresql_extensible", func() telegraf.Input {
		return &Postgresql{}
	})
}

// pulled from lib/pq
// ParseURL no longer needs to be used by clients of this library since supplying a URL as a
// connection string to sql.Open() is now supported:
//
//	sql.Open("postgres", "postgres://bob:secret@1.2.3.4:5432/mydb?sslmode=verify-full")
//
// It remains exported here for backwards-compatibility.
//
// ParseURL converts a url to a connection string for driver.Open.
// Example:
//
//	"postgres://bob:secret@1.2.3.4:5432/mydb?sslmode=verify-full"
//
// converts to:
//
//	"user=bob password=secret host=1.2.3.4 port=5432 dbname=mydb sslmode=verify-full"
//
// A minimal example:
//
//	"postgres://"
//
// This will be blank, causing driver.Open to use all of the defaults
func parseURL(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}

	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("invalid connection protocol: %s", u.Scheme)
	}

	var kvs []string
	escaper := strings.NewReplacer(` `, `\ `, `'`, `\'`, `\`, `\\`)
	accrue := func(k, v string) {
		if v != "" {
			kvs = append(kvs, k+"="+escaper.Replace(v))
		}
	}

	if u.User != nil {
		v := u.User.Username()
		accrue("user", v)

		v, _ = u.User.Password()
		accrue("password", v)
	}

	if host, port, err := net.SplitHostPort(u.Host); err != nil {
		accrue("host", u.Host)
	} else {
		accrue("host", host)
		accrue("port", port)
	}

	if u.Path != "" {
		accrue("dbname", u.Path[1:])
	}

	q := u.Query()
	for k := range q {
		accrue(k, q.Get(k))
	}

	sort.Strings(kvs) // Makes testing easier (not a performance concern)
	return strings.Join(kvs, " "), nil
}

func connect(address string) (*sql.DB, error) {
	if strings.HasPrefix(address, "postgres://") || strings.HasPrefix(address, "postgresql://") {
		return sql.Open("pgx", address)
	}

	config, err := pgx.ParseDSN(address)
	if err != nil {
		return nil, err
	}

	pool, err := pgx.NewConnPool(pgx.ConnPoolConfig{ConnConfig: config})
	if err != nil {
		return nil, err
	}

	return stdlib.OpenFromConnPool(pool)
}
