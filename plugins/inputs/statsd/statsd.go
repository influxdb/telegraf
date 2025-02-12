//go:generate ../../../tools/readme_config_includer/generator
package statsd

import (
	"bufio"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers/graphite"
	"github.com/influxdata/telegraf/selfstat"
)

//go:embed sample.conf
var sampleConfig string

var errParsing = errors.New("error parsing statsd line")

const (
	// udpMaxPacketSize is the UDP packet limit, see
	// https://en.wikipedia.org/wiki/User_Datagram_Protocol#Packet_structure
	udpMaxPacketSize int = 64 * 1024

	defaultFieldName           = "value"
	defaultProtocol            = "udp"
	defaultSeparator           = "_"
	defaultAllowPendingMessage = 10000
)

type Statsd struct {
	// Protocol used on listener - udp or tcp
	Protocol string `toml:"protocol"`

	// Address & Port to serve from
	ServiceAddress string `toml:"service_address"`

	// Number of messages allowed to queue up in between calls to Gather. If this
	// fills up, packets will get dropped until the next Gather interval is ran.
	AllowedPendingMessages int `toml:"allowed_pending_messages"`
	NumberWorkerThreads    int `toml:"number_workers_threads"`

	// Percentiles specifies the percentiles that will be calculated for timing
	// and histogram stats.
	Percentiles     []number `toml:"percentiles"`
	PercentileLimit int      `toml:"percentile_limit"`
	DeleteGauges    bool     `toml:"delete_gauges"`
	DeleteCounters  bool     `toml:"delete_counters"`
	DeleteSets      bool     `toml:"delete_sets"`
	DeleteTimings   bool     `toml:"delete_timings"`
	ConvertNames    bool     `toml:"convert_names"`
	FloatCounters   bool     `toml:"float_counters"`
	FloatTimings    bool     `toml:"float_timings"`
	FloatSets       bool     `toml:"float_sets"`

	EnableAggregationTemporality bool `toml:"enable_aggregation_temporality"`

	// MetricSeparator is the separator between parts of the metric name.
	MetricSeparator string `toml:"metric_separator"`
	// This flag enables parsing of tags in the dogstatsd extension to the
	// statsd protocol (http://docs.datadoghq.com/guides/dogstatsd/)
	ParseDataDogTags bool `toml:"parse_data_dog_tags" deprecated:"1.10.0;1.35.0;use 'datadog_extensions' instead"`

	// Parses extensions to statsd in the datadog statsd format
	// currently supports metrics and datadog tags.
	// http://docs.datadoghq.com/guides/dogstatsd/
	DataDogExtensions bool `toml:"datadog_extensions"`

	// Parses distribution metrics in the datadog statsd format.
	// Requires the DataDogExtension flag to be enabled.
	// https://docs.datadoghq.com/developers/metrics/types/?tab=distribution#definition
	DataDogDistributions bool `toml:"datadog_distributions"`

	// Either to keep or drop the container id as tag.
	// Requires the DataDogExtension flag to be enabled.
	// https://docs.datadoghq.com/developers/dogstatsd/datagram_shell/?tab=metrics#dogstatsd-protocol-v12
	DataDogKeepContainerTag bool `toml:"datadog_keep_container_tag"`

	// UDPPacketSize is deprecated, it's only here for legacy support
	// we now always create 1 max size buffer and then copy only what we need
	// into the in channel
	// see https://github.com/influxdata/telegraf/pull/992
	UDPPacketSize int `toml:"udp_packet_size" deprecated:"0.12.1;1.35.0;option is ignored"`

	ReadBufferSize      int              `toml:"read_buffer_size"`
	SanitizeNamesMethod string           `toml:"sanitize_name_method"`
	Templates           []string         `toml:"templates"` // bucket -> influx templates
	MaxTCPConnections   int              `toml:"max_tcp_connections"`
	TCPKeepAlive        bool             `toml:"tcp_keep_alive"`
	TCPKeepAlivePeriod  *config.Duration `toml:"tcp_keep_alive_period"`

	// Max duration for each metric to stay cached without being updated.
	MaxTTL config.Duration `toml:"max_ttl"`
	Log    telegraf.Logger `toml:"-"`

	sync.Mutex
	// Lock for preventing a data race during resource cleanup
	cleanup sync.Mutex
	wg      sync.WaitGroup
	// accept channel tracks how many active connections there are, if there
	// is an available bool in accept, then we are below the maximum and can
	// accept the connection
	accept chan bool
	// drops tracks the number of dropped metrics.
	drops int

	// Channel for all incoming statsd packets
	in   chan input
	done chan struct{}

	// Cache gauges, counters & sets so they can be aggregated as they arrive
	// gauges and counters map measurement/tags hash -> field name -> metrics
	// sets and timings map measurement/tags hash -> metrics
	// distributions aggregate measurement/tags and are published directly
	gauges        map[string]cachedgauge
	counters      map[string]cachedcounter
	sets          map[string]cachedset
	timings       map[string]cachedtimings
	distributions []cacheddistributions

	// Protocol listeners
	UDPConn     *net.UDPConn
	TCPlistener *net.TCPListener

	// track current connections so we can close them in Stop()
	conns          map[string]*net.TCPConn
	graphiteParser *graphite.Parser
	acc            telegraf.Accumulator
	bufPool        sync.Pool // pool of byte slices to handle parsing

	lastGatherTime time.Time

	Stats InternalStats
}

