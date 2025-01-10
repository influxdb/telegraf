//go:generate ../../../tools/readme_config_includer/generator
package jti_openconfig_telemetry

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
	authentication "github.com/influxdata/telegraf/plugins/inputs/jti_openconfig_telemetry/auth"
	telemetry "github.com/influxdata/telegraf/plugins/inputs/jti_openconfig_telemetry/oc"
)

//go:embed sample.conf
var sampleConfig string

var (
	// Regex to match and extract data points from path value in received key
	keyPathRegex = regexp.MustCompile(`/([^/]*)\[([A-Za-z0-9\-/]*=[^\[]*)]`)
)

type OpenConfigTelemetry struct {
	Servers         []string        `toml:"servers"`
	Sensors         []string        `toml:"sensors"`
	Username        string          `toml:"username"`
	Password        string          `toml:"password"`
	ClientID        string          `toml:"client_id"`
	TimestampSource string          `toml:"timestamp_source"`
	SampleFrequency config.Duration `toml:"sample_frequency"`
	StrAsTags       bool            `toml:"str_as_tags"`
	RetryDelay      config.Duration `toml:"retry_delay"`
	EnableTLS       bool            `toml:"enable_tls"`
	KeepAlivePeriod config.Duration `toml:"keep_alive_period"`
	common_tls.ClientConfig

	Log telegraf.Logger `toml:"-"`

	sensorsConfig   []sensorConfig
	grpcClientConns []grpcConnection
	wg              *sync.WaitGroup
}

// Structure to hold sensors path list and measurement name
type sensorConfig struct {
	measurementName string
	pathList        []*telemetry.Path
}

type grpcConnection struct {
	connection *grpc.ClientConn
	cancel     context.CancelFunc
}

func (g *grpcConnection) close() {
	g.connection.Close()
	g.cancel()
}

func (*OpenConfigTelemetry) SampleConfig() string {
	return sampleConfig
}

func (m *OpenConfigTelemetry) Init() error {
	switch m.TimestampSource {
	case "", "collection":
	case "data":
	default:
		return fmt.Errorf("unknown option for timestamp_source: %q", m.TimestampSource)
	}

	return nil
}

func (m *OpenConfigTelemetry) Start(acc telegraf.Accumulator) error {
	// Build sensors config
	if m.splitSensorConfig() == 0 {
		return errors.New("no valid sensor configuration available")
	}

	// Parse TLS config
	var creds credentials.TransportCredentials
	if m.EnableTLS {
		tlscfg, err := m.ClientConfig.TLSConfig()
		if err != nil {
			return err
		}
		creds = credentials.NewTLS(tlscfg)
	} else {
		creds = insecure.NewCredentials()
	}

	// Setup the basic connection options
	options := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
	}

	// Add keep-alive settings
	if m.KeepAlivePeriod > 0 {
		params := keepalive.ClientParameters{
			Time:    time.Duration(m.KeepAlivePeriod),
			Timeout: 2 * time.Duration(m.KeepAlivePeriod),
		}
		options = append(options, grpc.WithKeepaliveParams(params))
	}

	// Connect to given list of servers and start collecting data
	var grpcClientConn *grpc.ClientConn
	var wg sync.WaitGroup
	m.wg = &wg

	for _, server := range m.Servers {
		ctx, cancel := context.WithCancel(context.Background())
		if len(m.Username) > 0 {
			ctx = metadata.AppendToOutgoingContext(
				ctx,
				"username", m.Username,
				"password", m.Password,
				"clientid", m.ClientID,
			)
		}

		// Extract device address and port
		grpcServer, grpcPort, err := net.SplitHostPort(server)
		if err != nil {
			m.Log.Errorf("Invalid server address: %s", err.Error())
			cancel()
			continue
		}

		grpcClientConn, err = grpc.NewClient(server, options...)
		if err != nil {
			m.Log.Errorf("Failed to connect to %s: %s", server, err.Error())
		} else {
			m.Log.Debugf("Opened a new gRPC session to %s on port %s", grpcServer, grpcPort)
		}

		// Add to the list of client connections
		connection := grpcConnection{
			connection: grpcClientConn,
			cancel:     cancel,
		}
		m.grpcClientConns = append(m.grpcClientConns, connection)

		if m.Username != "" && m.Password != "" && m.ClientID != "" {
			if err := m.authenticate(ctx, server, grpcClientConn); err != nil {
				m.Log.Errorf("Error authenticating to %s: %v", grpcServer, err)
				continue
			}
		}

		// Subscribe and gather telemetry data
		m.collectData(ctx, grpcServer, grpcClientConn, acc)
	}

	return nil
}

func (*OpenConfigTelemetry) Gather(telegraf.Accumulator) error {
	return nil
}

func (m *OpenConfigTelemetry) Stop() {
	for _, grpcClientConn := range m.grpcClientConns {
		grpcClientConn.close()
	}
	m.wg.Wait()
}

