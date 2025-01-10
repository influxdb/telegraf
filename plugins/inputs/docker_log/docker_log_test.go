package docker_log

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

type mockClient struct {
	ContainerListF    func() ([]types.Container, error)
	ContainerInspectF func() (types.ContainerJSON, error)
	ContainerLogsF    func() (io.ReadCloser, error)
}

func (c *mockClient) ContainerList(context.Context, container.ListOptions) ([]types.Container, error) {
	return c.ContainerListF()
}

func (c *mockClient) ContainerInspect(context.Context, string) (types.ContainerJSON, error) {
	return c.ContainerInspectF()
}

func (c *mockClient) ContainerLogs(context.Context, string, container.LogsOptions) (io.ReadCloser, error) {
	return c.ContainerLogsF()
}

type response struct {
	io.Reader
}

func (*response) Close() error {
	return nil
}

func mustParse(layout, value string) time.Time {
	tm, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return tm
}

func Test(t *testing.T) {
	tests := []struct {
		name     string
		client   *mockClient
		expected []telegraf.Metric
	}{
		{
			name: "no containers",
			client: &mockClient{
				ContainerListF: func() ([]types.Container, error) {
					return nil, nil
				},
			},
		},
		{
			name: "one container tty",
			client: &mockClient{
				ContainerListF: func() ([]types.Container, error) {
					return []types.Container{
						{
							ID:    "deadbeef",
							Names: []string{"/telegraf"},
							Image: "influxdata/telegraf:1.11.0",
						},
					}, nil
				},
				ContainerInspectF: func() (types.ContainerJSON, error) {
					return types.ContainerJSON{
						Config: &container.Config{
							Tty: true,
						},
					}, nil
				},
				ContainerLogsF: func() (io.ReadCloser, error) {
					return &response{Reader: bytes.NewBufferString("2020-04-28T18:43:16.432691200Z hello\n")}, nil
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"docker_log",
					map[string]string{
						"container_name":    "telegraf",
						"container_image":   "influxdata/telegraf",
						"container_version": "1.11.0",
						"stream":            "tty",
						"source":            "deadbeef",
					},
					map[string]interface{}{
						"container_id": "deadbeef",
						"message":      "hello",
					},
					mustParse(time.RFC3339Nano, "2020-04-28T18:43:16.432691200Z"),
				),
			},
		},
		{
			name: "one container multiplex",
			client: &mockClient{
				ContainerListF: func() ([]types.Container, error) {
					return []types.Container{
						{
							ID:    "deadbeef",
							Names: []string{"/telegraf"},
							Image: "influxdata/telegraf:1.11.0",
						},
					}, nil
				},
				ContainerInspectF: func() (types.ContainerJSON, error) {
					return types.ContainerJSON{
						Config: &container.Config{
							Tty: false,
						},
					}, nil
				},
				ContainerLogsF: func() (io.ReadCloser, error) {
					var buf bytes.Buffer
					w := stdcopy.NewStdWriter(&buf, stdcopy.Stdout)
					_, err := w.Write([]byte("2020-04-28T18:42:16.432691200Z hello from stdout"))
					return &response{Reader: &buf}, err
				},
			},
			expected: []telegraf.Metric{
				testutil.MustMetric(
					"docker_log",
					map[string]string{
						"container_name":    "telegraf",
						"container_image":   "influxdata/telegraf",
						"container_version": "1.11.0",
						"stream":            "stdout",
						"source":            "deadbeef",
					},
					map[string]interface{}{
						"container_id": "deadbeef",
						"message":      "hello from stdout",
					},
					mustParse(time.RFC3339Nano, "2020-04-28T18:42:16.432691200Z"),
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var acc testutil.Accumulator
			plugin := &DockerLogs{
				Timeout:          config.Duration(time.Second * 5),
				newClient:        func(string, *tls.Config) (dockerClient, error) { return tt.client, nil },
				containerList:    make(map[string]context.CancelFunc),
				IncludeSourceTag: true,
			}

			err := plugin.Init()
			require.NoError(t, err)

			err = plugin.Gather(&acc)
			require.NoError(t, err)

			acc.Wait(len(tt.expected))
			plugin.Stop()

			require.Nil(t, acc.Errors) // no errors during gathering

			testutil.RequireMetricsEqual(t, tt.expected, acc.GetTelegrafMetrics())
		})
	}
}