type InternalStats struct {
	// Internal statistics counters
	MaxConnections     selfstat.Stat
	CurrentConnections selfstat.Stat
	TotalConnections   selfstat.Stat
	TCPPacketsRecv     selfstat.Stat
	TCPBytesRecv       selfstat.Stat
	UDPPacketsRecv     selfstat.Stat
	UDPPacketsDrop     selfstat.Stat
	UDPBytesRecv       selfstat.Stat
	ParseTimeNS        selfstat.Stat
	PendingMessages    selfstat.Stat
	MaxPendingMessages selfstat.Stat
}

// number will get parsed as an int or float depending on what is passed
type number float64

func (n *number) UnmarshalTOML(b []byte) error {
	value, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return err
	}

	*n = number(value)
	return nil
}

type input struct {
	*bytes.Buffer
	time.Time
	Addr string
}

// One statsd metric, form is <bucket>:<value>|<mtype>|@<samplerate>
type metric struct {
	name       string
	field      string
	bucket     string
	hash       string
	intvalue   int64
	floatvalue float64
	strvalue   string
	mtype      string
	additive   bool
	samplerate float64
	tags       map[string]string
}

type cachedset struct {
	name      string
	fields    map[string]map[string]bool
	tags      map[string]string
	expiresAt time.Time
}

type cachedgauge struct {
	name      string
	fields    map[string]interface{}
	tags      map[string]string
	expiresAt time.Time
}

type cachedcounter struct {
	name      string
	fields    map[string]interface{}
	tags      map[string]string
	expiresAt time.Time
}

type cachedtimings struct {
	name      string
	fields    map[string]runningStats
	tags      map[string]string
	expiresAt time.Time
}

type cacheddistributions struct {
	name  string
	value float64
	tags  map[string]string
}

func (*Statsd) SampleConfig() string {
	return sampleConfig
}

