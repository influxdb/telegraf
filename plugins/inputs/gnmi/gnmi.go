//go:generate ../../../tools/config_includer/generator
//go:generate ../../../tools/readme_config_includer/generator
package gnmi

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/gnxi/utils/xpath"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmi/proto/gnmi_ext"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/choice"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/common/yangmodel"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// Currently supported GNMI Extensions
var supportedExtensions = []string{"juniper_header"}

// Define the warning to show if we cannot get a metric name.
const emptyNameWarning = `Got empty metric-name for response (field %q), usually
indicating configuration issues as the response cannot be related to any
subscription.Please open an issue on https://github.com/influxdata/telegraf
including your device model and the following response data:
%+v
This message is only printed once.`

type GNMI struct {
	Addresses            []string          `toml:"addresses"`
	Subscriptions        []subscription    `toml:"subscription"`
	TagSubscriptions     []tagSubscription `toml:"tag_subscription"`
	Aliases              map[string]string `toml:"aliases"`
	Encoding             string            `toml:"encoding"`
	Origin               string            `toml:"origin"`
	Prefix               string            `toml:"prefix"`
	Target               string            `toml:"target"`
	UpdatesOnly          bool              `toml:"updates_only"`
	VendorSpecific       []string          `toml:"vendor_specific"`
	Username             config.Secret     `toml:"username"`
	Password             config.Secret     `toml:"password"`
	Redial               config.Duration   `toml:"redial"`
	MaxMsgSize           config.Size       `toml:"max_msg_size"`
	Depth                int32             `toml:"depth"`
	Trace                bool              `toml:"dump_responses"`
	CanonicalFieldNames  bool              `toml:"canonical_field_names"`
	TrimFieldNames       bool              `toml:"trim_field_names"`
	PrefixTagKeyWithPath bool              `toml:"prefix_tag_key_with_path"`
	GuessPathTag         bool              `toml:"guess_path_tag" deprecated:"1.30.0;1.35.0;use 'path_guessing_strategy' instead"`
	GuessPathStrategy    string            `toml:"path_guessing_strategy"`
	EnableTLS            bool              `toml:"enable_tls" deprecated:"1.27.0;1.35.0;use 'tls_enable' instead"`
	KeepaliveTime        config.Duration   `toml:"keepalive_time"`
	KeepaliveTimeout     config.Duration   `toml:"keepalive_timeout"`
	YangModelPaths       []string          `toml:"yang_model_paths"`
	Log                  telegraf.Logger   `toml:"-"`
	common_tls.ClientConfig

	// Internal state
	internalAliases map[*pathInfo]string
	decoder         *yangmodel.Decoder
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

type subscription struct {
	Name              string          `toml:"name"`
	Origin            string          `toml:"origin"`
	Path              string          `toml:"path"`
	SubscriptionMode  string          `toml:"subscription_mode"`
	SampleInterval    config.Duration `toml:"sample_interval"`
	SuppressRedundant bool            `toml:"suppress_redundant"`
	HeartbeatInterval config.Duration `toml:"heartbeat_interval"`
	TagOnly           bool            `toml:"tag_only" deprecated:"1.25.0;1.35.0;please use 'tag_subscription's instead"`

	fullPath *gnmi.Path
}

type tagSubscription struct {
	subscription
	Match    string   `toml:"match"`
	Elements []string `toml:"elements"`
}

func (*GNMI) SampleConfig() string {
	return sampleConfig
}

func (c *GNMI) Init() error {
	// Check options
	if time.Duration(c.Redial) <= 0 {
		return errors.New("redial duration must be positive")
	}

	// Check vendor_specific options configured by user
	if err := choice.CheckSlice(c.VendorSpecific, supportedExtensions); err != nil {
		return fmt.Errorf("unsupported vendor_specific option: %w", err)
	}

	// Check path guessing and handle deprecated option
	if c.GuessPathTag {
		if c.GuessPathStrategy == "" {
			c.GuessPathStrategy = "common path"
		}
		if c.GuessPathStrategy != "common path" {
			return errors.New("conflicting settings between 'guess_path_tag' and 'path_guessing_strategy'")
		}
	}
	switch c.GuessPathStrategy {
	case "", "none", "common path", "subscription":
	default:
		return fmt.Errorf("invalid 'path_guessing_strategy' %q", c.GuessPathStrategy)
	}

	// Use the new TLS option for enabling
	// Honor deprecated option
	enable := (c.ClientConfig.Enable != nil && *c.ClientConfig.Enable) || c.EnableTLS
	c.ClientConfig.Enable = &enable

	// Split the subscriptions into "normal" and "tag" subscription
	// and prepare them.
	for i := len(c.Subscriptions) - 1; i >= 0; i-- {
		subscription := c.Subscriptions[i]

		// Check the subscription
		if subscription.Name == "" {
			return fmt.Errorf("empty 'name' found for subscription %d", i+1)
		}
		if subscription.Path == "" {
			return fmt.Errorf("empty 'path' found for subscription %d", i+1)
		}

		// Support and convert legacy TagOnly subscriptions
		if subscription.TagOnly {
			tagSub := tagSubscription{
				subscription: subscription,
				Match:        "name",
			}
			c.TagSubscriptions = append(c.TagSubscriptions, tagSub)
			// Remove from the original subscriptions list
			c.Subscriptions = append(c.Subscriptions[:i], c.Subscriptions[i+1:]...)
			continue
		}
		if err := subscription.buildFullPath(c); err != nil {
			return err
		}
	}
	for idx := range c.TagSubscriptions {
		if err := c.TagSubscriptions[idx].buildFullPath(c); err != nil {
			return err
		}
		if c.TagSubscriptions[idx].TagOnly != c.TagSubscriptions[0].TagOnly {
			return errors.New("do not mix legacy tag_only subscriptions and tag subscriptions")
		}
		switch c.TagSubscriptions[idx].Match {
		case "":
			if len(c.TagSubscriptions[idx].Elements) > 0 {
				c.TagSubscriptions[idx].Match = "elements"
			} else {
				c.TagSubscriptions[idx].Match = "name"
			}
		case "unconditional":
		case "name":
		case "elements":
			if len(c.TagSubscriptions[idx].Elements) == 0 {
				return errors.New("tag_subscription must have at least one element")
			}
		default:
			return fmt.Errorf("unknown match type %q for tag-subscription %q", c.TagSubscriptions[idx].Match, c.TagSubscriptions[idx].Name)
		}
	}

	// Invert explicit alias list and prefill subscription names
	c.internalAliases = make(map[*pathInfo]string, len(c.Subscriptions)+len(c.Aliases)+len(c.TagSubscriptions))
	for _, s := range c.Subscriptions {
		if err := s.buildAlias(c.internalAliases); err != nil {
			return err
		}
	}
	for _, s := range c.TagSubscriptions {
		if err := s.buildAlias(c.internalAliases); err != nil {
			return err
		}
	}
	for alias, encodingPath := range c.Aliases {
		c.internalAliases[newInfoFromString(encodingPath)] = alias
	}
	c.Log.Debugf("Internal alias mapping: %+v", c.internalAliases)

	// Warn about configures insecure cipher suites
	insecure := common_tls.InsecureCiphers(c.ClientConfig.TLSCipherSuites)
	if len(insecure) > 0 {
		c.Log.Warnf("Configured insecure cipher suites: %s", strings.Join(insecure, ","))
	}

	// Check the TLS configuration
	if _, err := c.ClientConfig.TLSConfig(); err != nil {
		if errors.Is(err, common_tls.ErrCipherUnsupported) {
			secure, insecure := common_tls.Ciphers()
			c.Log.Info("Supported secure ciphers:")
			for _, name := range secure {
				c.Log.Infof("  %s", name)
			}
			c.Log.Info("Supported insecure ciphers:")
			for _, name := range insecure {
				c.Log.Infof("  %s", name)
			}
		}
		return err
	}

	// Load the YANG models if specified by the user
	if len(c.YangModelPaths) > 0 {
		decoder, err := yangmodel.NewDecoder(c.YangModelPaths...)
		if err != nil {
			return fmt.Errorf("creating YANG model decoder failed: %w", err)
		}
		c.decoder = decoder
	}

	return nil
}

func (c *GNMI) Start(acc telegraf.Accumulator) error {
	// Validate configuration
	request, err := c.newSubscribeRequest()
	if err != nil {
		return err
	}

	// Generate TLS config if enabled
	tlscfg, err := c.ClientConfig.TLSConfig()
	if err != nil {
		return err
	}

	// Prepare the context, optionally with credentials
	var ctx context.Context
	ctx, c.cancel = context.WithCancel(context.Background())

	if !c.Username.Empty() {
		usernameSecret, err := c.Username.Get()
		if err != nil {
			return fmt.Errorf("getting username failed: %w", err)
		}
		username := usernameSecret.String()
		usernameSecret.Destroy()

		passwordSecret, err := c.Password.Get()
		if err != nil {
			return fmt.Errorf("getting password failed: %w", err)
		}
		password := passwordSecret.String()
		passwordSecret.Destroy()

		ctx = metadata.AppendToOutgoingContext(ctx, "username", username, "password", password)
	}

	// Create a goroutine for each device, dial and subscribe
	c.wg.Add(len(c.Addresses))
	for _, addr := range c.Addresses {
		go func(addr string) {
			defer c.wg.Done()

			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				acc.AddError(fmt.Errorf("unable to parse address %s: %w", addr, err))
				return
			}
			h := handler{
				host:                host,
				port:                port,
				aliases:             c.internalAliases,
				tagsubs:             c.TagSubscriptions,
				maxMsgSize:          int(c.MaxMsgSize),
				vendorExt:           c.VendorSpecific,
				tagStore:            newTagStore(c.TagSubscriptions),
				trace:               c.Trace,
				canonicalFieldNames: c.CanonicalFieldNames,
				trimSlash:           c.TrimFieldNames,
				tagPathPrefix:       c.PrefixTagKeyWithPath,
				guessPathStrategy:   c.GuessPathStrategy,
				decoder:             c.decoder,
				log:                 c.Log,
				ClientParameters: keepalive.ClientParameters{
					Time:                time.Duration(c.KeepaliveTime),
					Timeout:             time.Duration(c.KeepaliveTimeout),
					PermitWithoutStream: false,
				},
			}
			for ctx.Err() == nil {
				if err := h.subscribeGNMI(ctx, acc, tlscfg, request); err != nil && ctx.Err() == nil {
					acc.AddError(err)
				}

				select {
				case <-ctx.Done():
				case <-time.After(time.Duration(c.Redial)):
				}
			}
		}(addr)
	}
	return nil
}

