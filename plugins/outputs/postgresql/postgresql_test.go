package postgresql

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/plugins/outputs/postgresql/utils"
	"github.com/influxdata/telegraf/testutil"
)

type Log struct {
	level  pgx.LogLevel
	format string
	args   []interface{}
}

func (l Log) String() string {
	// We have to use Errorf() as Sprintf() doesn't allow usage of %w.
	return fmt.Errorf("%s: "+l.format, append([]interface{}{l.level}, l.args...)...).Error()
}

// LogAccumulator is a log collector that satisfies telegraf.Logger.
type LogAccumulator struct {
	logs      []Log
	cond      *sync.Cond
	tb        testing.TB
	emitLevel pgx.LogLevel
}

func NewLogAccumulator(tb testing.TB) *LogAccumulator {
	return &LogAccumulator{
		cond: sync.NewCond(&sync.Mutex{}),
		tb:   tb,
	}
}

func (la *LogAccumulator) Level() telegraf.LogLevel {
	switch la.emitLevel {
	case pgx.LogLevelInfo:
		return telegraf.Info
	case pgx.LogLevelWarn:
		return telegraf.Warn
	case pgx.LogLevelError:
		return telegraf.Error
	case pgx.LogLevelNone:
		return telegraf.None
	}
	return telegraf.Debug
}

// Unused
func (*LogAccumulator) AddAttribute(string, interface{}) {}

func (la *LogAccumulator) append(level pgx.LogLevel, format string, args []interface{}) {
	la.tb.Helper()

	la.cond.L.Lock()
	log := Log{level, format, args}
	la.logs = append(la.logs, log)

	if la.emitLevel == 0 || log.level <= la.emitLevel {
		la.tb.Log(log.String())
	}

	la.cond.Broadcast()
	la.cond.L.Unlock()
}

func (la *LogAccumulator) HasLevel(level pgx.LogLevel) bool {
	la.cond.L.Lock()
	defer la.cond.L.Unlock()
	for _, log := range la.logs {
		if log.level > 0 && log.level <= level {
			return true
		}
	}
	return false
}

func (la *LogAccumulator) WaitLen(n int) []Log {
	la.cond.L.Lock()
	defer la.cond.L.Unlock()
	for len(la.logs) < n {
		la.cond.Wait()
	}
	return la.logs[:]
}

// Waits for a specific query log from pgx to show up.
func (la *LogAccumulator) WaitFor(f func(l Log) bool, waitCommit bool) {
	la.cond.L.Lock()
	defer la.cond.L.Unlock()
	i := 0
	var commitPid uint32
	for {
		for ; i < len(la.logs); i++ {
			log := la.logs[i]
			if commitPid == 0 {
				if f(log) {
					if !waitCommit {
						return
					}
					commitPid = log.args[1].(MSI)["pid"].(uint32)
				}
			} else {
				if len(log.args) < 2 {
					continue
				}
				data, ok := log.args[1].(MSI)
				if !ok || data["pid"] != commitPid {
					continue
				}
				if log.args[0] == "Exec" && data["sql"] == "commit" {
					return
				} else if log.args[0] == "Exec" && data["sql"] == "rollback" {
					// transaction aborted, start looking for another match
					commitPid = 0
				} else if log.level == pgx.LogLevelError {
					commitPid = 0
				}
			}
		}
		la.cond.Wait()
	}
}

func (la *LogAccumulator) WaitForQuery(str string, waitCommit bool) {
	la.WaitFor(func(log Log) bool {
		return log.format == "PG %s - %+v" &&
			(log.args[0].(string) == "Query" || log.args[0].(string) == "Exec") &&
			strings.Contains(log.args[1].(MSI)["sql"].(string), str)
	}, waitCommit)
}

func (la *LogAccumulator) WaitForCopy(tableName string, waitCommit bool) {
	la.WaitFor(func(log Log) bool {
		return log.format == "PG %s - %+v" &&
			log.args[0].(string) == "CopyFrom" &&
			log.args[1].(MSI)["tableName"].(pgx.Identifier)[1] == tableName
	}, waitCommit)
}

// Clear any stored logs.
// Do not run this while any WaitFor* operations are in progress.
func (la *LogAccumulator) Clear() {
	la.cond.L.Lock()
	if len(la.logs) > 0 {
		la.logs = nil
	}
	la.cond.L.Unlock()
}

func (la *LogAccumulator) Logs() []Log {
	la.cond.L.Lock()
	defer la.cond.L.Unlock()
	return la.logs[:]
}

func (la *LogAccumulator) Errorf(format string, args ...interface{}) {
	la.tb.Helper()
	la.append(pgx.LogLevelError, format, args)
}

