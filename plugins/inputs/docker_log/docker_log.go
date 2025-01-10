//go:generate ../../../tools/readme_config_includer/generator
package docker_log

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/internal/docker"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	// ensure *DockerLogs implements telegraf.ServiceInput
	_               telegraf.ServiceInput = (*DockerLogs)(nil)
	containerStates                       = []string{"created", "restarting", "running", "removing", "paused", "exited", "dead"}
)

const (
	defaultEndpoint = "unix:///var/run/docker.sock"
)

type DockerLogs struct {
	Endpoint              string          `toml:"endpoint"`
	FromBeginning         bool            `toml:"from_beginning"`
	Timeout               config.Duration `toml:"timeout"`
	LabelInclude          []string        `toml:"docker_label_include"`
	LabelExclude          []string        `toml:"docker_label_exclude"`
	ContainerInclude      []string        `toml:"container_name_include"`
	ContainerExclude      []string        `toml:"container_name_exclude"`
	ContainerStateInclude []string        `toml:"container_state_include"`
	ContainerStateExclude []string        `toml:"container_state_exclude"`
	IncludeSourceTag      bool            `toml:"source_tag"`

	common_tls.ClientConfig

	newEnvClient func() (dockerClient, error)
	newClient    func(string, *tls.Config) (dockerClient, error)

	client          dockerClient
	labelFilter     filter.Filter
	containerFilter filter.Filter
	stateFilter     filter.Filter
	opts            container.ListOptions
	wg              sync.WaitGroup
	mu              sync.Mutex
	containerList   map[string]context.CancelFunc

	// State of the plugin mapping container-ID to the timestamp of the
	// last record processed
	lastRecord    map[string]time.Time
	lastRecordMtx sync.Mutex
}

func (*DockerLogs) SampleConfig() string {
	return sampleConfig
}

func (d *DockerLogs) Init() error {
	var err error
	if d.Endpoint == "ENV" {
		d.client, err = d.newEnvClient()
		if err != nil {
			return err
		}
	} else {
		tlsConfig, err := d.ClientConfig.TLSConfig()
		if err != nil {
			return err
		}
		d.client, err = d.newClient(d.Endpoint, tlsConfig)
		if err != nil {
			return err
		}
	}

	// Create filters
	err = d.createLabelFilters()
	if err != nil {
		return err
	}
	err = d.createContainerFilters()
	if err != nil {
		return err
	}
	err = d.createContainerStateFilters()
	if err != nil {
		return err
	}

	filterArgs := filters.NewArgs()
	for _, state := range containerStates {
		if d.stateFilter.Match(state) {
			filterArgs.Add("status", state)
		}
	}

	if filterArgs.Len() != 0 {
		d.opts = container.ListOptions{
			Filters: filterArgs,
		}
	}

	d.lastRecord = make(map[string]time.Time)

	return nil
}

// Start is a noop which is required for a *DockerLogs to implement the telegraf.ServiceInput interface
func (*DockerLogs) Start(telegraf.Accumulator) error {
	return nil
}

func (d *DockerLogs) GetState() interface{} {
	d.lastRecordMtx.Lock()
	recordOffsets := make(map[string]time.Time, len(d.lastRecord))
	for k, v := range d.lastRecord {
		recordOffsets[k] = v
	}
	d.lastRecordMtx.Unlock()

	return recordOffsets
}

func (d *DockerLogs) SetState(state interface{}) error {
	recordOffsets, ok := state.(map[string]time.Time)
	if !ok {
		return fmt.Errorf("state has wrong type %T", state)
	}
	d.lastRecordMtx.Lock()
	for k, v := range recordOffsets {
		d.lastRecord[k] = v
	}
	d.lastRecordMtx.Unlock()

	return nil
}