func (s *Statsd) Start(ac telegraf.Accumulator) error {
	if s.ParseDataDogTags {
		s.DataDogExtensions = true
	}

	s.acc = ac

	// Make data structures
	s.lastGatherTime = time.Now()
	s.gauges = make(map[string]cachedgauge)
	s.counters = make(map[string]cachedcounter)
	s.sets = make(map[string]cachedset)
	s.timings = make(map[string]cachedtimings)
	s.distributions = make([]cacheddistributions, 0)

	s.Lock()
	defer s.Unlock()

	//
	tags := map[string]string{
		"address": s.ServiceAddress,
	}
	s.Stats.MaxConnections = selfstat.Register("statsd", "tcp_max_connections", tags)
	s.Stats.MaxConnections.Set(int64(s.MaxTCPConnections))
	s.Stats.CurrentConnections = selfstat.Register("statsd", "tcp_current_connections", tags)
	s.Stats.TotalConnections = selfstat.Register("statsd", "tcp_total_connections", tags)
	s.Stats.TCPPacketsRecv = selfstat.Register("statsd", "tcp_packets_received", tags)
	s.Stats.TCPBytesRecv = selfstat.Register("statsd", "tcp_bytes_received", tags)
	s.Stats.UDPPacketsRecv = selfstat.Register("statsd", "udp_packets_received", tags)
	s.Stats.UDPPacketsDrop = selfstat.Register("statsd", "udp_packets_dropped", tags)
	s.Stats.UDPBytesRecv = selfstat.Register("statsd", "udp_bytes_received", tags)
	s.Stats.ParseTimeNS = selfstat.Register("statsd", "parse_time_ns", tags)
	s.Stats.PendingMessages = selfstat.Register("statsd", "pending_messages", tags)
	s.Stats.MaxPendingMessages = selfstat.Register("statsd", "max_pending_messages", tags)
	s.Stats.MaxPendingMessages.Set(int64(s.AllowedPendingMessages))

	s.in = make(chan input, s.AllowedPendingMessages)
	s.done = make(chan struct{})
	s.accept = make(chan bool, s.MaxTCPConnections)
	s.conns = make(map[string]*net.TCPConn)
	s.bufPool = sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
	for i := 0; i < s.MaxTCPConnections; i++ {
		s.accept <- true
	}

	if s.MetricSeparator == "" {
		s.MetricSeparator = defaultSeparator
	}

	if s.isUDP() {
		address, err := net.ResolveUDPAddr(s.Protocol, s.ServiceAddress)
		if err != nil {
			return err
		}

		conn, err := net.ListenUDP(s.Protocol, address)
		if err != nil {
			return err
		}

		s.Log.Infof("UDP listening on %q", conn.LocalAddr().String())
		s.UDPConn = conn

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.udpListen(conn); err != nil {
				ac.AddError(err)
			}
		}()
	} else {
		address, err := net.ResolveTCPAddr("tcp", s.ServiceAddress)
		if err != nil {
			return err
		}
		listener, err := net.ListenTCP("tcp", address)
		if err != nil {
			return err
		}

		s.Log.Infof("TCP listening on %q", listener.Addr().String())
		s.TCPlistener = listener

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.tcpListen(listener); err != nil {
				ac.AddError(err)
			}
		}()
	}

	for i := 1; i <= s.NumberWorkerThreads; i++ {
		// Start the line parser
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.parser(); err != nil {
				ac.AddError(err)
			}
		}()
	}
	s.Log.Infof("Started the statsd service on %q", s.ServiceAddress)
	return nil
}

func (s *Statsd) Gather(acc telegraf.Accumulator) error {
	s.Lock()
	defer s.Unlock()
	now := time.Now()

	for _, m := range s.distributions {
		fields := map[string]interface{}{
			defaultFieldName: m.value,
		}
		if s.EnableAggregationTemporality {
			fields["start_time"] = s.lastGatherTime.Format(time.RFC3339)
		}
		acc.AddFields(m.name, fields, m.tags, now)
	}
	s.distributions = make([]cacheddistributions, 0)

	for _, m := range s.timings {
		// Defining a template to parse field names for timers allows us to split
		// out multiple fields per timer. In this case we prefix each stat with the
		// field name and store these all in a single measurement.
		fields := make(map[string]interface{})
		for fieldName, stats := range m.fields {
			var prefix string
			if fieldName != defaultFieldName {
				prefix = fieldName + "_"
			}
			fields[prefix+"mean"] = stats.mean()
			fields[prefix+"median"] = stats.median()
			fields[prefix+"stddev"] = stats.stddev()
			fields[prefix+"sum"] = stats.sum()
			fields[prefix+"upper"] = stats.upper()
			fields[prefix+"lower"] = stats.lower()
			if s.FloatTimings {
				fields[prefix+"count"] = float64(stats.count())
			} else {
				fields[prefix+"count"] = stats.count()
			}
			for _, percentile := range s.Percentiles {
				name := fmt.Sprintf("%s%v_percentile", prefix, percentile)
				fields[name] = stats.percentile(float64(percentile))
			}
		}
		if s.EnableAggregationTemporality {
			fields["start_time"] = s.lastGatherTime.Format(time.RFC3339)
		}

		acc.AddFields(m.name, fields, m.tags, now)
	}
	if s.DeleteTimings {
		s.timings = make(map[string]cachedtimings)
	}

	for _, m := range s.gauges {
		if s.EnableAggregationTemporality && m.fields != nil {
			m.fields["start_time"] = s.lastGatherTime.Format(time.RFC3339)
		}

		acc.AddGauge(m.name, m.fields, m.tags, now)
	}
	if s.DeleteGauges {
		s.gauges = make(map[string]cachedgauge)
	}

	for _, m := range s.counters {
		if s.EnableAggregationTemporality && m.fields != nil {
			m.fields["start_time"] = s.lastGatherTime.Format(time.RFC3339)
		}

		if s.FloatCounters {
			for key := range m.fields {
				m.fields[key] = float64(m.fields[key].(int64))
			}
		}
		acc.AddCounter(m.name, m.fields, m.tags, now)
	}
	if s.DeleteCounters {
		s.counters = make(map[string]cachedcounter)
	}

	for _, m := range s.sets {
		fields := make(map[string]interface{})
		for field, set := range m.fields {
			if s.FloatSets {
				fields[field] = float64(len(set))
			} else {
				fields[field] = int64(len(set))
			}
		}
		if s.EnableAggregationTemporality {
			fields["start_time"] = s.lastGatherTime.Format(time.RFC3339)
		}

		acc.AddFields(m.name, fields, m.tags, now)
	}
	if s.DeleteSets {
		s.sets = make(map[string]cachedset)
	}

	s.expireCachedMetrics()

	s.lastGatherTime = now
	return nil
}

