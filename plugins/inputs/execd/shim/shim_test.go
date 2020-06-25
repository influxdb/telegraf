package shim

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

type concBuffer struct {
	b bytes.Buffer
	sync.RWMutex
}

func (b *concBuffer) Read(p []byte) (n int, err error) {
	b.RLock()
	defer b.RUnlock()
	return b.b.Read(p)
}
func (b *concBuffer) Write(p []byte) (n int, err error) {
	b.Lock()
	defer b.Unlock()
	return b.b.Write(p)
}
func (b *concBuffer) String() string {
	b.RLock()
	defer b.RUnlock()
	return b.b.String()
}
func (b *concBuffer) Len() int {
	b.RLock()
	defer b.RUnlock()
	return b.b.Len()
}

func TestShimWorks(t *testing.T) {
	stdoutBytes := &concBuffer{}
	stdout = stdoutBytes
	var stdinWriter *io.PipeWriter
	stdin, stdinWriter = io.Pipe() // Hold the stdin pipe open.

	metricProcessed, exited := runInputPlugin(t, 10*time.Millisecond)
	// Close everything after the finish to not interfere with the next tests.
	defer func() {
		stdinWriter.Close()
		<-exited
	}()

	<-metricProcessed
	for stdoutBytes.Len() == 0 {
		t.Log("Waiting for bytes available in stdout")
		time.Sleep(10 * time.Millisecond)
	}

	out := stdoutBytes.String()
	require.Contains(t, out, "\n")
	metricLine := strings.Split(out, "\n")[0]
	require.Equal(t, "measurement,tag=tag field=1i 1234000005678", metricLine)
}

func TestShimStdinSignalingWorks(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	stdin = stdinReader
	stdout = stdoutWriter

	metricProcessed, exited := runInputPlugin(t, 40*time.Second)

	stdinWriter.Write([]byte("\n"))

	<-metricProcessed

	r := bufio.NewReader(stdoutReader)
	out, err := r.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "measurement,tag=tag field=1i 1234000005678\n", out)

	stdinWriter.Close()

	readUntilEmpty(r)

	// check that it exits cleanly
	<-exited
}

func runInputPlugin(t *testing.T, interval time.Duration) (metricProcessed chan bool, exited chan bool) {
	metricProcessed = make(chan bool, 10)
	exited = make(chan bool)
	inp := &testInput{
		metricProcessed: metricProcessed,
	}

	shim := New()
	shim.AddInput(inp)
	go func() {
		err := shim.Run(interval)
		require.NoError(t, err)
		exited <- true
	}()
	return metricProcessed, exited
}

type testInput struct {
	metricProcessed chan bool
}

func (i *testInput) SampleConfig() string {
	return ""
}

func (i *testInput) Description() string {
	return ""
}

func (i *testInput) Gather(acc telegraf.Accumulator) error {
	acc.AddFields("measurement",
		map[string]interface{}{
			"field": 1,
		},
		map[string]string{
			"tag": "tag",
		}, time.Unix(1234, 5678))
	i.metricProcessed <- true
	return nil
}

func (i *testInput) Start(acc telegraf.Accumulator) error {
	return nil
}

func (i *testInput) Stop() {
}

func TestLoadConfig(t *testing.T) {
	os.Setenv("SECRET_TOKEN", "xxxxxxxxxx")
	os.Setenv("SECRET_VALUE", `test"\test`)

	inputs.Add("test", func() telegraf.Input {
		return &serviceInput{}
	})

	c := "./testdata/plugin.conf"
	inputs, err := LoadConfig(&c)
	require.NoError(t, err)

	inp := inputs[0].(*serviceInput)

	require.Equal(t, "awesome name", inp.ServiceName)
	require.Equal(t, "xxxxxxxxxx", inp.SecretToken)
	require.Equal(t, `test"\test`, inp.SecretValue)
}

type serviceInput struct {
	ServiceName string `toml:"service_name"`
	SecretToken string `toml:"secret_token"`
	SecretValue string `toml:"secret_value"`
}

func (i *serviceInput) SampleConfig() string {
	return ""
}

func (i *serviceInput) Description() string {
	return ""
}

func (i *serviceInput) Gather(acc telegraf.Accumulator) error {
	acc.AddFields("measurement",
		map[string]interface{}{
			"field": 1,
		},
		map[string]string{
			"tag": "tag",
		}, time.Unix(1234, 5678))

	return nil
}

func (i *serviceInput) Start(acc telegraf.Accumulator) error {
	return nil
}

func (i *serviceInput) Stop() {
}

// we can get stuck if stdout gets clogged up and nobody's reading from it.
// make sure we keep it going
func readUntilEmpty(r *bufio.Reader) {
	go func() {
		var err error
		for err != io.EOF {
			_, err = r.ReadString('\n')
			time.Sleep(10 * time.Millisecond)
		}
	}()
}