func (d *DockerLogs) Gather(acc telegraf.Accumulator) error {
	ctx := context.Background()
	acc.SetPrecision(time.Nanosecond)

	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.Timeout))
	defer cancel()
	containers, err := d.client.ContainerList(ctx, d.opts)
	if err != nil {
		return err
	}

	for _, cntnr := range containers {
		if d.containerInContainerList(cntnr.ID) {
			continue
		}

		containerName := d.matchedContainerName(cntnr.Names)
		if containerName == "" {
			continue
		}

		ctx, cancel := context.WithCancel(context.Background())
		d.addToContainerList(cntnr.ID, cancel)

		// Start a new goroutine for every new container that has logs to collect
		d.wg.Add(1)
		go func(container types.Container) {
			defer d.wg.Done()
			defer d.removeFromContainerList(container.ID)

			err = d.tailContainerLogs(ctx, acc, container, containerName)
			if err != nil && !errors.Is(err, context.Canceled) {
				acc.AddError(err)
			}
		}(cntnr)
	}
	return nil
}

func (d *DockerLogs) Stop() {
	d.cancelTails()
	d.wg.Wait()
}

func (d *DockerLogs) addToContainerList(containerID string, cancel context.CancelFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.containerList[containerID] = cancel
}

func (d *DockerLogs) removeFromContainerList(containerID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.containerList, containerID)
}

func (d *DockerLogs) containerInContainerList(containerID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.containerList[containerID]
	return ok
}

func (d *DockerLogs) cancelTails() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, cancel := range d.containerList {
		cancel()
	}
}

func (d *DockerLogs) matchedContainerName(names []string) string {
	// Check if all container names are filtered; in practice I believe
	// this array is always of length 1.
	for _, name := range names {
		trimmedName := strings.TrimPrefix(name, "/")
		if !strings.Contains(trimmedName, "/") {
			match := d.containerFilter.Match(trimmedName)
			if match {
				return trimmedName
			}
		}
	}
	return ""
}

func (d *DockerLogs) hasTTY(ctx context.Context, cntnr types.Container) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.Timeout))
	defer cancel()
	c, err := d.client.ContainerInspect(ctx, cntnr.ID)
	if err != nil {
		return false, err
	}
	return c.Config.Tty, nil
}

func (d *DockerLogs) tailContainerLogs(
	ctx context.Context,
	acc telegraf.Accumulator,
	cntnr types.Container,
	containerName string,
) error {
	imageName, imageVersion := docker.ParseImage(cntnr.Image)
	tags := map[string]string{
		"container_name":    containerName,
		"container_image":   imageName,
		"container_version": imageVersion,
	}

	if d.IncludeSourceTag {
		tags["source"] = hostnameFromID(cntnr.ID)
	}

	// Add matching container labels as tags
	for k, label := range cntnr.Labels {
		if d.labelFilter.Match(k) {
			tags[k] = label
		}
	}

	hasTTY, err := d.hasTTY(ctx, cntnr)
	if err != nil {
		return err
	}

	since := time.Time{}.Format(time.RFC3339Nano)
	if !d.FromBeginning {
		d.lastRecordMtx.Lock()
		if ts, ok := d.lastRecord[cntnr.ID]; ok {
			since = ts.Format(time.RFC3339Nano)
		}
		d.lastRecordMtx.Unlock()
	}

	logOptions := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Details:    false,
		Follow:     true,
		Since:      since,
	}

	logReader, err := d.client.ContainerLogs(ctx, cntnr.ID, logOptions)
	if err != nil {
		return err
	}

	// If the container is using a TTY, there is only a single stream
	// (stdout), and data is copied directly from the container output stream,
	// no extra multiplexing or headers.
	//
	// If the container is *not* using a TTY, streams for stdout and stderr are
	// multiplexed.
	var last time.Time
	if hasTTY {
		last, err = tailStream(acc, tags, cntnr.ID, logReader, "tty")
	} else {
		last, err = tailMultiplexed(acc, tags, cntnr.ID, logReader)
	}
	if err != nil {
		return err
	}

	if ts, ok := d.lastRecord[cntnr.ID]; !ok || ts.Before(last) {
		d.lastRecordMtx.Lock()
		d.lastRecord[cntnr.ID] = last
		d.lastRecordMtx.Unlock()
	}

	return nil
}