func (*GNMI) Gather(telegraf.Accumulator) error {
	return nil
}

func (c *GNMI) Stop() {
	c.cancel()
	c.wg.Wait()
}

func (s *subscription) buildSubscription() (*gnmi.Subscription, error) {
	gnmiPath, err := parsePath(s.Origin, s.Path, "")
	if err != nil {
		return nil, err
	}
	mode, ok := gnmi.SubscriptionMode_value[strings.ToUpper(s.SubscriptionMode)]
	if !ok {
		return nil, fmt.Errorf("invalid subscription mode %s", s.SubscriptionMode)
	}
	return &gnmi.Subscription{
		Path:              gnmiPath,
		Mode:              gnmi.SubscriptionMode(mode),
		HeartbeatInterval: uint64(time.Duration(s.HeartbeatInterval).Nanoseconds()),
		SampleInterval:    uint64(time.Duration(s.SampleInterval).Nanoseconds()),
		SuppressRedundant: s.SuppressRedundant,
	}, nil
}

// Create a new gNMI SubscribeRequest
func (c *GNMI) newSubscribeRequest() (*gnmi.SubscribeRequest, error) {
	// Create subscription objects
	subscriptions := make([]*gnmi.Subscription, 0, len(c.Subscriptions)+len(c.TagSubscriptions))
	for _, subscription := range c.TagSubscriptions {
		sub, err := subscription.buildSubscription()
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, sub)
	}
	for _, subscription := range c.Subscriptions {
		sub, err := subscription.buildSubscription()
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, sub)
	}

	// Construct subscribe request
	gnmiPath, err := parsePath(c.Origin, c.Prefix, c.Target)
	if err != nil {
		return nil, err
	}

	// Do not provide an empty prefix. Required for Huawei NE40 router v8.21
	// (and possibly others). See https://github.com/influxdata/telegraf/issues/12273.
	if gnmiPath.Origin == "" && gnmiPath.Target == "" && len(gnmiPath.Elem) == 0 {
		gnmiPath = nil
	}

	if c.Encoding != "proto" && c.Encoding != "json" && c.Encoding != "json_ietf" && c.Encoding != "bytes" {
		return nil, fmt.Errorf("unsupported encoding %s", c.Encoding)
	}

	var extensions []*gnmi_ext.Extension
	if c.Depth > 0 {
		extensions = []*gnmi_ext.Extension{
			{
				Ext: &gnmi_ext.Extension_Depth{
					Depth: &gnmi_ext.Depth{
						Level: uint32(c.Depth),
					},
				},
			},
		}
	}

	return &gnmi.SubscribeRequest{
		Request: &gnmi.SubscribeRequest_Subscribe{
			Subscribe: &gnmi.SubscriptionList{
				Prefix:       gnmiPath,
				Mode:         gnmi.SubscriptionList_STREAM,
				Encoding:     gnmi.Encoding(gnmi.Encoding_value[strings.ToUpper(c.Encoding)]),
				Subscription: subscriptions,
				UpdatesOnly:  c.UpdatesOnly,
			},
		},
		Extension: extensions,
	}, nil
}