// Takes in XML path with predicates and returns list of tags+values along with a final
// XML path without predicates. If /events/event[id=2]/attributes[key='message']/value
// is given input, this function will emit /events/event/attributes/value as xmlpath and
// { /events/event/@id=2, /events/event/attributes/@key='message' } as tags
func spitTagsNPath(xmlpath string) (string, map[string]string) {
	subs := keyPathRegex.FindAllStringSubmatch(xmlpath, -1)
	tags := make(map[string]string)

	// Given XML path, this will spit out final path without predicates
	if len(subs) > 0 {
		for _, sub := range subs {
			tagKey := strings.Split(xmlpath, sub[0])[0] + "/" + strings.TrimSpace(sub[1]) + "/@"

			// If we have multiple keys in give path like /events/event[id=2 and type=3]/,
			// we must emit multiple tags
			for _, kv := range strings.Split(sub[2], " and ") {
				key := tagKey + strings.TrimSpace(strings.Split(kv, "=")[0])
				tagValue := strings.ReplaceAll(strings.Split(kv, "=")[1], "'", "")
				tags[key] = tagValue
			}

			xmlpath = strings.Replace(xmlpath, sub[0], "/"+strings.TrimSpace(sub[1]), 1)
		}
	}

	return xmlpath, tags
}

// Takes in a OC response, extracts tag information from keys and returns a
// list of groups with unique sets of tags+values
func (m *OpenConfigTelemetry) extractData(r *telemetry.OpenConfigData, grpcServer string) []dataGroup {
	// Use empty prefix. We will update this when we iterate over key-value pairs
	prefix := ""

	dgroups := make([]dataGroup, 0, 5*len(r.Kv))
	for _, v := range r.Kv {
		kv := make(map[string]interface{})

		if v.Key == "__prefix__" {
			prefix = v.GetStrValue()
			continue
		}

		// Also, lets use prefix if there is one
		xmlpath, finaltags := spitTagsNPath(prefix + v.Key)
		finaltags["device"] = grpcServer

		switch v.Value.(type) {
		case *telemetry.KeyValue_StrValue:
			// If StrAsTags is set, we treat all string values as tags
			if m.StrAsTags {
				finaltags[xmlpath] = v.GetStrValue()
			} else {
				kv[xmlpath] = v.GetStrValue()
			}
		case *telemetry.KeyValue_DoubleValue:
			kv[xmlpath] = v.GetDoubleValue()
		case *telemetry.KeyValue_IntValue:
			kv[xmlpath] = v.GetIntValue()
		case *telemetry.KeyValue_UintValue:
			kv[xmlpath] = v.GetUintValue()
		case *telemetry.KeyValue_SintValue:
			kv[xmlpath] = v.GetSintValue()
		case *telemetry.KeyValue_BoolValue:
			kv[xmlpath] = v.GetBoolValue()
		case *telemetry.KeyValue_BytesValue:
			kv[xmlpath] = v.GetBytesValue()
		}

		// Insert other tags from message
		finaltags["system_id"] = r.SystemId
		finaltags["path"] = r.Path

		// Insert derived key and value
		dgroups = collectionByKeys(dgroups).insert(finaltags, kv)

		// Insert data from message header
		dgroups = collectionByKeys(dgroups).insert(finaltags,
			map[string]interface{}{"_sequence": r.SequenceNumber})
		dgroups = collectionByKeys(dgroups).insert(finaltags,
			map[string]interface{}{"_timestamp": r.Timestamp})
		dgroups = collectionByKeys(dgroups).insert(finaltags,
			map[string]interface{}{"_component_id": r.ComponentId})
		dgroups = collectionByKeys(dgroups).insert(finaltags,
			map[string]interface{}{"_subcomponent_id": r.SubComponentId})
	}

	return dgroups
}

// Takes in sensor configuration and converts it into slice of sensorConfig objects
func (m *OpenConfigTelemetry) splitSensorConfig() int {
	var pathlist []*telemetry.Path
	var measurementName string
	var reportingRate uint32

	m.sensorsConfig = make([]sensorConfig, 0)
	for _, sensor := range m.Sensors {
		spathSplit := strings.Fields(sensor)
		reportingRate = uint32(time.Duration(m.SampleFrequency) / time.Millisecond)

		// Extract measurement name and custom reporting rate if specified. Custom
		// reporting rate will be specified at the beginning of sensor list,
		// followed by measurement name like "1000ms interfaces /interfaces"
		// where 1000ms is the custom reporting rate and interfaces is the
		// measurement name. If 1000ms is not given, we use global reporting rate
		// from sample_frequency. if measurement name is not given, we use first
		// sensor name as the measurement name. If first or the word after custom
		// reporting rate doesn't start with /, we treat it as measurement name
		// and exclude it from list of sensors to subscribe
		duration, err := time.ParseDuration(spathSplit[0])
		if err == nil {
			reportingRate = uint32(duration / time.Millisecond)
			spathSplit = spathSplit[1:]
		}

		if len(spathSplit) == 0 {
			m.Log.Error("No sensors are specified")
			continue
		}

		// Word after custom reporting rate is treated as measurement name
		measurementName = spathSplit[0]

		// If our word after custom reporting rate doesn't start with /, we treat
		// it as measurement name. Else we treat it as sensor
		if !strings.HasPrefix(measurementName, "/") {
			spathSplit = spathSplit[1:]
		}

		if len(spathSplit) == 0 {
			m.Log.Error("No valid sensors are specified")
			continue
		}

		// Iterate over our sensors and create pathlist to subscribe
		pathlist = make([]*telemetry.Path, 0)
		for _, path := range spathSplit {
			pathlist = append(pathlist, &telemetry.Path{Path: path,
				SampleFrequency: reportingRate})
		}

		m.sensorsConfig = append(m.sensorsConfig, sensorConfig{
			measurementName: measurementName, pathList: pathlist,
		})
	}

	return len(m.sensorsConfig)
}

