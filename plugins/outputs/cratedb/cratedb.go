//go:generate ../../../tools/readme_config_includer/generator
package cratedb

import (
	"context"
	"crypto/sha512"
	"database/sql"
	_ "embed"
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib" //to register stdlib from PostgreSQL Driver and Toolkit

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

const MaxInt64 = int64(^uint64(0) >> 1)

const tableCreationQuery = `
CREATE TABLE IF NOT EXISTS %s (
	"hash_id" LONG INDEX OFF,
	"timestamp" TIMESTAMP,
	"name" STRING,
	"tags" OBJECT(DYNAMIC),
	"fields" OBJECT(DYNAMIC),
	"day" TIMESTAMP GENERATED ALWAYS AS date_trunc('day', "timestamp"),
	PRIMARY KEY ("timestamp", "hash_id","day")
) PARTITIONED BY("day");
`

type CrateDB struct {
	URL          string          `toml:"url"`
	Timeout      config.Duration `toml:"timeout"`
	Table        string          `toml:"table"`
	TableCreate  bool            `toml:"table_create"`
	KeySeparator string          `toml:"key_separator"`

	db *sql.DB
}

func (*CrateDB) SampleConfig() string {
	return sampleConfig
}

func (c *CrateDB) Init() error {
	// Set defaults
	if c.KeySeparator == "" {
		c.KeySeparator = "_"
	}
	if c.Table == "" {
		c.Table = "metrics"
	}

	return nil
}

func (c *CrateDB) Connect() error {
	if c.db == nil {
		db, err := sql.Open("pgx", c.URL)
		if err != nil {
			return err
		}
		c.db = db
	}

	if c.TableCreate {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.Timeout))
		defer cancel()

		query := fmt.Sprintf(tableCreationQuery, c.Table)
		if _, err := c.db.ExecContext(ctx, query); err != nil {
			return &internal.StartupError{Err: err, Retry: true}
		}
	}

	return nil
}

func (c *CrateDB) Write(metrics []telegraf.Metric) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.Timeout))
	defer cancel()

	generatedSQL, err := insertSQL(c.Table, c.KeySeparator, metrics)
	if err != nil {
		return err
	}

	_, err = c.db.ExecContext(ctx, generatedSQL)
	if err != nil {
		return err
	}

	return nil
}

func insertSQL(table string, keyReplacement string, metrics []telegraf.Metric) (string, error) {
	rows := make([]string, 0, len(metrics))
	for _, m := range metrics {
		cols := []interface{}{
			hashID(m),
			m.Time().UTC(),
			m.Name(),
			m.Tags(),
			m.Fields(),
		}

		escapedCols := make([]string, 0, len(cols))
		for _, col := range cols {
			escaped, err := escapeValue(col, keyReplacement)
			if err != nil {
				return "", err
			}
			escapedCols = append(escapedCols, escaped)
		}
		rows = append(rows, `(`+strings.Join(escapedCols, ", ")+`)`)
	}
	query := `INSERT INTO ` + table + ` ("hash_id", "timestamp", "name", "tags", "fields")
VALUES
` + strings.Join(rows, " ,\n") + `;`
	return query, nil
}

// escapeValue returns a string version of val that is suitable for being used
// inside of a VALUES expression or similar. Unsupported types return an error.
//
// Warning: This is not ideal from a security perspective, but unfortunately
// CrateDB does not support enough of the PostgreSQL wire protocol to allow
// using pgx with $1, $2 placeholders [1]. Security conscious users of this
// plugin should probably refrain from using it in combination with untrusted
// inputs.
//
// [1] https://github.com/influxdata/telegraf/pull/3210#issuecomment-339273371
func escapeValue(val interface{}, keyReplacement string) (string, error) {
	switch t := val.(type) {
	case string:
		return escapeString(t, `'`), nil
	case int64, float64:
		return fmt.Sprint(t), nil
	case uint64:
		// The long type is the largest integer type in CrateDB and is the
		// size of a signed int64.  If our value is too large send the largest
		// possible value.
		if t <= uint64(MaxInt64) {
			return strconv.FormatInt(int64(t), 10), nil
		}
		return strconv.FormatInt(MaxInt64, 10), nil
	case bool:
		return strconv.FormatBool(t), nil
	case time.Time:
		// see https://crate.io/docs/crate/reference/sql/data_types.html#timestamp
		return escapeValue(t.Format("2006-01-02T15:04:05.999-0700"), keyReplacement)
	case map[string]string:
		return escapeObject(convertMap(t), keyReplacement)
	case map[string]interface{}:
		return escapeObject(t, keyReplacement)
	default:
		// This might be panic worthy under normal circumstances, but it's probably
		// better to not shut down the entire telegraf process because of one
		// misbehaving plugin.
		return "", fmt.Errorf("unexpected type: %T: %#v", t, t)
	}
}

// convertMap converts m from map[string]string to map[string]interface{} by
// copying it. Generics, oh generics where art thou?
func convertMap(m map[string]string) map[string]interface{} {
	c := make(map[string]interface{}, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

func escapeObject(m map[string]interface{}, keyReplacement string) (string, error) {
	// There is a decent chance that the implementation below doesn't catch all
	// edge cases, but it's hard to tell since the format seems to be a bit
	// underspecified.
	// See https://crate.io/docs/crate/reference/sql/data_types.html#object

	// We find all keys and sort them first because iterating a map in go is
	// randomized and we need consistent output for our unit tests.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Now we build our key = val pairs
	pairs := make([]string, 0, len(m))
	for _, k := range keys {
		key := escapeString(strings.ReplaceAll(k, ".", keyReplacement), `"`)

		// escape the value of the value at k (potentially recursive)
		val, err := escapeValue(m[k], keyReplacement)
		if err != nil {
			return "", err
		}

		pairs = append(pairs, key+" = "+val)
	}
	return `{` + strings.Join(pairs, ", ") + `}`, nil
}

// escapeString wraps s in the given quote string and replaces all occurrences
// of it inside of s with a double quote.
func escapeString(s string, quote string) string {
	return quote + strings.ReplaceAll(s, quote, quote+quote) + quote
}

// hashID returns a cryptographic hash int64 hash that includes the metric name
// and tags. It's used instead of m.HashID() because it's not considered stable
// and because a cryptographic hash makes more sense for the use case of
// deduplication.
// [1] https://github.com/influxdata/telegraf/pull/3210#discussion_r148411201
func hashID(m telegraf.Metric) int64 {
	h := sha512.New()
	h.Write([]byte(m.Name()))
	tags := m.Tags()
	tmp := make([]string, 0, len(tags))
	for k, v := range tags {
		tmp = append(tmp, k+v)
	}
	sort.Strings(tmp)

	for _, s := range tmp {
		h.Write([]byte(s))
	}
	sum := h.Sum(nil)

	// Note: We have to convert from uint64 to int64 below because CrateDB only
	// supports a signed 64 bit LONG type:
	//
	// CREATE TABLE my_long (val LONG);
	// INSERT INTO my_long(val) VALUES (14305102049502225714);
	// -> ERROR:  SQLParseException: For input string: "14305102049502225714"
	return int64(binary.LittleEndian.Uint64(sum))
}

func (c *CrateDB) Close() error {
	if c.db == nil {
		return nil
	}
	return c.db.Close()
}

func init() {
	outputs.Add("cratedb", func() telegraf.Output {
		return &CrateDB{
			Timeout: config.Duration(time.Second * 5),
		}
	})
}