func (s *Statsd) Stop() {
	s.Lock()
	s.Log.Infof("Stopping the statsd service")
	close(s.done)
	if s.isUDP() {
		if s.UDPConn != nil {
			s.UDPConn.Close()
		}
	} else {
		if s.TCPlistener != nil {
			s.TCPlistener.Close()
		}

		// Close all open TCP connections
		//  - get all conns from the s.conns map and put into slice
		//  - this is so the forget() function doesnt conflict with looping
		//    over the s.conns map
		var conns []*net.TCPConn
		s.cleanup.Lock()
		for _, conn := range s.conns {
			conns = append(conns, conn)
		}
		s.cleanup.Unlock()
		for _, conn := range conns {
			conn.Close()
		}
	}
	s.Unlock()

	s.wg.Wait()

	s.Lock()
	close(s.in)
	s.Log.Infof("Stopped listener service on %q", s.ServiceAddress)
	s.Unlock()
}

// tcpListen() starts listening for TCP packets on the configured port.
func (s *Statsd) tcpListen(listener *net.TCPListener) error {
	for {
		select {
		case <-s.done:
			return nil
		default:
			// Accept connection:
			conn, err := listener.AcceptTCP()
			if err != nil {
				return err
			}

			if s.TCPKeepAlive {
				if err := conn.SetKeepAlive(true); err != nil {
					return err
				}

				if s.TCPKeepAlivePeriod != nil {
					if err := conn.SetKeepAlivePeriod(time.Duration(*s.TCPKeepAlivePeriod)); err != nil {
						return err
					}
				}
			}

			select {
			case <-s.accept:
				// not over connection limit, handle the connection properly.
				s.wg.Add(1)
				// generate a random id for this TCPConn
				id, err := internal.RandomString(6)
				if err != nil {
					return err
				}

				s.remember(id, conn)
				go s.handler(conn, id)
			default:
				// We are over the connection limit, refuse & close.
				s.refuser(conn)
			}
		}
	}
}

