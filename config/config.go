package config

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/influxdata/toml"
	"github.com/influxdata/toml/ast"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	logging "github.com/influxdata/telegraf/logger"
	"github.com/influxdata/telegraf/models"
	"github.com/influxdata/telegraf/persister"
	"github.com/influxdata/telegraf/plugins/aggregators"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/outputs"
	"github.com/influxdata/telegraf/plugins/parsers"
	"github.com/influxdata/telegraf/plugins/parsers/csv"
	"github.com/influxdata/telegraf/plugins/processors"
	"github.com/influxdata/telegraf/plugins/secretstores"
	"github.com/influxdata/telegraf/plugins/serializers"
)

var (
	httpLoadConfigRetryInterval = 10 * time.Second

	// fetchURLRe is a regex to determine whether the requested file should
	// be fetched from a remote or read from the filesystem.
	fetchURLRe = regexp.MustCompile(`^\w+://`)

	// oldVarRe is a regex to reproduce pre v1.27.0 environment variable
	// replacement behavior
	oldVarRe = regexp.MustCompile(`\$(?i:(?P<named>[_a-z][_a-z0-9]*)|{(?:(?P<braced>[_a-z][_a-z0-9]*(?::?[-+?](.*))?)}|(?P<invalid>)))`)
	// OldEnvVarReplacement is a switch to allow going back to pre v1.27.0
	// environment variable replacement behavior
	OldEnvVarReplacement = false

	// Password specified via command-line
	Password Secret

	// telegrafVersion contains the parsed semantic Telegraf version
	telegrafVersion *semver.Version = semver.New("0.0.0-unknown")
)

// Config specifies the URL/user/password for the database that telegraf
// will be logging to, as well as all the plugins that the user has
// specified
type Config struct {
	toml              *toml.Config
	errs              []error // config load errors.
	UnusedFields      map[string]bool
	unusedFieldsMutex *sync.Mutex

	Tags               map[string]string
	InputFilters       []string
	OutputFilters      []string
	SecretStoreFilters []string

	SecretStores map[string]telegraf.SecretStore

	Agent       *AgentConfig
	Inputs      []*models.RunningInput
	Outputs     []*models.RunningOutput
	Aggregators []*models.RunningAggregator
	// Processors have a slice wrapper type because they need to be sorted
	Processors        models.RunningProcessors
	AggProcessors     models.RunningProcessors
	fileProcessors    OrderedPlugins
	fileAggProcessors OrderedPlugins

	// Parsers are created by their inputs during gather. Config doesn't keep track of them
	// like the other plugins because they need to be garbage collected (See issue #11809)

	Deprecations map[string][]int64

	Persister *persister.Persister

	NumberSecrets uint64

	seenAgentTable     bool
	seenAgentTableOnce sync.Once
}

// Ordered plugins used to keep the order in which they appear in a file
type OrderedPlugin struct {
	Line   int
	plugin any
}
type OrderedPlugins []*OrderedPlugin

func (op OrderedPlugins) Len() int           { return len(op) }
func (op OrderedPlugins) Swap(i, j int)      { op[i], op[j] = op[j], op[i] }
func (op OrderedPlugins) Less(i, j int) bool { return op[i].Line < op[j].Line }

// NewConfig creates a new struct to hold the Telegraf config.
// For historical reasons, It holds the actual instances of the running plugins
// once the configuration is parsed.
func NewConfig() *Config {
	c := &Config{
		UnusedFields:      make(map[string]bool),
		unusedFieldsMutex: &sync.Mutex{},

		// Agent defaults:
		Agent: &AgentConfig{
			Interval:                   Duration(10 * time.Second),
			RoundInterval:              true,
			FlushInterval:              Duration(10 * time.Second),
			LogfileRotationMaxArchives: 5,
		},

		Tags:               make(map[string]string),
		Inputs:             make([]*models.RunningInput, 0),
		Outputs:            make([]*models.RunningOutput, 0),
		Processors:         make([]*models.RunningProcessor, 0),
		AggProcessors:      make([]*models.RunningProcessor, 0),
		SecretStores:       make(map[string]telegraf.SecretStore),
		fileProcessors:     make([]*OrderedPlugin, 0),
		fileAggProcessors:  make([]*OrderedPlugin, 0),
		InputFilters:       make([]string, 0),
		OutputFilters:      make([]string, 0),
		SecretStoreFilters: make([]string, 0),
		Deprecations:       make(map[string][]int64),
	}

	// Handle unknown version
	if internal.Version != "" && internal.Version != "unknown" {
		telegrafVersion = semver.New(internal.Version)
	}

	tomlCfg := &toml.Config{
		NormFieldName: toml.DefaultConfig.NormFieldName,
		FieldToKey:    toml.DefaultConfig.FieldToKey,
		MissingField:  c.missingTomlField,
	}
	c.toml = tomlCfg

	return c
}