func (la *LogAccumulator) Error(args ...interface{}) {
	la.tb.Helper()
	la.append(pgx.LogLevelError, "%v", args)
}

func (la *LogAccumulator) Warnf(format string, args ...interface{}) {
	la.tb.Helper()
	la.append(pgx.LogLevelWarn, format, args)
}

func (la *LogAccumulator) Warn(args ...interface{}) {
	la.tb.Helper()
	la.append(pgx.LogLevelWarn, "%v", args)
}

func (la *LogAccumulator) Infof(format string, args ...interface{}) {
	la.tb.Helper()
	la.append(pgx.LogLevelInfo, format, args)
}

func (la *LogAccumulator) Info(args ...interface{}) {
	la.tb.Helper()
	la.append(pgx.LogLevelInfo, "%v", args)
}

func (la *LogAccumulator) Debugf(format string, args ...interface{}) {
	la.tb.Helper()
	la.append(pgx.LogLevelDebug, format, args)
}

func (la *LogAccumulator) Debug(args ...interface{}) {
	la.tb.Helper()
	la.append(pgx.LogLevelDebug, "%v", args)
}

func (la *LogAccumulator) Tracef(format string, args ...interface{}) {
	la.tb.Helper()
	la.append(pgx.LogLevelDebug, format, args)
}

func (la *LogAccumulator) Trace(args ...interface{}) {
	la.tb.Helper()
	la.append(pgx.LogLevelDebug, "%v", args)
}

var ctx = context.Background()

type PostgresqlTest struct {
	*Postgresql
	Logger *LogAccumulator
}

func newPostgresqlTest(tb testing.TB) (*PostgresqlTest, error) {
	if testing.Short() {
		tb.Skip("Skipping integration test in short mode")
	}

	servicePort := "5432"
	username := "postgres"
	password := "postgres"
	testDatabaseName := "telegraf_test"

	container := testutil.Container{
		Image:        "postgres:alpine",
		ExposedPorts: []string{servicePort},
		Env: map[string]string{
			"POSTGRES_USER":     username,
			"POSTGRES_PASSWORD": password,
			"POSTGRES_DB":       "telegraf_test",
		},
		WaitingFor: wait.ForAll(
			// the database comes up twice, once right away, then again a second
			// time after the docker entrypoint starts configuration
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort(nat.Port(servicePort)),
		),
	}
	tb.Cleanup(container.Terminate)

	if err := container.Start(); err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	p := newPostgresql()
	connection := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s",
		container.Address,
		container.Ports[servicePort],
		username,
		password,
		testDatabaseName,
	)
	p.Connection = config.NewSecret([]byte(connection))
	logger := NewLogAccumulator(tb)
	p.Logger = logger
	p.LogLevel = "debug"

	if err := p.Init(); err != nil {
		return nil, fmt.Errorf("failed to init plugin: %w", err)
	}

	pt := &PostgresqlTest{Postgresql: p}
	pt.Logger = logger

	return pt, nil
}

func TestPostgresqlConnectIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	require.NoError(t, p.Connect())
	require.EqualValues(t, 1, p.db.Stat().MaxConns())

	p, err = newPostgresqlTest(t)
	require.NoError(t, err)
	connection, err := p.Connection.Get()
	require.NoError(t, err)
	p.Connection = config.NewSecret([]byte(connection.String() + " pool_max_conns=2"))
	connection.Destroy()

	require.NoError(t, p.Init())
	require.NoError(t, p.Connect())
	require.EqualValues(t, 2, p.db.Stat().MaxConns())
}

func TestConnectionIssueAtStartup(t *testing.T) {
	// Test case for https://github.com/influxdata/telegraf/issues/14365
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	servicePort := "5432"
	username := "postgres"
	password := "postgres"
	testDatabaseName := "telegraf_test"

	container := testutil.Container{
		Image:        "postgres:alpine",
		ExposedPorts: []string{servicePort},
		Env: map[string]string{
			"POSTGRES_USER":     username,
			"POSTGRES_PASSWORD": password,
			"POSTGRES_DB":       "telegraf_test",
		},
		WaitingFor: wait.ForAll(
			// the database comes up twice, once right away, then again a second
			// time after the docker entrypoint starts configuration
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort(nat.Port(servicePort)),
		),
	}
	require.NoError(t, container.Start(), "failed to start container")
	defer container.Terminate()

	// Pause the container for connectivity issues
	require.NoError(t, container.Pause())

	// Create a model to be able to use the startup retry strategy
	dsn := config.NewSecret([]byte(fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s  connect_timeout=1",
		container.Address,
		container.Ports[servicePort],
		username,
		password,
		testDatabaseName,
	)))
	defer dsn.Destroy()
	plugin := newPostgresql()
	plugin.Connection = dsn
	plugin.Logger = testutil.Logger{}
	plugin.LogLevel = "debug"
	model := models.NewRunningOutput(
		plugin,
		&models.OutputConfig{
			Name:                 "postgres",
			StartupErrorBehavior: "retry",
		},
		1000, 1000,
	)
	require.NoError(t, model.Init())

	// The connect call should succeed even though the table creation was not
	// successful due to the "retry" strategy
	require.NoError(t, model.Connect())

	// Writing the metrics in this state should fail because we are not fully
	// started up
	metrics := testutil.MockMetrics()
	for _, m := range metrics {
		model.AddMetric(m)
	}
	require.ErrorIs(t, model.WriteBatch(), internal.ErrNotConnected)

	// Unpause the container, now writes should succeed
	require.NoError(t, container.Resume())
	require.NoError(t, model.WriteBatch())
	model.Close()
}