// udpListen starts listening for UDP packets on the configured port.
func (s *Statsd) udpListen(conn *net.UDPConn) error {
	if s.ReadBufferSize > 0 {
		if err := s.UDPConn.SetReadBuffer(s.ReadBufferSize); err != nil {
			return err
		}
	}

	buf := make([]byte, udpMaxPacketSize)
	for {
		select {
		case <-s.done:
			return nil
		default:
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				if !strings.Contains(err.Error(), "closed network") {
					s.Log.Errorf("Error reading: %s", err.Error())
					continue
				}
				return nil
			}
			s.Stats.UDPPacketsRecv.Incr(1)
			s.Stats.UDPBytesRecv.Incr(int64(n))
			b, ok := s.bufPool.Get().(*bytes.Buffer)
			if !ok {
				return errors.New("bufPool is not a bytes buffer")
			}
			b.Reset()
			b.Write(buf[:n])
			select {
			case s.in <- input{
				Buffer: b,
				Time:   time.Now(),
				Addr:   addr.IP.String()}:
				s.Stats.PendingMessages.Set(int64(len(s.in)))
			default:
				s.Stats.UDPPacketsDrop.Incr(1)
				s.drops++
				if s.drops == 1 || s.AllowedPendingMessages == 0 || s.drops%s.AllowedPendingMessages == 0 {
					s.Log.Errorf("Statsd message queue full. "+
						"We have dropped %d messages so far. "+
						"You may want to increase allowed_pending_messages in the config", s.drops)
				}
			}
		}
	}
}

// parser monitors the s.in channel, if there is a packet ready, it parses the
// packet into statsd strings and then calls parseStatsdLine, which parses a
// single statsd metric into a struct.
func (s *Statsd) parser() error {
	for {
		select {
		case <-s.done:
			return nil
		case in := <-s.in:
			s.Stats.PendingMessages.Set(int64(len(s.in)))
			start := time.Now()
			lines := strings.Split(in.Buffer.String(), "\n")
			s.bufPool.Put(in.Buffer)
			for _, line := range lines {
				line = strings.TrimSpace(line)
				switch {
				case line == "":
				case s.DataDogExtensions && strings.HasPrefix(line, "_e"):
					if err := s.parseEventMessage(in.Time, line, in.Addr); err != nil {
						// Log the line causing the parsing error and continue
						// with the next line to not stop the whole gathering
						// process.
						s.Log.Errorf("Parsing line failed: %v", err)
						s.Log.Debugf("  line was: %s", line)
					}
				default:
					if err := s.parseStatsdLine(line); err != nil {
						if !errors.Is(err, errParsing) {
							// Ignore parsing errors but error out on
							// everything else...
							return err
						}
					}
				}
			}
			elapsed := time.Since(start)
			s.Stats.ParseTimeNS.Set(elapsed.Nanoseconds())
		}
	}
}

