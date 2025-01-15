package sqlserver

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

func TestSqlServer_QueriesInclusionExclusion(t *testing.T) {
	cases := []map[string]interface{}{
		{
			"IncludeQuery": make([]string, 0),
			"ExcludeQuery": []string{"WaitStatsCategorized", "DatabaseIO", "ServerProperties", "MemoryClerk", "Schedulers", "VolumeSpace", "Cpu"},
			"queries":      []string{"PerformanceCounters", "SqlRequests"},
			"queriesTotal": 2,
		},
		{
			"IncludeQuery": []string{"PerformanceCounters", "SqlRequests"},
			"ExcludeQuery": []string{"SqlRequests", "WaitStatsCategorized", "DatabaseIO", "VolumeSpace", "Cpu"},
			"queries":      []string{"PerformanceCounters"},
			"queriesTotal": 1,
		},
	}

	for _, test := range cases {
		s := SQLServer{
			QueryVersion: 2,
			IncludeQuery: test["IncludeQuery"].([]string),
			ExcludeQuery: test["ExcludeQuery"].([]string),
			Log:          testutil.Logger{},
		}
		require.NoError(t, s.initQueries())
		require.Len(t, s.queries, test["queriesTotal"].(int))
		for _, query := range test["queries"].([]string) {
			require.Contains(t, s.queries, query)
		}
	}
}

func TestSqlServer_ParseMetrics(t *testing.T) {
	var acc testutil.Accumulator

	queries := make(mapQuery)
	queries["PerformanceCounters"] = query{ScriptName: "PerformanceCounters", Script: mockPerformanceCounters, ResultByRow: true}
	queries["WaitStatsCategorized"] = query{ScriptName: "WaitStatsCategorized", Script: mockWaitStatsCategorized, ResultByRow: false}
	queries["CPUHistory"] = query{ScriptName: "CPUHistory", Script: mockCPUHistory, ResultByRow: false}
	queries["DatabaseIO"] = query{ScriptName: "DatabaseIO", Script: mockDatabaseIO, ResultByRow: false}
	queries["DatabaseSize"] = query{ScriptName: "DatabaseSize", Script: mockDatabaseSize, ResultByRow: false}
	queries["DatabaseStats"] = query{ScriptName: "DatabaseStats", Script: mockDatabaseStats, ResultByRow: false}
	queries["DatabaseProperties"] = query{ScriptName: "DatabaseProperties", Script: mockDatabaseProperties, ResultByRow: false}
	queries["VolumeSpace"] = query{ScriptName: "VolumeSpace", Script: mockVolumeSpace, ResultByRow: false}
	queries["MemoryClerk"] = query{ScriptName: "MemoryClerk", Script: mockMemoryClerk, ResultByRow: false}
	queries["PerformanceMetrics"] = query{ScriptName: "PerformanceMetrics", Script: mockPerformanceMetrics, ResultByRow: false}

	var headers, mock, row []string
	var tags = make(map[string]string)
	var fields = make(map[string]interface{})

	for _, query := range queries {
		mock = strings.Split(query.Script, "\n")
		idx := 0

		for _, line := range mock {
			if idx == 0 { // headers in first line
				headers = strings.Split(line, ";")
			} else {
				row = strings.Split(line, ";")

				measurement := row[0]     // measurement
				tags[headers[1]] = row[1] // tag 'servername'
				tags[headers[2]] = row[2] // tag 'type'

				if query.ResultByRow {
					// set value by converting to float64
					value, err := strconv.ParseFloat(row[3], 64)
					// require
					require.NoError(t, err)

					// add value to Accumulator
					acc.AddFields(measurement,
						map[string]interface{}{"value": value},
						tags, time.Now())
					// assert
					acc.AssertContainsTaggedFields(t, measurement, map[string]interface{}{"value": value}, tags)
				} else {
					// set fields
					for i := 3; i < len(row); i++ {
						// set value by converting to float64
						value, err := strconv.ParseFloat(row[i], 64)
						// require
						require.NoError(t, err)

						fields[headers[i]] = value
					}
					// add fields to Accumulator
					acc.AddFields(measurement, fields, tags, time.Now())
					// assert
					acc.AssertContainsTaggedFields(t, measurement, fields, tags)
				}
			}
			idx++
		}
	}
}

func TestSqlServerIntegration_MultipleInstance(t *testing.T) {
	// Invoke Gather() from two separate configurations and
	//  confirm they don't interfere with each other
	t.Skip("Skipping as unable to open tcp connection with host '127.0.0.1:1433")

	testServer := "Server=127.0.0.1;Port=1433;User Id=SA;Password=ABCabc01;app name=telegraf;log=1"
	sl := config.NewSecret([]byte(testServer))
	s := &SQLServer{
		Servers:      []*config.Secret{&sl},
		ExcludeQuery: []string{"MemoryClerk"},
		Log:          testutil.Logger{},
	}
	sl2 := config.NewSecret([]byte(testServer))
	s2 := &SQLServer{
		Servers:      []*config.Secret{&sl2},
		ExcludeQuery: []string{"DatabaseSize"},
		Log:          testutil.Logger{},
	}

	var acc, acc2 testutil.Accumulator
	require.NoError(t, s.Start(&acc))
	err := s.Gather(&acc)
	require.NoError(t, err)

	require.NoError(t, s2.Start(&acc2))
	err = s2.Gather(&acc2)
	require.NoError(t, err)

	// acc includes size metrics, and excludes memory metrics
	require.False(t, acc.HasMeasurement("Memory breakdown (%)"))
	require.True(t, acc.HasMeasurement("Log size (bytes)"))

	// acc2 includes memory metrics, and excludes size metrics
	require.True(t, acc2.HasMeasurement("Memory breakdown (%)"))
	require.False(t, acc2.HasMeasurement("Log size (bytes)"))
}

func TestSqlServerIntegration_MultipleInstanceWithHealthMetric(t *testing.T) {
	// Invoke Gather() from two separate configurations and
	// confirm they don't interfere with each other.
	// This test is intentionally similar to TestSqlServer_MultipleInstanceIntegration.
	// It is separated to ensure that the health metric code does not affect other metrics
	t.Skip("Skipping as unable to open tcp connection with host '127.0.0.1:1433")

	testServer := "Server=127.0.0.1;Port=1433;User Id=SA;Password=ABCabc01;app name=telegraf;log=1"
	sl := config.NewSecret([]byte(testServer))
	s := &SQLServer{
		Servers:      []*config.Secret{&sl},
		ExcludeQuery: []string{"MemoryClerk"},
		Log:          testutil.Logger{},
	}

	sl2 := config.NewSecret([]byte(testServer))
	s2 := &SQLServer{
		Servers:      []*config.Secret{&sl2},
		ExcludeQuery: []string{"DatabaseSize"},
		HealthMetric: true,
		Log:          testutil.Logger{},
	}

	var acc, acc2 testutil.Accumulator
	require.NoError(t, s.Start(&acc))
	err := s.Gather(&acc)
	require.NoError(t, err)

	require.NoError(t, s2.Start(&acc))
	err = s2.Gather(&acc2)
	require.NoError(t, err)

	// acc includes size metrics, and excludes memory metrics and the health metric
	require.False(t, acc.HasMeasurement(healthMetricName))
	require.False(t, acc.HasMeasurement("Memory breakdown (%)"))
	require.True(t, acc.HasMeasurement("Log size (bytes)"))

	// acc2 includes memory metrics and the health metric, and excludes size metrics
	require.True(t, acc2.HasMeasurement(healthMetricName))
	require.True(t, acc2.HasMeasurement("Memory breakdown (%)"))
	require.False(t, acc2.HasMeasurement("Log size (bytes)"))

	sqlInstance, database := getConnectionIdentifiers(testServer)
	tags := map[string]string{healthMetricInstanceTag: sqlInstance, healthMetricDatabaseTag: database}
	require.True(t, acc2.HasPoint(healthMetricName, tags, healthMetricAttemptedQueries, 9))
	require.True(t, acc2.HasPoint(healthMetricName, tags, healthMetricSuccessfulQueries, 9))
}

