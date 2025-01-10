package hddtemp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/plugins/inputs/hddtemp/go-hddtemp"
	"github.com/influxdata/telegraf/testutil"
)

type mockFetcher struct {
}

func (*mockFetcher) Fetch(string) ([]hddtemp.Disk, error) {
	return []hddtemp.Disk{
		{
			DeviceName:  "Disk1",
			Model:       "Model1",
			Temperature: 13,
			Unit:        "C",
		},
		{
			DeviceName:  "Disk2",
			Model:       "Model2",
			Temperature: 14,
			Unit:        "C",
		},
	}, nil
}

func newMockFetcher() *mockFetcher {
	return &mockFetcher{}
}

func TestFetch(t *testing.T) {
	hddTemp := &HDDTemp{
		fetcher: newMockFetcher(),
		Address: "localhost",
		Devices: []string{"*"},
	}

	acc := &testutil.Accumulator{}
	err := hddTemp.Gather(acc)

	require.NoError(t, err)
	require.Equal(t, 2, acc.NFields())

	var tests = []struct {
		fields map[string]interface{}
		tags   map[string]string
	}{
		{
			map[string]interface{}{
				"temperature": int32(13),
			},
			map[string]string{
				"device": "Disk1",
				"model":  "Model1",
				"unit":   "C",
				"status": "",
				"source": "localhost",
			},
		},
		{
			map[string]interface{}{
				"temperature": int32(14),
			},
			map[string]string{
				"device": "Disk2",
				"model":  "Model2",
				"unit":   "C",
				"status": "",
				"source": "localhost",
			},
		},
	}

	for _, test := range tests {
		acc.AssertContainsTaggedFields(t, "hddtemp", test.fields, test.tags)
	}
}