// parseStatsdLine will parse the given statsd line, validating it as it goes.
// If the line is valid, it will be cached for the next call to Gather()
func (s *Statsd) parseStatsdLine(line string) error {
	lineTags := make(map[string]string)
	if s.DataDogExtensions {
		recombinedSegments := make([]string, 0)
		// datadog tags look like this:
		// users.online:1|c|@0.5|#country:china,environment:production
		// users.online:1|c|#sometagwithnovalue
		// we will split on the pipe and remove any elements that are datadog
		// tags, parse them, and rebuild the line sans the datadog tags
		pipesplit := strings.Split(line, "|")
		for _, segment := range pipesplit {
			if len(segment) > 0 && segment[0] == '#' {
				// we have ourselves a tag; they are comma separated
				parseDataDogTags(lineTags, segment[1:])
			} else if len(segment) > 0 && strings.HasPrefix(segment, "c:") {
				// This is optional container ID field
				if s.DataDogKeepContainerTag {
					lineTags["container"] = segment[2:]
				}
			} else {
				recombinedSegments = append(recombinedSegments, segment)
			}
		}
		line = strings.Join(recombinedSegments, "|")
	}

	// Validate splitting the line on ":"
	bits := strings.Split(line, ":")
	if len(bits) < 2 {
		s.Log.Errorf("Splitting ':', unable to parse metric: %s", line)
		return errParsing
	}

	// Extract bucket name from individual metric bits
	bucketName, bits := bits[0], bits[1:]

	// Add a metric for each bit available
	for _, bit := range bits {
		m := metric{}

		m.bucket = bucketName

		// Validate splitting the bit on "|"
		pipesplit := strings.Split(bit, "|")
		if len(pipesplit) < 2 {
			s.Log.Errorf("Splitting '|', unable to parse metric: %s", line)
			return errParsing
		} else if len(pipesplit) > 2 {
			sr := pipesplit[2]

			if strings.Contains(sr, "@") && len(sr) > 1 {
				samplerate, err := strconv.ParseFloat(sr[1:], 64)
				if err != nil {
					s.Log.Errorf("Parsing sample rate: %s", err.Error())
				} else {
					// sample rate successfully parsed
					m.samplerate = samplerate
				}
			} else {
				s.Log.Debugf("Sample rate must be in format like: "+
					"@0.1, @0.5, etc. Ignoring sample rate for line: %s", line)
			}
		}

		// Validate metric type
		switch pipesplit[1] {
		case "g", "c", "s", "ms", "h", "d":
			m.mtype = pipesplit[1]
		default:
			s.Log.Errorf("Metric type %q unsupported", pipesplit[1])
			return errParsing
		}

		// Parse the value
		if strings.HasPrefix(pipesplit[0], "-") || strings.HasPrefix(pipesplit[0], "+") {
			if m.mtype != "g" && m.mtype != "c" {
				s.Log.Errorf("+- values are only supported for gauges & counters, unable to parse metric: %s", line)
				return errParsing
			}
			m.additive = true
		}

		switch m.mtype {
		case "g", "ms", "h", "d":
			v, err := strconv.ParseFloat(pipesplit[0], 64)
			if err != nil {
				s.Log.Errorf("Parsing value to float64, unable to parse metric: %s", line)
				return errParsing
			}
			m.floatvalue = v
		case "c":
			var v int64
			v, err := strconv.ParseInt(pipesplit[0], 10, 64)
			if err != nil {
				v2, err2 := strconv.ParseFloat(pipesplit[0], 64)
				if err2 != nil {
					s.Log.Errorf("Parsing value to int64, unable to parse metric: %s", line)
					return errParsing
				}
				v = int64(v2)
			}
			// If a sample rate is given with a counter, divide value by the rate
			if m.samplerate != 0 && m.mtype == "c" {
				v = int64(float64(v) / m.samplerate)
			}
			m.intvalue = v
		case "s":
			m.strvalue = pipesplit[0]
		}

		// Parse the name & tags from bucket
		m.name, m.field, m.tags = s.parseName(m.bucket)
		switch m.mtype {
		case "c":
			m.tags["metric_type"] = "counter"

			if s.EnableAggregationTemporality {
				if s.DeleteCounters {
					m.tags["temporality"] = "delta"
				} else {
					m.tags["temporality"] = "cumulative"
				}
			}
		case "g":
			m.tags["metric_type"] = "gauge"
		case "s":
			m.tags["metric_type"] = "set"
		case "ms":
			m.tags["metric_type"] = "timing"
		case "h":
			m.tags["metric_type"] = "histogram"
		case "d":
			m.tags["metric_type"] = "distribution"
		}
		if len(lineTags) > 0 {
			for k, v := range lineTags {
				m.tags[k] = v
			}
		}

		// Make a unique key for the measurement name/tags
		var tg []string
		for k, v := range m.tags {
			tg = append(tg, k+"="+v)
		}
		sort.Strings(tg)
		tg = append(tg, m.name)
		m.hash = strings.Join(tg, "")

		s.aggregate(m)
	}

	return nil
}

// parseName parses the given bucket name with the list of bucket maps in the
// config file. If there is a match, it will parse the name of the metric and
// map of tags.
// Return values are (<name>, <field>, <tags>)
func (s *Statsd) parseName(bucket string) (name, field string, tags map[string]string) {
	s.Lock()
	defer s.Unlock()
	tags = make(map[string]string)

	bucketparts := strings.Split(bucket, ",")
	// Parse out any tags in the bucket
	if len(bucketparts) > 1 {
		for _, btag := range bucketparts[1:] {
			k, v := parseKeyValue(btag)
			if k != "" {
				tags[k] = v
			}
		}
	}

	name = bucketparts[0]
	switch s.SanitizeNamesMethod {
	case "":
	case "upstream":
		whitespace := regexp.MustCompile(`\s+`)
		name = whitespace.ReplaceAllString(name, "_")
		name = strings.ReplaceAll(name, "/", "-")
		allowedChars := regexp.MustCompile(`[^a-zA-Z_\-0-9\.;=]`)
		name = allowedChars.ReplaceAllString(name, "")
	default:
		s.Log.Errorf("Unknown sanitizae name method: %s", s.SanitizeNamesMethod)
	}

	p := s.graphiteParser
	var err error

	if p == nil || s.graphiteParser.Separator != s.MetricSeparator {
		p = &graphite.Parser{Separator: s.MetricSeparator, Templates: s.Templates}
		err = p.Init()
		s.graphiteParser = p
	}

	if err == nil {
		p.DefaultTags = tags
		//nolint:errcheck // unable to propagate
		name, tags, field, _ = p.ApplyTemplate(name)
	}

	if s.ConvertNames {
		name = strings.ReplaceAll(name, ".", "_")
		name = strings.ReplaceAll(name, "-", "__")
	}
	if field == "" {
		field = defaultFieldName
	}

	return name, field, tags
}