func newMetric(
	t *testing.T,
	suffix string,
	tags map[string]string,
	fields map[string]interface{},
) telegraf.Metric {
	return testutil.MustMetric(t.Name()+suffix, tags, fields, time.Now())
}

type MSS = map[string]string
type MSI = map[string]interface{}

func dbTableDump(t *testing.T, db *pgxpool.Pool, suffix string) []MSI {
	rows, err := db.Query(ctx, "SELECT * FROM "+pgx.Identifier{t.Name() + suffix}.Sanitize())
	require.NoError(t, err)
	defer rows.Close()

	var dump []MSI
	for rows.Next() {
		msi := MSI{}
		vals, err := rows.Values()
		require.NoError(t, err)
		for i, fd := range rows.FieldDescriptions() {
			msi[string(fd.Name)] = vals[i]
		}
		dump = append(dump, msi)
	}
	require.NoError(t, rows.Err())
	return dump
}

func TestWriteIntegration_sequential(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	require.NoError(t, p.Connect())

	metrics := []telegraf.Metric{
		newMetric(t, "_a", MSS{}, MSI{"v": 1}),
		newMetric(t, "_b", MSS{}, MSI{"v": 2}),
		newMetric(t, "_a", MSS{}, MSI{"v": 3}),
	}
	require.NoError(t, p.Write(metrics))

	dumpA := dbTableDump(t, p.db, "_a")
	dumpB := dbTableDump(t, p.db, "_b")

	require.Len(t, dumpA, 2)
	require.EqualValues(t, 1, dumpA[0]["v"])
	require.EqualValues(t, 3, dumpA[1]["v"])

	require.Len(t, dumpB, 1)
	require.EqualValues(t, 2, dumpB[0]["v"])

	p.Logger.Clear()
	require.NoError(t, p.Write(metrics))

	stmtCount := 0
	for _, log := range p.Logger.Logs() {
		if strings.Contains(log.String(), "info: PG ") {
			stmtCount++
		}
	}
	require.Equal(t, 6, stmtCount) // BEGIN, SAVEPOINT, COPY table _a, SAVEPOINT, COPY table _b, COMMIT
}

func TestWriteIntegration_concurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	p.dbConfig.MaxConns = 3
	require.NoError(t, p.Connect())

	// Write a metric so it creates a table we can lock.
	metrics := []telegraf.Metric{
		newMetric(t, "_a", MSS{}, MSI{"v": 1}),
	}
	require.NoError(t, p.Write(metrics))
	p.Logger.WaitForCopy(t.Name()+"_a", false)
	// clear so that the WaitForCopy calls below don't pick up this one
	p.Logger.Clear()

	// Lock the table so that we ensure the writes hangs and the plugin has to open another connection.
	tx, err := p.db.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx) //nolint:errcheck // ignore the returned error as we cannot do anything about it anyway
	_, err = tx.Exec(ctx, "LOCK TABLE "+utils.QuoteIdentifier(t.Name()+"_a"))
	require.NoError(t, err)

	metrics = []telegraf.Metric{
		newMetric(t, "_a", MSS{}, MSI{"v": 2}),
	}
	require.NoError(t, p.Write(metrics))

	// Note, there is technically a possible race here, where it doesn't try to insert into _a until after _b. However
	// this should be practically impossible, and trying to engineer a solution to account for it would be even more
	// complex than we already are.

	metrics = []telegraf.Metric{
		newMetric(t, "_b", MSS{}, MSI{"v": 3}),
	}
	require.NoError(t, p.Write(metrics))

	p.Logger.WaitForCopy(t.Name()+"_b", false)
	// release the lock on table _a
	require.NoError(t, tx.Rollback(ctx))
	p.Logger.WaitForCopy(t.Name()+"_a", false)

	dumpA := dbTableDump(t, p.db, "_a")
	dumpB := dbTableDump(t, p.db, "_b")

	require.Len(t, dumpA, 2)
	require.EqualValues(t, 1, dumpA[0]["v"])
	require.EqualValues(t, 2, dumpA[1]["v"])

	require.Len(t, dumpB, 1)
	require.EqualValues(t, 3, dumpB[0]["v"])

	// We should have had 3 connections. One for the lock, and one for each table.
	require.EqualValues(t, 3, p.db.Stat().TotalConns())
}

