//go:generate ../../../tools/readme_config_includer/generator
package mysql

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/gofrs/uuid/v5"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/mysql/v1"
	"github.com/influxdata/telegraf/plugins/inputs/mysql/v2"
)

//go:embed sample.conf
var sampleConfig string

var tlsRe = regexp.MustCompile(`([\?&])(?:tls=custom)($|&)`)

const (
	defaultPerfEventsStatementsDigestTextLimit = 120
	defaultPerfEventsStatementsLimit           = 250
	defaultPerfEventsStatementsTimeLimit       = 86400
	defaultGatherGlobalVars                    = true
	localhost                                  = ""
)

type Mysql struct {
	Servers                             []*config.Secret `toml:"servers"`
	PerfEventsStatementsDigestTextLimit int64            `toml:"perf_events_statements_digest_text_limit"`
	PerfEventsStatementsLimit           int64            `toml:"perf_events_statements_limit"`
	PerfEventsStatementsTimeLimit       int64            `toml:"perf_events_statements_time_limit"`
	TableSchemaDatabases                []string         `toml:"table_schema_databases"`
	GatherProcessList                   bool             `toml:"gather_process_list"`
	GatherUserStatistics                bool             `toml:"gather_user_statistics"`
	GatherInfoSchemaAutoInc             bool             `toml:"gather_info_schema_auto_inc"`
	GatherInnoDBMetrics                 bool             `toml:"gather_innodb_metrics"`
	GatherSlaveStatus                   bool             `toml:"gather_slave_status"`
	GatherReplicaStatus                 bool             `toml:"gather_replica_status"`
	GatherAllSlaveChannels              bool             `toml:"gather_all_slave_channels"`
	MariadbDialect                      bool             `toml:"mariadb_dialect"`
	GatherBinaryLogs                    bool             `toml:"gather_binary_logs"`
	GatherTableIOWaits                  bool             `toml:"gather_table_io_waits"`
	GatherTableLockWaits                bool             `toml:"gather_table_lock_waits"`
	GatherIndexIOWaits                  bool             `toml:"gather_index_io_waits"`
	GatherEventWaits                    bool             `toml:"gather_event_waits"`
	GatherTableSchema                   bool             `toml:"gather_table_schema"`
	GatherFileEventsStats               bool             `toml:"gather_file_events_stats"`
	GatherPerfEventsStatements          bool             `toml:"gather_perf_events_statements"`
	GatherGlobalVars                    bool             `toml:"gather_global_variables"`
	GatherPerfSummaryPerAccountPerEvent bool             `toml:"gather_perf_sum_per_acc_per_event"`
	PerfSummaryEvents                   []string         `toml:"perf_summary_events"`
	IntervalSlow                        config.Duration  `toml:"interval_slow"`
	MetricVersion                       int              `toml:"metric_version"`
	Log                                 telegraf.Logger  `toml:"-"`
	tls.ClientConfig

	lastT               time.Time
	getStatusQuery      string
	loggedConvertFields map[string]bool
}

func (*Mysql) SampleConfig() string {
	return sampleConfig
}

func (m *Mysql) Init() error {
	switch {
	case m.MariadbDialect && m.GatherReplicaStatus:
		m.getStatusQuery = replicaStatusQueryMariadb
	case m.MariadbDialect:
		m.getStatusQuery = slaveStatusQueryMariadb
	case m.GatherReplicaStatus:
		m.getStatusQuery = replicaStatusQuery
	default:
		m.getStatusQuery = slaveStatusQuery
	}
	// Default to localhost if nothing specified.
	if len(m.Servers) == 0 {
		s := config.NewSecret([]byte(localhost))
		m.Servers = append(m.Servers, &s)
	}

	m.loggedConvertFields = make(map[string]bool)

	// Register the TLS configuration. Due to the registry being a global
	// one for the mysql package, we need to define unique IDs to avoid
	// side effects and races between different plugin instances. Therefore,
	// we decorate the "custom" naming of the "tls" parameter with an UUID.
	tlsuuid, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("cannot create UUID: %w", err)
	}
	tlsid := "custom-" + tlsuuid.String()
	tlsConfig, err := m.ClientConfig.TLSConfig()
	if err != nil {
		return fmt.Errorf("registering TLS config: %w", err)
	}
	if tlsConfig != nil {
		if err := mysql.RegisterTLSConfig(tlsid, tlsConfig); err != nil {
			return err
		}
	}

	// Adapt the DSN string
	for i, server := range m.Servers {
		dsnSecret, err := server.Get()
		if err != nil {
			return fmt.Errorf("getting server %d failed: %w", i, err)
		}
		dsn := dsnSecret.String()
		dsnSecret.Destroy()

		// Reference the custom TLS config of _THIS_ plugin instance
		if tlsRe.MatchString(dsn) {
			dsn = tlsRe.ReplaceAllString(dsn, "${1}tls="+tlsid+"${2}")
		}

		conf, err := mysql.ParseDSN(dsn)
		if err != nil {
			return fmt.Errorf("parsing %q failed: %w", dsn, err)
		}

		// Set the default timeout if none specified
		if conf.Timeout == 0 {
			conf.Timeout = time.Second * 5
		}

		if err := server.Set([]byte(conf.FormatDSN())); err != nil {
			return fmt.Errorf("replacing server %q failed: %w", dsn, err)
		}

		m.Servers[i] = server
	}

	return nil
}

func (m *Mysql) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup

	// Loop through each server and collect metrics
	for _, server := range m.Servers {
		wg.Add(1)
		go func(s *config.Secret) {
			defer wg.Done()
			acc.AddError(m.gatherServer(s, acc))
		}(server)
	}

	wg.Wait()
	return nil
}

// These are const but can't be declared as such because golang doesn't allow const maps
var (
	// status counter
	generalThreadStates = map[string]uint32{
		"after create":              uint32(0),
		"altering table":            uint32(0),
		"analyzing":                 uint32(0),
		"checking permissions":      uint32(0),
		"checking table":            uint32(0),
		"cleaning up":               uint32(0),
		"closing tables":            uint32(0),
		"converting heap to myisam": uint32(0),
		"copying to tmp table":      uint32(0),
		"creating sort index":       uint32(0),
		"creating table":            uint32(0),
		"creating tmp table":        uint32(0),
		"deleting":                  uint32(0),
		"executing":                 uint32(0),
		"execution of init_command": uint32(0),
		"end":                       uint32(0),
		"freeing items":             uint32(0),
		"flushing tables":           uint32(0),
		"fulltext initialization":   uint32(0),
		"idle":                      uint32(0),
		"init":                      uint32(0),
		"killed":                    uint32(0),
		"waiting for lock":          uint32(0),
		"logging slow query":        uint32(0),
		"login":                     uint32(0),
		"manage keys":               uint32(0),
		"opening tables":            uint32(0),
		"optimizing":                uint32(0),
		"preparing":                 uint32(0),
		"reading from net":          uint32(0),
		"removing duplicates":       uint32(0),
		"removing tmp table":        uint32(0),
		"reopen tables":             uint32(0),
		"repair by sorting":         uint32(0),
		"repair done":               uint32(0),
		"repair with keycache":      uint32(0),
		"replication master":        uint32(0),
		"rolling back":              uint32(0),
		"searching rows for update": uint32(0),
		"sending data":              uint32(0),
		"sorting for group":         uint32(0),
		"sorting for order":         uint32(0),
		"sorting index":             uint32(0),
		"sorting result":            uint32(0),
		"statistics":                uint32(0),
		"updating":                  uint32(0),
		"waiting for tables":        uint32(0),
		"waiting for table flush":   uint32(0),
		"waiting on cond":           uint32(0),
		"writing to net":            uint32(0),
		"other":                     uint32(0),
	}
	// plaintext statuses
	stateStatusMappings = map[string]string{
		"user sleep":     "idle",
		"creating index": "altering table",
		"committing alter table to storage engine": "altering table",
		"discard or import tablespace":             "altering table",
		"rename":                                   "altering table",
		"setup":                                    "altering table",
		"renaming result table":                    "altering table",
		"preparing for alter table":                "altering table",
		"copying to group table":                   "copying to tmp table",
		"copy to tmp table":                        "copying to tmp table",
		"query end":                                "end",
		"update":                                   "updating",
		"updating main table":                      "updating",
		"updating reference tables":                "updating",
		"system lock":                              "waiting for lock",
		"user lock":                                "waiting for lock",
		"table lock":                               "waiting for lock",
		"deleting from main table":                 "deleting",
		"deleting from reference tables":           "deleting",
	}
)