// Parse the key,value out of a string that looks like "key=value"
func parseKeyValue(keyValue string) (key, val string) {
	split := strings.Split(keyValue, "=")
	// Must be exactly 2 to get anything meaningful out of them
	if len(split) == 2 {
		key = split[0]
		val = split[1]
	} else if len(split) == 1 {
		val = split[0]
	} else if len(split) > 2 {
		// fix: https://github.com/influxdata/telegraf/issues/10113
		// fix: value has "=" parse error
		// uri=/service/endpoint?sampleParam={paramValue} parse value key="uri", val="/service/endpoint?sampleParam\={paramValue}"
		key = split[0]
		val = strings.Join(split[1:], "=")
	}

	return key, val
}

// aggregate takes in a metric. It then
// aggregates and caches the current value(s). It does not deal with the
// Delete* options, because those are dealt with in the Gather function.
func (s *Statsd) aggregate(m metric) {
	s.Lock()
	defer s.Unlock()

	switch m.mtype {
	case "d":
		if s.DataDogExtensions && s.DataDogDistributions {
			cached := cacheddistributions{
				name:  m.name,
				value: m.floatvalue,
				tags:  m.tags,
			}
			s.distributions = append(s.distributions, cached)
		}
	case "ms", "h":
		// Check if the measurement exists
		cached, ok := s.timings[m.hash]
		if !ok {
			cached = cachedtimings{
				name:   m.name,
				fields: make(map[string]runningStats),
				tags:   m.tags,
			}
		}
		// Check if the field exists. If we've not enabled multiple fields per timer
		// this will be the default field name, eg. "value"
		field, ok := cached.fields[m.field]
		if !ok {
			field = runningStats{
				percLimit: s.PercentileLimit,
			}
		}
		if m.samplerate > 0 {
			for i := 0; i < int(1.0/m.samplerate); i++ {
				field.addValue(m.floatvalue)
			}
		} else {
			field.addValue(m.floatvalue)
		}
		cached.fields[m.field] = field
		cached.expiresAt = time.Now().Add(time.Duration(s.MaxTTL))
		s.timings[m.hash] = cached
	case "c":
		// check if the measurement exists
		cached, ok := s.counters[m.hash]
		if !ok {
			cached = cachedcounter{
				name:   m.name,
				fields: make(map[string]interface{}),
				tags:   m.tags,
			}
		}
		// check if the field exists
		_, ok = cached.fields[m.field]
		if !ok {
			cached.fields[m.field] = int64(0)
		}
		cached.fields[m.field] = cached.fields[m.field].(int64) + m.intvalue
		cached.expiresAt = time.Now().Add(time.Duration(s.MaxTTL))
		s.counters[m.hash] = cached
	case "g":
		// check if the measurement exists
		cached, ok := s.gauges[m.hash]
		if !ok {
			cached = cachedgauge{
				name:   m.name,
				fields: make(map[string]interface{}),
				tags:   m.tags,
			}
		}
		// check if the field exists
		_, ok = cached.fields[m.field]
		if !ok {
			cached.fields[m.field] = float64(0)
		}
		if m.additive {
			cached.fields[m.field] = cached.fields[m.field].(float64) + m.floatvalue
		} else {
			cached.fields[m.field] = m.floatvalue
		}

		cached.expiresAt = time.Now().Add(time.Duration(s.MaxTTL))
		s.gauges[m.hash] = cached
	case "s":
		// check if the measurement exists
		cached, ok := s.sets[m.hash]
		if !ok {
			cached = cachedset{
				name:   m.name,
				fields: make(map[string]map[string]bool),
				tags:   m.tags,
			}
		}
		// check if the field exists
		_, ok = cached.fields[m.field]
		if !ok {
			cached.fields[m.field] = make(map[string]bool)
		}
		cached.fields[m.field][m.strvalue] = true
		cached.expiresAt = time.Now().Add(time.Duration(s.MaxTTL))
		s.sets[m.hash] = cached
	}
}