// Test that the bad metric is dropped, and the rest of the batch succeeds.
func TestWriteIntegration_sequentialPermError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	require.NoError(t, p.Connect())

	metrics := []telegraf.Metric{
		newMetric(t, "_a", MSS{}, MSI{"v": 1}),
		newMetric(t, "_b", MSS{}, MSI{"v": 2}),
	}
	require.NoError(t, p.Write(metrics))

	metrics = []telegraf.Metric{
		newMetric(t, "_a", MSS{}, MSI{"v": "a"}),
		newMetric(t, "_b", MSS{}, MSI{"v": 3}),
	}
	require.NoError(t, p.Write(metrics))

	dumpA := dbTableDump(t, p.db, "_a")
	dumpB := dbTableDump(t, p.db, "_b")
	require.Len(t, dumpA, 1)
	require.Len(t, dumpB, 2)

	haveError := false
	for _, l := range p.Logger.Logs() {
		if strings.Contains(l.String(), "write error") {
			haveError = true
			break
		}
	}
	require.True(t, haveError, "write error not found in log")
}

// Test that in a bach with only 1 sub-batch, that we don't return an error.
func TestWriteIntegration_sequentialSinglePermError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	require.NoError(t, p.Connect())

	metrics := []telegraf.Metric{
		newMetric(t, "", MSS{}, MSI{"v": 1}),
	}
	require.NoError(t, p.Write(metrics))

	metrics = []telegraf.Metric{
		newMetric(t, "", MSS{}, MSI{"v": "a"}),
	}
	require.NoError(t, p.Write(metrics))
}

// Test that the bad metric is dropped, and the rest of the batch succeeds.
func TestWriteIntegration_concurrentPermError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	p.dbConfig.MaxConns = 2
	require.NoError(t, p.Connect())

	metrics := []telegraf.Metric{
		newMetric(t, "_a", MSS{}, MSI{"v": 1}),
	}
	require.NoError(t, p.Write(metrics))
	p.Logger.WaitForCopy(t.Name()+"_a", false)

	metrics = []telegraf.Metric{
		newMetric(t, "_a", MSS{}, MSI{"v": "a"}),
		newMetric(t, "_b", MSS{}, MSI{"v": 2}),
	}
	require.NoError(t, p.Write(metrics))
	p.Logger.WaitFor(func(l Log) bool {
		return strings.Contains(l.String(), "write error")
	}, false)
	p.Logger.WaitForCopy(t.Name()+"_b", false)

	dumpA := dbTableDump(t, p.db, "_a")
	dumpB := dbTableDump(t, p.db, "_b")
	require.Len(t, dumpA, 1)
	require.Len(t, dumpB, 1)
}

// Verify that in sequential mode, errors are returned allowing telegraf agent to handle & retry
func TestWriteIntegration_sequentialTempError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	require.NoError(t, p.Connect())

	// To avoid a race condition, we need to know when our goroutine has started listening to the log.
	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		// Wait for the CREATE TABLE, and then kill the connection.
		// The WaitFor callback holds a lock on the log. Meaning it will block logging of the next action. So we trigger
		// on CREATE TABLE so that there's a few statements to go before the COMMIT.
		p.Logger.WaitFor(func(log Log) bool {
			if strings.Contains(log.String(), "release wg") {
				wg.Done()
			}

			if !strings.Contains(log.String(), "CREATE TABLE") {
				return false
			}
			pid := log.args[1].(MSI)["pid"].(uint32)

			conf := p.db.Config().ConnConfig
			conf.Logger = nil
			c, err := pgx.ConnectConfig(context.Background(), conf)
			if err != nil {
				t.Error(err)
				return true
			}
			_, err = c.Exec(context.Background(), "SELECT pg_terminate_backend($1)", pid)
			if err != nil {
				t.Error(err)
			}
			return true
		}, false)
	}()

	p.Logger.Infof("release wg")
	wg.Wait()

	metrics := []telegraf.Metric{
		newMetric(t, "_a", MSS{}, MSI{"v": 1}),
	}
	require.Error(t, p.Write(metrics))
}

