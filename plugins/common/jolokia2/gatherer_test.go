package jolokia2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJolokia2_makeReadRequests(t *testing.T) {
	cases := []struct {
		metric   Metric
		expected []ReadRequest
	}{
		{
			metric: Metric{
				Name:  "object",
				Mbean: "test:foo=bar",
			},
			expected: []ReadRequest{
				{
					Mbean:      "test:foo=bar",
					Attributes: make([]string, 0),
				},
			},
		}, {
			metric: Metric{
				Name:  "object_with_an_attribute",
				Mbean: "test:foo=bar",
				Paths: []string{"biz"},
			},
			expected: []ReadRequest{
				{
					Mbean:      "test:foo=bar",
					Attributes: []string{"biz"},
				},
			},
		}, {
			metric: Metric{
				Name:  "object_with_attributes",
				Mbean: "test:foo=bar",
				Paths: []string{"baz", "biz"},
			},
			expected: []ReadRequest{
				{
					Mbean:      "test:foo=bar",
					Attributes: []string{"baz", "biz"},
				},
			},
		}, {
			metric: Metric{
				Name:  "object_with_an_attribute_and_path",
				Mbean: "test:foo=bar",
				Paths: []string{"biz/baz"},
			},
			expected: []ReadRequest{
				{
					Mbean:      "test:foo=bar",
					Attributes: []string{"biz"},
					Path:       "baz",
				},
			},
		}, {
			metric: Metric{
				Name:  "object_with_an_attribute_and_a_deep_path",
				Mbean: "test:foo=bar",
				Paths: []string{"biz/baz/fiz/faz"},
			},
			expected: []ReadRequest{
				{
					Mbean:      "test:foo=bar",
					Attributes: []string{"biz"},
					Path:       "baz/fiz/faz",
				},
			},
		}, {
			metric: Metric{
				Name:  "object_with_attributes_and_paths",
				Mbean: "test:foo=bar",
				Paths: []string{"baz/biz", "faz/fiz"},
			},
			expected: []ReadRequest{
				{
					Mbean:      "test:foo=bar",
					Attributes: []string{"baz"},
					Path:       "biz",
				},
				{
					Mbean:      "test:foo=bar",
					Attributes: []string{"faz"},
					Path:       "fiz",
				},
			},
		},
	}

	for _, c := range cases {
		payload := makeReadRequests([]Metric{c.metric})

		require.Equal(t, len(c.expected), len(payload), "Failing case: "+c.metric.Name)
		for _, actual := range payload {
			require.Contains(t, c.expected, actual, "Failing case: "+c.metric.Name)
		}
	}
}