// AgentConfig defines configuration that will be used by the Telegraf agent
type AgentConfig struct {
	// Interval at which to gather information
	Interval Duration

	// RoundInterval rounds collection interval to 'interval'.
	//     ie, if Interval=10s then always collect on :00, :10, :20, etc.
	RoundInterval bool

	// Collected metrics are rounded to the precision specified. Precision is
	// specified as an interval with an integer + unit (e.g. 0s, 10ms, 2us, 4s).
	// Valid time units are "ns", "us" (or "µs"), "ms", "s".
	//
	// By default, or when set to "0s", precision will be set to the same
	// timestamp order as the collection interval, with the maximum being 1s:
	//   ie, when interval = "10s", precision will be "1s"
	//       when interval = "250ms", precision will be "1ms"
	//
	// Precision will NOT be used for service inputs. It is up to each individual
	// service input to set the timestamp at the appropriate precision.
	Precision Duration

	// CollectionJitter is used to jitter the collection by a random amount.
	// Each plugin will sleep for a random time within jitter before collecting.
	// This can be used to avoid many plugins querying things like sysfs at the
	// same time, which can have a measurable effect on the system.
	CollectionJitter Duration

	// CollectionOffset is used to shift the collection by the given amount.
	// This can be used to avoid many plugins querying constraint devices
	// at the same time by manually scheduling them in time.
	CollectionOffset Duration

	// FlushInterval is the Interval at which to flush data
	FlushInterval Duration

	// FlushJitter Jitters the flush interval by a random amount.
	// This is primarily to avoid large write spikes for users running a large
	// number of telegraf instances.
	// ie, a jitter of 5s and interval 10s means flushes will happen every 10-15s
	FlushJitter Duration

	// MetricBatchSize is the maximum number of metrics that is written to an
	// output plugin in one call.
	MetricBatchSize int

	// MetricBufferLimit is the max number of metrics that each output plugin
	// will cache. The buffer is cleared when a successful write occurs. When
	// full, the oldest metrics will be overwritten. This number should be a
	// multiple of MetricBatchSize. Due to current implementation, this could
	// not be less than 2 times MetricBatchSize.
	MetricBufferLimit int

	// FlushBufferWhenFull tells Telegraf to flush the metric buffer whenever
	// it fills up, regardless of FlushInterval. Setting this option to true
	// does _not_ deactivate FlushInterval.
	FlushBufferWhenFull bool `toml:"flush_buffer_when_full" deprecated:"0.13.0;1.35.0;option is ignored"`

	// TODO(cam): Remove UTC and parameter, they are no longer
	// valid for the agent config. Leaving them here for now for backwards-
	// compatibility
	UTC bool `toml:"utc" deprecated:"1.0.0;1.35.0;option is ignored"`

	// Debug is the option for running in debug mode
	Debug bool `toml:"debug"`

	// Quiet is the option for running in quiet mode
	Quiet bool `toml:"quiet"`

	// Log target controls the destination for logs and can be one of "file",
	// "stderr" or, on Windows, "eventlog". When set to "file", the output file
	// is determined by the "logfile" setting
	LogTarget string `toml:"logtarget" deprecated:"1.32.0;1.40.0;use 'logformat' and 'logfile' instead"`

	// Log format controls the way messages are logged and can be one of "text",
	// "structured" or, on Windows, "eventlog".
	LogFormat string `toml:"logformat"`

	// Name of the file to be logged to or stderr if empty. Ignored for "eventlog" format.
	Logfile string `toml:"logfile"`

	// Message key for structured logs, to override the default of "msg".
	// Ignored if "logformat" is not "structured".
	StructuredLogMessageKey string `toml:"structured_log_message_key"`

	// The file will be rotated after the time interval specified.  When set
	// to 0 no time based rotation is performed.
	LogfileRotationInterval Duration `toml:"logfile_rotation_interval"`

	// The logfile will be rotated when it becomes larger than the specified
	// size.  When set to 0 no size based rotation is performed.
	LogfileRotationMaxSize Size `toml:"logfile_rotation_max_size"`

	// Maximum number of rotated archives to keep, any older logs are deleted.
	// If set to -1, no archives are removed.
	LogfileRotationMaxArchives int `toml:"logfile_rotation_max_archives"`

	// Pick a timezone to use when logging or type 'local' for local time.
	LogWithTimezone string `toml:"log_with_timezone"`

	Hostname     string
	OmitHostname bool

	// Method for translating SNMP objects. 'netsnmp' to call external programs,
	// 'gosmi' to use the built-in library.
	SnmpTranslator string `toml:"snmp_translator"`

	// Name of the file to load the state of plugins from and store the state to.
	// If uncommented and not empty, this file will be used to save the state of
	// stateful plugins on termination of Telegraf. If the file exists on start,
	// the state in the file will be restored for the plugins.
	Statefile string `toml:"statefile"`

	// Flag to always keep tags explicitly defined in the plugin itself and
	// ensure those tags always pass filtering.
	AlwaysIncludeLocalTags bool `toml:"always_include_local_tags"`

	// Flag to always keep tags explicitly defined in the global tags section
	// and ensure those tags always pass filtering.
	AlwaysIncludeGlobalTags bool `toml:"always_include_global_tags"`

	// Flag to skip running processors after aggregators
	// By default, processors are run a second time after aggregators. Changing
	// this setting to true will skip the second run of processors.
	SkipProcessorsAfterAggregators *bool `toml:"skip_processors_after_aggregators"`

	// Number of attempts to obtain a remote configuration via a URL during
	// startup. Set to -1 for unlimited attempts.
	ConfigURLRetryAttempts int `toml:"config_url_retry_attempts"`

	// BufferStrategy is the metric buffer type to use for a given output plugin.
	// Supported types currently are "memory" and "disk".
	BufferStrategy string `toml:"buffer_strategy"`

	// BufferDirectory is the directory to store buffer files for serialized
	// to disk metrics when using the "disk" buffer strategy.
	BufferDirectory string `toml:"buffer_directory"`
}

// InputNames returns a list of strings of the configured inputs.
func (c *Config) InputNames() []string {
	name := make([]string, 0, len(c.Inputs))
	for _, input := range c.Inputs {
		name = append(name, input.Config.Name)
	}
	return PluginNameCounts(name)
}

// AggregatorNames returns a list of strings of the configured aggregators.
func (c *Config) AggregatorNames() []string {
	name := make([]string, 0, len(c.Aggregators))
	for _, aggregator := range c.Aggregators {
		name = append(name, aggregator.Config.Name)
	}
	return PluginNameCounts(name)
}

// ProcessorNames returns a list of strings of the configured processors.
func (c *Config) ProcessorNames() []string {
	name := make([]string, 0, len(c.Processors))
	for _, processor := range c.Processors {
		name = append(name, processor.Config.Name)
	}
	return PluginNameCounts(name)
}

// OutputNames returns a list of strings of the configured outputs.
func (c *Config) OutputNames() []string {
	name := make([]string, 0, len(c.Outputs))
	for _, output := range c.Outputs {
		name = append(name, output.Config.Name)
	}
	return PluginNameCounts(name)
}

// SecretstoreNames returns a list of strings of the configured secret-stores.
func (c *Config) SecretstoreNames() []string {
	names := make([]string, 0, len(c.SecretStores))
	for name := range c.SecretStores {
		names = append(names, name)
	}
	return PluginNameCounts(names)
}

// PluginNameCounts returns a list of sorted plugin names and their count
func PluginNameCounts(plugins []string) []string {
	names := make(map[string]int)
	for _, plugin := range plugins {
		names[plugin]++
	}

	var namecount []string
	for name, count := range names {
		if count == 1 {
			namecount = append(namecount, name)
		} else {
			namecount = append(namecount, fmt.Sprintf("%s (%dx)", name, count))
		}
	}

	sort.Strings(namecount)
	return namecount
}

// ListTags returns a string of tags specified in the config,
// line-protocol style
func (c *Config) ListTags() string {
	tags := make([]string, 0, len(c.Tags))
	for k, v := range c.Tags {
		tags = append(tags, fmt.Sprintf("%s=%s", k, v))
	}

	sort.Strings(tags)

	return strings.Join(tags, " ")
}

func sliceContains(name string, list []string) bool {
	for _, b := range list {
		if b == name {
			return true
		}
	}
	return false
}

// WalkDirectory collects all toml files that need to be loaded
func WalkDirectory(path string) ([]string, error) {
	var files []string
	walkfn := func(thispath string, info os.FileInfo, _ error) error {
		if info == nil {
			log.Printf("W! Telegraf is not permitted to read %s", thispath)
			return nil
		}

		if info.IsDir() {
			if strings.HasPrefix(info.Name(), "..") {
				// skip Kubernetes mounts, preventing loading the same config twice
				return filepath.SkipDir
			}

			return nil
		}
		name := info.Name()
		if len(name) < 6 || name[len(name)-5:] != ".conf" {
			return nil
		}
		files = append(files, thispath)
		return nil
	}
	return files, filepath.Walk(path, walkfn)
}

