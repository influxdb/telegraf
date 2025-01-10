package postgresql_extensible

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/common/postgresql"
	"github.com/influxdata/telegraf/testutil"
)

func queryRunner(t *testing.T, q []query) *testutil.Accumulator {
	servicePort := "5432"
	container := testutil.Container{
		Image:        "postgres:alpine",
		ExposedPorts: []string{servicePort},
		Env: map[string]string{
			"POSTGRES_HOST_AUTH_METHOD": "trust",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort(nat.Port(servicePort)),
		),
	}

	require.NoError(t, container.Start(), "failed to start container")
	defer container.Terminate()

	addr := fmt.Sprintf(
		"host=%s port=%s user=postgres sslmode=disable",
		container.Address,
		container.Ports[servicePort],
	)

	p := &Postgresql{
		Log: testutil.Logger{},
		Config: postgresql.Config{
			Address:     config.NewSecret([]byte(addr)),
			IsPgBouncer: false,
		},
		Databases: []string{"postgres"},
		Query:     q,
	}
	require.NoError(t, p.Init())

	var acc testutil.Accumulator
	require.NoError(t, p.Start(&acc))
	defer p.Stop()
	require.NoError(t, acc.GatherError(p.Gather))

	return &acc
}

func TestPostgresqlGeneratesMetricsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	acc := queryRunner(t, []query{{
		Sqlquery:   "select * from pg_stat_database",
		MinVersion: 901,
		Withdbname: false,
		Tagvalue:   "",
	}})
	testutil.PrintMetrics(acc.GetTelegrafMetrics())

	intMetrics := []string{
		"xact_commit",
		"xact_rollback",
		"blks_read",
		"blks_hit",
		"tup_returned",
		"tup_fetched",
		"tup_inserted",
		"tup_updated",
		"tup_deleted",
		"conflicts",
		"temp_files",
		"temp_bytes",
		"deadlocks",
		"numbackends",
		"datid",
	}

	var int32Metrics []string

	floatMetrics := []string{
		"blk_read_time",
		"blk_write_time",
	}

	stringMetrics := []string{
		"datname",
	}

	metricsCounted := 0

	for _, metric := range intMetrics {
		require.True(t, acc.HasInt64Field("postgresql", metric))
		metricsCounted++
	}

	for _, metric := range int32Metrics {
		require.True(t, acc.HasInt32Field("postgresql", metric))
		metricsCounted++
	}

	for _, metric := range floatMetrics {
		require.True(t, acc.HasFloatField("postgresql", metric))
		metricsCounted++
	}

	for _, metric := range stringMetrics {
		require.True(t, acc.HasStringField("postgresql", metric))
		metricsCounted++
	}

	require.Positive(t, metricsCounted)
	require.Equal(t, len(floatMetrics)+len(intMetrics)+len(int32Metrics)+len(stringMetrics), metricsCounted)
}

func TestPostgresqlQueryOutputTestsIntegration(t *testing.T) {
	const measurement = "postgresql"

	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	examples := map[string]func(*testutil.Accumulator){
		"SELECT 10.0::float AS myvalue": func(acc *testutil.Accumulator) {
			v, found := acc.FloatField(measurement, "myvalue")
			require.True(t, found)
			require.InDelta(t, 10.0, v, testutil.DefaultDelta)
		},
		"SELECT 10.0 AS myvalue": func(acc *testutil.Accumulator) {
			v, found := acc.StringField(measurement, "myvalue")
			require.True(t, found)
			require.Equal(t, "10.0", v)
		},
		"SELECT 'hello world' AS myvalue": func(acc *testutil.Accumulator) {
			v, found := acc.StringField(measurement, "myvalue")
			require.True(t, found)
			require.Equal(t, "hello world", v)
		},
		"SELECT true AS myvalue": func(acc *testutil.Accumulator) {
			v, found := acc.BoolField(measurement, "myvalue")
			require.True(t, found)
			require.True(t, v)
		},
		"SELECT timestamp'1980-07-23' as ts, true AS myvalue": func(acc *testutil.Accumulator) {
			expectedTime := time.Date(1980, 7, 23, 0, 0, 0, 0, time.UTC)
			v, found := acc.BoolField(measurement, "myvalue")
			require.True(t, found)
			require.True(t, v)
			require.True(t, acc.HasTimestamp(measurement, expectedTime))
		},
	}

	for q, assertions := range examples {
		acc := queryRunner(t, []query{{
			Sqlquery:   q,
			MinVersion: 901,
			Withdbname: false,
			Tagvalue:   "",
			Timestamp:  "ts",
		}})
		assertions(acc)
	}
}