// Math constants
const (
	picoSeconds = 1e12
)

// metric queries
const (
	globalStatusQuery          = `SHOW GLOBAL STATUS`
	globalVariablesQuery       = `SHOW GLOBAL VARIABLES`
	slaveStatusQuery           = `SHOW SLAVE STATUS`
	replicaStatusQuery         = `SHOW REPLICA STATUS`
	slaveStatusQueryMariadb    = `SHOW ALL SLAVES STATUS`
	replicaStatusQueryMariadb  = `SHOW ALL REPLICAS STATUS`
	binaryLogsQuery            = `SHOW BINARY LOGS`
	infoSchemaProcessListQuery = `
        SELECT COALESCE(command,''),COALESCE(state,''),count(*)
        FROM information_schema.processlist
        WHERE ID != connection_id()
        GROUP BY command,state
        ORDER BY null`
	infoSchemaUserStatisticsQuery = `
        SELECT *
        FROM information_schema.user_statistics`
	infoSchemaAutoIncQuery = `
        SELECT table_schema, table_name, column_name, auto_increment,
          CAST(pow(2, case data_type
            when 'tinyint'   then 7
            when 'smallint'  then 15
            when 'mediumint' then 23
            when 'int'       then 31
            when 'bigint'    then 63
            end+(column_type like '% unsigned'))-1 as decimal(19)) as max_int
          FROM information_schema.tables t
          JOIN information_schema.columns c USING (table_schema,table_name)
          WHERE c.extra = 'auto_increment' AND t.auto_increment IS NOT NULL
    `
	innoDBMetricsQuery = `
        SELECT NAME, COUNT
        FROM information_schema.INNODB_METRICS
        WHERE status='enabled'
    `
	innoDBMetricsQueryMariadb = `
        EXECUTE IMMEDIATE CONCAT("
            SELECT NAME, COUNT
            FROM information_schema.INNODB_METRICS
            WHERE ", IF(version() REGEXP '10\.[1-4]\\..*',"status='enabled'", "ENABLED=1"), "
        ");
	`
	perfTableIOWaitsQuery = `
        SELECT OBJECT_SCHEMA, OBJECT_NAME, COUNT_FETCH, COUNT_INSERT, COUNT_UPDATE, COUNT_DELETE,
        SUM_TIMER_FETCH, SUM_TIMER_INSERT, SUM_TIMER_UPDATE, SUM_TIMER_DELETE
        FROM performance_schema.table_io_waits_summary_by_table
        WHERE OBJECT_SCHEMA NOT IN ('mysql', 'performance_schema')
    `
	perfIndexIOWaitsQuery = `
        SELECT OBJECT_SCHEMA, OBJECT_NAME, ifnull(INDEX_NAME, 'NONE') as INDEX_NAME,
        COUNT_FETCH, COUNT_INSERT, COUNT_UPDATE, COUNT_DELETE,
        SUM_TIMER_FETCH, SUM_TIMER_INSERT, SUM_TIMER_UPDATE, SUM_TIMER_DELETE
        FROM performance_schema.table_io_waits_summary_by_index_usage
        WHERE OBJECT_SCHEMA NOT IN ('mysql', 'performance_schema')
    `
	perfTableLockWaitsQuery = `
        SELECT
            OBJECT_SCHEMA,
            OBJECT_NAME,
            COUNT_READ_NORMAL,
            COUNT_READ_WITH_SHARED_LOCKS,
            COUNT_READ_HIGH_PRIORITY,
            COUNT_READ_NO_INSERT,
            COUNT_READ_EXTERNAL,
            COUNT_WRITE_ALLOW_WRITE,
            COUNT_WRITE_CONCURRENT_INSERT,
            COUNT_WRITE_LOW_PRIORITY,
            COUNT_WRITE_NORMAL,
            COUNT_WRITE_EXTERNAL,
            SUM_TIMER_READ_NORMAL,
            SUM_TIMER_READ_WITH_SHARED_LOCKS,
            SUM_TIMER_READ_HIGH_PRIORITY,
            SUM_TIMER_READ_NO_INSERT,
            SUM_TIMER_READ_EXTERNAL,
            SUM_TIMER_WRITE_ALLOW_WRITE,
            SUM_TIMER_WRITE_CONCURRENT_INSERT,
            SUM_TIMER_WRITE_LOW_PRIORITY,
            SUM_TIMER_WRITE_NORMAL,
            SUM_TIMER_WRITE_EXTERNAL
        FROM performance_schema.table_lock_waits_summary_by_table
        WHERE OBJECT_SCHEMA NOT IN ('mysql', 'performance_schema', 'information_schema')
    `
	perfEventsStatementsQuery = `
        SELECT
            ifnull(SCHEMA_NAME, 'NONE') as SCHEMA_NAME,
            DIGEST,
            LEFT(DIGEST_TEXT, %d) as DIGEST_TEXT,
            COUNT_STAR,
            SUM_TIMER_WAIT,
            SUM_ERRORS,
            SUM_WARNINGS,
            SUM_ROWS_AFFECTED,
            SUM_ROWS_SENT,
            SUM_ROWS_EXAMINED,
            SUM_CREATED_TMP_DISK_TABLES,
            SUM_CREATED_TMP_TABLES,
            SUM_SORT_MERGE_PASSES,
            SUM_SORT_ROWS,
            SUM_NO_INDEX_USED
        FROM performance_schema.events_statements_summary_by_digest
        WHERE SCHEMA_NAME NOT IN ('mysql', 'performance_schema', 'information_schema')
            AND last_seen > DATE_SUB(NOW(), INTERVAL %d SECOND)
        ORDER BY SUM_TIMER_WAIT DESC
        LIMIT %d
    `
	perfEventWaitsQuery = `
        SELECT EVENT_NAME, COUNT_STAR, SUM_TIMER_WAIT
        FROM performance_schema.events_waits_summary_global_by_event_name
    `
	perfFileEventsQuery = `
        SELECT
            EVENT_NAME,
            COUNT_READ, SUM_TIMER_READ, SUM_NUMBER_OF_BYTES_READ,
            COUNT_WRITE, SUM_TIMER_WRITE, SUM_NUMBER_OF_BYTES_WRITE,
            COUNT_MISC, SUM_TIMER_MISC
        FROM performance_schema.file_summary_by_event_name
    `
	tableSchemaQuery = `
        SELECT
            TABLE_SCHEMA,
            TABLE_NAME,
            TABLE_TYPE,
            ifnull(ENGINE, 'NONE') as ENGINE,
            ifnull(VERSION, '0') as VERSION,
            ifnull(ROW_FORMAT, 'NONE') as ROW_FORMAT,
            ifnull(TABLE_ROWS, '0') as TABLE_ROWS,
            ifnull(DATA_LENGTH, '0') as DATA_LENGTH,
            ifnull(INDEX_LENGTH, '0') as INDEX_LENGTH,
            ifnull(DATA_FREE, '0') as DATA_FREE,
            ifnull(CREATE_OPTIONS, 'NONE') as CREATE_OPTIONS
        FROM information_schema.tables
        WHERE TABLE_SCHEMA = '%s'
    `
	dbListQuery = `
        SELECT
            SCHEMA_NAME
            FROM information_schema.schemata
        WHERE SCHEMA_NAME NOT IN ('mysql', 'performance_schema', 'information_schema')
    `
	perfSchemaTablesQuery = `
		SELECT
			table_name
			FROM information_schema.tables
		WHERE table_schema = 'performance_schema' AND table_name = ?
	`

	perfSummaryPerAccountPerEvent = `
        SELECT
			coalesce(user, "unknown"),
			coalesce(host, "unknown"),
			coalesce(event_name, "unknown"),
			count_star,
			sum_timer_wait,
			min_timer_wait,
			avg_timer_wait,
			max_timer_wait,
			sum_lock_time,
			sum_errors,
			sum_warnings,
			sum_rows_affected,
			sum_rows_sent,
			sum_rows_examined,
			sum_created_tmp_disk_tables,
			sum_created_tmp_tables,
			sum_select_full_join,
			sum_select_full_range_join,
			sum_select_range,
			sum_select_range_check,
			sum_select_scan,
			sum_sort_merge_passes,
			sum_sort_range,
			sum_sort_rows,
			sum_sort_scan,
			sum_no_index_used,
			sum_no_good_index_used
		FROM performance_schema.events_statements_summary_by_account_by_event_name
	`
)

