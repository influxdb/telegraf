package sql

import (
	"context"
	gosql "database/sql"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestSqlQuote(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
}

func TestSqlCreateStatement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
}

func TestSqlInsertStatement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
}

func pwgen(n int) string {
	charset := []byte("abcdedfghijklmnopqrstABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	nchars := len(charset)
	buffer := make([]byte, n)

	for i := range buffer {
		buffer[i] = charset[rand.Intn(nchars)]
	}

	return string(buffer)
}

func stableMetric(
	name string,
	tags []telegraf.Tag,
	fields []telegraf.Field,
	tm time.Time,
	tp ...telegraf.ValueType,
) telegraf.Metric {
	// We want to compare the output of this plugin with expected
	// output. Maps don't preserve order so comparison fails. There's
	// no metric constructor that takes a slice of tag and slice of
	// field, just the one that takes maps.
	//
	// To preserve order, construct the metric without tags and fields
	// and then add them using AddTag and AddField.  Those are stable.
	m := metric.New(name, map[string]string{}, map[string]interface{}{}, tm, tp...)
	for _, tag := range tags {
		m.AddTag(tag.Key, tag.Value)
	}
	for _, field := range fields {
		m.AddField(field.Key, field.Value)
	}
	return m
}

var (
	// 2021-05-17T22:04:45+00:00
	// or 2021-05-17T16:04:45-06:00
	ts = time.Unix(1621289085, 0)

	testMetrics = []telegraf.Metric{
		stableMetric(
			"metric_one",
			[]telegraf.Tag{
				{
					Key:   "tag_one",
					Value: "tag1",
				},
				{
					Key:   "tag_two",
					Value: "tag2",
				},
			},
			[]telegraf.Field{
				{
					Key:   "int64_one",
					Value: int64(1234),
				},
				{
					Key:   "int64_two",
					Value: int64(2345),
				},
			},
			ts,
		),
		stableMetric(
			"metric_two",
			[]telegraf.Tag{
				{
					Key:   "tag_three",
					Value: "tag3",
				},
			},
			[]telegraf.Field{
				{
					Key:   "string_one",
					Value: "string1",
				},
			},
			ts,
		),
	}
)

func TestMysqlIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	initdb, err := filepath.Abs("testdata/mariadb/initdb")
	require.NoError(t, err)

	// initdb/script.sql creates this database
	const dbname = "foo"

	// The mariadb image lets you set the root password through an env
	// var. We'll use root to insert and query test data.
	const username = "root"

	password := pwgen(32)
	outDir, err := ioutil.TempDir("", "tg-mysql-*")
	require.NoError(t, err)
	defer os.RemoveAll(outDir)

	ctx := context.Background()
	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "mariadb",
			Env: map[string]string{
				"MARIADB_ROOT_PASSWORD": password,
			},
			BindMounts: map[string]string{
				initdb: "/docker-entrypoint-initdb.d",
				outDir: "/out",
			},
			ExposedPorts: []string{"3306/tcp"},
			WaitingFor:   wait.ForListeningPort("3306/tcp"),
		},
		Started: true,
	}
	mariadbContainer, err := testcontainers.GenericContainer(ctx, req)
	require.NoError(t, err, "starting container failed")
	defer func() {
		require.NoError(t, mariadbContainer.Terminate(ctx), "terminating container failed")
	}()

	// Get the connection details from the container
	host, err := mariadbContainer.Host(ctx)
	require.NoError(t, err, "getting container host address failed")
	require.NotEmpty(t, host)
	natPort, err := mariadbContainer.MappedPort(ctx, "3306/tcp")
	require.NoError(t, err, "getting container host port failed")
	port := natPort.Port()
	require.NotEmpty(t, port)

	//use the plugin to write to the database
	address := fmt.Sprintf("%v:%v@tcp(%v:%v)/%v",
		username, password, host, port, dbname,
	)
	p := newSQL()
	p.Log = testutil.Logger{}
	p.Driver = "mysql"
	p.Address = address
	p.Convert.Timestamp = "TEXT" //disable mysql default current_timestamp()

	require.NoError(t, p.Connect())
	require.NoError(t, p.Write(
		testMetrics,
	))

	//dump the database
	var rc int
	rc, err = mariadbContainer.Exec(ctx, []string{
		"bash",
		"-c",
		"mariadb-dump --user=" + username +
			" --password=" + password +
			" --compact --skip-opt " +
			dbname +
			" > /out/dump",
	})
	require.NoError(t, err)
	require.Equal(t, 0, rc)
	dumpfile := filepath.Join(outDir, "dump")
	require.FileExists(t, dumpfile)

	//compare the dump to what we expected
	expected, err := ioutil.ReadFile("testdata/mariadb/expected.sql")
	require.NoError(t, err)
	actual, err := ioutil.ReadFile(dumpfile)
	require.NoError(t, err)
	require.Equal(t, string(expected), string(actual))
}

func TestPostgresIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	initdb, err := filepath.Abs("testdata/postgres/initdb")
	require.NoError(t, err)

	// initdb/init.sql creates this database
	const dbname = "foo"

	// default username for postgres is postgres
	const username = "postgres"

	password := pwgen(32)
	outDir, err := ioutil.TempDir("", "tg-postgres-*")
	require.NoError(t, err)
	defer os.RemoveAll(outDir)

	ctx := context.Background()
	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "postgres",
			Env: map[string]string{
				"POSTGRES_PASSWORD": password,
			},
			BindMounts: map[string]string{
				initdb: "/docker-entrypoint-initdb.d",
				outDir: "/out",
			},
			ExposedPorts: []string{"5432/tcp"},
			WaitingFor:   wait.ForListeningPort("5432/tcp"),
		},
		Started: true,
	}
	cont, err := testcontainers.GenericContainer(ctx, req)
	require.NoError(t, err, "starting container failed")
	defer func() {
		require.NoError(t, cont.Terminate(ctx), "terminating container failed")
	}()

	// Get the connection details from the container
	host, err := cont.Host(ctx)
	require.NoError(t, err, "getting container host address failed")
	require.NotEmpty(t, host)
	natPort, err := cont.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err, "getting container host port failed")
	port := natPort.Port()
	require.NotEmpty(t, port)

	//use the plugin to write to the database
	// host, port, username, password, dbname
	address := fmt.Sprintf("postgres://%v:%v@%v:%v/%v",
		username, password, host, port, dbname,
	)
	p := newSQL()
	p.Log = testutil.Logger{}
	p.Driver = "pgx"
	p.Address = address
	//p.Convert.Timestamp = "TEXT" //disable mysql default current_timestamp()

	require.NoError(t, p.Connect())
	require.NoError(t, p.Write(
		testMetrics,
	))

	//dump the database
	//psql -u postgres
	var rc int
	rc, err = cont.Exec(ctx, []string{
		"bash",
		"-c",
		"pg_dump" +
			" --username=" + username +
			//" --password=" + password +
			//			" --compact --skip-opt " +
			" --no-comments" +
			//" --data-only" +
			" " + dbname +
			// pg_dump's output has comments that include build info
			// of postgres and pg_dump. The build info changes with
			// each release. To prevent these changes from causing the
			// test to fail, we strip out comments. Also strip out
			// blank lines.
			"|grep -E -v '(^--|^$)'" +
			" > /out/dump 2>&1",
	})
	require.NoError(t, err)
	require.Equal(t, 0, rc)
	dumpfile := filepath.Join(outDir, "dump")
	require.FileExists(t, dumpfile)

	//compare the dump to what we expected
	expected, err := ioutil.ReadFile("testdata/postgres/expected.sql")
	require.NoError(t, err)
	actual, err := ioutil.ReadFile(dumpfile)
	require.NoError(t, err)
	require.Equal(t, string(expected), string(actual))
}

func TestSqlite(t *testing.T) {
	//initdb, err := filepath.Abs("testdata/sqlite/initdb")
	//require.NoError(t, err)

	outDir, err := ioutil.TempDir("", "tg-sqlite-*")
	require.NoError(t, err)
	//defer os.RemoveAll(outDir)

	dbfile := filepath.Join(outDir, "db")

	//use the plugin to write to the database
	//address := fmt.Sprintf("file:%v", dbfile)
	address := dbfile // accepts a path or a file: URI
	p := newSQL()
	p.Log = testutil.Logger{}
	p.Driver = "sqlite"
	p.Address = address
	//p.Convert.Timestamp = "TEXT" //disable mysql default current_timestamp()

	require.NoError(t, p.Connect())
	require.NoError(t, p.Write(
		testMetrics,
	))

	//read directly from the database
	db, err := gosql.Open("sqlite", address)
	require.NoError(t, err)
	defer db.Close()

	var countMetricOne int
	require.NoError(t, db.QueryRow("select count(*) from metric_one").Scan(&countMetricOne))
	require.Equal(t, 1, countMetricOne)

	var countMetricTwo int
	require.NoError(t, db.QueryRow("select count(*) from metric_one").Scan(&countMetricTwo))
	require.Equal(t, 1, countMetricTwo)

	var rows *gosql.Rows

	// Check that tables were created as expected
	rows, err = db.Query("select sql from sqlite_master")
	require.NoError(t, err)
	var sql string
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&sql))
	require.Equal(t,
		"CREATE TABLE metric_one(timestamp TIMESTAMP,tag_one TEXT,tag_two TEXT,int64_one INT,int64_two INT)",
		sql,
	)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&sql))
	require.Equal(t,
		"CREATE TABLE metric_two(timestamp TIMESTAMP,tag_three TEXT,string_one TEXT)",
		sql,
	)
	require.False(t, rows.Next())
	require.NoError(t, rows.Close()) //nolint:sqlclosecheck

	// sqlite stores dates as strings. They may be in the local
	// timezone. The test needs to parse them back into a time.Time to
	// check them.
	timeLayout := "2006-01-02 15:04:05 -0700 MST"
	var actualTime time.Time

	// Check contents of table metric_one
	rows, err = db.Query("select timestamp, tag_one, tag_two, int64_one, int64_two from metric_one")
	require.NoError(t, err)
	require.True(t, rows.Next())
	var (
		a    string
		b, c string
		d, e int64
	)
	require.NoError(t, rows.Scan(&a, &b, &c, &d, &e))
	actualTime, err = time.Parse(timeLayout, a)
	require.NoError(t, err)
	require.Equal(t, ts, actualTime)
	require.Equal(t, "tag1", b)
	require.Equal(t, "tag2", c)
	require.Equal(t, int64(1234), d)
	require.Equal(t, int64(2345), e)
	require.False(t, rows.Next())
	require.NoError(t, rows.Close()) //nolint:sqlclosecheck

	// Check contents of table metric_one
	rows, err = db.Query("select timestamp, tag_three, string_one from metric_two")
	require.NoError(t, err)
	require.True(t, rows.Next())
	var (
		f, g, h string
	)
	require.NoError(t, rows.Scan(&f, &g, &h))
	actualTime, err = time.Parse(timeLayout, f)
	require.NoError(t, err)
	require.Equal(t, ts, actualTime)
	require.Equal(t, "tag3", g)
	require.Equal(t, "string1", h)
	require.False(t, rows.Next())
	require.NoError(t, rows.Close()) //nolint:sqlclosecheck
}