func TestSqlServer_HealthMetric(t *testing.T) {
	fakeServer1 := "localhost\\fakeinstance1;Database=fakedb1;Password=ABCabc01;"
	fakeServer2 := "localhost\\fakeinstance2;Database=fakedb2;Password=ABCabc01;"

	fs1 := config.NewSecret([]byte(fakeServer1))
	fs2 := config.NewSecret([]byte(fakeServer2))

	s1 := &SQLServer{
		Servers:      []*config.Secret{&fs1, &fs2},
		IncludeQuery: []string{"DatabaseSize", "MemoryClerk"},
		HealthMetric: true,
		AuthMethod:   "connection_string",
		Log:          testutil.Logger{},
	}

	sl2 := config.NewSecret([]byte(fakeServer1))
	s2 := &SQLServer{
		Servers:      []*config.Secret{&sl2},
		IncludeQuery: []string{"DatabaseSize"},
		AuthMethod:   "connection_string",
		Log:          testutil.Logger{},
	}

	// acc1 should have the health metric because it is specified in the config
	var acc1 testutil.Accumulator
	require.NoError(t, s1.Start(&acc1))
	require.NoError(t, s1.Gather(&acc1))
	require.True(t, acc1.HasMeasurement(healthMetricName))

	// There will be 2 attempted queries (because we specified 2 queries in IncludeQuery)
	// Both queries should fail because the specified SQL instances do not exist
	sqlInstance1, database1 := getConnectionIdentifiers(fakeServer1)
	tags1 := map[string]string{healthMetricInstanceTag: sqlInstance1, healthMetricDatabaseTag: database1}
	require.True(t, acc1.HasPoint(healthMetricName, tags1, healthMetricAttemptedQueries, 2))
	require.True(t, acc1.HasPoint(healthMetricName, tags1, healthMetricSuccessfulQueries, 0))

	sqlInstance2, database2 := getConnectionIdentifiers(fakeServer2)
	tags2 := map[string]string{healthMetricInstanceTag: sqlInstance2, healthMetricDatabaseTag: database2}
	require.True(t, acc1.HasPoint(healthMetricName, tags2, healthMetricAttemptedQueries, 2))
	require.True(t, acc1.HasPoint(healthMetricName, tags2, healthMetricSuccessfulQueries, 0))

	// acc2 should not have the health metric because it is not specified in the config
	var acc2 testutil.Accumulator
	require.NoError(t, s2.Gather(&acc2))
	require.False(t, acc2.HasMeasurement(healthMetricName))
}

func TestSqlServer_MultipleInit(t *testing.T) {
	s := &SQLServer{Log: testutil.Logger{}}
	s2 := &SQLServer{
		ExcludeQuery: []string{"DatabaseSize"},
		Log:          testutil.Logger{},
	}

	require.NoError(t, s.initQueries())
	_, ok := s.queries["DatabaseSize"]
	require.True(t, ok)

	require.NoError(t, s.initQueries())
	_, ok = s2.queries["DatabaseSize"]
	require.False(t, ok)
	s.Stop()
	s2.Stop()
}

func TestSqlServer_ConnectionString(t *testing.T) {
	// URL format
	connectionString := "sqlserver://username:password@hostname.database.windows.net?database=databasename&connection+timeout=30"
	sqlInstance, database := getConnectionIdentifiers(connectionString)
	require.Equal(t, "hostname.database.windows.net", sqlInstance)
	require.Equal(t, "databasename", database)

	connectionString = "    sqlserver://hostname2.somethingelse.net:1433?database=databasename2"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "hostname2.somethingelse.net", sqlInstance)
	require.Equal(t, "databasename2", database)

	connectionString = "sqlserver://hostname3:1433/SqlInstanceName3?database=databasename3"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "hostname3\\SqlInstanceName3", sqlInstance)
	require.Equal(t, "databasename3", database)

	connectionString = " sqlserver://hostname4/SqlInstanceName4?database=databasename4&connection%20timeout=30"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "hostname4\\SqlInstanceName4", sqlInstance)
	require.Equal(t, "databasename4", database)

	connectionString = "	sqlserver://username:password@hostname5?connection%20timeout=30"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "hostname5", sqlInstance)
	require.Equal(t, emptyDatabaseName, database)

	// odbc format
	connectionString = "odbc:server=hostname.database.windows.net;user id=sa;database=master;Trusted_Connection=Yes;Integrated Security=true;"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "hostname.database.windows.net", sqlInstance)
	require.Equal(t, "master", database)

	connectionString = "   odbc:server=192.168.0.1;user id=somethingelse;Integrated Security=true;Database=mydb   "
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "192.168.0.1", sqlInstance)
	require.Equal(t, "mydb", database)

	connectionString = " odbc:Server=servername\\instancename;Database=dbname;"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "servername\\instancename", sqlInstance)
	require.Equal(t, "dbname", database)

	connectionString = "server=hostname2.database.windows.net;user id=sa;Trusted_Connection=Yes;Integrated Security=true;"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "hostname2.database.windows.net", sqlInstance)
	require.Equal(t, emptyDatabaseName, database)

	connectionString = "invalid connection string"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, emptySQLInstance, sqlInstance)
	require.Equal(t, emptyDatabaseName, database)

	// Key/value format
	connectionString = "  server=hostname.database.windows.net;user id=sa;database=master;Trusted_Connection=Yes;Integrated Security=true"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "hostname.database.windows.net", sqlInstance)
	require.Equal(t, "master", database)

	connectionString = " server=192.168.0.1;user id=somethingelse;Integrated Security=true;Database=mydb;"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "192.168.0.1", sqlInstance)
	require.Equal(t, "mydb", database)

	connectionString = "Server=servername\\instancename;Database=dbname;  "
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "servername\\instancename", sqlInstance)
	require.Equal(t, "dbname", database)

	connectionString = "server=hostname2.database.windows.net;user id=sa;Trusted_Connection=Yes;Integrated Security=true  "
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, "hostname2.database.windows.net", sqlInstance)
	require.Equal(t, emptyDatabaseName, database)

	connectionString = "invalid connection string"
	sqlInstance, database = getConnectionIdentifiers(connectionString)
	require.Equal(t, emptySQLInstance, sqlInstance)
	require.Equal(t, emptyDatabaseName, database)
}

func TestSqlServerIntegration_AGQueriesApplicableForDatabaseTypeSQLServer(t *testing.T) {
	// This test case checks where Availability Group (AG / HADR) queries return an output when included for processing for DatabaseType = SQLServer
	// And they should not be processed when DatabaseType = AzureSQLDB

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if os.Getenv("AZURESQL_POOL_CONNECTION_STRING") == "" {
		t.Skip("Missing environment variable AZURESQL_POOL_CONNECTION_STRING")
	}
	testServer := os.Getenv("AZURESQL_POOL_CONNECTION_STRING")

	sl := config.NewSecret([]byte(testServer))
	s := &SQLServer{
		Servers:      []*config.Secret{&sl},
		DatabaseType: "SQLServer",
		IncludeQuery: []string{"SQLServerAvailabilityReplicaStates", "SQLServerDatabaseReplicaStates"},
		Log:          testutil.Logger{},
	}

	sl2 := config.NewSecret([]byte(testServer))
	s2 := &SQLServer{
		Servers:      []*config.Secret{&sl2},
		DatabaseType: "AzureSQLDB",
		IncludeQuery: []string{"SQLServerAvailabilityReplicaStates", "SQLServerDatabaseReplicaStates"},
		Log:          testutil.Logger{},
	}

	var acc, acc2 testutil.Accumulator
	require.NoError(t, s.Start(&acc))
	err := s.Gather(&acc)
	require.NoError(t, err)

	err = s2.Gather(&acc2)
	require.NoError(t, s2.Start(&acc))
	require.NoError(t, err)

	// acc includes size metrics, and excludes memory metrics
	require.True(t, acc.HasMeasurement("sqlserver_hadr_replica_states"))
	require.True(t, acc.HasMeasurement("sqlserver_hadr_dbreplica_states"))

	// acc2 includes memory metrics, and excludes size metrics
	require.False(t, acc2.HasMeasurement("sqlserver_hadr_replica_states"))
	require.False(t, acc2.HasMeasurement("sqlserver_hadr_dbreplica_states"))
	s.Stop()
	s2.Stop()
}