func (m *Mysql) gatherServer(server *config.Secret, acc telegraf.Accumulator) error {
	dsnSecret, err := server.Get()
	if err != nil {
		return err
	}
	dsn := dsnSecret.String()
	dsnSecret.Destroy()
	servtag := getDSNTag(dsn)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	err = m.gatherGlobalStatuses(db, servtag, acc)
	if err != nil {
		return err
	}

	if m.GatherGlobalVars {
		// Global Variables may be gathered less often
		interval := time.Duration(m.IntervalSlow)
		if interval >= time.Second && time.Since(m.lastT) >= interval {
			if err := m.gatherGlobalVariables(db, servtag, acc); err != nil {
				return err
			}
			m.lastT = time.Now()
		}
	}

	if m.GatherBinaryLogs {
		err = gatherBinaryLogs(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherProcessList {
		err = m.gatherProcessListStatuses(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherUserStatistics {
		err = m.gatherUserStatisticsStatuses(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherSlaveStatus || m.GatherReplicaStatus {
		err = m.gatherSlaveStatuses(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherInfoSchemaAutoInc {
		err = m.gatherInfoSchemaAutoIncStatuses(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherInnoDBMetrics {
		err = m.gatherInnoDBMetrics(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherPerfSummaryPerAccountPerEvent {
		err = m.gatherPerfSummaryPerAccountPerEvent(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherTableIOWaits {
		err = gatherPerfTableIOWaits(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherIndexIOWaits {
		err = gatherPerfIndexIOWaits(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherTableLockWaits {
		err = gatherPerfTableLockWaits(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherEventWaits {
		err = gatherPerfEventWaits(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherFileEventsStats {
		err = gatherPerfFileEventsStatuses(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherPerfEventsStatements {
		err = m.gatherPerfEventsStatements(db, servtag, acc)
		if err != nil {
			return err
		}
	}

	if m.GatherTableSchema {
		err = m.gatherTableSchema(db, servtag, acc)
		if err != nil {
			return err
		}
	}
	return nil
}

// gatherGlobalVariables can be used to fetch all global variables from
// MySQL environment.
func (m *Mysql) gatherGlobalVariables(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	// run query
	rows, err := db.Query(globalVariablesQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	var key string
	var val sql.RawBytes

	// parse DSN and save server tag
	tags := map[string]string{"server": servtag}
	fields := make(map[string]interface{})
	for rows.Next() {
		if err := rows.Scan(&key, &val); err != nil {
			return err
		}
		key = strings.ToLower(key)

		// parse mysql version and put into field and tag
		if strings.Contains(key, "version") {
			fields[key] = string(val)
			tags[key] = string(val)
		}

		value, err := m.parseGlobalVariables(key, val)
		if err != nil {
			errString := fmt.Errorf("error parsing mysql global variable %q=%q: %w", key, string(val), err)
			if m.MetricVersion < 2 {
				m.Log.Debug(errString)
			} else {
				acc.AddError(errString)
			}
		} else {
			fields[key] = value
		}

		// Send 20 fields at a time
		if len(fields) >= 20 {
			acc.AddFields("mysql_variables", fields, tags)
			fields = make(map[string]interface{})
		}
	}
	// Send any remaining fields
	if len(fields) > 0 {
		acc.AddFields("mysql_variables", fields, tags)
	}
	return nil
}

func (m *Mysql) parseGlobalVariables(key string, value sql.RawBytes) (interface{}, error) {
	if m.MetricVersion < 2 {
		return v1.ParseValue(value)
	}
	return v2.ConvertGlobalVariables(key, value)
}

// gatherSlaveStatuses can be used to get replication analytics
// When the server is slave, then it returns only one row.
// If the multi-source replication is set, then everything works differently
// This code does not work with multi-source replication.
func (m *Mysql) gatherSlaveStatuses(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	// run query
	var rows *sql.Rows
	var err error

	rows, err = db.Query(m.getStatusQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	tags := map[string]string{"server": servtag}
	fields := make(map[string]interface{})

	// for each channel record
	for rows.Next() {
		// to save the column names as a field key
		// scanning keys and values separately

		// get columns names, and create an array with its length
		cols, err := rows.ColumnTypes()
		if err != nil {
			return err
		}

		vals := make([]sql.RawBytes, len(cols))
		valPtrs := make([]interface{}, len(cols))
		// fill the array with sql.Rawbytes
		for i := range vals {
			vals[i] = sql.RawBytes{}
			valPtrs[i] = &vals[i]
		}
		if err := rows.Scan(valPtrs...); err != nil {
			return err
		}

		// range over columns, and try to parse values
		for i, col := range cols {
			colName := col.Name()

			if m.MetricVersion >= 2 {
				colName = strings.ToLower(colName)
			}

			colValue := vals[i]

			if m.GatherAllSlaveChannels &&
				(strings.EqualFold(colName, "channel_name") || strings.EqualFold(colName, "connection_name")) {
				// Since the default channel name is empty, we need this block
				channelName := "default"
				if len(colValue) > 0 {
					channelName = string(colValue)
				}
				tags["channel"] = channelName
				continue
			}

			if len(colValue) == 0 {
				continue
			}

			value, err := m.parseValueByDatabaseTypeName(colValue, col.DatabaseTypeName())
			if err != nil {
				errString := fmt.Errorf("error parsing mysql slave status %q=%q: %w", colName, string(colValue), err)
				if m.MetricVersion < 2 {
					m.Log.Debug(errString)
				} else {
					acc.AddError(errString)
				}
				continue
			}

			fields["slave_"+colName] = value
		}
		acc.AddFields("mysql", fields, tags)

		// Only the first row is relevant if not all slave-channels should be gathered,
		// so break here and skip the remaining rows
		if !m.GatherAllSlaveChannels {
			break
		}
	}

	return nil
}

// gatherBinaryLogs can be used to collect size and count of all binary files
// binlogs metric requires the MySQL server to turn it on in configuration
func gatherBinaryLogs(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	// run query
	rows, err := db.Query(binaryLogsQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	// parse DSN and save host as a tag
	tags := map[string]string{"server": servtag}
	var (
		size      uint64
		count     uint64
		fileSize  uint64
		fileName  string
		encrypted string
	)

	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	numColumns := len(columns)

	// iterate over rows and count the size and count of files
	for rows.Next() {
		if numColumns == 3 {
			if err := rows.Scan(&fileName, &fileSize, &encrypted); err != nil {
				return err
			}
		} else {
			if err := rows.Scan(&fileName, &fileSize); err != nil {
				return err
			}
		}

		size += fileSize
		count++
	}
	fields := map[string]interface{}{
		"binary_size_bytes":  size,
		"binary_files_count": count,
	}

	acc.AddFields("mysql", fields, tags)
	return nil
}

// gatherGlobalStatuses can be used to get MySQL status metrics
// the mappings of actual names and names of each status to be exported
// to output is provided on mappings variable
func (m *Mysql) gatherGlobalStatuses(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	// run query
	rows, err := db.Query(globalStatusQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	// parse the DSN and save host name as a tag
	tags := map[string]string{"server": servtag}
	fields := make(map[string]interface{})
	for rows.Next() {
		var key string
		var val sql.RawBytes

		if err := rows.Scan(&key, &val); err != nil {
			return err
		}

		if m.MetricVersion < 2 {
			var found bool
			for _, mapped := range v1.Mappings {
				if strings.HasPrefix(key, mapped.OnServer) {
					// convert numeric values to integer
					var i int
					v := string(val)
					switch v {
					case "ON", "true":
						i = 1
					case "OFF", "false":
						i = 0
					default:
						if i, err = strconv.Atoi(v); err != nil {
							// Make the value a <nil> value to prevent adding
							// the field containing nonsense values.
							i = 0
							if !m.loggedConvertFields[key] {
								m.Log.Warnf("Cannot convert value %q for key %q to integer outputting zero...", v, key)
								m.loggedConvertFields[key] = true
							}
						}
					}
					fields[mapped.InExport+key[len(mapped.OnServer):]] = i
					found = true
				}
			}
			// Send 20 fields at a time
			if len(fields) >= 20 {
				acc.AddFields("mysql", fields, tags)
				fields = make(map[string]interface{})
			}
			if found {
				continue
			}

			// search for specific values
			switch key {
			case "Queries":
				i, err := strconv.ParseInt(string(val), 10, 64)
				if err != nil {
					acc.AddError(fmt.Errorf("error parsing mysql %q int value: %w", key, err))
				} else {
					fields["queries"] = i
				}
			case "Questions":
				i, err := strconv.ParseInt(string(val), 10, 64)
				if err != nil {
					acc.AddError(fmt.Errorf("error parsing mysql %q int value: %w", key, err))
				} else {
					fields["questions"] = i
				}
			case "Slow_queries":
				i, err := strconv.ParseInt(string(val), 10, 64)
				if err != nil {
					acc.AddError(fmt.Errorf("error parsing mysql %q int value: %w", key, err))
				} else {
					fields["slow_queries"] = i
				}
			case "Connections":
				i, err := strconv.ParseInt(string(val), 10, 64)
				if err != nil {
					acc.AddError(fmt.Errorf("error parsing mysql %q int value: %w", key, err))
				} else {
					fields["connections"] = i
				}
			case "Syncs":
				i, err := strconv.ParseInt(string(val), 10, 64)
				if err != nil {
					acc.AddError(fmt.Errorf("error parsing mysql %q int value: %w", key, err))
				} else {
					fields["syncs"] = i
				}
			case "Uptime":
				i, err := strconv.ParseInt(string(val), 10, 64)
				if err != nil {
					acc.AddError(fmt.Errorf("error parsing mysql %q int value: %w", key, err))
				} else {
					fields["uptime"] = i
				}
			}
		} else {
			key = strings.ToLower(key)
			value, err := v2.ConvertGlobalStatus(key, val)
			if err != nil {
				acc.AddError(fmt.Errorf("error parsing mysql global status %q=%q: %w", key, string(val), err))
			} else {
				fields[key] = value
			}
		}

		// Send 20 fields at a time
		if len(fields) >= 20 {
			acc.AddFields("mysql", fields, tags)
			fields = make(map[string]interface{})
		}
	}
	// Send any remaining fields
	if len(fields) > 0 {
		acc.AddFields("mysql", fields, tags)
	}

	return nil
}

// GatherProcessList can be used to collect metrics on each running command
// and its state with its running count
func (m *Mysql) gatherProcessListStatuses(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	// run query
	rows, err := db.Query(infoSchemaProcessListQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	var (
		command string
		state   string
		count   uint32
	)

	fields := make(map[string]interface{})

	// mapping of state with its counts
	stateCounts := make(map[string]uint32, len(generalThreadStates))
	// set map with keys and default values
	for k, v := range generalThreadStates {
		stateCounts[k] = v
	}

	for rows.Next() {
		err = rows.Scan(&command, &state, &count)
		if err != nil {
			return err
		}
		// each state has its mapping
		foundState := findThreadState(command, state)
		// count each state
		stateCounts[foundState] += count
	}

	tags := map[string]string{"server": servtag}
	for s, c := range stateCounts {
		fields[newNamespace("threads", s)] = c
	}
	if m.MetricVersion < 2 {
		acc.AddFields("mysql_info_schema", fields, tags)
	} else {
		acc.AddFields("mysql_process_list", fields, tags)
	}

	// get count of connections from each user
	connRows, err := db.Query("SELECT user, sum(1) AS connections FROM INFORMATION_SCHEMA.PROCESSLIST GROUP BY user")
	if err != nil {
		return err
	}
	defer connRows.Close()

	for connRows.Next() {
		var user string
		var connections int64

		err = connRows.Scan(&user, &connections)
		if err != nil {
			return err
		}

		tags := map[string]string{"server": servtag, "user": user}
		fields := make(map[string]interface{})

		fields["connections"] = connections
		acc.AddFields("mysql_users", fields, tags)
	}

	return nil
}

// GatherUserStatisticsStatuses can be used to collect metrics on each running command
// and its state with its running count
func (m *Mysql) gatherUserStatisticsStatuses(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	// run query
	rows, err := db.Query(infoSchemaUserStatisticsQuery)
	if err != nil {
		// disable collecting if table is not found (mysql specific error)
		// (suppresses repeat errors)
		if strings.Contains(err.Error(), "nknown table 'user_statistics'") {
			m.GatherUserStatistics = false
		}
		return err
	}
	defer rows.Close()

	cols, err := columnsToLower(rows.Columns())
	if err != nil {
		return err
	}

	read, err := getColSlice(rows)
	if err != nil {
		return err
	}

	for rows.Next() {
		err = rows.Scan(read...)
		if err != nil {
			return err
		}

		tags := map[string]string{"server": servtag, "user": *read[0].(*string)}
		fields := make(map[string]interface{}, len(cols))

		for i := range cols {
			if i == 0 {
				continue // skip "user"
			}
			switch v := read[i].(type) {
			case *int64:
				fields[cols[i]] = *v
			case *float64:
				fields[cols[i]] = *v
			case *string:
				fields[cols[i]] = *v
			default:
				return fmt.Errorf("unknown column type - %T", v)
			}
		}
		acc.AddFields("mysql_user_stats", fields, tags)
	}
	return nil
}

// columnsToLower converts selected column names to lowercase.
func columnsToLower(s []string, e error) ([]string, error) {
	if e != nil {
		return nil, e
	}
	d := make([]string, len(s))

	for i := range s {
		d[i] = strings.ToLower(s[i])
	}
	return d, nil
}

// getColSlice returns an in interface slice that can be used in the row.Scan().
func getColSlice(rows *sql.Rows) ([]interface{}, error) {
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	l := len(columnTypes)

	// list of all possible column names
	var (
		user                     string
		totalConnections         int64
		concurrentConnections    int64
		connectedTime            int64
		busyTime                 int64
		cpuTime                  int64
		bytesReceived            int64
		bytesSent                int64
		binlogBytesWritten       int64
		rowsRead                 int64
		rowsSent                 int64
		rowsDeleted              int64
		rowsInserted             int64
		rowsUpdated              int64
		selectCommands           int64
		updateCommands           int64
		otherCommands            int64
		commitTransactions       int64
		rollbackTransactions     int64
		deniedConnections        int64
		lostConnections          int64
		accessDenied             int64
		emptyQueries             int64
		totalSslConnections      int64
		maxStatementTimeExceeded int64
		// maria specific
		fbusyTime float64
		fcpuTime  float64
		// percona specific
		rowsFetched   int64
		tableRowsRead int64
	)

	switch l {
	case 23: // maria5
		return []interface{}{
			&user,
			&totalConnections,
			&concurrentConnections,
			&connectedTime,
			&fbusyTime,
			&fcpuTime,
			&bytesReceived,
			&bytesSent,
			&binlogBytesWritten,
			&rowsRead,
			&rowsSent,
			&rowsDeleted,
			&rowsInserted,
			&rowsUpdated,
			&selectCommands,
			&updateCommands,
			&otherCommands,
			&commitTransactions,
			&rollbackTransactions,
			&deniedConnections,
			&lostConnections,
			&accessDenied,
			&emptyQueries,
		}, nil
	case 25: // maria10
		return []interface{}{
			&user,
			&totalConnections,
			&concurrentConnections,
			&connectedTime,
			&fbusyTime,
			&fcpuTime,
			&bytesReceived,
			&bytesSent,
			&binlogBytesWritten,
			&rowsRead,
			&rowsSent,
			&rowsDeleted,
			&rowsInserted,
			&rowsUpdated,
			&selectCommands,
			&updateCommands,
			&otherCommands,
			&commitTransactions,
			&rollbackTransactions,
			&deniedConnections,
			&lostConnections,
			&accessDenied,
			&emptyQueries,
			&totalSslConnections,
			&maxStatementTimeExceeded,
		}, nil
	case 21: // mysql 5.5
		return []interface{}{
			&user,
			&totalConnections,
			&concurrentConnections,
			&connectedTime,
			&busyTime,
			&cpuTime,
			&bytesReceived,
			&bytesSent,
			&binlogBytesWritten,
			&rowsFetched,
			&rowsUpdated,
			&tableRowsRead,
			&selectCommands,
			&updateCommands,
			&otherCommands,
			&commitTransactions,
			&rollbackTransactions,
			&deniedConnections,
			&lostConnections,
			&accessDenied,
			&emptyQueries,
		}, nil
	case 22: // percona
		cols := make([]interface{}, 0, 22)
		for i, ct := range columnTypes {
			// The first column is the user and has to be a string
			if i == 0 {
				cols = append(cols, new(string))
				continue
			}

			// Percona 8 has some special fields that are float instead of ints
			// see: https://github.com/influxdata/telegraf/issues/7360
			switch ct.ScanType().Kind() {
			case reflect.Float32, reflect.Float64:
				cols = append(cols, new(float64))
			default:
				// Keep old type for backward compatibility
				cols = append(cols, new(int64))
			}
		}

		return cols, nil
	}

	return nil, fmt.Errorf("not Supported - %d columns", l)
}

// gatherPerfTableIOWaits can be used to get total count and time of I/O wait event for each table and process
func gatherPerfTableIOWaits(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	rows, err := db.Query(perfTableIOWaitsQuery)
	if err != nil {
		return err
	}

	defer rows.Close()
	var (
		objSchema, objName                                string
		countFetch, countInsert, countUpdate, countDelete float64
		timeFetch, timeInsert, timeUpdate, timeDelete     float64
	)

	for rows.Next() {
		err = rows.Scan(&objSchema, &objName,
			&countFetch, &countInsert, &countUpdate, &countDelete,
			&timeFetch, &timeInsert, &timeUpdate, &timeDelete,
		)

		if err != nil {
			return err
		}

		tags := map[string]string{
			"server": servtag,
			"schema": objSchema,
			"name":   objName,
		}

		fields := map[string]interface{}{
			"table_io_waits_total_fetch":          countFetch,
			"table_io_waits_total_insert":         countInsert,
			"table_io_waits_total_update":         countUpdate,
			"table_io_waits_total_delete":         countDelete,
			"table_io_waits_seconds_total_fetch":  timeFetch / picoSeconds,
			"table_io_waits_seconds_total_insert": timeInsert / picoSeconds,
			"table_io_waits_seconds_total_update": timeUpdate / picoSeconds,
			"table_io_waits_seconds_total_delete": timeDelete / picoSeconds,
		}

		acc.AddFields("mysql_perf_schema", fields, tags)
	}
	return nil
}

// gatherPerfIndexIOWaits can be used to get total count and time of I/O wait event for each index and process
func gatherPerfIndexIOWaits(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	rows, err := db.Query(perfIndexIOWaitsQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	var (
		objSchema, objName, indexName                     string
		countFetch, countInsert, countUpdate, countDelete float64
		timeFetch, timeInsert, timeUpdate, timeDelete     float64
	)

	for rows.Next() {
		err = rows.Scan(&objSchema, &objName, &indexName,
			&countFetch, &countInsert, &countUpdate, &countDelete,
			&timeFetch, &timeInsert, &timeUpdate, &timeDelete,
		)

		if err != nil {
			return err
		}

		tags := map[string]string{
			"server": servtag,
			"schema": objSchema,
			"name":   objName,
			"index":  indexName,
		}
		fields := map[string]interface{}{
			"index_io_waits_total_fetch":         countFetch,
			"index_io_waits_seconds_total_fetch": timeFetch / picoSeconds,
		}

		// update write columns only when index is NONE
		if indexName == "NONE" {
			fields["index_io_waits_total_insert"] = countInsert
			fields["index_io_waits_total_update"] = countUpdate
			fields["index_io_waits_total_delete"] = countDelete

			fields["index_io_waits_seconds_total_insert"] = timeInsert / picoSeconds
			fields["index_io_waits_seconds_total_update"] = timeUpdate / picoSeconds
			fields["index_io_waits_seconds_total_delete"] = timeDelete / picoSeconds
		}

		acc.AddFields("mysql_perf_schema", fields, tags)
	}
	return nil
}

// gatherInfoSchemaAutoIncStatuses can be used to get auto incremented values of the column
func (m *Mysql) gatherInfoSchemaAutoIncStatuses(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	rows, err := db.Query(infoSchemaAutoIncQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	var (
		schema, table, column string
		incValue, maxInt      uint64
	)

	for rows.Next() {
		if err := rows.Scan(&schema, &table, &column, &incValue, &maxInt); err != nil {
			return err
		}
		tags := map[string]string{
			"server": servtag,
			"schema": schema,
			"table":  table,
			"column": column,
		}
		fields := make(map[string]interface{})
		fields["auto_increment_column"] = incValue
		fields["auto_increment_column_max"] = maxInt

		if m.MetricVersion < 2 {
			acc.AddFields("mysql_info_schema", fields, tags)
		} else {
			acc.AddFields("mysql_table_schema", fields, tags)
		}
	}
	return nil
}

// gatherInnoDBMetrics can be used to fetch enabled metrics from
// information_schema.INNODB_METRICS
func (m *Mysql) gatherInnoDBMetrics(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	var (
		query string
	)

	if m.MariadbDialect {
		query = innoDBMetricsQueryMariadb
	} else {
		query = innoDBMetricsQuery
	}

	// run query
	rows, err := db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	// parse DSN and save server tag
	tags := map[string]string{"server": servtag}
	fields := make(map[string]interface{})
	for rows.Next() {
		var key string
		var val sql.RawBytes
		if err := rows.Scan(&key, &val); err != nil {
			return err
		}

		key = strings.ToLower(key)
		value, err := m.parseValueByDatabaseTypeName(val, "BIGINT")
		if err != nil {
			acc.AddError(fmt.Errorf("error parsing mysql InnoDB metric %q=%q: %w", key, string(val), err))
			continue
		}

		fields[key] = value

		// Send 20 fields at a time
		if len(fields) >= 20 {
			acc.AddFields("mysql_innodb", fields, tags)
			fields = make(map[string]interface{})
		}
	}
	// Send any remaining fields
	if len(fields) > 0 {
		acc.AddFields("mysql_innodb", fields, tags)
	}
	return nil
}

// gatherPerfSummaryPerAccountPerEvent can be used to fetch enabled metrics from
// performance_schema.events_statements_summary_by_account_by_event_name
func (m *Mysql) gatherPerfSummaryPerAccountPerEvent(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	sqlQuery := perfSummaryPerAccountPerEvent

	var rows *sql.Rows
	var err error

	var (
		srcUser                 string
		srcHost                 string
		eventName               string
		countStar               float64
		sumTimerWait            float64
		minTimerWait            float64
		avgTimerWait            float64
		maxTimerWait            float64
		sumLockTime             float64
		sumErrors               float64
		sumWarnings             float64
		sumRowsAffected         float64
		sumRowsSent             float64
		sumRowsExamined         float64
		sumCreatedTmpDiskTables float64
		sumCreatedTmpTables     float64
		sumSelectFullJoin       float64
		sumSelectFullRangeJoin  float64
		sumSelectRange          float64
		sumSelectRangeCheck     float64
		sumSelectScan           float64
		sumSortMergePasses      float64
		sumSortRange            float64
		sumSortRows             float64
		sumSortScan             float64
		sumNoIndexUsed          float64
		sumNoGoodIndexUsed      float64
	)

	var events []interface{}
	// if we have perf_summary_events set - select only listed events (adding filter criteria for rows)
	if len(m.PerfSummaryEvents) > 0 {
		sqlQuery += " WHERE EVENT_NAME IN ("
		for i, eventName := range m.PerfSummaryEvents {
			if i > 0 {
				sqlQuery += ", "
			}
			sqlQuery += "?"
			events = append(events, eventName)
		}
		sqlQuery += ")"

		rows, err = db.Query(sqlQuery, events...)
	} else {
		// otherwise no filter, hence, select all rows
		rows, err = db.Query(perfSummaryPerAccountPerEvent)
	}

	if err != nil {
		return err
	}
	defer rows.Close()

	// parse DSN and save server tag
	tags := map[string]string{"server": servtag}
	for rows.Next() {
		if err := rows.Scan(
			&srcUser,
			&srcHost,
			&eventName,
			&countStar,
			&sumTimerWait,
			&minTimerWait,
			&avgTimerWait,
			&maxTimerWait,
			&sumLockTime,
			&sumErrors,
			&sumWarnings,
			&sumRowsAffected,
			&sumRowsSent,
			&sumRowsExamined,
			&sumCreatedTmpDiskTables,
			&sumCreatedTmpTables,
			&sumSelectFullJoin,
			&sumSelectFullRangeJoin,
			&sumSelectRange,
			&sumSelectRangeCheck,
			&sumSelectScan,
			&sumSortMergePasses,
			&sumSortRange,
			&sumSortRows,
			&sumSortScan,
			&sumNoIndexUsed,
			&sumNoGoodIndexUsed,
		); err != nil {
			return err
		}
		srcUser = strings.ToLower(srcUser)
		srcHost = strings.ToLower(srcHost)

		sqlLWTags := copyTags(tags)
		sqlLWTags["src_user"] = srcUser
		sqlLWTags["src_host"] = srcHost
		sqlLWTags["event"] = eventName
		sqlLWFields := map[string]interface{}{
			"count_star":                  countStar,
			"sum_timer_wait":              sumTimerWait,
			"min_timer_wait":              minTimerWait,
			"avg_timer_wait":              avgTimerWait,
			"max_timer_wait":              maxTimerWait,
			"sum_lock_time":               sumLockTime,
			"sum_errors":                  sumErrors,
			"sum_warnings":                sumWarnings,
			"sum_rows_affected":           sumRowsAffected,
			"sum_rows_sent":               sumRowsSent,
			"sum_rows_examined":           sumRowsExamined,
			"sum_created_tmp_disk_tables": sumCreatedTmpDiskTables,
			"sum_created_tmp_tables":      sumCreatedTmpTables,
			"sum_select_full_join":        sumSelectFullJoin,
			"sum_select_full_range_join":  sumSelectFullRangeJoin,
			"sum_select_range":            sumSelectRange,
			"sum_select_range_check":      sumSelectRangeCheck,
			"sum_select_scan":             sumSelectScan,
			"sum_sort_merge_passes":       sumSortMergePasses,
			"sum_sort_range":              sumSortRange,
			"sum_sort_rows":               sumSortRows,
			"sum_sort_scan":               sumSortScan,
			"sum_no_index_used":           sumNoIndexUsed,
			"sum_no_good_index_used":      sumNoGoodIndexUsed,
		}
		acc.AddFields("mysql_perf_acc_event", sqlLWFields, sqlLWTags)
	}

	return nil
}

// gatherPerfTableLockWaits can be used to get
// the total number and time for SQL and external lock wait events
// for each table and operation
// requires the MySQL server to be enabled to save this metric
func gatherPerfTableLockWaits(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	// check if table exists,
	// if performance_schema is not enabled, tables do not exist
	// then there is no need to scan them
	var tableName string
	err := db.QueryRow(perfSchemaTablesQuery, "table_lock_waits_summary_by_table").Scan(&tableName)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return err
	}

	rows, err := db.Query(perfTableLockWaitsQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	var (
		objectSchema               string
		objectName                 string
		countReadNormal            float64
		countReadWithSharedLocks   float64
		countReadHighPriority      float64
		countReadNoInsert          float64
		countReadExternal          float64
		countWriteAllowWrite       float64
		countWriteConcurrentInsert float64
		countWriteLowPriority      float64
		countWriteNormal           float64
		countWriteExternal         float64
		timeReadNormal             float64
		timeReadWithSharedLocks    float64
		timeReadHighPriority       float64
		timeReadNoInsert           float64
		timeReadExternal           float64
		timeWriteAllowWrite        float64
		timeWriteConcurrentInsert  float64
		timeWriteLowPriority       float64
		timeWriteNormal            float64
		timeWriteExternal          float64
	)

	for rows.Next() {
		err = rows.Scan(
			&objectSchema,
			&objectName,
			&countReadNormal,
			&countReadWithSharedLocks,
			&countReadHighPriority,
			&countReadNoInsert,
			&countReadExternal,
			&countWriteAllowWrite,
			&countWriteConcurrentInsert,
			&countWriteLowPriority,
			&countWriteNormal,
			&countWriteExternal,
			&timeReadNormal,
			&timeReadWithSharedLocks,
			&timeReadHighPriority,
			&timeReadNoInsert,
			&timeReadExternal,
			&timeWriteAllowWrite,
			&timeWriteConcurrentInsert,
			&timeWriteLowPriority,
			&timeWriteNormal,
			&timeWriteExternal,
		)

		if err != nil {
			return err
		}
		tags := map[string]string{
			"server": servtag,
			"schema": objectSchema,
			"table":  objectName,
		}

		sqlLWTags := copyTags(tags)
		sqlLWTags["perf_query"] = "sql_lock_waits_total"
		sqlLWFields := map[string]interface{}{
			"read_normal":             countReadNormal,
			"read_with_shared_locks":  countReadWithSharedLocks,
			"read_high_priority":      countReadHighPriority,
			"read_no_insert":          countReadNoInsert,
			"write_normal":            countWriteNormal,
			"write_allow_write":       countWriteAllowWrite,
			"write_concurrent_insert": countWriteConcurrentInsert,
			"write_low_priority":      countWriteLowPriority,
		}
		acc.AddFields("mysql_perf_schema", sqlLWFields, sqlLWTags)

		externalLWTags := copyTags(tags)
		externalLWTags["perf_query"] = "external_lock_waits_total"
		externalLWFields := map[string]interface{}{
			"read":  countReadExternal,
			"write": countWriteExternal,
		}
		acc.AddFields("mysql_perf_schema", externalLWFields, externalLWTags)

		sqlLWSecTotalTags := copyTags(tags)
		sqlLWSecTotalTags["perf_query"] = "sql_lock_waits_seconds_total"
		sqlLWSecTotalFields := map[string]interface{}{
			"read_normal":             timeReadNormal / picoSeconds,
			"read_with_shared_locks":  timeReadWithSharedLocks / picoSeconds,
			"read_high_priority":      timeReadHighPriority / picoSeconds,
			"read_no_insert":          timeReadNoInsert / picoSeconds,
			"write_normal":            timeWriteNormal / picoSeconds,
			"write_allow_write":       timeWriteAllowWrite / picoSeconds,
			"write_concurrent_insert": timeWriteConcurrentInsert / picoSeconds,
			"write_low_priority":      timeWriteLowPriority / picoSeconds,
		}
		acc.AddFields("mysql_perf_schema", sqlLWSecTotalFields, sqlLWSecTotalTags)

		externalLWSecTotalTags := copyTags(tags)
		externalLWSecTotalTags["perf_query"] = "external_lock_waits_seconds_total"
		externalLWSecTotalFields := map[string]interface{}{
			"read":  timeReadExternal / picoSeconds,
			"write": timeWriteExternal / picoSeconds,
		}
		acc.AddFields("mysql_perf_schema", externalLWSecTotalFields, externalLWSecTotalTags)
	}
	return nil
}

// gatherPerfEventWaits can be used to get total time and number of event waits
func gatherPerfEventWaits(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	rows, err := db.Query(perfEventWaitsQuery)
	if err != nil {
		return err
	}
	defer rows.Close()

	var (
		event               string
		starCount, timeWait float64
	)

	tags := map[string]string{
		"server": servtag,
	}
	for rows.Next() {
		if err := rows.Scan(&event, &starCount, &timeWait); err != nil {
			return err
		}
		tags["event_name"] = event
		fields := map[string]interface{}{
			"events_waits_total":         starCount,
			"events_waits_seconds_total": timeWait / picoSeconds,
		}

		acc.AddFields("mysql_perf_schema", fields, tags)
	}
	return nil
}

// gatherPerfFileEvents can be used to get stats on file events
func gatherPerfFileEventsStatuses(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	rows, err := db.Query(perfFileEventsQuery)
	if err != nil {
		return err
	}

	defer rows.Close()

	var (
		eventName                                 string
		countRead, countWrite, countMisc          float64
		sumTimerRead, sumTimerWrite, sumTimerMisc float64
		sumNumBytesRead, sumNumBytesWrite         float64
	)

	tags := map[string]string{
		"server": servtag,
	}
	for rows.Next() {
		err = rows.Scan(
			&eventName,
			&countRead, &sumTimerRead, &sumNumBytesRead,
			&countWrite, &sumTimerWrite, &sumNumBytesWrite,
			&countMisc, &sumTimerMisc,
		)
		if err != nil {
			return err
		}

		tags["event_name"] = eventName
		fields := make(map[string]interface{})

		miscTags := copyTags(tags)
		miscTags["mode"] = "misc"
		fields["file_events_total"] = countWrite
		fields["file_events_seconds_total"] = sumTimerMisc / picoSeconds
		acc.AddFields("mysql_perf_schema", fields, miscTags)

		readTags := copyTags(tags)
		readTags["mode"] = "read"
		fields["file_events_total"] = countRead
		fields["file_events_seconds_total"] = sumTimerRead / picoSeconds
		fields["file_events_bytes_totals"] = sumNumBytesRead
		acc.AddFields("mysql_perf_schema", fields, readTags)

		writeTags := copyTags(tags)
		writeTags["mode"] = "write"
		fields["file_events_total"] = countWrite
		fields["file_events_seconds_total"] = sumTimerWrite / picoSeconds
		fields["file_events_bytes_totals"] = sumNumBytesWrite
		acc.AddFields("mysql_perf_schema", fields, writeTags)
	}

	return nil
}

// gatherPerfEventsStatements can be used to get attributes of each event
func (m *Mysql) gatherPerfEventsStatements(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	query := fmt.Sprintf(
		perfEventsStatementsQuery,
		m.PerfEventsStatementsDigestTextLimit,
		m.PerfEventsStatementsTimeLimit,
		m.PerfEventsStatementsLimit,
	)

	rows, err := db.Query(query)
	if err != nil {
		return err
	}

	defer rows.Close()

	var (
		schemaName, digest, digestText       string
		count, queryTime, errs, warnings     float64
		rowsAffected, rowsSent, rowsExamined float64
		tmpTables, tmpDiskTables             float64
		sortMergePasses, sortRows            float64
		noIndexUsed                          float64
	)

	tags := map[string]string{
		"server": servtag,
	}

	for rows.Next() {
		err = rows.Scan(
			&schemaName, &digest, &digestText,
			&count, &queryTime, &errs, &warnings,
			&rowsAffected, &rowsSent, &rowsExamined,
			&tmpTables, &tmpDiskTables,
			&sortMergePasses, &sortRows,
			&noIndexUsed,
		)

		if err != nil {
			return err
		}
		tags["schema"] = schemaName
		tags["digest"] = digest
		tags["digest_text"] = digestText

		fields := map[string]interface{}{
			"events_statements_total":                   count,
			"events_statements_seconds_total":           queryTime / picoSeconds,
			"events_statements_errors_total":            errs,
			"events_statements_warnings_total":          warnings,
			"events_statements_rows_affected_total":     rowsAffected,
			"events_statements_rows_sent_total":         rowsSent,
			"events_statements_rows_examined_total":     rowsExamined,
			"events_statements_tmp_tables_total":        tmpTables,
			"events_statements_tmp_disk_tables_total":   tmpDiskTables,
			"events_statements_sort_merge_passes_total": sortMergePasses,
			"events_statements_sort_rows_total":         sortRows,
			"events_statements_no_index_used_total":     noIndexUsed,
		}

		acc.AddFields("mysql_perf_schema", fields, tags)
	}
	return nil
}

// gatherTableSchema can be used to gather stats on each schema
func (m *Mysql) gatherTableSchema(db *sql.DB, servtag string, acc telegraf.Accumulator) error {
	var dbList []string

	// if the list of databases if empty, then get all databases
	if len(m.TableSchemaDatabases) == 0 {
		rows, err := db.Query(dbListQuery)
		if err != nil {
			return err
		}
		defer rows.Close()

		var database string
		for rows.Next() {
			err = rows.Scan(&database)
			if err != nil {
				return err
			}

			dbList = append(dbList, database)
		}
	} else {
		dbList = m.TableSchemaDatabases
	}

	for _, database := range dbList {
		err := m.gatherSchemaForDB(db, database, servtag, acc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Mysql) gatherSchemaForDB(db *sql.DB, database, servtag string, acc telegraf.Accumulator) error {
	rows, err := db.Query(fmt.Sprintf(tableSchemaQuery, database))
	if err != nil {
		return err
	}
	defer rows.Close()

	var (
		tableSchema   string
		tableName     string
		tableType     string
		engine        string
		version       float64
		rowFormat     string
		tableRows     float64
		dataLength    float64
		indexLength   float64
		dataFree      float64
		createOptions string
	)

	for rows.Next() {
		err = rows.Scan(
			&tableSchema,
			&tableName,
			&tableType,
			&engine,
			&version,
			&rowFormat,
			&tableRows,
			&dataLength,
			&indexLength,
			&dataFree,
			&createOptions,
		)
		if err != nil {
			return err
		}
		tags := map[string]string{"server": servtag}
		tags["schema"] = tableSchema
		tags["table"] = tableName

		if m.MetricVersion < 2 {
			acc.AddFields(newNamespace("info_schema", "table_rows"),
				map[string]interface{}{"value": tableRows}, tags)

			dlTags := copyTags(tags)
			dlTags["component"] = "data_length"
			acc.AddFields(newNamespace("info_schema", "table_size", "data_length"),
				map[string]interface{}{"value": dataLength}, dlTags)

			ilTags := copyTags(tags)
			ilTags["component"] = "index_length"
			acc.AddFields(newNamespace("info_schema", "table_size", "index_length"),
				map[string]interface{}{"value": indexLength}, ilTags)

			dfTags := copyTags(tags)
			dfTags["component"] = "data_free"
			acc.AddFields(newNamespace("info_schema", "table_size", "data_free"),
				map[string]interface{}{"value": dataFree}, dfTags)
		} else {
			acc.AddFields("mysql_table_schema",
				map[string]interface{}{"rows": tableRows}, tags)

			acc.AddFields("mysql_table_schema",
				map[string]interface{}{"data_length": dataLength}, tags)

			acc.AddFields("mysql_table_schema",
				map[string]interface{}{"index_length": indexLength}, tags)

			acc.AddFields("mysql_table_schema",
				map[string]interface{}{"data_free": dataFree}, tags)
		}

		versionTags := copyTags(tags)
		versionTags["type"] = tableType
		versionTags["engine"] = engine
		versionTags["row_format"] = rowFormat
		versionTags["create_options"] = createOptions

		if m.MetricVersion < 2 {
			acc.AddFields(newNamespace("info_schema", "table_version"),
				map[string]interface{}{"value": version}, versionTags)
		} else {
			acc.AddFields("mysql_table_schema_version",
				map[string]interface{}{"table_version": version}, versionTags)
		}
	}
	return nil
}

func (m *Mysql) parseValueByDatabaseTypeName(value sql.RawBytes, databaseTypeName string) (interface{}, error) {
	if m.MetricVersion < 2 {
		return v1.ParseValue(value)
	}

	switch databaseTypeName {
	case "INT":
		return v2.ParseInt(value)
	case "BIGINT":
		return v2.ParseUint(value)
	case "VARCHAR":
		return v2.ParseString(value)
	default:
		m.Log.Debugf("unknown database type name %q in parseValueByDatabaseTypeName", databaseTypeName)
		return v2.ParseValue(value)
	}
}

// findThreadState can be used to find thread state by command and plain state
func findThreadState(rawCommand, rawState string) string {
	var (
		// replace '_' symbol with space
		command = strings.ReplaceAll(strings.ToLower(rawCommand), "_", " ")
		state   = strings.ReplaceAll(strings.ToLower(rawState), "_", " ")
	)
	// if the state is already valid, then return it
	if _, ok := generalThreadStates[state]; ok {
		return state
	}

	// if state is plain, return the mapping
	if mappedState, ok := stateStatusMappings[state]; ok {
		return mappedState
	}
	// if the state is any lock, return the special state
	if strings.Contains(state, "waiting for") && strings.Contains(state, "lock") {
		return "waiting for lock"
	}

	if command == "sleep" && state == "" {
		return "idle"
	}

	if command == "query" {
		return "executing"
	}

	if command == "binlog dump" {
		return "replication master"
	}
	// if no mappings found and state is invalid, then return "other" state
	return "other"
}

// newNamespace can be used to make a namespace
func newNamespace(words ...string) string {
	return strings.ReplaceAll(strings.Join(words, "_"), " ", "_")
}

func copyTags(in map[string]string) map[string]string {
	out := make(map[string]string)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func getDSNTag(dsn string) string {
	conf, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "127.0.0.1:3306"
	}
	return conf.Addr
}

func init() {
	inputs.Add("mysql", func() telegraf.Input {
		return &Mysql{
			PerfEventsStatementsDigestTextLimit: defaultPerfEventsStatementsDigestTextLimit,
			PerfEventsStatementsLimit:           defaultPerfEventsStatementsLimit,
			PerfEventsStatementsTimeLimit:       defaultPerfEventsStatementsTimeLimit,
			GatherGlobalVars:                    defaultGatherGlobalVars,
		}
	})
}
