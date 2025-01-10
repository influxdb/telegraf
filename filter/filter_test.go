package filter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompile(t *testing.T) {
	f, err := Compile(nil)
	require.NoError(t, err)
	require.Nil(t, f)

	f, err = Compile([]string{"cpu"})
	require.NoError(t, err)
	require.True(t, f.Match("cpu"))
	require.False(t, f.Match("cpu0"))
	require.False(t, f.Match("mem"))

	f, err = Compile([]string{"cpu*"})
	require.NoError(t, err)
	require.True(t, f.Match("cpu"))
	require.True(t, f.Match("cpu0"))
	require.False(t, f.Match("mem"))

	f, err = Compile([]string{"cpu", "mem"})
	require.NoError(t, err)
	require.True(t, f.Match("cpu"))
	require.False(t, f.Match("cpu0"))
	require.True(t, f.Match("mem"))

	f, err = Compile([]string{"cpu", "mem", "net*"})
	require.NoError(t, err)
	require.True(t, f.Match("cpu"))
	require.False(t, f.Match("cpu0"))
	require.True(t, f.Match("mem"))
	require.True(t, f.Match("network"))

	f, err = Compile([]string{"cpu.*.count"}, '.')
	require.NoError(t, err)
	require.False(t, f.Match("cpu.count"))
	require.True(t, f.Match("cpu.measurement.count"))
	require.False(t, f.Match("cpu.field.measurement.count"))

	f, err = Compile([]string{"cpu.*.count"}, '.', ',')
	require.NoError(t, err)
	require.True(t, f.Match("cpu.measurement.count"))
	require.False(t, f.Match("cpu.,.count")) // ',' is not considered under * as it is specified as a separator
	require.False(t, f.Match("cpu.field,measurement.count"))
}

func TestIncludeExclude(t *testing.T) {
	labels := []string{"best", "com_influxdata", "timeseries", "com_influxdata_telegraf", "ever"}
	tags := make([]string, 0, len(labels))

	filter, err := NewIncludeExcludeFilter(nil, []string{"com_influx*"})
	if err != nil {
		t.Fatalf("Failed to create include/exclude filter - %v", err)
	}

	for i := range labels {
		if filter.Match(labels[i]) {
			tags = append(tags, labels[i])
		}
	}

	require.Equal(t, []string{"best", "timeseries", "ever"}, tags)
}

var benchbool bool

func BenchmarkFilterSingleNoGlobFalse(b *testing.B) {
	f, err := Compile([]string{"cpu"})
	require.NoError(b, err)
	var tmp bool
	for n := 0; n < b.N; n++ {
		tmp = f.Match("network")
	}
	benchbool = tmp
}

func BenchmarkFilterSingleNoGlobTrue(b *testing.B) {
	f, err := Compile([]string{"cpu"})
	require.NoError(b, err)
	var tmp bool
	for n := 0; n < b.N; n++ {
		tmp = f.Match("cpu")
	}
	benchbool = tmp
}

func BenchmarkFilter(b *testing.B) {
	f, err := Compile([]string{"cpu", "mem", "net*"})
	require.NoError(b, err)
	var tmp bool
	for n := 0; n < b.N; n++ {
		tmp = f.Match("network")
	}
	benchbool = tmp
}

func BenchmarkFilterNoGlob(b *testing.B) {
	f, err := Compile([]string{"cpu", "mem", "net"})
	require.NoError(b, err)
	var tmp bool
	for n := 0; n < b.N; n++ {
		tmp = f.Match("net")
	}
	benchbool = tmp
}

func BenchmarkFilter2(b *testing.B) {
	f, err := Compile([]string{"aa", "bb", "c", "ad", "ar", "at", "aq",
		"aw", "az", "axxx", "ab", "cpu", "mem", "net*"})
	require.NoError(b, err)
	var tmp bool
	for n := 0; n < b.N; n++ {
		tmp = f.Match("network")
	}
	benchbool = tmp
}

func BenchmarkFilter2NoGlob(b *testing.B) {
	f, err := Compile([]string{"aa", "bb", "c", "ad", "ar", "at", "aq",
		"aw", "az", "axxx", "ab", "cpu", "mem", "net"})
	require.NoError(b, err)
	var tmp bool
	for n := 0; n < b.N; n++ {
		tmp = f.Match("net")
	}
	benchbool = tmp
}