// Verify that when using concurrency, errors are not returned, but instead logged and automatically retried
func TestWriteIntegration_concurrentTempError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	p.dbConfig.MaxConns = 2
	require.NoError(t, p.Connect())

	// To avoid a race condition, we need to know when our goroutine has started listening to the log.
	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		// Wait for the CREATE TABLE, and then kill the connection.
		// The WaitFor callback holds a lock on the log. Meaning it will block logging of the next action. So we trigger
		// on CREATE TABLE so that there's a few statements to go before the COMMIT.
		p.Logger.WaitFor(func(log Log) bool {
			if strings.Contains(log.String(), "release wg") {
				wg.Done()
			}

			if !strings.Contains(log.String(), "CREATE TABLE") {
				return false
			}
			pid := log.args[1].(MSI)["pid"].(uint32)

			conf := p.db.Config().ConnConfig
			conf.Logger = nil
			c, err := pgx.ConnectConfig(context.Background(), conf)
			if err != nil {
				t.Error(err)
				return true
			}
			_, err = c.Exec(context.Background(), "SELECT pg_terminate_backend($1)", pid)
			if err != nil {
				t.Error(err)
			}
			return true
		}, false)
	}()
	p.Logger.Infof("release wg")
	wg.Wait()

	metrics := []telegraf.Metric{
		newMetric(t, "_a", MSS{}, MSI{"v": 1}),
	}
	require.NoError(t, p.Write(metrics))

	p.Logger.WaitForCopy(t.Name()+"_a", false)
	dumpA := dbTableDump(t, p.db, "_a")
	require.Len(t, dumpA, 1)

	haveError := false
	for _, l := range p.Logger.Logs() {
		if strings.Contains(l.String(), "write error") {
			haveError = true
			break
		}
	}
	require.True(t, haveError, "write error not found in log")
}

func TestTimestampColumnNameIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	p.TimestampColumnName = "timestamp"
	require.NoError(t, p.Init())
	require.NoError(t, p.Connect())

	metrics := []telegraf.Metric{
		metric.New(t.Name(), map[string]string{}, map[string]interface{}{"v": 42}, time.Unix(1691747345, 0)),
	}
	require.NoError(t, p.Write(metrics))

	dump := dbTableDump(t, p.db, "")
	require.Len(t, dump, 1)
	require.EqualValues(t, 42, dump[0]["v"])
	require.EqualValues(t, time.Unix(1691747345, 0).UTC(), dump[0]["timestamp"])
	require.NotContains(t, dump[0], "time")

	p.Logger.Clear()
	require.NoError(t, p.Write(metrics))

	stmtCount := 0
	for _, log := range p.Logger.Logs() {
		if strings.Contains(log.String(), "info: PG ") {
			stmtCount++
		}
	}
	require.Equal(t, 3, stmtCount) // BEGIN, COPY metrics table, COMMIT
}

func TestWriteTagTableIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	p.TagsAsForeignKeys = true
	require.NoError(t, p.Connect())

	metrics := []telegraf.Metric{
		newMetric(t, "", MSS{"tag": "foo"}, MSI{"v": 1}),
	}
	require.NoError(t, p.Write(metrics))

	dump := dbTableDump(t, p.db, "")
	require.Len(t, dump, 1)
	require.EqualValues(t, 1, dump[0]["v"])

	dumpTags := dbTableDump(t, p.db, p.TagTableSuffix)
	require.Len(t, dumpTags, 1)
	require.EqualValues(t, dump[0]["tag_id"], dumpTags[0]["tag_id"])
	require.EqualValues(t, "foo", dumpTags[0]["tag"])

	p.Logger.Clear()
	require.NoError(t, p.Write(metrics))

	stmtCount := 0
	for _, log := range p.Logger.Logs() {
		if strings.Contains(log.String(), "info: PG ") {
			stmtCount++
		}
	}
	require.Equal(t, 3, stmtCount) // BEGIN, COPY metrics table, COMMIT
}

// Verify that when using TagsAsForeignKeys and a tag can't be written, that we still add the metrics.
func TestWriteIntegration_tagError(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	p.TagsAsForeignKeys = true
	require.NoError(t, p.Connect())

	metrics := []telegraf.Metric{
		newMetric(t, "", MSS{"tag": "foo"}, MSI{"v": 1}),
	}
	require.NoError(t, p.Write(metrics))

	// It'll have the table cached, so won't know we dropped it, will try insert, and get error.
	_, err = p.db.Exec(ctx, "DROP TABLE \""+t.Name()+"_tag\"")
	require.NoError(t, err)

	metrics = []telegraf.Metric{
		newMetric(t, "", MSS{"tag": "foo"}, MSI{"v": 2}),
	}
	require.NoError(t, p.Write(metrics))

	dump := dbTableDump(t, p.db, "")
	require.Len(t, dump, 2)
	require.EqualValues(t, 1, dump[0]["v"])
	require.EqualValues(t, 2, dump[1]["v"])
}