// ParsePath from XPath-like string to gNMI path structure
func parsePath(origin, pathToParse, target string) (*gnmi.Path, error) {
	gnmiPath, err := xpath.ToGNMIPath(pathToParse)
	if err != nil {
		return nil, err
	}
	gnmiPath.Origin = origin
	gnmiPath.Target = target
	return gnmiPath, err
}

func (s *subscription) buildFullPath(c *GNMI) error {
	var err error
	if s.fullPath, err = xpath.ToGNMIPath(s.Path); err != nil {
		return err
	}
	s.fullPath.Origin = s.Origin
	s.fullPath.Target = c.Target
	if c.Prefix != "" {
		prefix, err := xpath.ToGNMIPath(c.Prefix)
		if err != nil {
			return err
		}
		s.fullPath.Elem = append(prefix.Elem, s.fullPath.Elem...)
		if s.Origin == "" && c.Origin != "" {
			s.fullPath.Origin = c.Origin
		}
	}
	return nil
}

func (s *subscription) buildAlias(aliases map[*pathInfo]string) error {
	// Build the subscription path without keys
	path, err := parsePath(s.Origin, s.Path, "")
	if err != nil {
		return err
	}
	info := newInfoFromPathWithoutKeys(path)

	// If the user didn't provide a measurement name, use last path element
	name := s.Name
	if name == "" && len(info.segments) > 0 {
		name = info.segments[len(info.segments)-1].id
	}
	if name != "" {
		aliases[info] = name
	}
	return nil
}

func newGNMI() telegraf.Input {
	return &GNMI{
		Encoding: "proto",
		Redial:   config.Duration(10 * time.Second),
	}
}

func init() {
	inputs.Add("gnmi", newGNMI)
	// Backwards compatible alias:
	inputs.Add("cisco_telemetry_gnmi", newGNMI)
}