func TestPostgresqlFieldOutputIntegration(t *testing.T) {
	const measurement = "postgresql"
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	acc := queryRunner(t, []query{{
		Sqlquery:   "select * from pg_stat_database",
		MinVersion: 901,
		Withdbname: false,
		Tagvalue:   "",
	}})

	intMetrics := []string{
		"xact_commit",
		"xact_rollback",
		"blks_read",
		"blks_hit",
		"tup_returned",
		"tup_fetched",
		"tup_inserted",
		"tup_updated",
		"tup_deleted",
		"conflicts",
		"temp_files",
		"temp_bytes",
		"deadlocks",
		"numbackends",
		"datid",
	}

	var int32Metrics []string

	floatMetrics := []string{
		"blk_read_time",
		"blk_write_time",
	}

	stringMetrics := []string{
		"datname",
	}

	for _, field := range intMetrics {
		_, found := acc.Int64Field(measurement, field)
		require.Truef(t, found, "expected %s to be an integer", field)
	}

	for _, field := range int32Metrics {
		_, found := acc.Int32Field(measurement, field)
		require.Truef(t, found, "expected %s to be an int32", field)
	}

	for _, field := range floatMetrics {
		_, found := acc.FloatField(measurement, field)
		require.Truef(t, found, "expected %s to be a float64", field)
	}

	for _, field := range stringMetrics {
		_, found := acc.StringField(measurement, field)
		require.Truef(t, found, "expected %s to be a str", field)
	}
}

func TestPostgresqlSqlScript(t *testing.T) {
	q := []query{{
		Script:     "testdata/test.sql",
		MinVersion: 901,
		Withdbname: false,
		Tagvalue:   "",
	}}

	addr := fmt.Sprintf(
		"host=%s user=postgres sslmode=disable",
		testutil.GetLocalHost(),
	)

	p := &Postgresql{
		Log: testutil.Logger{},
		Config: postgresql.Config{
			Address:     config.NewSecret([]byte(addr)),
			IsPgBouncer: false,
		},
		Databases: []string{"postgres"},
		Query:     q,
	}
	require.NoError(t, p.Init())

	var acc testutil.Accumulator
	require.NoError(t, p.Start(&acc))
	defer p.Stop()
	require.NoError(t, acc.GatherError(p.Gather))
}

func TestPostgresqlIgnoresUnwantedColumnsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	addr := fmt.Sprintf(
		"host=%s user=postgres sslmode=disable",
		testutil.GetLocalHost(),
	)

	p := &Postgresql{
		Log: testutil.Logger{},
		Config: postgresql.Config{
			Address: config.NewSecret([]byte(addr)),
		},
	}
	require.NoError(t, p.Init())

	var acc testutil.Accumulator
	require.NoError(t, p.Start(&acc))
	defer p.Stop()
	require.NoError(t, acc.GatherError(p.Gather))

	require.NotEmpty(t, ignoredColumns)
	for col := range ignoredColumns {
		require.False(t, acc.HasMeasurement(col))
	}
}

func TestAccRow(t *testing.T) {
	p := Postgresql{
		Log: testutil.Logger{},
		Config: postgresql.Config{
			Address:       config.NewSecret(nil),
			OutputAddress: "server",
		},
	}
	require.NoError(t, p.Init())

	var acc testutil.Accumulator
	columns := []string{"datname", "cat"}

	tests := []struct {
		fields fakeRow
		dbName string
		server string
	}{
		{
			fields: fakeRow{
				fields: []interface{}{1, "gato"},
			},
			dbName: "postgres",
			server: "server",
		},
		{
			fields: fakeRow{
				fields: []interface{}{nil, "gato"},
			},
			dbName: "postgres",
			server: "server",
		},
		{
			fields: fakeRow{
				fields: []interface{}{"name", "gato"},
			},
			dbName: "name",
			server: "server",
		},
	}
	for _, tt := range tests {
		q := query{Measurement: "pgTEST", additionalTags: make(map[string]bool)}
		require.NoError(t, p.accRow(&acc, tt.fields, columns, q, time.Now()))
		require.Len(t, acc.Metrics, 1)
		metric := acc.Metrics[0]
		require.Equal(t, tt.dbName, metric.Tags["db"])
		require.Equal(t, tt.server, metric.Tags["server"])
		acc.ClearMetrics()
	}
}

type fakeRow struct {
	fields []interface{}
}

func (f fakeRow) Scan(dest ...interface{}) error {
	if len(f.fields) != len(dest) {
		return errors.New("nada matchy buddy")
	}

	for i, d := range dest {
		switch d := d.(type) {
		case *interface{}:
			*d = f.fields[i]
		default:
			return fmt.Errorf("bad type %T", d)
		}
	}
	return nil
}