// Verify that when using TagsAsForeignKeys and ForeignTagConstraint and a tag can't be written, that we drop the metrics.
func TestWriteIntegration_tagError_foreignConstraint(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	p.TagsAsForeignKeys = true
	p.ForeignTagConstraint = true
	require.NoError(t, p.Connect())

	metrics := []telegraf.Metric{
		newMetric(t, "", MSS{"tag": "foo"}, MSI{"v": 1}),
	}
	require.NoError(t, p.Write(metrics))

	// It'll have the table cached, so won't know we dropped it, will try insert, and get error.
	_, err = p.db.Exec(ctx, "DROP TABLE \""+t.Name()+"_tag\"")
	require.NoError(t, err)

	metrics = []telegraf.Metric{
		newMetric(t, "", MSS{"tag": "bar"}, MSI{"v": 2}),
	}
	require.NoError(t, p.Write(metrics))
	haveError := false
	for _, l := range p.Logger.Logs() {
		if strings.Contains(l.String(), "write error") {
			haveError = true
			break
		}
	}
	require.True(t, haveError, "write error not found in log")

	dump := dbTableDump(t, p.db, "")
	require.Len(t, dump, 1)
	require.EqualValues(t, 1, dump[0]["v"])
}

func TestWriteIntegration_utf8(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	p.TagsAsForeignKeys = true
	require.NoError(t, p.Connect())

	metrics := []telegraf.Metric{
		newMetric(t, "Ѧ𝙱Ƈᗞ",
			MSS{"ăѣ𝔠ծ": "𝘈Ḇ𝖢𝕯٤ḞԍНǏ𝙅ƘԸⲘ𝙉০Ρ𝗤Ɍ𝓢ȚЦ𝒱Ѡ𝓧ƳȤ"},
			MSI{"АḂⲤ𝗗": "𝘢ƀ𝖼ḋếᵮℊ𝙝Ꭵ𝕛кιṃդⱺ𝓅𝘲𝕣𝖘ŧ𝑢ṽẉ𝘅ყž𝜡"},
		),
	}
	require.NoError(t, p.Write(metrics))

	dump := dbTableDump(t, p.db, "Ѧ𝙱Ƈᗞ")
	require.Len(t, dump, 1)
	require.EqualValues(t, "𝘢ƀ𝖼ḋếᵮℊ𝙝Ꭵ𝕛кιṃդⱺ𝓅𝘲𝕣𝖘ŧ𝑢ṽẉ𝘅ყž𝜡", dump[0]["АḂⲤ𝗗"])

	dumpTags := dbTableDump(t, p.db, "Ѧ𝙱Ƈᗞ"+p.TagTableSuffix)
	require.Len(t, dumpTags, 1)
	require.EqualValues(t, dump[0]["tag_id"], dumpTags[0]["tag_id"])
	require.EqualValues(t, "𝘈Ḇ𝖢𝕯٤ḞԍНǏ𝙅ƘԸⲘ𝙉০Ρ𝗤Ɍ𝓢ȚЦ𝒱Ѡ𝓧ƳȤ", dumpTags[0]["ăѣ𝔠ծ"])
}

func TestWriteIntegration_UnsignedIntegers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	p.Uint64Type = PgUint8
	require.NoError(t, p.Init())
	if err := p.Connect(); err != nil {
		if strings.Contains(err.Error(), "retrieving OID for uint8 data type") {
			t.Skipf("pguint extension is not installed")
			t.SkipNow()
		}
		require.NoError(t, err)
	}

	metrics := []telegraf.Metric{
		newMetric(t, "", MSS{}, MSI{"v": uint64(math.MaxUint64)}),
	}
	require.NoError(t, p.Write(metrics))

	dump := dbTableDump(t, p.db, "")

	require.Len(t, dump, 1)
	require.EqualValues(t, uint64(math.MaxUint64), dump[0]["v"])
}

