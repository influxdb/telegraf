//go:build !mips && !mipsle && !mips64 && !ppc64 && !riscv64 && !loong64 && !mips64le && !(windows && (386 || arm))

package sql

import (
	gosql "database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestSqlite(t *testing.T) {
	dbfile := filepath.Join(t.TempDir(), "db")
	defer os.Remove(dbfile)

	// Use the plugin to write to the database address :=
	// fmt.Sprintf("file:%v", dbfile)
	address := dbfile // accepts a path or a file: URI
	p := newSQL()
	p.Log = testutil.Logger{}
	p.Driver = "sqlite"
	p.DataSourceName = address
	require.NoError(t, p.Init())

	require.NoError(t, p.Connect())
	defer p.Close()
	require.NoError(t, p.Write(testMetrics))

	// read directly from the database
	db, err := gosql.Open("sqlite", address)
	require.NoError(t, err)
	defer db.Close()

	var countMetricOne int
	require.NoError(t, db.QueryRow("select count(*) from metric_one").Scan(&countMetricOne))
	require.Equal(t, 1, countMetricOne)

	var countMetricTwo int
	require.NoError(t, db.QueryRow("select count(*) from metric_two").Scan(&countMetricTwo))
	require.Equal(t, 1, countMetricTwo)

	var rows *gosql.Rows

	// Check that tables were created as expected
	rows, err = db.Query("select sql from sqlite_master")
	require.NoError(t, err)
	defer rows.Close()
	var sql string
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&sql))
	require.Equal(t,
		`CREATE TABLE "metric_one"("timestamp" TIMESTAMP,"tag_one" TEXT,"tag_two" TEXT,"int64_one" INT,`+
			`"int64_two" INT,"bool_one" BOOL,"bool_two" BOOL,"uint64_one" INT UNSIGNED,"float64_one" DOUBLE)`,
		sql,
	)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&sql))
	require.Equal(t,
		`CREATE TABLE "metric_two"("timestamp" TIMESTAMP,"tag_three" TEXT,"string_one" TEXT)`,
		sql,
	)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&sql))
	require.Equal(t,
		`CREATE TABLE "metric three"("timestamp" TIMESTAMP,"tag four" TEXT,"string two" TEXT)`,
		sql,
	)
	require.False(t, rows.Next())

	// sqlite stores dates as strings. They may be in the local
	// timezone. The test needs to parse them back into a time.Time to
	// check them.
	// timeLayout := "2006-01-02 15:04:05 -0700 MST"
	timeLayout := "2006-01-02T15:04:05Z"
	var actualTime time.Time

	// Check contents of tables
	rows2, err := db.Query("select timestamp, tag_one, tag_two, int64_one, int64_two from metric_one")
	require.NoError(t, err)
	defer rows2.Close()
	require.True(t, rows2.Next())
	var (
		a    string
		b, c string
		d, e int64
	)
	require.NoError(t, rows2.Scan(&a, &b, &c, &d, &e))
	actualTime, err = time.Parse(timeLayout, a)
	require.NoError(t, err)
	require.Equal(t, ts, actualTime.UTC())
	require.Equal(t, "tag1", b)
	require.Equal(t, "tag2", c)
	require.Equal(t, int64(1234), d)
	require.Equal(t, int64(2345), e)
	require.False(t, rows2.Next())

	rows3, err := db.Query("select timestamp, tag_three, string_one from metric_two")
	require.NoError(t, err)
	defer rows3.Close()
	require.True(t, rows3.Next())
	var (
		f, g, h string
	)
	require.NoError(t, rows3.Scan(&f, &g, &h))
	actualTime, err = time.Parse(timeLayout, f)
	require.NoError(t, err)
	require.Equal(t, ts, actualTime.UTC())
	require.Equal(t, "tag3", g)
	require.Equal(t, "string1", h)
	require.False(t, rows3.Next())

	rows4, err := db.Query(`select timestamp, "tag four", "string two" from "metric three"`)
	require.NoError(t, err)
	defer rows4.Close()
	require.True(t, rows4.Next())
	var (
		i, j, k string
	)
	require.NoError(t, rows4.Scan(&i, &j, &k))
	actualTime, err = time.Parse(timeLayout, i)
	require.NoError(t, err)
	require.Equal(t, ts, actualTime.UTC())
	require.Equal(t, "tag4", j)
	require.Equal(t, "string2", k)
	require.False(t, rows4.Next())
}