// Try to find a default config file at these locations (in order):
//  1. $TELEGRAF_CONFIG_PATH
//  2. $HOME/.telegraf/telegraf.conf
//  3. /etc/telegraf/telegraf.conf and /etc/telegraf/telegraf.d/*.conf
func GetDefaultConfigPath() ([]string, error) {
	envfile := os.Getenv("TELEGRAF_CONFIG_PATH")
	homefile := os.ExpandEnv("${HOME}/.telegraf/telegraf.conf")
	etcfile := "/etc/telegraf/telegraf.conf"
	etcfolder := "/etc/telegraf/telegraf.d"

	if runtime.GOOS == "windows" {
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" { // Should never happen
			programFiles = `C:\Program Files`
		}
		etcfile = programFiles + `\Telegraf\telegraf.conf`
		etcfolder = programFiles + `\Telegraf\telegraf.d\`
	}

	for _, path := range []string{envfile, homefile} {
		if isURL(path) {
			return []string{path}, nil
		}
		if _, err := os.Stat(path); err == nil {
			return []string{path}, nil
		}
	}

	// At this point we need to check if the files under /etc/telegraf are
	// populated and return them all.
	confFiles := make([]string, 0)
	if _, err := os.Stat(etcfile); err == nil {
		confFiles = append(confFiles, etcfile)
	}
	if _, err := os.Stat(etcfolder); err == nil {
		files, err := WalkDirectory(etcfolder)
		if err != nil {
			log.Printf("W! unable walk %q: %s", etcfolder, err)
		}
		confFiles = append(confFiles, files...)
	}
	if len(confFiles) > 0 {
		return confFiles, nil
	}

	// if we got here, we didn't find a file in a default location
	return nil, fmt.Errorf("no config file specified, and could not find one"+
		" in $TELEGRAF_CONFIG_PATH, %s, %s, or %s/*.conf", homefile, etcfile, etcfolder)
}

// isURL checks if string is valid url
func isURL(str string) bool {
	u, err := url.Parse(str)
	return err == nil && u.Scheme != "" && u.Host != ""
}

// LoadConfig loads the given config files and applies it to c
func (c *Config) LoadConfig(path string) error {
	if !c.Agent.Quiet {
		log.Printf("I! Loading config: %s", path)
	}

	data, _, err := LoadConfigFileWithRetries(path, c.Agent.ConfigURLRetryAttempts)
	if err != nil {
		return fmt.Errorf("loading config file %s failed: %w", path, err)
	}

	if err = c.LoadConfigData(data); err != nil {
		return fmt.Errorf("loading config file %s failed: %w", path, err)
	}

	return nil
}

func (c *Config) LoadAll(configFiles ...string) error {
	for _, fConfig := range configFiles {
		if err := c.LoadConfig(fConfig); err != nil {
			return err
		}
	}

	// Sort the processors according to their `order` setting while
	// using a stable sort to keep the file loading / file position order.
	sort.Stable(c.Processors)
	sort.Stable(c.AggProcessors)

	// Set snmp agent translator default
	if c.Agent.SnmpTranslator == "" {
		c.Agent.SnmpTranslator = "netsnmp"
	}

	// Check if there is enough lockable memory for the secret
	count := secretCount.Load()
	if count < 0 {
		log.Printf("E! Invalid secret count %d, please report this incident including your configuration!", count)
		count = 0
	}
	c.NumberSecrets = uint64(count)

	// Let's link all secrets to their secret-stores
	return c.LinkSecrets()
}

// LoadConfigData loads TOML-formatted config data
func (c *Config) LoadConfigData(data []byte) error {
	tbl, err := parseConfig(data)
	if err != nil {
		return fmt.Errorf("error parsing data: %w", err)
	}

	// Parse tags tables first:
	for _, tableName := range []string{"tags", "global_tags"} {
		if val, ok := tbl.Fields[tableName]; ok {
			subTable, ok := val.(*ast.Table)
			if !ok {
				return fmt.Errorf("invalid configuration, bad table name %q", tableName)
			}
			if err = c.toml.UnmarshalTable(subTable, c.Tags); err != nil {
				return fmt.Errorf("error parsing table name %q: %w", tableName, err)
			}
		}
	}

	// Parse agent table:
	if val, ok := tbl.Fields["agent"]; ok {
		if c.seenAgentTable {
			c.seenAgentTableOnce.Do(func() {
				log.Printf("W! Overlapping settings in multiple agent tables are not supported: may cause undefined behavior")
			})
		}
		c.seenAgentTable = true

		subTable, ok := val.(*ast.Table)
		if !ok {
			return errors.New("invalid configuration, error parsing agent table")
		}
		if err = c.toml.UnmarshalTable(subTable, c.Agent); err != nil {
			return fmt.Errorf("error parsing [agent]: %w", err)
		}
	}

	if !c.Agent.OmitHostname {
		if c.Agent.Hostname == "" {
			hostname, err := os.Hostname()
			if err != nil {
				return err
			}

			c.Agent.Hostname = hostname
		}

		c.Tags["host"] = c.Agent.Hostname
	}

	// Warn when explicitly setting the old snmp translator
	if c.Agent.SnmpTranslator == "netsnmp" {
		PrintOptionValueDeprecationNotice("agent", "snmp_translator", "netsnmp", telegraf.DeprecationInfo{
			Since:     "1.25.0",
			RemovalIn: "1.40.0",
			Notice:    "Use 'gosmi' instead",
		})
	}

	// Set up the persister if requested
	if c.Agent.Statefile != "" {
		c.Persister = &persister.Persister{
			Filename: c.Agent.Statefile,
		}
	}

	if len(c.UnusedFields) > 0 {
		return fmt.Errorf(
			"line %d: configuration specified the fields %q, but they were not used. "+
				"This is either a typo or this config option does not exist in this version.",
			tbl.Line, keys(c.UnusedFields))
	}

	// Initialize the file-sorting slices
	c.fileProcessors = make(OrderedPlugins, 0)
	c.fileAggProcessors = make(OrderedPlugins, 0)

	// Parse all the rest of the plugins:
	for name, val := range tbl.Fields {
		subTable, ok := val.(*ast.Table)
		if !ok {
			return fmt.Errorf("invalid configuration, error parsing field %q as table", name)
		}

		switch name {
		case "agent", "global_tags", "tags":
		case "outputs":
			for pluginName, pluginVal := range subTable.Fields {
				switch pluginSubTable := pluginVal.(type) {
				// legacy [outputs.influxdb] support
				case *ast.Table:
					if err = c.addOutput(pluginName, pluginSubTable); err != nil {
						return fmt.Errorf("error parsing %s, %w", pluginName, err)
					}
				case []*ast.Table:
					for _, t := range pluginSubTable {
						if err = c.addOutput(pluginName, t); err != nil {
							return fmt.Errorf("error parsing %s array, %w", pluginName, err)
						}
					}
				default:
					return fmt.Errorf("unsupported config format: %s",
						pluginName)
				}
				if len(c.UnusedFields) > 0 {
					return fmt.Errorf(
						"plugin %s.%s: line %d: configuration specified the fields %q, but they were not used. "+
							"This is either a typo or this config option does not exist in this version.",
						name, pluginName, subTable.Line, keys(c.UnusedFields))
				}
			}
		case "inputs", "plugins":
			for pluginName, pluginVal := range subTable.Fields {
				switch pluginSubTable := pluginVal.(type) {
				// legacy [inputs.cpu] support
				case *ast.Table:
					if err = c.addInput(pluginName, pluginSubTable); err != nil {
						return fmt.Errorf("error parsing %s, %w", pluginName, err)
					}
				case []*ast.Table:
					for _, t := range pluginSubTable {
						if err = c.addInput(pluginName, t); err != nil {
							return fmt.Errorf("error parsing %s, %w", pluginName, err)
						}
					}
				default:
					return fmt.Errorf("unsupported config format: %s",
						pluginName)
				}
				if len(c.UnusedFields) > 0 {
					return fmt.Errorf(
						"plugin %s.%s: line %d: configuration specified the fields %q, but they were not used. "+
							"This is either a typo or this config option does not exist in this version.",
						name, pluginName, subTable.Line, keys(c.UnusedFields))
				}
			}
		case "processors":
			for pluginName, pluginVal := range subTable.Fields {
				switch pluginSubTable := pluginVal.(type) {
				case []*ast.Table:
					for _, t := range pluginSubTable {
						if err = c.addProcessor(pluginName, t); err != nil {
							return fmt.Errorf("error parsing %s, %w", pluginName, err)
						}
					}
				default:
					return fmt.Errorf("unsupported config format: %s",
						pluginName)
				}
				if len(c.UnusedFields) > 0 {
					return fmt.Errorf(
						"plugin %s.%s: line %d: configuration specified the fields %q, but they were not used. "+
							"This is either a typo or this config option does not exist in this version.",
						name,
						pluginName,
						subTable.Line,
						keys(c.UnusedFields),
					)
				}
			}
		case "aggregators":
			for pluginName, pluginVal := range subTable.Fields {
				switch pluginSubTable := pluginVal.(type) {
				case []*ast.Table:
					for _, t := range pluginSubTable {
						if err = c.addAggregator(pluginName, t); err != nil {
							return fmt.Errorf("error parsing %s, %w", pluginName, err)
						}
					}
				default:
					return fmt.Errorf("unsupported config format: %s",
						pluginName)
				}
				if len(c.UnusedFields) > 0 {
					return fmt.Errorf(
						"plugin %s.%s: line %d: configuration specified the fields %q, but they were not used. "+
							"This is either a typo or this config option does not exist in this version.",
						name, pluginName, subTable.Line, keys(c.UnusedFields))
				}
			}
		case "secretstores":
			for pluginName, pluginVal := range subTable.Fields {
				switch pluginSubTable := pluginVal.(type) {
				case []*ast.Table:
					for _, t := range pluginSubTable {
						if err = c.addSecretStore(pluginName, t); err != nil {
							return fmt.Errorf("error parsing %s, %w", pluginName, err)
						}
					}
				default:
					return fmt.Errorf("unsupported config format: %s", pluginName)
				}
				if len(c.UnusedFields) > 0 {
					msg := "plugin %s.%s: line %d: configuration specified the fields %q, but they were not used. " +
						"This is either a typo or this config option does not exist in this version."
					return fmt.Errorf(msg, name, pluginName, subTable.Line, keys(c.UnusedFields))
				}
			}

		// Assume it's an input for legacy config file support if no other
		// identifiers are present
		default:
			if err = c.addInput(name, subTable); err != nil {
				return fmt.Errorf("error parsing %s, %w", name, err)
			}
		}
	}

	// Sort the processor according to the order they appeared in this file
	// In a later stage, we sort them using the `order` option.
	sort.Sort(c.fileProcessors)
	for _, op := range c.fileProcessors {
		c.Processors = append(c.Processors, op.plugin.(*models.RunningProcessor))
	}

	sort.Sort(c.fileAggProcessors)
	for _, op := range c.fileAggProcessors {
		c.AggProcessors = append(c.AggProcessors, op.plugin.(*models.RunningProcessor))
	}

	return nil
}

// trimBOM trims the Byte-Order-Marks from the beginning of the file.
// this is for Windows compatibility only.
// see https://github.com/influxdata/telegraf/issues/1378
func trimBOM(f []byte) []byte {
	return bytes.TrimPrefix(f, []byte("\xef\xbb\xbf"))
}

// LoadConfigFile loads the content of a configuration file and returns it
// together with a flag denoting if the file is from a remote location such
// as a web server.
func LoadConfigFile(config string) ([]byte, bool, error) {
	return LoadConfigFileWithRetries(config, 0)
}

func LoadConfigFileWithRetries(config string, urlRetryAttempts int) ([]byte, bool, error) {
	if fetchURLRe.MatchString(config) {
		u, err := url.Parse(config)
		if err != nil {
			return nil, true, err
		}

		switch u.Scheme {
		case "https", "http":
			data, err := fetchConfig(u, urlRetryAttempts)
			return data, true, err
		default:
			return nil, true, fmt.Errorf("scheme %q not supported", u.Scheme)
		}
	}

	// If it isn't a https scheme, try it as a file
	buffer, err := os.ReadFile(config)
	if err != nil {
		return nil, false, err
	}

	mimeType := http.DetectContentType(buffer)
	if !strings.Contains(mimeType, "text/plain") {
		return nil, false, fmt.Errorf("provided config is not a TOML file: %s", config)
	}

	return buffer, false, nil
}

func fetchConfig(u *url.URL, urlRetryAttempts int) ([]byte, error) {
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	if v, exists := os.LookupEnv("INFLUX_TOKEN"); exists {
		req.Header.Add("Authorization", "Token "+v)
	}
	req.Header.Add("Accept", "application/toml")
	req.Header.Set("User-Agent", internal.ProductToken())

	var totalAttempts int
	if urlRetryAttempts == -1 {
		totalAttempts = -1
		log.Printf("Using unlimited number of attempts to fetch HTTP config")
	} else if urlRetryAttempts == 0 {
		totalAttempts = 3
	} else if urlRetryAttempts > 0 {
		totalAttempts = urlRetryAttempts
	} else {
		return nil, fmt.Errorf("invalid number of attempts: %d", urlRetryAttempts)
	}

	attempt := 0
	for {
		body, err := requestURLConfig(req)
		if err == nil {
			return body, nil
		}

		log.Printf("Error getting HTTP config (attempt %d of %d): %s", attempt, totalAttempts, err)
		if urlRetryAttempts != -1 && attempt >= totalAttempts {
			return nil, err
		}

		time.Sleep(httpLoadConfigRetryInterval)
		attempt++
	}
}

func requestURLConfig(req *http.Request) ([]byte, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to HTTP config server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch HTTP config: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, nil
}

// parseConfig loads a TOML configuration from a provided path and
// returns the AST produced from the TOML parser. When loading the file, it
// will find environment variables and replace them.
func parseConfig(contents []byte) (*ast.Table, error) {
	contents = trimBOM(contents)
	var err error
	contents, err = removeComments(contents)
	if err != nil {
		return nil, err
	}
	outputBytes, err := substituteEnvironment(contents, OldEnvVarReplacement)
	if err != nil {
		return nil, err
	}
	return toml.Parse(outputBytes)
}

func (c *Config) addAggregator(name string, table *ast.Table) error {
	creator, ok := aggregators.Aggregators[name]
	if !ok {
		// Handle removed, deprecated plugins
		if di, deprecated := aggregators.Deprecations[name]; deprecated {
			printHistoricPluginDeprecationNotice("aggregators", name, di)
			return errors.New("plugin deprecated")
		}
		return fmt.Errorf("undefined but requested aggregator: %s", name)
	}
	aggregator := creator()

	conf, err := c.buildAggregator(name, table)
	if err != nil {
		return err
	}

	if err := c.toml.UnmarshalTable(table, aggregator); err != nil {
		return err
	}

	if err := c.printUserDeprecation("aggregators", name, aggregator); err != nil {
		return err
	}

	c.Aggregators = append(c.Aggregators, models.NewRunningAggregator(aggregator, conf))
	return nil
}

func (c *Config) addSecretStore(name string, table *ast.Table) error {
	if len(c.SecretStoreFilters) > 0 && !sliceContains(name, c.SecretStoreFilters) {
		return nil
	}

	storeID := c.getFieldString(table, "id")
	if storeID == "" {
		return fmt.Errorf("%q secret-store without ID", name)
	}
	if !secretStorePattern.MatchString(storeID) {
		return fmt.Errorf("invalid secret-store ID %q, must only contain letters, numbers or underscore", storeID)
	}

	creator, ok := secretstores.SecretStores[name]
	if !ok {
		// Handle removed, deprecated plugins
		if di, deprecated := secretstores.Deprecations[name]; deprecated {
			printHistoricPluginDeprecationNotice("secretstores", name, di)
			return errors.New("plugin deprecated")
		}
		return fmt.Errorf("undefined but requested secretstores: %s", name)
	}
	store := creator(storeID)

	if err := c.toml.UnmarshalTable(table, store); err != nil {
		return err
	}

	if err := c.printUserDeprecation("secretstores", name, store); err != nil {
		return err
	}

	logger := logging.New("secretstores", name, "")
	models.SetLoggerOnPlugin(store, logger)

	if err := store.Init(); err != nil {
		return fmt.Errorf("error initializing secret-store %q: %w", storeID, err)
	}

	if _, found := c.SecretStores[storeID]; found {
		return fmt.Errorf("duplicate ID %q for secretstore %q", storeID, name)
	}
	c.SecretStores[storeID] = store
	return nil
}

func (c *Config) LinkSecrets() error {
	for _, s := range unlinkedSecrets {
		resolvers := make(map[string]telegraf.ResolveFunc)
		for _, ref := range s.GetUnlinked() {
			// Split the reference and lookup the resolver
			storeID, key := splitLink(ref)
			store, found := c.SecretStores[storeID]
			if !found {
				return fmt.Errorf("unknown secret-store for %q", ref)
			}
			resolver, err := store.GetResolver(key)
			if err != nil {
				return fmt.Errorf("retrieving resolver for %q failed: %w", ref, err)
			}
			resolvers[ref] = resolver
		}
		// Inject the resolver list into the secret
		if err := s.Link(resolvers); err != nil {
			return fmt.Errorf("retrieving resolver failed: %w", err)
		}
	}
	return nil
}

func (c *Config) probeParser(parentCategory, parentName string, table *ast.Table) bool {
	dataFormat := c.getFieldString(table, "data_format")
	if dataFormat == "" {
		dataFormat = setDefaultParser(parentCategory, parentName)
	}

	creator, ok := parsers.Parsers[dataFormat]
	if !ok {
		return false
	}

	// Try to parse the options to detect if any of them is misspelled
	parser := creator("")
	//nolint:errcheck // We don't actually use the parser, so no need to check the error.
	c.toml.UnmarshalTable(table, parser)

	return true
}

func (c *Config) addParser(parentcategory, parentname string, table *ast.Table) (*models.RunningParser, error) {
	conf := &models.ParserConfig{
		Parent: parentname,
	}

	conf.DataFormat = c.getFieldString(table, "data_format")
	if conf.DataFormat == "" {
		conf.DataFormat = setDefaultParser(parentcategory, parentname)
	} else if conf.DataFormat == "influx" {
		influxParserType := c.getFieldString(table, "influx_parser_type")
		if influxParserType == "upstream" {
			conf.DataFormat = "influx_upstream"
		}
	}
	conf.LogLevel = c.getFieldString(table, "log_level")

	creator, ok := parsers.Parsers[conf.DataFormat]
	if !ok {
		return nil, fmt.Errorf("undefined but requested parser: %s", conf.DataFormat)
	}
	parser := creator(parentname)

	// Handle reset-mode of CSV parsers to stay backward compatible (see issue #12022)
	if conf.DataFormat == "csv" && parentcategory == "inputs" {
		if parentname == "exec" {
			csvParser := parser.(*csv.Parser)
			csvParser.ResetMode = "always"
		}
	}

	if err := c.toml.UnmarshalTable(table, parser); err != nil {
		return nil, err
	}

	running := models.NewRunningParser(parser, conf)
	err := running.Init()
	return running, err
}

func (c *Config) probeSerializer(table *ast.Table) bool {
	dataFormat := c.getFieldString(table, "data_format")
	if dataFormat == "" {
		dataFormat = "influx"
	}

	creator, ok := serializers.Serializers[dataFormat]
	if !ok {
		return false
	}

	// Try to parse the options to detect if any of them is misspelled
	serializer := creator()
	//nolint:errcheck // We don't actually use the parser, so no need to check the error.
	c.toml.UnmarshalTable(table, serializer)

	return true
}

func (c *Config) addSerializer(parentname string, table *ast.Table) (*models.RunningSerializer, error) {
	conf := &models.SerializerConfig{
		Parent: parentname,
	}
	conf.DataFormat = c.getFieldString(table, "data_format")
	if conf.DataFormat == "" {
		conf.DataFormat = "influx"
	}
	conf.LogLevel = c.getFieldString(table, "log_level")

	creator, ok := serializers.Serializers[conf.DataFormat]
	if !ok {
		return nil, fmt.Errorf("undefined but requested serializer: %s", conf.DataFormat)
	}
	serializer := creator()

	if err := c.toml.UnmarshalTable(table, serializer); err != nil {
		return nil, err
	}

	running := models.NewRunningSerializer(serializer, conf)
	err := running.Init()
	return running, err
}

func (c *Config) addProcessor(name string, table *ast.Table) error {
	creator, ok := processors.Processors[name]
	if !ok {
		// Handle removed, deprecated plugins
		if di, deprecated := processors.Deprecations[name]; deprecated {
			printHistoricPluginDeprecationNotice("processors", name, di)
			return errors.New("plugin deprecated")
		}
		return fmt.Errorf("undefined but requested processor: %s", name)
	}

	// For processors with parsers we need to compute the set of
	// options that is not covered by both, the parser and the processor.
	// We achieve this by keeping a local book of missing entries
	// that counts the number of misses. In case we have a parser
	// for the input both need to miss the entry. We count the
	// missing entries at the end.
	missCount := make(map[string]int)
	missCountThreshold := 0
	c.setLocalMissingTomlFieldTracker(missCount)
	defer c.resetMissingTomlFieldTracker()

	// Set up the processor running before the aggregators
	processorBeforeConfig, err := c.buildProcessor("processors", name, table)
	if err != nil {
		return err
	}
	processorBefore, count, err := c.setupProcessor(processorBeforeConfig.Name, creator, table)
	if err != nil {
		return err
	}
	rf := models.NewRunningProcessor(processorBefore, processorBeforeConfig)
	c.fileProcessors = append(c.fileProcessors, &OrderedPlugin{table.Line, rf})

	// Setup another (new) processor instance running after the aggregator
	processorAfterConfig, err := c.buildProcessor("aggprocessors", name, table)
	if err != nil {
		return err
	}
	processorAfter, _, err := c.setupProcessor(processorAfterConfig.Name, creator, table)
	if err != nil {
		return err
	}
	rf = models.NewRunningProcessor(processorAfter, processorAfterConfig)
	c.fileAggProcessors = append(c.fileAggProcessors, &OrderedPlugin{table.Line, rf})

	// Check the number of misses against the threshold. We need to double
	// the count as the processor setup is executed twice.
	missCountThreshold = 2 * count
	for key, count := range missCount {
		if count <= missCountThreshold {
			continue
		}
		if err := c.missingTomlField(nil, key); err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) setupProcessor(name string, creator processors.StreamingCreator, table *ast.Table) (telegraf.StreamingProcessor, int, error) {
	var optionTestCount int

	streamingProcessor := creator()

	var processor interface{}
	if p, ok := streamingProcessor.(processors.HasUnwrap); ok {
		processor = p.Unwrap()
	} else {
		processor = streamingProcessor
	}

	// If the (underlying) processor has a SetParser or SetParserFunc function,
	// it can accept arbitrary data-formats, so build the requested parser and
	// set it.
	if t, ok := processor.(telegraf.ParserPlugin); ok {
		parser, err := c.addParser("processors", name, table)
		if err != nil {
			return nil, 0, fmt.Errorf("adding parser failed: %w", err)
		}
		t.SetParser(parser)
		optionTestCount++
	}

	if t, ok := processor.(telegraf.ParserFuncPlugin); ok {
		if !c.probeParser("processors", name, table) {
			return nil, 0, errors.New("parser not found")
		}
		t.SetParserFunc(func() (telegraf.Parser, error) {
			return c.addParser("processors", name, table)
		})
		optionTestCount++
	}

	// If the (underlying) processor has a SetSerializer function it can accept
	// arbitrary data-formats, so build the requested serializer and set it.
	if t, ok := processor.(telegraf.SerializerPlugin); ok {
		serializer, err := c.addSerializer(name, table)
		if err != nil {
			return nil, 0, fmt.Errorf("adding serializer failed: %w", err)
		}
		t.SetSerializer(serializer)
		optionTestCount++
	}
	if t, ok := processor.(telegraf.SerializerFuncPlugin); ok {
		if !c.probeSerializer(table) {
			return nil, 0, errors.New("serializer not found")
		}
		t.SetSerializerFunc(func() (telegraf.Serializer, error) {
			return c.addSerializer(name, table)
		})
		optionTestCount++
	}

	if err := c.toml.UnmarshalTable(table, processor); err != nil {
		return nil, 0, fmt.Errorf("unmarshalling failed: %w", err)
	}

	err := c.printUserDeprecation("processors", name, processor)
	return streamingProcessor, optionTestCount, err
}

func (c *Config) addOutput(name string, table *ast.Table) error {
	if len(c.OutputFilters) > 0 && !sliceContains(name, c.OutputFilters) {
		return nil
	}

	// For outputs with serializers we need to compute the set of
	// options that is not covered by both, the serializer and the input.
	// We achieve this by keeping a local book of missing entries
	// that counts the number of misses. In case we have a parser
	// for the input both need to miss the entry. We count the
	// missing entries at the end.
	missThreshold := 0
	missCount := make(map[string]int)
	c.setLocalMissingTomlFieldTracker(missCount)
	defer c.resetMissingTomlFieldTracker()

	creator, ok := outputs.Outputs[name]
	if !ok {
		// Handle removed, deprecated plugins
		if di, deprecated := outputs.Deprecations[name]; deprecated {
			printHistoricPluginDeprecationNotice("outputs", name, di)
			return errors.New("plugin deprecated")
		}
		return fmt.Errorf("undefined but requested output: %s", name)
	}
	output := creator()

	// If the output has a SetSerializer function, then this means it can write
	// arbitrary types of output, so build the serializer and set it.
	if t, ok := output.(telegraf.SerializerPlugin); ok {
		missThreshold = 1
		serializer, err := c.addSerializer(name, table)
		if err != nil {
			return err
		}
		t.SetSerializer(serializer)
	}

	if t, ok := output.(telegraf.SerializerFuncPlugin); ok {
		missThreshold = 1
		if !c.probeSerializer(table) {
			return errors.New("serializer not found")
		}
		t.SetSerializerFunc(func() (telegraf.Serializer, error) {
			return c.addSerializer(name, table)
		})
	}

	outputConfig, err := c.buildOutput(name, table)
	if err != nil {
		return err
	}

	if err := c.toml.UnmarshalTable(table, output); err != nil {
		return err
	}

	if err := c.printUserDeprecation("outputs", name, output); err != nil {
		return err
	}

	if c, ok := interface{}(output).(interface{ TLSConfig() (*tls.Config, error) }); ok {
		if _, err := c.TLSConfig(); err != nil {
			return err
		}
	}

	// Check the number of misses against the threshold
	for key, count := range missCount {
		if count <= missThreshold {
			continue
		}
		if err := c.missingTomlField(nil, key); err != nil {
			return err
		}
	}

	ro := models.NewRunningOutput(output, outputConfig, c.Agent.MetricBatchSize, c.Agent.MetricBufferLimit)
	c.Outputs = append(c.Outputs, ro)

	return nil
}

func (c *Config) addInput(name string, table *ast.Table) error {
	if len(c.InputFilters) > 0 && !sliceContains(name, c.InputFilters) {
		return nil
	}

	// For inputs with parsers we need to compute the set of
	// options that is not covered by both, the parser and the input.
	// We achieve this by keeping a local book of missing entries
	// that counts the number of misses. In case we have a parser
	// for the input both need to miss the entry. We count the
	// missing entries at the end.
	missCount := make(map[string]int)
	missCountThreshold := 0
	c.setLocalMissingTomlFieldTracker(missCount)
	defer c.resetMissingTomlFieldTracker()

	creator, ok := inputs.Inputs[name]
	if !ok {
		// Handle removed, deprecated plugins
		if di, deprecated := inputs.Deprecations[name]; deprecated {
			printHistoricPluginDeprecationNotice("inputs", name, di)
			return errors.New("plugin deprecated")
		}

		return fmt.Errorf("undefined but requested input: %s", name)
	}
	input := creator()

	// If the input has a SetParser or SetParserFunc function, it can accept
	// arbitrary data-formats, so build the requested parser and set it.
	if t, ok := input.(telegraf.ParserPlugin); ok {
		missCountThreshold = 1
		parser, err := c.addParser("inputs", name, table)
		if err != nil {
			return fmt.Errorf("adding parser failed: %w", err)
		}
		t.SetParser(parser)
	}

	if t, ok := input.(telegraf.ParserFuncPlugin); ok {
		missCountThreshold = 1
		if !c.probeParser("inputs", name, table) {
			return errors.New("parser not found")
		}
		t.SetParserFunc(func() (telegraf.Parser, error) {
			return c.addParser("inputs", name, table)
		})
	}

	pluginConfig, err := c.buildInput(name, table)
	if err != nil {
		return err
	}

	if err := c.toml.UnmarshalTable(table, input); err != nil {
		return err
	}

	if err := c.printUserDeprecation("inputs", name, input); err != nil {
		return err
	}

	if c, ok := interface{}(input).(interface{ TLSConfig() (*tls.Config, error) }); ok {
		if _, err := c.TLSConfig(); err != nil {
			return err
		}
	}

	// Check the number of misses against the threshold
	for key, count := range missCount {
		if count <= missCountThreshold {
			continue
		}
		if err := c.missingTomlField(nil, key); err != nil {
			return err
		}
	}

	rp := models.NewRunningInput(input, pluginConfig)
	rp.SetDefaultTags(c.Tags)
	c.Inputs = append(c.Inputs, rp)

	return nil
}

// buildAggregator parses Aggregator specific items from the ast.Table,
// builds the filter and returns a
// models.AggregatorConfig to be inserted into models.RunningAggregator
func (c *Config) buildAggregator(name string, tbl *ast.Table) (*models.AggregatorConfig, error) {
	conf := &models.AggregatorConfig{
		Name:   name,
		Delay:  time.Millisecond * 100,
		Period: time.Second * 30,
		Grace:  time.Second * 0,
	}

	if period, found := c.getFieldDuration(tbl, "period"); found {
		conf.Period = period
	}
	if delay, found := c.getFieldDuration(tbl, "delay"); found {
		conf.Delay = delay
	}
	if grace, found := c.getFieldDuration(tbl, "grace"); found {
		conf.Grace = grace
	}

	conf.DropOriginal = c.getFieldBool(tbl, "drop_original")
	conf.MeasurementPrefix = c.getFieldString(tbl, "name_prefix")
	conf.MeasurementSuffix = c.getFieldString(tbl, "name_suffix")
	conf.NameOverride = c.getFieldString(tbl, "name_override")
	conf.Alias = c.getFieldString(tbl, "alias")
	conf.LogLevel = c.getFieldString(tbl, "log_level")

	conf.Tags = make(map[string]string)
	if node, ok := tbl.Fields["tags"]; ok {
		if subtbl, ok := node.(*ast.Table); ok {
			if err := c.toml.UnmarshalTable(subtbl, conf.Tags); err != nil {
				return nil, fmt.Errorf("could not parse tags for input %s", name)
			}
		}
	}

	if c.hasErrs() {
		return nil, c.firstErr()
	}

	var err error
	conf.Filter, err = c.buildFilter("aggregators."+name, tbl)
	if err != nil {
		return conf, err
	}

	// Generate an ID for the plugin
	conf.ID, err = generatePluginID("aggregators."+name, tbl)
	return conf, err
}

// buildProcessor parses Processor specific items from the ast.Table,
// builds the filter and returns a
// models.ProcessorConfig to be inserted into models.RunningProcessor
func (c *Config) buildProcessor(category, name string, tbl *ast.Table) (*models.ProcessorConfig, error) {
	conf := &models.ProcessorConfig{Name: name}

	conf.Order = c.getFieldInt64(tbl, "order")
	conf.Alias = c.getFieldString(tbl, "alias")
	conf.LogLevel = c.getFieldString(tbl, "log_level")

	if c.hasErrs() {
		return nil, c.firstErr()
	}

	var err error
	conf.Filter, err = c.buildFilter(category+"."+name, tbl)
	if err != nil {
		return conf, err
	}

	// Generate an ID for the plugin
	conf.ID, err = generatePluginID(category+"."+name, tbl)
	return conf, err
}

// buildFilter builds a Filter
// (tags, fields, namepass, namedrop, metricpass) to
// be inserted into the models.OutputConfig/models.InputConfig
// to be used for glob filtering on tags and measurements
func (c *Config) buildFilter(plugin string, tbl *ast.Table) (models.Filter, error) {
	f := models.Filter{}

	f.NamePass = c.getFieldStringSlice(tbl, "namepass")
	f.NamePassSeparators = c.getFieldString(tbl, "namepass_separator")
	f.NameDrop = c.getFieldStringSlice(tbl, "namedrop")
	f.NameDropSeparators = c.getFieldString(tbl, "namedrop_separator")

	oldPass := c.getFieldStringSlice(tbl, "pass")
	if len(oldPass) > 0 {
		PrintOptionDeprecationNotice(plugin, "pass", telegraf.DeprecationInfo{
			Since:     "0.10.4",
			RemovalIn: "1.35.0",
			Notice:    "use 'fieldinclude' instead",
		})
		f.FieldInclude = append(f.FieldInclude, oldPass...)
	}

	oldFieldPass := c.getFieldStringSlice(tbl, "fieldpass")
	if len(oldFieldPass) > 0 {
		PrintOptionDeprecationNotice(plugin, "fieldpass", telegraf.DeprecationInfo{
			Since:     "1.29.0",
			RemovalIn: "1.40.0",
			Notice:    "use 'fieldinclude' instead",
		})
		f.FieldInclude = append(f.FieldInclude, oldFieldPass...)
	}

	fieldInclude := c.getFieldStringSlice(tbl, "fieldinclude")
	if len(fieldInclude) > 0 {
		f.FieldInclude = append(f.FieldInclude, fieldInclude...)
	}

	oldDrop := c.getFieldStringSlice(tbl, "drop")
	if len(oldDrop) > 0 {
		PrintOptionDeprecationNotice(plugin, "drop", telegraf.DeprecationInfo{
			Since:     "0.10.4",
			RemovalIn: "1.35.0",
			Notice:    "use 'fieldexclude' instead",
		})
		f.FieldExclude = append(f.FieldExclude, oldDrop...)
	}

	oldFieldDrop := c.getFieldStringSlice(tbl, "fielddrop")
	if len(oldFieldDrop) > 0 {
		PrintOptionDeprecationNotice(plugin, "fielddrop", telegraf.DeprecationInfo{
			Since:     "1.29.0",
			RemovalIn: "1.40.0",
			Notice:    "use 'fieldexclude' instead",
		})
		f.FieldExclude = append(f.FieldExclude, oldFieldDrop...)
	}

	fieldExclude := c.getFieldStringSlice(tbl, "fieldexclude")
	if len(fieldExclude) > 0 {
		f.FieldExclude = append(f.FieldExclude, fieldExclude...)
	}

	f.TagPassFilters = c.getFieldTagFilter(tbl, "tagpass")
	f.TagDropFilters = c.getFieldTagFilter(tbl, "tagdrop")

	f.TagExclude = c.getFieldStringSlice(tbl, "tagexclude")
	f.TagInclude = c.getFieldStringSlice(tbl, "taginclude")

	f.MetricPass = c.getFieldString(tbl, "metricpass")

	if c.hasErrs() {
		return f, c.firstErr()
	}

	if err := f.Compile(); err != nil {
		return f, err
	}

	return f, nil
}

// buildInput parses input specific items from the ast.Table,
// builds the filter and returns a
// models.InputConfig to be inserted into models.RunningInput
func (c *Config) buildInput(name string, tbl *ast.Table) (*models.InputConfig, error) {
	cp := &models.InputConfig{
		Name:                    name,
		AlwaysIncludeLocalTags:  c.Agent.AlwaysIncludeLocalTags,
		AlwaysIncludeGlobalTags: c.Agent.AlwaysIncludeGlobalTags,
	}
	cp.Interval, _ = c.getFieldDuration(tbl, "interval")
	cp.Precision, _ = c.getFieldDuration(tbl, "precision")
	cp.CollectionJitter, _ = c.getFieldDuration(tbl, "collection_jitter")
	cp.CollectionOffset, _ = c.getFieldDuration(tbl, "collection_offset")
	cp.StartupErrorBehavior = c.getFieldString(tbl, "startup_error_behavior")
	cp.TimeSource = c.getFieldString(tbl, "time_source")

	cp.MeasurementPrefix = c.getFieldString(tbl, "name_prefix")
	cp.MeasurementSuffix = c.getFieldString(tbl, "name_suffix")
	cp.NameOverride = c.getFieldString(tbl, "name_override")
	cp.Alias = c.getFieldString(tbl, "alias")
	cp.LogLevel = c.getFieldString(tbl, "log_level")

	cp.Tags = make(map[string]string)
	if node, ok := tbl.Fields["tags"]; ok {
		if subtbl, ok := node.(*ast.Table); ok {
			if err := c.toml.UnmarshalTable(subtbl, cp.Tags); err != nil {
				return nil, fmt.Errorf("could not parse tags for input %s", name)
			}
		}
	}

	if c.hasErrs() {
		return nil, c.firstErr()
	}

	var err error
	cp.Filter, err = c.buildFilter("inputs."+name, tbl)
	if err != nil {
		return cp, err
	}

	// Generate an ID for the plugin
	cp.ID, err = generatePluginID("inputs."+name, tbl)
	return cp, err
}

// buildOutput parses output specific items from the ast.Table,
// builds the filter and returns a
// models.OutputConfig to be inserted into models.RunningInput
// Note: error exists in the return for future calls that might require error
func (c *Config) buildOutput(name string, tbl *ast.Table) (*models.OutputConfig, error) {
	filter, err := c.buildFilter("outputs."+name, tbl)
	if err != nil {
		return nil, err
	}
	oc := &models.OutputConfig{
		Name:            name,
		Filter:          filter,
		BufferStrategy:  c.Agent.BufferStrategy,
		BufferDirectory: c.Agent.BufferDirectory,
	}

	// TODO: support FieldPass/FieldDrop on outputs

	oc.FlushInterval, _ = c.getFieldDuration(tbl, "flush_interval")
	oc.FlushJitter, _ = c.getFieldDuration(tbl, "flush_jitter")
	oc.MetricBufferLimit = c.getFieldInt(tbl, "metric_buffer_limit")
	oc.MetricBatchSize = c.getFieldInt(tbl, "metric_batch_size")
	oc.Alias = c.getFieldString(tbl, "alias")
	oc.NameOverride = c.getFieldString(tbl, "name_override")
	oc.NameSuffix = c.getFieldString(tbl, "name_suffix")
	oc.NamePrefix = c.getFieldString(tbl, "name_prefix")
	oc.StartupErrorBehavior = c.getFieldString(tbl, "startup_error_behavior")
	oc.LogLevel = c.getFieldString(tbl, "log_level")

	if c.hasErrs() {
		return nil, c.firstErr()
	}

	if oc.BufferStrategy == "disk" {
		log.Printf("W! Using disk buffer strategy for plugin outputs.%s, this is an experimental feature", name)
	}

	// Generate an ID for the plugin
	oc.ID, err = generatePluginID("outputs."+name, tbl)
	return oc, err
}

func (c *Config) missingTomlField(_ reflect.Type, key string) error {
	switch key {
	// General options to ignore
	case "alias", "always_include_local_tags",
		"buffer_strategy", "buffer_directory",
		"collection_jitter", "collection_offset",
		"data_format", "delay", "drop", "drop_original",
		"fielddrop", "fieldexclude", "fieldinclude", "fieldpass", "flush_interval", "flush_jitter",
		"grace",
		"interval",
		"log_level", "lvm", // What is this used for?
		"metric_batch_size", "metric_buffer_limit", "metricpass",
		"name_override", "name_prefix", "name_suffix", "namedrop", "namedrop_separator", "namepass", "namepass_separator",
		"order",
		"pass", "period", "precision",
		"tagdrop", "tagexclude", "taginclude", "tagpass", "tags", "startup_error_behavior":

	// Secret-store options to ignore
	case "id":

	// Parser and serializer options to ignore
	case "data_type", "influx_parser_type":

	default:
		c.unusedFieldsMutex.Lock()
		c.UnusedFields[key] = true
		c.unusedFieldsMutex.Unlock()
	}
	return nil
}

func (c *Config) setLocalMissingTomlFieldTracker(counter map[string]int) {
	f := func(t reflect.Type, key string) error {
		// Check if we are in a root element that might share options among
		// each other. Those root elements are plugins of all types.
		// All other elements are subtables of their respective plugin and
		// should just be hit once anyway. Therefore, we mark them with a
		// high number to handle them correctly later.
		pt := reflect.PointerTo(t)
		root := pt.Implements(reflect.TypeOf((*telegraf.Input)(nil)).Elem())
		root = root || pt.Implements(reflect.TypeOf((*telegraf.ServiceInput)(nil)).Elem())
		root = root || pt.Implements(reflect.TypeOf((*telegraf.Output)(nil)).Elem())
		root = root || pt.Implements(reflect.TypeOf((*telegraf.Aggregator)(nil)).Elem())
		root = root || pt.Implements(reflect.TypeOf((*telegraf.Processor)(nil)).Elem())
		root = root || pt.Implements(reflect.TypeOf((*telegraf.StreamingProcessor)(nil)).Elem())
		root = root || pt.Implements(reflect.TypeOf((*telegraf.Parser)(nil)).Elem())
		root = root || pt.Implements(reflect.TypeOf((*telegraf.Serializer)(nil)).Elem())

		c, ok := counter[key]
		if !root {
			counter[key] = 100
		} else if !ok {
			counter[key] = 1
		} else {
			counter[key] = c + 1
		}
		return nil
	}
	c.toml.MissingField = f
}

func (c *Config) resetMissingTomlFieldTracker() {
	c.toml.MissingField = c.missingTomlField
}

func (c *Config) getFieldString(tbl *ast.Table, fieldName string) string {
	if node, ok := tbl.Fields[fieldName]; ok {
		if kv, ok := node.(*ast.KeyValue); ok {
			if str, ok := kv.Value.(*ast.String); ok {
				return str.Value
			}
		}
	}

	return ""
}

func (c *Config) getFieldDuration(tbl *ast.Table, fieldName string) (time.Duration, bool) {
	if node, ok := tbl.Fields[fieldName]; ok {
		if kv, ok := node.(*ast.KeyValue); ok {
			if str, ok := kv.Value.(*ast.String); ok {
				d, err := time.ParseDuration(str.Value)
				if err != nil {
					c.addError(tbl, fmt.Errorf("error parsing duration: %w", err))
					return 0, false
				}
				return d, true
			}
		}
	}

	return 0, false
}

func (c *Config) getFieldBool(tbl *ast.Table, fieldName string) bool {
	if node, ok := tbl.Fields[fieldName]; ok {
		if kv, ok := node.(*ast.KeyValue); ok {
			switch t := kv.Value.(type) {
			case *ast.Boolean:
				target, err := t.Boolean()
				if err != nil {
					c.addError(tbl, fmt.Errorf("unknown boolean value type %q, expecting boolean", kv.Value))
					return false
				}
				return target
			case *ast.String:
				target, err := strconv.ParseBool(t.Value)
				if err != nil {
					c.addError(tbl, fmt.Errorf("unknown boolean value type %q, expecting boolean", kv.Value))
					return false
				}
				return target
			default:
				c.addError(tbl, fmt.Errorf("unknown boolean value type %q, expecting boolean", kv.Value.Source()))
				return false
			}
		}
	}

	return false
}

func (c *Config) getFieldInt(tbl *ast.Table, fieldName string) int {
	if node, ok := tbl.Fields[fieldName]; ok {
		if kv, ok := node.(*ast.KeyValue); ok {
			if iAst, ok := kv.Value.(*ast.Integer); ok {
				i, err := iAst.Int()
				if err != nil {
					c.addError(tbl, fmt.Errorf("unexpected int type %q, expecting int", iAst.Value))
					return 0
				}
				return int(i)
			}
		}
	}

	return 0
}

func (c *Config) getFieldInt64(tbl *ast.Table, fieldName string) int64 {
	if node, ok := tbl.Fields[fieldName]; ok {
		if kv, ok := node.(*ast.KeyValue); ok {
			if iAst, ok := kv.Value.(*ast.Integer); ok {
				i, err := iAst.Int()
				if err != nil {
					c.addError(tbl, fmt.Errorf("unexpected int type %q, expecting int", iAst.Value))
					return 0
				}
				return i
			}
			c.addError(tbl, fmt.Errorf("found unexpected format while parsing %q, expecting int", fieldName))
			return 0
		}
	}

	return 0
}

func (c *Config) getFieldStringSlice(tbl *ast.Table, fieldName string) []string {
	var target []string
	if node, ok := tbl.Fields[fieldName]; ok {
		if kv, ok := node.(*ast.KeyValue); ok {
			ary, ok := kv.Value.(*ast.Array)
			if !ok {
				c.addError(tbl, fmt.Errorf("found unexpected format while parsing %q, expecting string array/slice format", fieldName))
				return target
			}
			for _, elem := range ary.Value {
				if str, ok := elem.(*ast.String); ok {
					target = append(target, str.Value)
				}
			}
		}
	}

	return target
}

func (c *Config) getFieldTagFilter(tbl *ast.Table, fieldName string) []models.TagFilter {
	var target []models.TagFilter
	if node, ok := tbl.Fields[fieldName]; ok {
		if subTbl, ok := node.(*ast.Table); ok {
			for name, val := range subTbl.Fields {
				if kv, ok := val.(*ast.KeyValue); ok {
					ary, ok := kv.Value.(*ast.Array)
					if !ok {
						c.addError(tbl, fmt.Errorf("found unexpected format while parsing %q, expecting string array/slice format on each entry", fieldName))
						return nil
					}

					tagFilter := models.TagFilter{Name: name}
					for _, elem := range ary.Value {
						if str, ok := elem.(*ast.String); ok {
							tagFilter.Values = append(tagFilter.Values, str.Value)
						}
					}
					target = append(target, tagFilter)
				}
			}
		}
	}

	return target
}

func keys(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

func setDefaultParser(category, name string) string {
	// Legacy support, exec plugin originally parsed JSON by default.
	if category == "inputs" && name == "exec" {
		return "json"
	}

	return "influx"
}

func (c *Config) hasErrs() bool {
	return len(c.errs) > 0
}

func (c *Config) firstErr() error {
	if len(c.errs) == 0 {
		return nil
	}
	return c.errs[0]
}

func (c *Config) addError(tbl *ast.Table, err error) {
	c.errs = append(c.errs, fmt.Errorf("line %d:%d: %w", tbl.Line, tbl.Position, err))
}