func TestSqlServerIntegration_AGQueryFieldsOutputBasedOnSQLServerVersion(t *testing.T) {
	// This test case checks where Availability Group (AG / HADR) queries return specific fields
	// supported by corresponding SQL Server version database being connected to.

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	if os.Getenv("AZURESQL_POOL_CONNECTION_STRING_2019") == "" {
		t.Skip("Missing environment variable AZURESQL_POOL_CONNECTION_STRING_2019")
	}
	if os.Getenv("AZURESQL_POOL_CONNECTION_STRING_2012") == "" {
		t.Skip("Missing environment variable AZURESQL_POOL_CONNECTION_STRING_2012")
	}

	testServer2019 := os.Getenv("AZURESQL_POOL_CONNECTION_STRING_2019")
	testServer2012 := os.Getenv("AZURESQL_POOL_CONNECTION_STRING_2012")

	sl2019 := config.NewSecret([]byte(testServer2019))
	sl2012 := config.NewSecret([]byte(testServer2012))

	s2019 := &SQLServer{
		Servers:      []*config.Secret{&sl2019},
		DatabaseType: "SQLServer",
		IncludeQuery: []string{"SQLServerAvailabilityReplicaStates", "SQLServerDatabaseReplicaStates"},
		Log:          testutil.Logger{},
	}
	s2012 := &SQLServer{
		Servers:      []*config.Secret{&sl2012},
		DatabaseType: "SQLServer",
		IncludeQuery: []string{"SQLServerAvailabilityReplicaStates", "SQLServerDatabaseReplicaStates"},
		Log:          testutil.Logger{},
	}

	var acc2019, acc2012 testutil.Accumulator
	require.NoError(t, s2019.Start(&acc2019))
	err := s2019.Gather(&acc2019)
	require.NoError(t, err)

	err = s2012.Gather(&acc2012)
	require.NoError(t, s2012.Start(&acc2012))
	require.NoError(t, err)

	// acc2019 includes new HADR query fields
	require.True(t, acc2019.HasField("sqlserver_hadr_replica_states", "basic_features"))
	require.True(t, acc2019.HasField("sqlserver_hadr_replica_states", "is_distributed"))
	require.True(t, acc2019.HasField("sqlserver_hadr_replica_states", "seeding_mode"))
	require.True(t, acc2019.HasTag("sqlserver_hadr_replica_states", "seeding_mode_desc"))
	require.True(t, acc2019.HasField("sqlserver_hadr_dbreplica_states", "is_primary_replica"))
	require.True(t, acc2019.HasField("sqlserver_hadr_dbreplica_states", "secondary_lag_seconds"))

	// acc2012 does not include new HADR query fields
	require.False(t, acc2012.HasField("sqlserver_hadr_replica_states", "basic_features"))
	require.False(t, acc2012.HasField("sqlserver_hadr_replica_states", "is_distributed"))
	require.False(t, acc2012.HasField("sqlserver_hadr_replica_states", "seeding_mode"))
	require.False(t, acc2012.HasTag("sqlserver_hadr_replica_states", "seeding_mode_desc"))
	require.False(t, acc2012.HasField("sqlserver_hadr_dbreplica_states", "is_primary_replica"))
	require.False(t, acc2012.HasField("sqlserver_hadr_dbreplica_states", "secondary_lag_seconds"))
	s2019.Stop()
	s2012.Stop()
}

const mockPerformanceMetrics = `measurement;servername;type;Point In Time Recovery;Available physical memory (bytes);Average pending disk IO;` +
	`Average runnable tasks;Average tasks;Buffer pool rate (bytes/sec);Connection memory per connection (bytes);Memory grant pending;` +
	`Page File Usage (%);Page lookup per batch request;Page split per batch request;Readahead per page read;Signal wait (%);` +
	`Sql compilation per batch request;Sql recompilation per batch request;Total target memory ratio
Performance metrics;WIN8-DEV;Performance metrics;0;6353158144;0;0;7;2773;415061;0;25;229371;130;10;18;188;52;14`

const mockWaitStatsCategorized = `measurement;servername;type;I/O;Latch;Lock;Network;Service broker;Memory;Buffer;CLR;XEvent;Other;Total
Wait time (ms);WIN8-DEV;Wait stats;0;0;0;0;0;0;0;0;0;0;0
Wait tasks;WIN8-DEV;Wait stats;0;0;0;0;0;0;0;0;0;1;1`

const mockCPUHistory = `measurement;servername;type;SQL process;External process;SystemIdle
CPU (%);WIN8-DEV;CPU;0;2;98`

const mockDatabaseIO = `measurement;servername;type;AdventureWorks2014;Australian;DOC.Azure;master;model;msdb;ngMon;ResumeCloud;tempdb;Total
Log writes (bytes/sec);WIN8-DEV;Database IO;0;0;0;0;0;0;0;0;159744;159744
Rows writes (bytes/sec);WIN8-DEV;Database IO;0;0;0;0;0;0;0;0;0;0
Log reads (bytes/sec);WIN8-DEV;Database IO;0;0;0;0;0;0;0;0;0;0
Rows reads (bytes/sec);WIN8-DEV;Database IO;0;0;0;0;0;0;0;0;6553;6553
Log (writes/sec);WIN8-DEV;Database IO;0;0;0;0;0;0;0;0;2;2
Rows (writes/sec);WIN8-DEV;Database IO;0;0;0;0;0;0;0;0;0;0
Log (reads/sec);WIN8-DEV;Database IO;0;0;0;0;0;0;0;0;0;0
Rows (reads/sec);WIN8-DEV;Database IO;0;0;0;0;0;0;0;0;0;0`

const mockDatabaseSize = `measurement;servername;type;AdventureWorks2014;Australian;DOC.Azure;master;model;msdb;ngMon;ResumeCloud;tempdb
Log size (bytes);WIN8-DEV;Database size;538968064;1048576;786432;2359296;4325376;30212096;1048576;786432;4194304
Rows size (bytes);WIN8-DEV;Database size;2362703872;3211264;26083328;5111808;3342336;24051712;46137344;10551296;1073741824`

const mockDatabaseProperties string = `measurement;servername;type;AdventureWorks2014;Australian;DOC.Azure;master;model;msdb;ngMon;ResumeCloud;tempdb;total
Recovery Model FULL;WIN8-DEV;Database properties;1;0;0;0;1;0;0;0;0;2
Recovery Model BULK_LOGGED;WIN8-DEV;Database properties;0;0;0;0;0;0;0;0;0;0
Recovery Model SIMPLE;WIN8-DEV;Database properties;0;1;1;1;0;1;1;1;1;7
State ONLINE;WIN8-DEV;Database properties;1;1;1;1;1;1;1;1;1;9
State RESTORING;WIN8-DEV;Database properties;0;0;0;0;0;0;0;0;0;0
State RECOVERING;WIN8-DEV;Database properties;0;0;0;0;0;0;0;0;0;0
State RECOVERY_PENDING;WIN8-DEV;Database properties;0;0;0;0;0;0;0;0;0;0
State SUSPECT;WIN8-DEV;Database properties;0;0;0;0;0;0;0;0;0;0
State EMERGENCY;WIN8-DEV;Database properties;0;0;0;0;0;0;0;0;0;0
State OFFLINE;WIN8-DEV;Database properties;0;0;0;0;0;0;0;0;0;0`

const mockMemoryClerk = `measurement;servername;type;Buffer pool;Cache (objects);Cache (sql plans);Other
Memory breakdown (%);WIN8-DEV;Memory clerk;31.30;0.30;14.00;54.50
Memory breakdown (bytes);WIN8-DEV;Memory clerk;51986432.00;409600.00;23166976.00;90365952.00`

const mockDatabaseStats = `measurement;servername;type;AdventureWorks2014;Australian;DOC.Azure;master;model;msdb;ngMon;ResumeCloud;tempdb
Log read latency (ms);WIN8-DEV;Database stats;24;20;11;15;20;46;0;0;3
Log write latency (ms);WIN8-DEV;Database stats;3;0;0;2;0;1;0;0;0
Rows read latency (ms);WIN8-DEV;Database stats;42;23;52;31;19;29;59;50;71
Rows write latency (ms);WIN8-DEV;Database stats;0;0;0;9;0;0;0;0;0
Rows (average bytes/read);WIN8-DEV;Database stats;62580;58056;59603;63015;62968;63042;58056;58919;176703
Rows (average bytes/write);WIN8-DEV;Database stats;8192;0;0;8192;8192;0;0;0;32768
Log (average bytes/read);WIN8-DEV;Database stats;69358;50322;74313;41642;19569;29857;45641;18432;143945
Log (average bytes/write);WIN8-DEV;Database stats;4096;4096;0;5324;4915;4096;4096;32768;52379`

const mockVolumeSpace = `measurement;servername;type;C:;D: (DATA);L: (LOG)
Volume total space (bytes);WIN8-DEV;OS Volume space;135338651648.00;32075874304.00;10701701120.00
Volume available space (bytes);WIN8-DEV;OS Volume space;54297817088.00;28439674880.00;10107355136.00
Volume used space (bytes);WIN8-DEV;OS Volume space;81040834560.00;3636199424.00;594345984.00
Volume used space (%);WIN8-DEV;OS Volume space;60.00;11.00;6.00`

