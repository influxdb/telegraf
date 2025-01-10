package cloudwatch

import (
	"math"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
)

// Test that each tag becomes one dimension
func TestBuildDimensions(t *testing.T) {
	const maxDimensions = 10

	testPoint := testutil.TestMetric(1)
	dimensions := BuildDimensions(testPoint.Tags())

	tagKeys := make([]string, 0, len(testPoint.Tags()))
	for k := range testPoint.Tags() {
		tagKeys = append(tagKeys, k)
	}

	sort.Strings(tagKeys)

	if len(testPoint.Tags()) >= maxDimensions {
		require.Len(t, dimensions, maxDimensions, "Number of dimensions should be less than MaxDimensions")
	} else {
		require.Equal(t, len(testPoint.Tags()), len(dimensions), "Number of dimensions should be equal to number of tags")
	}

	for i, key := range tagKeys {
		if i >= 10 {
			break
		}
		require.Equal(t, key, *dimensions[i].Name, "Key should be equal")
		require.Equal(t, testPoint.Tags()[key], *dimensions[i].Value, "Value should be equal")
	}
}

// Test that metrics with valid values have a MetricDatum created where as non valid do not.
// Skips "time.Time" type as something is converting the value to string.
func TestBuildMetricDatums(t *testing.T) {
	zero := 0.0
	validMetrics := []telegraf.Metric{
		testutil.TestMetric(1),
		testutil.TestMetric(int32(1)),
		testutil.TestMetric(int64(1)),
		testutil.TestMetric(float64(1)),
		testutil.TestMetric(float64(0)),
		testutil.TestMetric(math.Copysign(zero, -1)), // the CW documentation does not call out -0 as rejected
		testutil.TestMetric(float64(8.515920e-109)),
		testutil.TestMetric(float64(1.174271e+108)), // largest should be 1.174271e+108
		testutil.TestMetric(true),
	}
	invalidMetrics := []telegraf.Metric{
		testutil.TestMetric("Foo"),
		testutil.TestMetric(math.Log(-1.0)),
		testutil.TestMetric(float64(8.515919e-109)), // smallest should be 8.515920e-109
		testutil.TestMetric(float64(1.174272e+108)), // largest should be 1.174271e+108
	}
	for _, point := range validMetrics {
		datums := BuildMetricDatum(false, false, point)
		require.Lenf(t, datums, 1, "Valid point should create a Datum {value: %v}", point)
	}
	for _, point := range invalidMetrics {
		datums := BuildMetricDatum(false, false, point)
		require.Emptyf(t, datums, "Valid point should not create a Datum {value: %v}", point)
	}

	statisticMetric := metric.New(
		"test1",
		map[string]string{"tag1": "value1"},
		map[string]interface{}{"value_max": float64(10), "value_min": float64(0), "value_sum": float64(100), "value_count": float64(20)},
		time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC),
	)
	datums := BuildMetricDatum(true, false, statisticMetric)
	require.Lenf(t, datums, 1, "Valid point should create a Datum {value: %v}", statisticMetric)

	multiFieldsMetric := metric.New(
		"test1",
		map[string]string{"tag1": "value1"},
		map[string]interface{}{"valueA": float64(10), "valueB": float64(0), "valueC": float64(100), "valueD": float64(20)},
		time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC),
	)
	datums = BuildMetricDatum(true, false, multiFieldsMetric)
	require.Lenf(t, datums, 4, "Each field should create a Datum {value: %v}", multiFieldsMetric)

	multiStatisticMetric := metric.New(
		"test1",
		map[string]string{"tag1": "value1"},
		map[string]interface{}{
			"valueA_max": float64(10), "valueA_min": float64(0), "valueA_sum": float64(100), "valueA_count": float64(20),
			"valueB_max": float64(10), "valueB_min": float64(0), "valueB_sum": float64(100), "valueB_count": float64(20),
			"valueC_max": float64(10), "valueC_min": float64(0), "valueC_sum": float64(100),
			"valueD": float64(10), "valueE": float64(0),
		},
		time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC),
	)
	datums = BuildMetricDatum(true, false, multiStatisticMetric)
	require.Lenf(t, datums, 7, "Valid point should create a Datum {value: %v}", multiStatisticMetric)
}

func TestMetricDatumResolution(t *testing.T) {
	const expectedStandardResolutionValue = int32(60)
	const expectedHighResolutionValue = int32(1)

	m := testutil.TestMetric(1)

	standardResolutionDatum := BuildMetricDatum(false, false, m)
	actualStandardResolutionValue := *standardResolutionDatum[0].StorageResolution
	require.Equal(t, expectedStandardResolutionValue, actualStandardResolutionValue)

	highResolutionDatum := BuildMetricDatum(false, true, m)
	actualHighResolutionValue := *highResolutionDatum[0].StorageResolution
	require.Equal(t, expectedHighResolutionValue, actualHighResolutionValue)
}

func TestBuildMetricDatums_SkipEmptyTags(t *testing.T) {
	input := testutil.MustMetric(
		"cpu",
		map[string]string{
			"host": "example.org",
			"foo":  "",
		},
		map[string]interface{}{
			"value": int64(42),
		},
		time.Unix(0, 0),
	)

	datums := BuildMetricDatum(true, false, input)
	require.Len(t, datums[0].Dimensions, 1)
}

func TestPartitionDatums(t *testing.T) {
	testDatum := types.MetricDatum{
		MetricName: aws.String("Foo"),
		Value:      aws.Float64(1),
	}

	zeroDatum := make([]types.MetricDatum, 0)
	oneDatum := []types.MetricDatum{testDatum}
	twoDatum := []types.MetricDatum{testDatum, testDatum}
	threeDatum := []types.MetricDatum{testDatum, testDatum, testDatum}

	require.Empty(t, PartitionDatums(2, zeroDatum))
	require.Equal(t, [][]types.MetricDatum{oneDatum}, PartitionDatums(2, oneDatum))
	require.Equal(t, [][]types.MetricDatum{oneDatum}, PartitionDatums(2, oneDatum))
	require.Equal(t, [][]types.MetricDatum{twoDatum}, PartitionDatums(2, twoDatum))
	require.Equal(t, [][]types.MetricDatum{twoDatum, oneDatum}, PartitionDatums(2, threeDatum))
}
