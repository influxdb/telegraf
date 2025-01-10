package multifile

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestFileTypes(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	m := MultiFile{
		BaseDir:   path.Join(wd, `testdata`),
		FailEarly: true,
		Files: []file{
			{Name: `bool.txt`, Dest: `examplebool`, Conversion: `bool`},
			{Name: `float.txt`, Dest: `examplefloat`, Conversion: `float`},
			{Name: `int.txt`, Dest: `examplefloatX`, Conversion: `float(3)`},
			{Name: `int.txt`, Dest: `exampleint`, Conversion: `int`},
			{Name: `string.txt`, Dest: `examplestring`},
			{Name: `tag.txt`, Dest: `exampletag`, Conversion: `tag`},
			{Name: `int.txt`, Conversion: `int`},
		},
	}

	var acc testutil.Accumulator

	require.NoError(t, m.Init())
	require.NoError(t, m.Gather(&acc))
	require.Equal(t, map[string]string{"exampletag": "test"}, acc.Metrics[0].Tags)
	require.Equal(t, map[string]interface{}{
		"examplebool":   true,
		"examplestring": "hello world",
		"exampleint":    int64(123456),
		"int.txt":       int64(123456),
		"examplefloat":  123.456,
		"examplefloatX": 123.456,
	}, acc.Metrics[0].Fields)
}

func failEarly(failEarly bool, t *testing.T) error {
	wd, err := os.Getwd()
	require.NoError(t, err)

	m := MultiFile{
		BaseDir:   path.Join(wd, `testdata`),
		FailEarly: failEarly,
		Files: []file{
			{Name: `int.txt`, Dest: `exampleint`, Conversion: `int`},
			{Name: `int.txt`, Dest: `exampleerror`, Conversion: `bool`},
		},
	}

	var acc testutil.Accumulator

	require.NoError(t, m.Init())
	err = m.Gather(&acc)

	if err == nil {
		require.Equal(t, map[string]interface{}{
			"exampleint": int64(123456),
		}, acc.Metrics[0].Fields)
	}

	return err
}

func TestFailEarly(t *testing.T) {
	err := failEarly(false, t)
	require.NoError(t, err)
	err = failEarly(true, t)
	require.Error(t, err)
}
