package logzio

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/outputs"
	lg "github.com/logzio/logzio-go"
)

const (
	defaultLogzioCheckDiskSpace = true
	defaultLogzioDiskThreshold  = 98 // represent % of the disk
	defaultLogzioDrainDuration  = "3s"
	defaultLogzioURL            = "https://listener.logz.io:8071"

	minDiskThreshold = 0
	maxDiskThreshold = 100

	logzioDescription = "Send aggregate metrics to Logz.io"
	logzioType        = "telegraf"
)

var sampleConfig = `
  ## Set to true if Logz.io sender checks the disk space before adding metrics to the disk queue.
  # check_disk_space = true

  ## The percent of used file system space at which the sender will stop queueing. 
  ## When we will reach that percentage, the file system in which the queue is stored will drop 
  ## all new logs until the percentage of used space drops below that threshold.
  # disk_threshold = 98

  ## How often Logz.io sender should drain the queue.
  ## Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
  # drain_duration = "3s"

  ## Where Logz.io sender should store the queue
  ## queue_dir = Sprintf("%s%s%s%s%d", os.TempDir(), string(os.PathSeparator),
  ##                     "logzio-buffer", string(os.PathSeparator), time.Now().UnixNano())

  ## Logz.io account token
  token = "your logz.io token" # required

  ## Use your listener URL for your Logz.io account region.
  # url = "https://listener.logz.io:8071"
`

type Logzio struct {
	CheckDiskSpace bool            `toml:"check_disk_space"`
	DiskThreshold  int             `toml:"disk_threshold"`
	DrainDuration  string          `toml:"drain_duration"`
	Log            telegraf.Logger `toml:"-"`
	QueueDir       string          `toml:"queue_dir"`
	Token          string          `toml:"token"`
	URL            string          `toml:"url"`

	sender *lg.LogzioSender
}

type Metric struct {
	Metric     map[string]interface{} `json:"metrics"`
	Dimensions map[string]string      `json:"dimensions"`
	Time       time.Time              `json:"@timestamp"`
	Type       string                 `json:"type"`
}

func (l *Logzio) initializeSender() error {
	if l.Token == "" || l.Token == "your logz.io token" {
		return fmt.Errorf("token is required")
	}

	drainDuration, err := time.ParseDuration(l.DrainDuration)
	if err != nil {
		return fmt.Errorf("failed to parse drain_duration: %s", err)
	}

	diskThreshold := l.DiskThreshold
	if diskThreshold < minDiskThreshold || diskThreshold > maxDiskThreshold {
		return fmt.Errorf("threshold has to be between %d and %d", minDiskThreshold, maxDiskThreshold)
	}

	l.sender, err = lg.New(
		l.Token,
		lg.SetCheckDiskSpace(l.CheckDiskSpace),
		lg.SetDrainDiskThreshold(l.DiskThreshold),
		lg.SetDrainDuration(drainDuration),
		lg.SetTempDirectory(l.QueueDir),
		lg.SetUrl(l.URL),
	)

	if err != nil {
		return fmt.Errorf("failed to create new logzio sender: %v", err)
	}

	l.Log.Infof("Successfuly created Logz.io sender: %s %s %s %d", l.URL, l.QueueDir,
		l.DrainDuration, l.DiskThreshold)
	return nil
}

// Connect to the Output
func (l *Logzio) Connect() error {
	l.Log.Debug("Connecting to logz.io output...")
	return l.initializeSender()
}

// Close any connections to the Output
func (l *Logzio) Close() error {
	l.Log.Debug("Closing logz.io output")
	l.sender.Stop()
	return nil
}

// Description returns a one-sentence description on the Output
func (l *Logzio) Description() string {
	return logzioDescription
}

// SampleConfig returns the default configuration of the Output
func (l *Logzio) SampleConfig() string {
	return sampleConfig
}

// Write takes in group of points to be written to the Output
func (l *Logzio) Write(metrics []telegraf.Metric) error {
	if len(metrics) == 0 {
		return nil
	}

	l.Log.Debugf("Recived %d metrics", len(metrics))
	for _, metric := range metrics {
		m := l.parseMetric(metric)

		serialized, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("Failed to marshal: %+v\n", m)
		}

		err = l.sender.Send(serialized)
		if err != nil {
			return fmt.Errorf("Failed to send metric: %v\n", err)
		}
	}

	return nil
}

func (l *Logzio) parseMetric(metric telegraf.Metric) *Metric {
	return &Metric{
		Metric: map[string]interface{}{
			metric.Name(): metric.Fields(),
		},
		Dimensions: metric.Tags(),
		Time:       metric.Time(),
		Type:       logzioType,
	}
}

func CreateDefultLogizoOutput() *Logzio {
	return &Logzio{
		CheckDiskSpace: defaultLogzioCheckDiskSpace,
		DiskThreshold:  defaultLogzioDiskThreshold,
		DrainDuration:  defaultLogzioDrainDuration,
		QueueDir: fmt.Sprintf("%s%s%s%s%d", os.TempDir(), string(os.PathSeparator),
			"logzio-queue", string(os.PathSeparator), time.Now().UnixNano()),
		URL: defaultLogzioURL,
	}
}

func init() {
	outputs.Add("logzio", func() telegraf.Output {
		return CreateDefultLogizoOutput()
	})
}