// Last ditch effort to find any concurrency issues.
func TestStressConcurrencyIntegration(t *testing.T) {
	t.Skip("Skipping very long test - run locally with no timeout")

	metrics := []telegraf.Metric{
		newMetric(t, "", MSS{"foo": "bar"}, MSI{"a": 1}),
		newMetric(t, "", MSS{"pop": "tart"}, MSI{"b": 1}),
		newMetric(t, "", MSS{"foo": "bar", "pop": "tart"}, MSI{"a": 2, "b": 2}),
		newMetric(t, "_b", MSS{"foo": "bar"}, MSI{"a": 1}),
	}

	concurrency := 4
	loops := 100

	pctl, err := newPostgresqlTest(t)
	require.NoError(t, err)
	pctl.Logger.emitLevel = pgx.LogLevelWarn
	require.NoError(t, pctl.Connect())

	for i := 0; i < loops; i++ {
		var wgStart, wgDone sync.WaitGroup
		wgStart.Add(concurrency)
		wgDone.Add(concurrency)
		for j := 0; j < concurrency; j++ {
			go func() {
				mShuf := make([]telegraf.Metric, len(metrics))
				copy(mShuf, metrics)
				rand.Shuffle(len(mShuf), func(a, b int) { mShuf[a], mShuf[b] = mShuf[b], mShuf[a] })

				p, err := newPostgresqlTest(t)
				if err != nil {
					t.Error(err)
				}

				p.TagsAsForeignKeys = true
				p.Logger.emitLevel = pgx.LogLevelWarn
				p.dbConfig.MaxConns = int32(rand.Intn(3) + 1)
				if err := p.Connect(); err != nil {
					t.Error(err)
				}
				wgStart.Done()
				wgStart.Wait()

				if err := p.Write(mShuf); err != nil {
					t.Error(err)
				}
				if err := p.Close(); err != nil {
					t.Error(err)
				}
				if p.Logger.HasLevel(pgx.LogLevelWarn) {
					t.Errorf("logger mustn't have a warning level")
				}

				wgDone.Done()
			}()
		}
		wgDone.Wait()

		if t.Failed() {
			break
		}
	}
}

func TestLongColumnNamesErrorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup the plugin
	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	require.NoError(t, p.Init())
	require.NoError(t, p.Connect())

	// Define the metric to send
	metrics := []telegraf.Metric{
		metric.New(
			t.Name(),
			map[string]string{},
			map[string]interface{}{
				"a_field_with_a_some_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63": int64(0),
				"value": 42,
			},
			time.Unix(0, 0).UTC(),
		),
		metric.New(
			t.Name(),
			map[string]string{},
			map[string]interface{}{
				"a_field_with_a_some_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63": int64(1),
				"value": 43,
			},
			time.Unix(0, 1).UTC(),
		),
		metric.New(
			t.Name(),
			map[string]string{},
			map[string]interface{}{
				"a_field_with_a_some_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63": int64(2),
				"value": 44,
			},
			time.Unix(0, 2).UTC(),
		),
		metric.New(
			t.Name(),
			map[string]string{},
			map[string]interface{}{
				"a_field_with_another_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63": int64(99),
				"value": 45,
			},
			time.Unix(0, 9).UTC(),
		),
	}
	require.NoError(t, p.Write(metrics))
	require.NoError(t, p.Write(metrics))

	// Check if the logging is restricted to once per field and all columns are
	// mentioned
	var longColLogErrs []string
	for _, l := range p.Logger.logs {
		msg := l.String()
		if l.level == pgx.LogLevelError && strings.Contains(msg, "Column name too long") {
			longColLogErrs = append(longColLogErrs, strings.TrimPrefix(msg, "error: Column name too long: "))
		}
	}
	excpectedLongColumns := []string{
		`"a_field_with_a_some_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63"`,
		`"a_field_with_another_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63"`,
	}
	require.ElementsMatch(t, excpectedLongColumns, longColLogErrs)

	// Denote the expected data in the table
	expected := []map[string]interface{}{
		{"time": time.Unix(0, 0).Unix(), "value": int64(42)},
		{"time": time.Unix(0, 1).Unix(), "value": int64(43)},
		{"time": time.Unix(0, 2).Unix(), "value": int64(44)},
		{"time": time.Unix(0, 9).Unix(), "value": int64(45)},
		{"time": time.Unix(0, 0).Unix(), "value": int64(42)},
		{"time": time.Unix(0, 1).Unix(), "value": int64(43)},
		{"time": time.Unix(0, 2).Unix(), "value": int64(44)},
		{"time": time.Unix(0, 9).Unix(), "value": int64(45)},
	}

	// Get the actual table data nd convert the time to a timestamp for
	// easier comparison
	dump := dbTableDump(t, p.db, "")
	require.Len(t, dump, len(expected))
	for i, actual := range dump {
		if raw, found := actual["time"]; found {
			if t, ok := raw.(time.Time); ok {
				actual["time"] = t.Unix()
			}
		}
		require.EqualValues(t, expected[i], actual)
	}
}

func TestLongColumnNamesClipIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup the plugin
	p, err := newPostgresqlTest(t)
	require.NoError(t, err)
	p.ColumnNameLenLimit = 63
	require.NoError(t, p.Init())
	require.NoError(t, p.Connect())

	// Define the metric to send
	metrics := []telegraf.Metric{
		metric.New(
			t.Name(),
			map[string]string{},
			map[string]interface{}{
				"a_field_with_a_some_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63": int64(0),
				"value": 42,
			},
			time.Unix(0, 0).UTC(),
		),
		metric.New(
			t.Name(),
			map[string]string{},
			map[string]interface{}{
				"a_field_with_a_some_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63": int64(1),
				"value": 43,
			},
			time.Unix(0, 1).UTC(),
		),
		metric.New(
			t.Name(),
			map[string]string{},
			map[string]interface{}{
				"a_field_with_a_some_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63": int64(2),
				"value": 44,
			},
			time.Unix(0, 2).UTC(),
		),
		metric.New(
			t.Name(),
			map[string]string{},
			map[string]interface{}{
				"a_field_with_another_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63": int64(99),
				"value": 45,
			},
			time.Unix(0, 9).UTC(),
		),
	}
	require.NoError(t, p.Write(metrics))
	require.NoError(t, p.Write(metrics))

	// Check if the logging is restricted to once per field and all columns are mentioned
	var longColLogWarns []string
	var longColLogErrs []string
	for _, l := range p.Logger.logs {
		msg := l.String()
		if l.level == pgx.LogLevelWarn && strings.Contains(msg, "Limiting too long column name") {
			longColLogWarns = append(longColLogWarns, strings.TrimPrefix(msg, "warn: Limiting too long column name: "))
			continue
		}
		if l.level == pgx.LogLevelError && strings.Contains(msg, "Column name too long") {
			longColLogErrs = append(longColLogErrs, strings.TrimPrefix(msg, "error: Column name too long: "))
			continue
		}
	}

	excpectedLongColumns := []string{
		`"a_field_with_a_some_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63"`,
		`"a_field_with_another_very_long_name_exceeding_the_column_name_limit_of_postgres_of_63"`,
	}
	require.ElementsMatch(t, excpectedLongColumns, longColLogWarns)
	require.Empty(t, longColLogErrs)

	// Denote the expected data in the table
	expected := []map[string]interface{}{
		{
			"time": time.Unix(0, 0).Unix(),
			"a_field_with_a_some_very_long_name_exceeding_the_column_name_li": int64(0),
			"a_field_with_another_very_long_name_exceeding_the_column_name_l": nil,
			"value": int64(42),
		},
		{
			"time": time.Unix(0, 1).Unix(),
			"a_field_with_a_some_very_long_name_exceeding_the_column_name_li": int64(1),
			"a_field_with_another_very_long_name_exceeding_the_column_name_l": nil,
			"value": int64(43),
		},
		{
			"time": time.Unix(0, 2).Unix(),
			"a_field_with_a_some_very_long_name_exceeding_the_column_name_li": int64(2),
			"a_field_with_another_very_long_name_exceeding_the_column_name_l": nil,
			"value": int64(44),
		},
		{
			"time": time.Unix(0, 9).Unix(),
			"a_field_with_a_some_very_long_name_exceeding_the_column_name_li": nil,
			"a_field_with_another_very_long_name_exceeding_the_column_name_l": int64(99),
			"value": int64(45),
		},
		{
			"time": time.Unix(0, 0).Unix(),
			"a_field_with_a_some_very_long_name_exceeding_the_column_name_li": int64(0),
			"a_field_with_another_very_long_name_exceeding_the_column_name_l": nil,
			"value": int64(42),
		},
		{
			"time": time.Unix(0, 1).Unix(),
			"a_field_with_a_some_very_long_name_exceeding_the_column_name_li": int64(1),
			"a_field_with_another_very_long_name_exceeding_the_column_name_l": nil,
			"value": int64(43),
		},
		{
			"time": time.Unix(0, 2).Unix(),
			"a_field_with_a_some_very_long_name_exceeding_the_column_name_li": int64(2),
			"a_field_with_another_very_long_name_exceeding_the_column_name_l": nil,
			"value": int64(44),
		},
		{
			"time": time.Unix(0, 9).Unix(),
			"a_field_with_a_some_very_long_name_exceeding_the_column_name_li": nil,
			"a_field_with_another_very_long_name_exceeding_the_column_name_l": int64(99),
			"value": int64(45),
		},
	}

	// Get the actual table data nd convert the time to a timestamp for
	// easier comparison
	dump := dbTableDump(t, p.db, "")
	require.Len(t, dump, len(expected))
	for i, actual := range dump {
		if raw, found := actual["time"]; found {
			if t, ok := raw.(time.Time); ok {
				actual["time"] = t.Unix()
			}
		}
		require.EqualValues(t, expected[i], actual)
	}
}