const mockPerformanceCounters = `measurement;servername;type;value
AU cleanup batches/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
AU cleanups/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
By-reference Lob Create Count | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
By-reference Lob Use Count | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Count Lob Readahead | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Count Pull In Row | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Count Push Off Row | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Deferred dropped AUs | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Deferred Dropped rowsets | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Dropped rowset cleanups/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Dropped rowsets skipped/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Extent Deallocations/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Extents Allocated/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;2
Failed AU cleanup batches/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Failed leaf page cookie | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Failed tree page cookie | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Forwarded Records/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
FreeSpace Page Fetches/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
FreeSpace Scans/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Full Scans/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Index Searches/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;1208
InSysXact waits/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
LobHandle Create Count | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
LobHandle Destroy Count | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
LobSS Provider Create Count | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
LobSS Provider Destroy Count | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
LobSS Provider Truncation Count | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Mixed page allocations/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;10
Page compression attempts/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Page Deallocations/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Page Splits/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;20
Pages Allocated/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;22
Pages compressed/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Probe Scans/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;6
Range Scans/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;45
Scan Point Revalidations/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Skipped Ghosted Records/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Table Lock Escalations/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Used leaf page cookie | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Used tree page cookie | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Workfiles Created/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;8
Worktables Created/sec | SQLServer:Access Methods;WIN8-DEV;Performance counters;2
Worktables From Cache Base | SQLServer:Access Methods;WIN8-DEV;Performance counters;0
Worktables From Cache Ratio | SQLServer:Access Methods;WIN8-DEV;Performance counters;1
Bytes Received from Replica/sec | _Total | SQLServer:Availability Replica;WIN8-DEV;Performance counters;0
Bytes Sent to Replica/sec | _Total | SQLServer:Availability Replica;WIN8-DEV;Performance counters;0
Bytes Sent to Transport/sec | _Total | SQLServer:Availability Replica;WIN8-DEV;Performance counters;0
Flow Control Time (ms/sec) | _Total | SQLServer:Availability Replica;WIN8-DEV;Performance counters;0
Flow Control/sec | _Total | SQLServer:Availability Replica;WIN8-DEV;Performance counters;0
Receives from Replica/sec | _Total | SQLServer:Availability Replica;WIN8-DEV;Performance counters;0
Resent Messages/sec | _Total | SQLServer:Availability Replica;WIN8-DEV;Performance counters;0
Sends to Replica/sec | _Total | SQLServer:Availability Replica;WIN8-DEV;Performance counters;0
Sends to Transport/sec | _Total | SQLServer:Availability Replica;WIN8-DEV;Performance counters;0
Batches >=000000ms & <000001ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;369
Batches >=000000ms & <000001ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=000000ms & <000001ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;370
Batches >=000000ms & <000001ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=000001ms & <000002ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;24
Batches >=000001ms & <000002ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;24
Batches >=000001ms & <000002ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;16
Batches >=000001ms & <000002ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;16
Batches >=000002ms & <000005ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;10
Batches >=000002ms & <000005ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;30
Batches >=000002ms & <000005ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;8
Batches >=000002ms & <000005ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;23
Batches >=000005ms & <000010ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;30
Batches >=000005ms & <000010ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;211
Batches >=000005ms & <000010ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;21
Batches >=000005ms & <000010ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;148
Batches >=000010ms & <000020ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;30
Batches >=000010ms & <000020ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;432
Batches >=000010ms & <000020ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;21
Batches >=000010ms & <000020ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;305
Batches >=000020ms & <000050ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;46
Batches >=000020ms & <000050ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;1545
Batches >=000020ms & <000050ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;37
Batches >=000020ms & <000050ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;1261
Batches >=000050ms & <000100ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;35
Batches >=000050ms & <000100ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;2463
Batches >=000050ms & <000100ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;18
Batches >=000050ms & <000100ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;1343
Batches >=000100ms & <000200ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;1
Batches >=000100ms & <000200ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;161
Batches >=000100ms & <000200ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;3
Batches >=000100ms & <000200ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;373
Batches >=000200ms & <000500ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=000200ms & <000500ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=000200ms & <000500ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;1
Batches >=000200ms & <000500ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;255
Batches >=000500ms & <001000ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=000500ms & <001000ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=000500ms & <001000ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;2
Batches >=000500ms & <001000ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;1291
Batches >=001000ms & <002000ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=001000ms & <002000ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=001000ms & <002000ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;19
Batches >=001000ms & <002000ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;21560
Batches >=002000ms & <005000ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=002000ms & <005000ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=002000ms & <005000ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;1
Batches >=002000ms & <005000ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;2257
Batches >=005000ms & <010000ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=005000ms & <010000ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=005000ms & <010000ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;19
Batches >=005000ms & <010000ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;97479
Batches >=010000ms & <020000ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=010000ms & <020000ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=010000ms & <020000ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=010000ms & <020000ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=020000ms & <050000ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=020000ms & <050000ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=020000ms & <050000ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=020000ms & <050000ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=050000ms & <100000ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=050000ms & <100000ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=050000ms & <100000ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=050000ms & <100000ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=100000ms | CPU Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=100000ms | CPU Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=100000ms | Elapsed Time:Requests | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Batches >=100000ms | Elapsed Time:Total(ms) | SQLServer:Batch Resp Statistics;WIN8-DEV;Performance counters;0
Stored Procedures Invoked/sec | _Total | SQLServer:Broker Activation;WIN8-DEV;Performance counters;0
Task Limit Reached | _Total | SQLServer:Broker Activation;WIN8-DEV;Performance counters;3
Task Limit Reached/sec | _Total | SQLServer:Broker Activation;WIN8-DEV;Performance counters;0
Tasks Aborted/sec | _Total | SQLServer:Broker Activation;WIN8-DEV;Performance counters;0
Tasks Running | _Total | SQLServer:Broker Activation;WIN8-DEV;Performance counters;0
Tasks Started/sec | _Total | SQLServer:Broker Activation;WIN8-DEV;Performance counters;0
Activation Errors Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Broker Transaction Rollbacks | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Corrupted Messages Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Dequeued TransmissionQ Msgs/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Dialog Timer Event Count | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Dropped Messages Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued Local Messages Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued Local Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued Messages Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued P1 Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued P10 Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued P2 Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued P3 Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued P4 Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued P5 Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued P6 Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued P7 Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued P8 Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued P9 Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued TransmissionQ Msgs/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued Transport Msg Frag Tot | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued Transport Msg Frags/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued Transport Msgs Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Enqueued Transport Msgs/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Forwarded Messages Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Forwarded Messages/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Forwarded Msg Byte Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Forwarded Msg Bytes/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Forwarded Msg Discarded Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Forwarded Msgs Discarded/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Forwarded Pending Msg Bytes | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Forwarded Pending Msg Count | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
SQL RECEIVE Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
SQL RECEIVEs/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
SQL SEND Total | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
SQL SENDs/sec | SQLServer:Broker Statistics;WIN8-DEV;Performance counters;0
Avg. Length of Batched Writes | SQLServer:Broker TO Statistics;WIN8-DEV;Performance counters;0
Avg. Length of Batched Writes BS | SQLServer:Broker TO Statistics;WIN8-DEV;Performance counters;1
Avg. Time Between Batches (ms) | SQLServer:Broker TO Statistics;WIN8-DEV;Performance counters;2062
Avg. Time Between Batches Base | SQLServer:Broker TO Statistics;WIN8-DEV;Performance counters;1
Avg. Time to Write Batch (ms) | SQLServer:Broker TO Statistics;WIN8-DEV;Performance counters;0
Avg. Time to Write Batch Base | SQLServer:Broker TO Statistics;WIN8-DEV;Performance counters;1
Transmission Obj Gets/Sec | SQLServer:Broker TO Statistics;WIN8-DEV;Performance counters;0
Transmission Obj Set Dirty/Sec | SQLServer:Broker TO Statistics;WIN8-DEV;Performance counters;0
Transmission Obj Writes/Sec | SQLServer:Broker TO Statistics;WIN8-DEV;Performance counters;0
Current Bytes for Recv I/O | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Current Bytes for Send I/O | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Current Msg Frags for Send I/O | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment P1 Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment P10 Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment P2 Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment P3 Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment P4 Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment P5 Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment P6 Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment P7 Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment P8 Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment P9 Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment Receives/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Message Fragment Sends/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Msg Fragment Recv Size Avg | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Msg Fragment Recv Size Avg Base | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Msg Fragment Send Size Avg | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Msg Fragment Send Size Avg Base | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Open Connection Count | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Pending Bytes for Recv I/O | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Pending Bytes for Send I/O | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Pending Msg Frags for Recv I/O | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Pending Msg Frags for Send I/O | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Receive I/O bytes/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Receive I/O Len Avg | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Receive I/O Len Avg Base | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Receive I/Os/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Recv I/O Buffer Copies bytes/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Recv I/O Buffer Copies Count | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Send I/O bytes/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Send I/O Len Avg | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Send I/O Len Avg Base | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Send I/Os/sec | SQLServer:Broker/DBM Transport;WIN8-DEV;Performance counters;0
Background writer pages/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Buffer cache hit ratio | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;1
Buffer cache hit ratio base | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;2448
Checkpoint pages/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Database pages | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;6676
Extension allocated pages | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Extension free pages | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Extension in use as percentage | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Extension outstanding IO counter | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Extension page evictions/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Extension page reads/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Extension page unreferenced time | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Extension page writes/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Free list stalls/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Integral Controller Slope | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;10
Lazy writes/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Page life expectancy | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;29730
Page lookups/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;2534
Page reads/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Page writes/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Readahead pages/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Readahead time/sec | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;0
Target pages | SQLServer:Buffer Manager;WIN8-DEV;Performance counters;16367616
Database pages | 000 | SQLServer:Buffer Node;WIN8-DEV;Performance counters;6676
Local node page lookups/sec | 000 | SQLServer:Buffer Node;WIN8-DEV;Performance counters;0
Page life expectancy | 000 | SQLServer:Buffer Node;WIN8-DEV;Performance counters;29730
Remote node page lookups/sec | 000 | SQLServer:Buffer Node;WIN8-DEV;Performance counters;0
Cache Entries Count | _Total | SQLServer:Catalog Metadata;WIN8-DEV;Performance counters;2428
Cache Entries Count | mssqlsystemresource | SQLServer:Catalog Metadata;WIN8-DEV;Performance counters;2204
Cache Entries Pinned Count | _Total | SQLServer:Catalog Metadata;WIN8-DEV;Performance counters;0
Cache Entries Pinned Count | mssqlsystemresource | SQLServer:Catalog Metadata;WIN8-DEV;Performance counters;0
Cache Hit Ratio | _Total | SQLServer:Catalog Metadata;WIN8-DEV;Performance counters;1
Cache Hit Ratio | mssqlsystemresource | SQLServer:Catalog Metadata;WIN8-DEV;Performance counters;1
Cache Hit Ratio Base | _Total | SQLServer:Catalog Metadata;WIN8-DEV;Performance counters;71
Cache Hit Ratio Base | mssqlsystemresource | SQLServer:Catalog Metadata;WIN8-DEV;Performance counters;30
CLR Execution | SQLServer:CLR;WIN8-DEV;Performance counters;327033
Active cursors | _Total | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Active cursors | API Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Active cursors | TSQL Global Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Active cursors | TSQL Local Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cache Hit Ratio | _Total | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cache Hit Ratio | API Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cache Hit Ratio | TSQL Global Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cache Hit Ratio | TSQL Local Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cache Hit Ratio Base | _Total | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cache Hit Ratio Base | API Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cache Hit Ratio Base | TSQL Global Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cache Hit Ratio Base | TSQL Local Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cached Cursor Counts | _Total | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cached Cursor Counts | API Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cached Cursor Counts | TSQL Global Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cached Cursor Counts | TSQL Local Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor Cache Use Counts/sec | _Total | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor Cache Use Counts/sec | API Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor Cache Use Counts/sec | TSQL Global Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor Cache Use Counts/sec | TSQL Local Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor memory usage | _Total | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor memory usage | API Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor memory usage | TSQL Global Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor memory usage | TSQL Local Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor Requests/sec | _Total | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor Requests/sec | API Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor Requests/sec | TSQL Global Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor Requests/sec | TSQL Local Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor worktable usage | _Total | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor worktable usage | API Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor worktable usage | TSQL Global Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Cursor worktable usage | TSQL Local Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Number of active cursor plans | _Total | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Number of active cursor plans | API Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Number of active cursor plans | TSQL Global Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Number of active cursor plans | TSQL Local Cursor | SQLServer:Cursor Manager by Type;WIN8-DEV;Performance counters;0
Async population count | SQLServer:Cursor Manager Total;WIN8-DEV;Performance counters;0
Cursor conversion rate | SQLServer:Cursor Manager Total;WIN8-DEV;Performance counters;0
Cursor flushes | SQLServer:Cursor Manager Total;WIN8-DEV;Performance counters;0
File Bytes Received/sec | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Log Bytes Received/sec | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Log remaining for undo | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Log Send Queue | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Mirrored Write Transactions/sec | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Recovery Queue | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Redo blocked/sec | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Redo Bytes Remaining | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Redone Bytes/sec | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Total Log requiring undo | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Transaction Delay | _Total | SQLServer:Database Replica;WIN8-DEV;Performance counters;0
Active Transactions | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Active Transactions | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Backup/Restore Throughput/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Backup/Restore Throughput/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Bulk Copy Rows/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Bulk Copy Rows/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Bulk Copy Throughput/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Bulk Copy Throughput/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Commit table entries | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Commit table entries | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Data File(s) Size (KB) | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;3512576
Data File(s) Size (KB) | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;40960
DBCC Logical Scan Bytes/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
DBCC Logical Scan Bytes/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Group Commit Time/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Group Commit Time/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Bytes Flushed/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;307200
Log Bytes Flushed/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Cache Hit Ratio | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Cache Hit Ratio | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Cache Hit Ratio Base | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Cache Hit Ratio Base | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Cache Reads/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Cache Reads/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log File(s) Size (KB) | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;570992
Log File(s) Size (KB) | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;1016
Log File(s) Used Size (KB) | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;315480
Log File(s) Used Size (KB) | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;634
Log Flush Wait Time | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Flush Wait Time | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Flush Waits/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Flush Waits/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Flush Write Time (ms) | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;1
Log Flush Write Time (ms) | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Flushes/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;5
Log Flushes/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Growths | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Growths | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Pool Cache Misses/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Pool Cache Misses/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Pool Disk Reads/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Pool Disk Reads/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Pool Requests/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Pool Requests/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Shrinks | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Shrinks | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Log Truncations | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;5
Log Truncations | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Percent Log Used | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;55
Percent Log Used | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;62
Repl. Pending Xacts | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Repl. Pending Xacts | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Repl. Trans. Rate | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Repl. Trans. Rate | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Shrink Data Movement Bytes/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Shrink Data Movement Bytes/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Tracked transactions/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
Tracked transactions/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Transactions/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;6
Transactions/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Write Transactions/sec | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;3
Write Transactions/sec | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
XTP Memory Used (KB) | _Total | SQLServer:Databases;WIN8-DEV;Performance counters;0
XTP Memory Used (KB) | mssqlsystemresource | SQLServer:Databases;WIN8-DEV;Performance counters;0
Usage | '#' and '##' as the name of temporary tables and stored procedures | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | '::' function calling syntax | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | '@' and names that start with '@@' as Transact-SQL identifiers | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | ADDING TAPE DEVICE | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | ALL Permission | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | ALTER DATABASE WITH TORN_PAGE_DETECTION | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | ALTER LOGIN WITH SET CREDENTIAL | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | asymmetric_keys | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | asymmetric_keys.attested_by | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Azeri_Cyrillic_90 | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Azeri_Latin_90 | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | BACKUP DATABASE or LOG TO TAPE | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | certificates | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | certificates.attested_by | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Create/alter SOAP endpoint | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | CREATE_DROP_DEFAULT | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | CREATE_DROP_RULE | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Data types: text ntext or image | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Database compatibility level 100 | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Database compatibility level 110 | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;4
Usage | Database compatibility level 90 | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Database Mirroring | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DATABASEPROPERTY | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DATABASEPROPERTYEX('IsFullTextEnabled') | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DBCC [UN]PINTABLE | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DBCC DBREINDEX | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DBCC INDEXDEFRAG | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DBCC SHOWCONTIG | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DBCC_EXTENTINFO | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DBCC_IND | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DEFAULT keyword as a default value | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Deprecated Attested Option | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Deprecated encryption algorithm | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DESX algorithm | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | dm_fts_active_catalogs | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | dm_fts_active_catalogs.is_paused | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | dm_fts_active_catalogs.previous_status | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | dm_fts_active_catalogs.previous_status_description | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | dm_fts_active_catalogs.row_count_in_thousands | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | dm_fts_active_catalogs.status | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | dm_fts_active_catalogs.status_description | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | dm_fts_active_catalogs.worker_count | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | dm_fts_memory_buffers | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | dm_fts_memory_buffers.row_count | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | DROP INDEX with two-part name | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | endpoint_webmethods | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | EXTPROP_LEVEL0TYPE | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | EXTPROP_LEVEL0USER | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | FILE_ID | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fn_get_sql | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fn_servershareddrives | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fn_trace_geteventinfo | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fn_trace_getfilterinfo | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fn_trace_getinfo | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fn_trace_gettable | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fn_virtualservernodes | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fulltext_catalogs | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fulltext_catalogs.data_space_id | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fulltext_catalogs.file_id | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | fulltext_catalogs.path | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | FULLTEXTCATALOGPROPERTY('LogSize') | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | FULLTEXTCATALOGPROPERTY('PopulateStatus') | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | FULLTEXTSERVICEPROPERTY('ConnectTimeout') | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | FULLTEXTSERVICEPROPERTY('DataTimeout') | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | FULLTEXTSERVICEPROPERTY('ResourceUsage') | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | GROUP BY ALL | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Hindi | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | IDENTITYCOL | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | IN PATH | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Index view select list without COUNT_BIG(*) | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | INDEX_OPTION | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | INDEXKEY_PROPERTY | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Indirect TVF hints | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | INSERT NULL into TIMESTAMP columns | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | INSERT_HINTS | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Korean_Wansung_Unicode | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Lithuanian_Classic | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Macedonian | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | MODIFY FILEGROUP READONLY | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | MODIFY FILEGROUP READWRITE | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | More than two-part column name | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Multiple table hints without comma | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | NOLOCK or READUNCOMMITTED in UPDATE or DELETE | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Numbered stored procedures | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | numbered_procedure_parameters | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | numbered_procedures | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | objidupdate | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Old NEAR Syntax | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | OLEDB for ad hoc connections | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | PERMISSIONS | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | READTEXT | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | REMSERVER | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | RESTORE DATABASE or LOG WITH MEDIAPASSWORD | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | RESTORE DATABASE or LOG WITH PASSWORD | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Returning results from trigger | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | ROWGUIDCOL | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | SET ANSI_NULLS OFF | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | SET ANSI_PADDING OFF | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | SET CONCAT_NULL_YIELDS_NULL OFF | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | SET ERRLVL | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | SET FMTONLY ON | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | SET OFFSETS | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | SET REMOTE_PROC_TRANSACTIONS | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | SET ROWCOUNT | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | SETUSER | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | soap_endpoints | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_addapprole | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_addextendedproc | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_addlogin | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_addremotelogin | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_addrole | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_addrolemember | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_addserver | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_addsrvrolemember | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_addtype | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_adduser | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_approlepassword | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_attach_db | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_attach_single_file_db | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_bindefault | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_bindrule | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_bindsession | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_certify_removable | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_change_users_login | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_changedbowner | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_changeobjectowner | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'affinity mask' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'affinity64 mask' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'allow updates' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'c2 audit mode' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'default trace enabled' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'disallow results from triggers' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'ft crawl bandwidth (max)' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'ft crawl bandwidth (min)' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'ft notify bandwidth (max)' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'ft notify bandwidth (min)' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'locks' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'open objects' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'priority boost' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'remote proc trans' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_configure 'set working set size' | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_control_dbmasterkey_password | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_create_removable | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_db_increased_partitions | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_db_selective_xml_index | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_db_vardecimal_storage_format | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_dbcmptlevel | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_dbfixedrolepermission | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_dbremove | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_defaultdb | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_defaultlanguage | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_denylogin | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_depends | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_detach_db @keepfulltextindexfile | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_dropapprole | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_dropextendedproc | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_droplogin | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_dropremotelogin | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_droprole | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_droprolemember | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_dropsrvrolemember | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_droptype | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_dropuser | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_estimated_rowsize_reduction_for_vardecimal | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_fulltext_catalog | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_fulltext_column | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_fulltext_database | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_fulltext_service @action=clean_up | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_fulltext_service @action=connect_timeout | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_fulltext_service @action=data_timeout | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_fulltext_service @action=resource_usage | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_fulltext_table | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_getbindtoken | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_grantdbaccess | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_grantlogin | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_help_fulltext_catalog_components | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_help_fulltext_catalogs | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_help_fulltext_catalogs_cursor | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_help_fulltext_columns | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_help_fulltext_columns_cursor | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_help_fulltext_tables | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_help_fulltext_tables_cursor | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_helpdevice | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_helpextendedproc | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_helpremotelogin | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_indexoption | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_lock | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_password | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_remoteoption | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_renamedb | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_resetstatus | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_revokedbaccess | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_revokelogin | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_srvrolepermission | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_trace_create | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_trace_getdata | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_trace_setevent | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_trace_setfilter | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_trace_setstatus | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_unbindefault | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sp_unbindrule | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | SQL_AltDiction_CP1253_CS_AS | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sql_dependencies | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | String literals as column aliases | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysaltfiles | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | syscacheobjects | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | syscolumns | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | syscomments | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysconfigures | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysconstraints | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | syscurconfigs | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysdatabases | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysdepends | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysdevices | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysfilegroups | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysfiles | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysforeignkeys | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysfulltextcatalogs | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysindexes | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysindexkeys | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | syslockinfo | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | syslogins | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysmembers | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysmessages | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysobjects | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysoledbusers | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysopentapes | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysperfinfo | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | syspermissions | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysprocesses | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysprotects | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysreferences | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysremotelogins | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysservers | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | systypes | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | sysusers | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Table hint without WITH | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Text in row table option | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | TEXTPTR | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | TEXTVALID | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | TIMESTAMP | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | UPDATETEXT or WRITETEXT | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | USER_ID | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Using OLEDB for linked servers | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | Vardecimal storage format | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | XMLDATA | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | XP_API | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;3
Usage | xp_grantlogin | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | xp_loginconfig | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Usage | xp_revokelogin | SQLServer:Deprecated Features;WIN8-DEV;Performance counters;0
Distributed Query | Average execution time (ms) | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
Distributed Query | Cumulative execution time (ms) per second | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
Distributed Query | Execs in progress | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
Distributed Query | Execs started per second | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
DTC calls | Average execution time (ms) | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
DTC calls | Cumulative execution time (ms) per second | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
DTC calls | Execs in progress | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
DTC calls | Execs started per second | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
Extended Procedures | Average execution time (ms) | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
Extended Procedures | Cumulative execution time (ms) per second | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
Extended Procedures | Execs in progress | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
Extended Procedures | Execs started per second | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
OLEDB calls | Average execution time (ms) | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
OLEDB calls | Cumulative execution time (ms) per second | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
OLEDB calls | Execs in progress | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
OLEDB calls | Execs started per second | SQLServer:Exec Statistics;WIN8-DEV;Performance counters;0
Avg time delete FileTable item | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Avg time FileTable enumeration | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Avg time FileTable handle kill | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Avg time move FileTable item | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Avg time per file I/O request | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Avg time per file I/O response | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Avg time rename FileTable item | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Avg time to get FileTable item | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Avg time update FileTable item | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable db operations/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable enumeration reqs/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable file I/O requests/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable file I/O response/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable item delete reqs/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable item get requests/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable item move reqs/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable item rename reqs/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable item update reqs/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable kill handle ops/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
FileTable table operations/sec | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Time delete FileTable item BASE | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Time FileTable enumeration BASE | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Time FileTable handle kill BASE | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Time move FileTable item BASE | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Time per file I/O request BASE | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Time per file I/O response BASE | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Time rename FileTable item BASE | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Time to get FileTable item BASE | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Time update FileTable item BASE | SQLServer:FileTable;WIN8-DEV;Performance counters;0
Active Temp Tables | SQLServer:General Statistics;WIN8-DEV;Performance counters;2
Connection Reset/sec | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Event Notifications Delayed Drop | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
HTTP Authenticated Requests | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Logical Connections | SQLServer:General Statistics;WIN8-DEV;Performance counters;2
Logins/sec | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Logouts/sec | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Mars Deadlocks | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Non-atomic yield rate | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Processes blocked | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
SOAP Empty Requests | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
SOAP Method Invocations | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
SOAP Session Initiate Requests | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
SOAP Session Terminate Requests | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
SOAP SQL Requests | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
SOAP WSDL Requests | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
SQL Trace IO Provider Lock Waits | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Temp Tables Creation Rate | SQLServer:General Statistics;WIN8-DEV;Performance counters;1
Temp Tables For Destruction | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Tempdb recovery unit id | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Tempdb rowset id | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Trace Event Notification Queue | SQLServer:General Statistics;WIN8-DEV;Performance counters;0
Transactions | SQLServer:General Statistics;WIN8-DEV;Performance counters;1
User Connections | SQLServer:General Statistics;WIN8-DEV;Performance counters;2
Avg. Bytes/Read | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. Bytes/Read BASE | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. Bytes/Transfer | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. Bytes/Transfer BASE | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. Bytes/Write | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. Bytes/Write BASE | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. microsec/Read | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. microsec/Read BASE | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. microsec/Transfer | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. microsec/Transfer BASE | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. microsec/Write | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Avg. microsec/Write BASE | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
HTTP Storage IO retry/sec | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Outstanding HTTP Storage IO | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Read Bytes/Sec | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Reads/Sec | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Total Bytes/Sec | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Transfers/Sec | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Write Bytes/Sec | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Writes/Sec | _Total | SQLServer:HTTP Storage;WIN8-DEV;Performance counters;0
Average Latch Wait Time (ms) | SQLServer:Latches;WIN8-DEV;Performance counters;0
Average Latch Wait Time Base | SQLServer:Latches;WIN8-DEV;Performance counters;0
Latch Waits/sec | SQLServer:Latches;WIN8-DEV;Performance counters;0
Number of SuperLatches | SQLServer:Latches;WIN8-DEV;Performance counters;0
SuperLatch Demotions/sec | SQLServer:Latches;WIN8-DEV;Performance counters;0
SuperLatch Promotions/sec | SQLServer:Latches;WIN8-DEV;Performance counters;0
Total Latch Wait Time (ms) | SQLServer:Latches;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | _Total | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | AllocUnit | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | Application | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | Database | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | Extent | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | File | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | HoBT | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | Key | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | Metadata | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | Object | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | OIB | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | Page | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | RID | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time (ms) | RowGroup | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | _Total | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | AllocUnit | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | Application | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | Database | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | Extent | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | File | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | HoBT | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | Key | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | Metadata | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | Object | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | OIB | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | Page | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | RID | SQLServer:Locks;WIN8-DEV;Performance counters;0
Average Wait Time Base | RowGroup | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Requests/sec | _Total | SQLServer:Locks;WIN8-DEV;Performance counters;381
Lock Requests/sec | AllocUnit | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Requests/sec | Application | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Requests/sec | Database | SQLServer:Locks;WIN8-DEV;Performance counters;27
Lock Requests/sec | Extent | SQLServer:Locks;WIN8-DEV;Performance counters;23
Lock Requests/sec | File | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Requests/sec | HoBT | SQLServer:Locks;WIN8-DEV;Performance counters;1
Lock Requests/sec | Key | SQLServer:Locks;WIN8-DEV;Performance counters;133
Lock Requests/sec | Metadata | SQLServer:Locks;WIN8-DEV;Performance counters;71
Lock Requests/sec | Object | SQLServer:Locks;WIN8-DEV;Performance counters;93
Lock Requests/sec | OIB | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Requests/sec | Page | SQLServer:Locks;WIN8-DEV;Performance counters;25
Lock Requests/sec | RID | SQLServer:Locks;WIN8-DEV;Performance counters;8
Lock Requests/sec | RowGroup | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | _Total | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | AllocUnit | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | Application | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | Database | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | Extent | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | File | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | HoBT | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | Key | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | Metadata | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | Object | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | OIB | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | Page | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | RID | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts (timeout > 0)/sec | RowGroup | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | _Total | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | AllocUnit | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | Application | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | Database | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | Extent | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | File | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | HoBT | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | Key | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | Metadata | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | Object | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | OIB | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | Page | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | RID | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Timeouts/sec | RowGroup | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | _Total | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | AllocUnit | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | Application | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | Database | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | Extent | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | File | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | HoBT | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | Key | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | Metadata | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | Object | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | OIB | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | Page | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | RID | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Wait Time (ms) | RowGroup | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | _Total | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | AllocUnit | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | Application | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | Database | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | Extent | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | File | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | HoBT | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | Key | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | Metadata | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | Object | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | OIB | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | Page | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | RID | SQLServer:Locks;WIN8-DEV;Performance counters;0
Lock Waits/sec | RowGroup | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | _Total | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | AllocUnit | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | Application | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | Database | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | Extent | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | File | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | HoBT | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | Key | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | Metadata | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | Object | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | OIB | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | Page | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | RID | SQLServer:Locks;WIN8-DEV;Performance counters;0
Number of Deadlocks/sec | RowGroup | SQLServer:Locks;WIN8-DEV;Performance counters;0
Internal benefit | Buffer Pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;0
Internal benefit | Column store object pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;0
Memory broker clerk size | Buffer Pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;6676
Memory broker clerk size | Column store object pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;4
Periodic evictions (pages) | Buffer Pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;0
Periodic evictions (pages) | Column store object pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;0
Pressure evictions (pages/sec) | Buffer Pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;0
Pressure evictions (pages/sec) | Column store object pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;0
Simulation benefit | Buffer Pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;0
Simulation benefit | Column store object pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;0
Simulation size | Buffer Pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;0
Simulation size | Column store object pool | SQLServer:Memory Broker Clerks;WIN8-DEV;Performance counters;0
Connection Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;1192
Database Cache Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;53408
External benefit of memory | SQLServer:Memory Manager;WIN8-DEV;Performance counters;0
Free Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;6552
Granted Workspace Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;0
Lock Blocks | SQLServer:Memory Manager;WIN8-DEV;Performance counters;0
Lock Blocks Allocated | SQLServer:Memory Manager;WIN8-DEV;Performance counters;3050
Lock Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;768
Lock Owner Blocks | SQLServer:Memory Manager;WIN8-DEV;Performance counters;0
Lock Owner Blocks Allocated | SQLServer:Memory Manager;WIN8-DEV;Performance counters;5550
Log Pool Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;1296
Maximum Workspace Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;1154160
Memory Grants Outstanding | SQLServer:Memory Manager;WIN8-DEV;Performance counters;0
Memory Grants Pending | SQLServer:Memory Manager;WIN8-DEV;Performance counters;0
Optimizer Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;984
Reserved Server Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;0
SQL Cache Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;2088
Stolen Server Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;173608
Target Server Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;1536000
Total Server Memory (KB) | SQLServer:Memory Manager;WIN8-DEV;Performance counters;233568
Database Node Memory (KB) | 000 | SQLServer:Memory Node;WIN8-DEV;Performance counters;53408
Foreign Node Memory (KB) | 000 | SQLServer:Memory Node;WIN8-DEV;Performance counters;0
Free Node Memory (KB) | 000 | SQLServer:Memory Node;WIN8-DEV;Performance counters;6552
Stolen Node Memory (KB) | 000 | SQLServer:Memory Node;WIN8-DEV;Performance counters;173592
Target Node Memory (KB) | 000 | SQLServer:Memory Node;WIN8-DEV;Performance counters;1535976
Total Node Memory (KB) | 000 | SQLServer:Memory Node;WIN8-DEV;Performance counters;233552
Cache Hit Ratio | _Total | SQLServer:Plan Cache;WIN8-DEV;Performance counters;1
Cache Hit Ratio | Bound Trees | SQLServer:Plan Cache;WIN8-DEV;Performance counters;1
Cache Hit Ratio | Extended Stored Procedures | SQLServer:Plan Cache;WIN8-DEV;Performance counters;1
Cache Hit Ratio | Object Plans | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Hit Ratio | SQL Plans | SQLServer:Plan Cache;WIN8-DEV;Performance counters;1
Cache Hit Ratio | Temporary Tables & Table Variables | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Hit Ratio Base | _Total | SQLServer:Plan Cache;WIN8-DEV;Performance counters;6
Cache Hit Ratio Base | Bound Trees | SQLServer:Plan Cache;WIN8-DEV;Performance counters;6
Cache Hit Ratio Base | Extended Stored Procedures | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Hit Ratio Base | Object Plans | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Hit Ratio Base | SQL Plans | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Hit Ratio Base | Temporary Tables & Table Variables | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Object Counts | _Total | SQLServer:Plan Cache;WIN8-DEV;Performance counters;230
Cache Object Counts | Bound Trees | SQLServer:Plan Cache;WIN8-DEV;Performance counters;90
Cache Object Counts | Extended Stored Procedures | SQLServer:Plan Cache;WIN8-DEV;Performance counters;4
Cache Object Counts | Object Plans | SQLServer:Plan Cache;WIN8-DEV;Performance counters;2
Cache Object Counts | SQL Plans | SQLServer:Plan Cache;WIN8-DEV;Performance counters;134
Cache Object Counts | Temporary Tables & Table Variables | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Objects in use | _Total | SQLServer:Plan Cache;WIN8-DEV;Performance counters;1
Cache Objects in use | Bound Trees | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Objects in use | Extended Stored Procedures | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Objects in use | Object Plans | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Objects in use | SQL Plans | SQLServer:Plan Cache;WIN8-DEV;Performance counters;1
Cache Objects in use | Temporary Tables & Table Variables | SQLServer:Plan Cache;WIN8-DEV;Performance counters;0
Cache Pages | _Total | SQLServer:Plan Cache;WIN8-DEV;Performance counters;5759
Cache Pages | Bound Trees | SQLServer:Plan Cache;WIN8-DEV;Performance counters;1055
Cache Pages | Extended Stored Procedures | SQLServer:Plan Cache;WIN8-DEV;Performance counters;6
Cache Pages | Object Plans | SQLServer:Plan Cache;WIN8-DEV;Performance counters;50
Cache Pages | SQL Plans | SQLServer:Plan Cache;WIN8-DEV;Performance counters;4646
Cache Pages | Temporary Tables & Table Variables | SQLServer:Plan Cache;WIN8-DEV;Performance counters;2
Active memory grant amount (KB) | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Active memory grant amount (KB) | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Active memory grants count | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Active memory grants count | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Avg Disk Read IO (ms) | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Avg Disk Read IO (ms) | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Avg Disk Read IO (ms) Base | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Avg Disk Read IO (ms) Base | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Avg Disk Write IO (ms) | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Avg Disk Write IO (ms) | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Avg Disk Write IO (ms) Base | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Avg Disk Write IO (ms) Base | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Cache memory target (KB) | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1231200
Cache memory target (KB) | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1231200
Compile memory target (KB) | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1231200
Compile memory target (KB) | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1231200
CPU control effect % | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;7
CPU control effect % | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
CPU usage % | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
CPU usage % | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
CPU usage % base | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
CPU usage % base | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
CPU usage target % | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;7
CPU usage target % | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Read Bytes/sec | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Read Bytes/sec | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Read IO Throttled/sec | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Read IO Throttled/sec | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Read IO/sec | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Read IO/sec | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Write Bytes/sec | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Write Bytes/sec | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Write IO Throttled/sec | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Write IO Throttled/sec | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Write IO/sec | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Disk Write IO/sec | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Max memory (KB) | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1459200
Max memory (KB) | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1459200
Memory grant timeouts/sec | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Memory grant timeouts/sec | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Memory grants/sec | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1
Memory grants/sec | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Pending memory grants count | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Pending memory grants count | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;0
Query exec memory target (KB) | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1154160
Query exec memory target (KB) | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1154160
Target memory (KB) | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1459200
Target memory (KB) | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;1459200
Used memory (KB) | default | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;52624
Used memory (KB) | internal | SQLServer:Resource Pool Stats;WIN8-DEV;Performance counters;120976
Errors/sec | _Total | SQLServer:SQL Errors;WIN8-DEV;Performance counters;0
Errors/sec | DB Offline Errors | SQLServer:SQL Errors;WIN8-DEV;Performance counters;0
Errors/sec | Info Errors | SQLServer:SQL Errors;WIN8-DEV;Performance counters;0
Errors/sec | Kill Connection Errors | SQLServer:SQL Errors;WIN8-DEV;Performance counters;0
Errors/sec | User Errors | SQLServer:SQL Errors;WIN8-DEV;Performance counters;0
Auto-Param Attempts/sec | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;0
Batch Requests/sec | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;0
Failed Auto-Params/sec | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;0
Forced Parameterizations/sec | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;0
Guided plan executions/sec | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;0
Misguided plan executions/sec | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;0
Safe Auto-Params/sec | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;0
SQL Attention rate | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;0
SQL Compilations/sec | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;1
SQL Re-Compilations/sec | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;1
Unsafe Auto-Params/sec | SQLServer:SQL Statistics;WIN8-DEV;Performance counters;0
Free Space in tempdb (KB) | SQLServer:Transactions;WIN8-DEV;Performance counters;1045504
Longest Transaction Running Time | SQLServer:Transactions;WIN8-DEV;Performance counters;0
NonSnapshot Version Transactions | SQLServer:Transactions;WIN8-DEV;Performance counters;0
Snapshot Transactions | SQLServer:Transactions;WIN8-DEV;Performance counters;0
Transactions | SQLServer:Transactions;WIN8-DEV;Performance counters;14
Update conflict ratio | SQLServer:Transactions;WIN8-DEV;Performance counters;0
Update conflict ratio base | SQLServer:Transactions;WIN8-DEV;Performance counters;0
Update Snapshot Transactions | SQLServer:Transactions;WIN8-DEV;Performance counters;0
Version Cleanup rate (KB/s) | SQLServer:Transactions;WIN8-DEV;Performance counters;0
Version Generation rate (KB/s) | SQLServer:Transactions;WIN8-DEV;Performance counters;0
Version Store Size (KB) | SQLServer:Transactions;WIN8-DEV;Performance counters;0
Version Store unit count | SQLServer:Transactions;WIN8-DEV;Performance counters;2
Version Store unit creation | SQLServer:Transactions;WIN8-DEV;Performance counters;2
Version Store unit truncation | SQLServer:Transactions;WIN8-DEV;Performance counters;0
Query | User counter 1 | SQLServer:User Settable;WIN8-DEV;Performance counters;0
Query | User counter 10 | SQLServer:User Settable;WIN8-DEV;Performance counters;0
Query | User counter 2 | SQLServer:User Settable;WIN8-DEV;Performance counters;0
Query | User counter 3 | SQLServer:User Settable;WIN8-DEV;Performance counters;0
Query | User counter 4 | SQLServer:User Settable;WIN8-DEV;Performance counters;0
Query | User counter 5 | SQLServer:User Settable;WIN8-DEV;Performance counters;0
Query | User counter 6 | SQLServer:User Settable;WIN8-DEV;Performance counters;0
Query | User counter 7 | SQLServer:User Settable;WIN8-DEV;Performance counters;0
Query | User counter 8 | SQLServer:User Settable;WIN8-DEV;Performance counters;0
Query | User counter 9 | SQLServer:User Settable;WIN8-DEV;Performance counters;0
Lock waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Lock waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Lock waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Lock waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Log buffer waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Log buffer waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Log buffer waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Log buffer waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Log write waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Log write waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Log write waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Log write waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Memory grant queue waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Memory grant queue waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Memory grant queue waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Memory grant queue waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Network IO waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Network IO waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Network IO waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Network IO waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Non-Page latch waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Non-Page latch waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Non-Page latch waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Non-Page latch waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Page IO latch waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Page IO latch waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Page IO latch waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Page IO latch waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Page latch waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Page latch waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Page latch waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Page latch waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Thread-safe memory objects waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Thread-safe memory objects waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Thread-safe memory objects waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Thread-safe memory objects waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Transaction ownership waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Transaction ownership waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Transaction ownership waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Transaction ownership waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Wait for the worker | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Wait for the worker | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Wait for the worker | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Wait for the worker | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Workspace synchronization waits | Average wait time (ms) | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Workspace synchronization waits | Cumulative wait time (ms) per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Workspace synchronization waits | Waits in progress | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Workspace synchronization waits | Waits started per second | SQLServer:Wait Statistics;WIN8-DEV;Performance counters;0
Active parallel threads | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Active parallel threads | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Active requests | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;1
Active requests | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Blocked tasks | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Blocked tasks | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
CPU usage % | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
CPU usage % | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
CPU usage % base | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
CPU usage % base | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Max request cpu time (ms) | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;161
Max request cpu time (ms) | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Max request memory grant (KB) | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;9816
Max request memory grant (KB) | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Query optimizations/sec | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;1
Query optimizations/sec | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Queued requests | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Queued requests | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Reduced memory grants/sec | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Reduced memory grants/sec | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Requests completed/sec | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Requests completed/sec | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Suboptimal plans/sec | default | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Suboptimal plans/sec | internal | SQLServer:Workload Group Stats;WIN8-DEV;Performance counters;0
Cursor deletes/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Cursor inserts/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Cursor scans started/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Cursor unique violations/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Cursor updates/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Cursor write conflicts/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Dusty corner scan retries/sec (user-issued) | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Expired rows removed/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Expired rows touched/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Rows returned/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Rows touched/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Tentatively-deleted rows touched/sec | MSSQLSERVER | XTP Cursors;WIN8-DEV;Performance counters;0
Dusty corner scan retries/sec (GC-issued) | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Main GC work items/sec | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Parallel GC work item/sec | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Rows processed/sec | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Rows processed/sec (first in bucket and removed) | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Rows processed/sec (first in bucket) | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Rows processed/sec (marked for unlink) | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Rows processed/sec (no sweep needed) | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Sweep expired rows removed/sec | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Sweep expired rows touched/sec | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Sweep expiring rows touched/sec | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Sweep rows touched/sec | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Sweep scans started/sec | MSSQLSERVER | XTP Garbage Collection;WIN8-DEV;Performance counters;0
Dusty corner scan retries/sec (Phantom-issued) | MSSQLSERVER | XTP Phantom Processor;WIN8-DEV;Performance counters;0
Phantom expired rows removed/sec | MSSQLSERVER | XTP Phantom Processor;WIN8-DEV;Performance counters;0
Phantom expired rows touched/sec | MSSQLSERVER | XTP Phantom Processor;WIN8-DEV;Performance counters;0
Phantom expiring rows touched/sec | MSSQLSERVER | XTP Phantom Processor;WIN8-DEV;Performance counters;0
Phantom rows touched/sec | MSSQLSERVER | XTP Phantom Processor;WIN8-DEV;Performance counters;0
Phantom scans started/sec | MSSQLSERVER | XTP Phantom Processor;WIN8-DEV;Performance counters;0
Checkpoints Closed | MSSQLSERVER | XTP Storage;WIN8-DEV;Performance counters;0
Checkpoints Completed | MSSQLSERVER | XTP Storage;WIN8-DEV;Performance counters;0
Core Merges Completed | MSSQLSERVER | XTP Storage;WIN8-DEV;Performance counters;0
Merge Policy Evaluations | MSSQLSERVER | XTP Storage;WIN8-DEV;Performance counters;0
Merge Requests Outstanding | MSSQLSERVER | XTP Storage;WIN8-DEV;Performance counters;0
Merges Abandoned | MSSQLSERVER | XTP Storage;WIN8-DEV;Performance counters;0
Merges Installed | MSSQLSERVER | XTP Storage;WIN8-DEV;Performance counters;0
Total Files Merged | MSSQLSERVER | XTP Storage;WIN8-DEV;Performance counters;0
Log bytes written/sec | MSSQLSERVER | XTP Transaction Log;WIN8-DEV;Performance counters;0
Log records written/sec | MSSQLSERVER | XTP Transaction Log;WIN8-DEV;Performance counters;0
Cascading aborts/sec | MSSQLSERVER | XTP Transactions;WIN8-DEV;Performance counters;0
Commit dependencies taken/sec | MSSQLSERVER | XTP Transactions;WIN8-DEV;Performance counters;0
Read-only transactions prepared/sec | MSSQLSERVER | XTP Transactions;WIN8-DEV;Performance counters;0
Save point refreshes/sec | MSSQLSERVER | XTP Transactions;WIN8-DEV;Performance counters;0
Save point rollbacks/sec | MSSQLSERVER | XTP Transactions;WIN8-DEV;Performance counters;0
Save points created/sec | MSSQLSERVER | XTP Transactions;WIN8-DEV;Performance counters;0
Transaction validation failures/sec | MSSQLSERVER | XTP Transactions;WIN8-DEV;Performance counters;0
Transactions aborted by user/sec | MSSQLSERVER | XTP Transactions;WIN8-DEV;Performance counters;0
Transactions aborted/sec | MSSQLSERVER | XTP Transactions;WIN8-DEV;Performance counters;0
Transactions created/sec | MSSQLSERVER | XTP Transactions;WIN8-DEV;Performance counters;0`