// handler handles a single TCP Connection
func (s *Statsd) handler(conn *net.TCPConn, id string) {
	s.Stats.CurrentConnections.Incr(1)
	s.Stats.TotalConnections.Incr(1)
	// connection cleanup function
	defer func() {
		s.wg.Done()
		conn.Close()

		// Add one connection potential back to channel when this one closes
		s.accept <- true
		s.forget(id)
		s.Stats.CurrentConnections.Incr(-1)
	}()

	var remoteIP string
	if addr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		remoteIP = addr.IP.String()
	}

	var n int
	scanner := bufio.NewScanner(conn)
	for {
		select {
		case <-s.done:
			return
		default:
			if !scanner.Scan() {
				return
			}
			n = len(scanner.Bytes())
			if n == 0 {
				continue
			}
			s.Stats.TCPBytesRecv.Incr(int64(n))
			s.Stats.TCPPacketsRecv.Incr(1)

			b := s.bufPool.Get().(*bytes.Buffer)
			b.Reset()
			b.Write(scanner.Bytes())
			b.WriteByte('\n')

			select {
			case s.in <- input{Buffer: b, Time: time.Now(), Addr: remoteIP}:
				s.Stats.PendingMessages.Set(int64(len(s.in)))
			default:
				s.drops++
				if s.drops == 1 || s.drops%s.AllowedPendingMessages == 0 {
					s.Log.Errorf("Statsd message queue full. "+
						"We have dropped %d messages so far. "+
						"You may want to increase allowed_pending_messages in the config", s.drops)
				}
			}
		}
	}
}

// refuser refuses a TCP connection
func (s *Statsd) refuser(conn *net.TCPConn) {
	conn.Close()
	s.Log.Infof("Refused TCP Connection from %s", conn.RemoteAddr())
	s.Log.Warn("Maximum TCP Connections reached, you may want to adjust max_tcp_connections")
}

// forget a TCP connection
func (s *Statsd) forget(id string) {
	s.cleanup.Lock()
	defer s.cleanup.Unlock()
	delete(s.conns, id)
}

// remember a TCP connection
func (s *Statsd) remember(id string, conn *net.TCPConn) {
	s.cleanup.Lock()
	defer s.cleanup.Unlock()
	s.conns[id] = conn
}

// IsUDP returns true if the protocol is UDP, false otherwise.
func (s *Statsd) isUDP() bool {
	return strings.HasPrefix(s.Protocol, "udp")
}

func (s *Statsd) expireCachedMetrics() {
	// If Max TTL wasn't configured, skip expiration.
	if s.MaxTTL == 0 {
		return
	}

	now := time.Now()

	for key, cached := range s.gauges {
		if now.After(cached.expiresAt) {
			delete(s.gauges, key)
		}
	}

	for key, cached := range s.sets {
		if now.After(cached.expiresAt) {
			delete(s.sets, key)
		}
	}

	for key, cached := range s.timings {
		if now.After(cached.expiresAt) {
			delete(s.timings, key)
		}
	}

	for key, cached := range s.counters {
		if now.After(cached.expiresAt) {
			delete(s.counters, key)
		}
	}
}

func init() {
	inputs.Add("statsd", func() telegraf.Input {
		return &Statsd{
			Protocol:               defaultProtocol,
			ServiceAddress:         ":8125",
			MaxTCPConnections:      250,
			MetricSeparator:        "_",
			AllowedPendingMessages: defaultAllowPendingMessage,
			DeleteCounters:         true,
			DeleteGauges:           true,
			DeleteSets:             true,
			DeleteTimings:          true,
			NumberWorkerThreads:    5,
		}
	})
}