func parseLine(line []byte) (time.Time, string, error) {
	parts := bytes.SplitN(line, []byte(" "), 2)

	if len(parts) == 1 {
		parts = append(parts, []byte(""))
	}

	tsString := string(parts[0])

	// Keep any leading space, but remove whitespace from end of line.
	// This preserves space in, for example, stacktraces, while removing
	// annoying end of line characters and is similar to how other logging
	// plugins such as syslog behave.
	message := bytes.TrimRightFunc(parts[1], unicode.IsSpace)

	ts, err := time.Parse(time.RFC3339Nano, tsString)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("error parsing timestamp %q: %w", tsString, err)
	}

	return ts, string(message), nil
}

func tailStream(
	acc telegraf.Accumulator,
	baseTags map[string]string,
	containerID string,
	reader io.ReadCloser,
	stream string,
) (time.Time, error) {
	defer reader.Close()

	tags := make(map[string]string, len(baseTags)+1)
	for k, v := range baseTags {
		tags[k] = v
	}
	tags["stream"] = stream

	r := bufio.NewReaderSize(reader, 64*1024)

	var lastTs time.Time
	for {
		line, err := r.ReadBytes('\n')

		if len(line) != 0 {
			ts, message, err := parseLine(line)
			if err != nil {
				acc.AddError(err)
			} else {
				acc.AddFields("docker_log", map[string]interface{}{
					"container_id": containerID,
					"message":      message,
				}, tags, ts)
			}

			// Store the last processed timestamp
			if ts.After(lastTs) {
				lastTs = ts
			}
		}

		if err != nil {
			if err == io.EOF {
				return lastTs, nil
			}
			return time.Time{}, err
		}
	}
}

func tailMultiplexed(
	acc telegraf.Accumulator,
	tags map[string]string,
	containerID string,
	src io.ReadCloser,
) (time.Time, error) {
	outReader, outWriter := io.Pipe()
	errReader, errWriter := io.Pipe()

	var tsStdout, tsStderr time.Time
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var err error
		tsStdout, err = tailStream(acc, tags, containerID, outReader, "stdout")
		if err != nil {
			acc.AddError(err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		var err error
		tsStderr, err = tailStream(acc, tags, containerID, errReader, "stderr")
		if err != nil {
			acc.AddError(err)
		}
	}()

	_, err := stdcopy.StdCopy(outWriter, errWriter, src)

	// Ignore the returned errors as we cannot do anything if the closing fails
	_ = outWriter.Close()
	_ = errWriter.Close()
	_ = src.Close()
	wg.Wait()

	if err != nil {
		return time.Time{}, err
	}
	if tsStdout.After(tsStderr) {
		return tsStdout, nil
	}
	return tsStderr, nil
}

// Following few functions have been inherited from telegraf docker input plugin
func (d *DockerLogs) createContainerFilters() error {
	containerFilter, err := filter.NewIncludeExcludeFilter(d.ContainerInclude, d.ContainerExclude)
	if err != nil {
		return err
	}
	d.containerFilter = containerFilter
	return nil
}

func (d *DockerLogs) createLabelFilters() error {
	labelFilter, err := filter.NewIncludeExcludeFilter(d.LabelInclude, d.LabelExclude)
	if err != nil {
		return err
	}
	d.labelFilter = labelFilter
	return nil
}

func (d *DockerLogs) createContainerStateFilters() error {
	if len(d.ContainerStateInclude) == 0 && len(d.ContainerStateExclude) == 0 {
		d.ContainerStateInclude = []string{"running"}
	}
	stateFilter, err := filter.NewIncludeExcludeFilter(d.ContainerStateInclude, d.ContainerStateExclude)
	if err != nil {
		return err
	}
	d.stateFilter = stateFilter
	return nil
}

func hostnameFromID(id string) string {
	if len(id) > 12 {
		return id[0:12]
	}
	return id
}

func init() {
	inputs.Add("docker_log", func() telegraf.Input {
		return &DockerLogs{
			Timeout:       config.Duration(time.Second * 5),
			Endpoint:      defaultEndpoint,
			newEnvClient:  newEnvClient,
			newClient:     newClient,
			containerList: make(map[string]context.CancelFunc),
		}
	})
}
