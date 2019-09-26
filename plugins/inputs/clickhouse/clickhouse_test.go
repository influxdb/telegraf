package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	//"github.com/stretchr/testify/require"
)

func TestClusterIncludeExcludeFilter(t *testing.T) {
	ch := ClickHouse{}
	if assert.Equal(t, "", ch.clusterIncludeExcludeFilter()) {
		ch.ClusterExclude = []string{"test_cluster"}
		if assert.Equal(t, "WHERE cluster NOT IN ('test_cluster')", ch.clusterIncludeExcludeFilter()) {
			ch.ClusterInclude = []string{"cluster"}
			if assert.Equal(t, "WHERE cluster IN ('cluster')", ch.clusterIncludeExcludeFilter()) {
				ch.ClusterExclude = []string{}
				ch.ClusterInclude = []string{"cluster1", "cluster2"}
				assert.Equal(t, "WHERE cluster IN ('cluster1', 'cluster2')", ch.clusterIncludeExcludeFilter())
			}
		}
	}
}

func TestChInt64(t *testing.T) {
	assets := map[string]int64{
		`"1"`:  1,
		"1":    1,
		"42":   42,
		`"42"`: 42,
	}
	for src, expected := range assets {
		var v chInt64
		if err := v.UnmarshalJSON([]byte(src)); assert.NoError(t, err) {
			assert.Equal(t, expected, v.toInt64())
		}
	}
}
