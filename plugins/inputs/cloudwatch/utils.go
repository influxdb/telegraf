package cloudwatch

import (
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/influxdata/telegraf/internal"
)

func dimensionsMatch(ref []*dimension, values []types.Dimension) bool {
	for _, rd := range ref {
		var found bool
		for _, vd := range values {
			if rd.Name == *vd.Name && (rd.valueMatcher == nil || rd.valueMatcher.Match(*vd.Value)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func metricMatch(cm *cloudwatchMetric, m types.Metric) bool {
	if !slices.Contains(cm.MetricNames, *m.MetricName) {
		return false
	}
	return dimensionsMatch(cm.Dimensions, m.Dimensions)
}

func sanitizeMeasurement(namespace string) string {
	namespace = strings.ReplaceAll(namespace, "/", "_")
	namespace = snakeCase(namespace)
	return "cloudwatch_" + namespace
}

func snakeCase(s string) string {
	s = internal.SnakeCase(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "__", "_")
	return s
}

func ctod(cDimensions []types.Dimension) *map[string]string {
	dimensions := make(map[string]string, len(cDimensions))
	for i := range cDimensions {
		dimensions[snakeCase(*cDimensions[i].Name)] = *cDimensions[i].Value
	}
	return &dimensions
}