// Subscribes and collects OpenConfig telemetry data from given server
func (m *OpenConfigTelemetry) collectData(
	ctx context.Context,
	grpcServer string,
	grpcClientConn *grpc.ClientConn,
	acc telegraf.Accumulator,
) {
	c := telemetry.NewOpenConfigTelemetryClient(grpcClientConn)
	for _, sensor := range m.sensorsConfig {
		m.wg.Add(1)
		go func(ctx context.Context, sensor sensorConfig) {
			defer m.wg.Done()

			for {
				stream, err := c.TelemetrySubscribe(
					ctx,
					&telemetry.SubscriptionRequest{PathList: sensor.pathList},
				)
				if err != nil {
					rpcStatus, _ := status.FromError(err)
					if rpcStatus.Code() == codes.Unauthenticated {
						if m.Username != "" && m.Password != "" && m.ClientID != "" {
							err := m.authenticate(ctx, grpcServer, grpcClientConn)
							if err == nil {
								time.Sleep(1 * time.Second)
								continue
							}
							acc.AddError(fmt.Errorf("could not re-authenticate: %w", err))
						}
					} else if rpcStatus.Code() != codes.Unavailable {
						// If service is currently unavailable and may come back later, retry
						acc.AddError(fmt.Errorf("could not subscribe to %s on %q: %w", sensor.measurementName, grpcServer, err))
						return
					}

					// Retry with delay. If delay is not provided, use default
					if time.Duration(m.RetryDelay) > 0 {
						m.Log.Debugf("Retrying %s from %s with timeout %v", sensor.measurementName, grpcServer, time.Duration(m.RetryDelay))
						time.Sleep(time.Duration(m.RetryDelay))
						continue
					}
					return
				}

				m.Log.Debugf("Successfully subscribed to %s on %s", sensor.measurementName, grpcServer)

				for {
					r, err := stream.Recv()
					if err != nil {
						// If we encounter error in the stream, break so we can retry
						// the connection
						acc.AddError(fmt.Errorf("failed to read from %s from %s: %w", sensor.measurementName, grpcServer, err))
						time.Sleep(1 * time.Second)
						break
					}

					m.Log.Debugf("Received from %s on %s: %v", sensor.measurementName, grpcServer, r)

					// Create a point and add to batch
					tags := make(map[string]string)

					// Insert additional tags
					tags["device"] = grpcServer

					dgroups := m.extractData(r, grpcServer)

					// Print final data collection
					m.Log.Debugf("Available collection for %s on %s: %v", sensor.measurementName, grpcServer, dgroups)

					timestamp := time.Now()
					// Iterate through data groups and add them
					for _, group := range dgroups {
						if m.TimestampSource == "data" {
							// OpenConfig timestamp is in milliseconds since epoch
							ts, ok := group.data["_timestamp"].(uint64)
							if ok {
								timestamp = time.UnixMilli(int64(ts))
							} else {
								m.Log.Warnf("Invalid type %T for _timestamp %v", group.data["_timestamp"], group.data["_timestamp"])
							}
						}

						if len(group.tags) == 0 {
							acc.AddFields(sensor.measurementName, group.data, tags, timestamp)
						} else {
							acc.AddFields(sensor.measurementName, group.data, group.tags, timestamp)
						}
					}
				}
			}
		}(ctx, sensor)
	}
}

func (m *OpenConfigTelemetry) authenticate(ctx context.Context, server string, grpcClientConn *grpc.ClientConn) error {
	lc := authentication.NewLoginClient(grpcClientConn)
	loginReply, err := lc.LoginCheck(
		ctx,
		&authentication.LoginRequest{
			UserName: m.Username,
			Password: m.Password,
			ClientId: m.ClientID,
		},
	)
	if err != nil {
		return fmt.Errorf("could not initiate login check for %s: %w", server, err)
	}

	// Check if the user is authenticated. Bail if auth error
	if !loginReply.Result {
		return fmt.Errorf("failed to authenticate the user for %s", server)
	}

	return nil
}

func init() {
	inputs.Add("jti_openconfig_telemetry", func() telegraf.Input {
		return &OpenConfigTelemetry{
			RetryDelay:      config.Duration(time.Second),
			KeepAlivePeriod: config.Duration(10 * time.Second),
			StrAsTags:       false,
			TimestampSource: "collection",
		}
	})
}
