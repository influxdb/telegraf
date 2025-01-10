//go:generate ../../../tools/readme_config_includer/generator
package ctrlx_datalayer

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/boschrexroth/ctrlx-datalayer-golang/pkg/sseclient"
	"github.com/boschrexroth/ctrlx-datalayer-golang/pkg/token"
	"github.com/google/uuid"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/choice"
	"github.com/influxdata/telegraf/metric"
	common_http "github.com/influxdata/telegraf/plugins/common/http"
	"github.com/influxdata/telegraf/plugins/inputs"
	parsers_json "github.com/influxdata/telegraf/plugins/parsers/json"
)

// This plugin is based on the official ctrlX CORE API. Documentation can be found in OpenAPI format at:
// https://boschrexroth.github.io/rest-api-description/ctrlx-automation/ctrlx-core/
// Used APIs are:
// * ctrlX CORE - Authorization and Authentication API
// * ctrlX CORE - Data Layer API
//
// All communication between the device and this input plugin is based
// on https REST and HTML5 Server Sent Events (sse).

//go:embed sample.conf
var sampleConfig string

// CtrlXDataLayer encapsulated the configuration as well as the state of this plugin.
type CtrlXDataLayer struct {
	Server   string        `toml:"server"`
	Username config.Secret `toml:"username"`
	Password config.Secret `toml:"password"`

	Log          telegraf.Logger `toml:"-"`
	Subscription []subscription

	url    string
	wg     sync.WaitGroup
	cancel context.CancelFunc

	acc          telegraf.Accumulator
	connection   *http.Client
	tokenManager token.TokenManager
	common_http.HTTPClientConfig
}

func (*CtrlXDataLayer) SampleConfig() string {
	return sampleConfig
}

func (c *CtrlXDataLayer) Init() error {
	// Check all configured subscriptions for valid settings
	for i := range c.Subscription {
		sub := &c.Subscription[i]
		sub.applyDefaultSettings()
		if !choice.Contains(sub.QueueBehaviour, queueBehaviours) {
			c.Log.Infof("The right queue behaviour values are %v", queueBehaviours)
			return fmt.Errorf("subscription %d: setting 'queue_behaviour' %q is invalid", i, sub.QueueBehaviour)
		}
		if !choice.Contains(sub.ValueChange, valueChanges) {
			c.Log.Infof("The right value change values are %v", valueChanges)
			return fmt.Errorf("subscription %d: setting 'value_change' %q is invalid", i, sub.ValueChange)
		}
		if len(sub.Nodes) == 0 {
			c.Log.Warn("A configured subscription has no nodes configured")
		}
		sub.index = i
	}

	// Generate valid communication url based on configured server address
	u := url.URL{
		Scheme: "https",
		Host:   c.Server,
	}
	c.url = u.String()
	if _, err := url.Parse(c.url); err != nil {
		return errors.New("invalid server address")
	}

	return nil
}

func (c *CtrlXDataLayer) Start(acc telegraf.Accumulator) error {
	var ctx context.Context
	ctx, c.cancel = context.WithCancel(context.Background())

	var err error
	c.connection, err = c.HTTPClientConfig.CreateClient(ctx, c.Log)
	if err != nil {
		return fmt.Errorf("failed to create http client: %w", err)
	}

	username, err := c.Username.Get()
	if err != nil {
		return fmt.Errorf("getting username failed: %w", err)
	}

	password, err := c.Password.Get()
	if err != nil {
		username.Destroy()
		return fmt.Errorf("getting password failed: %w", err)
	}

	c.tokenManager = token.TokenManager{
		Url:        c.url,
		Username:   username.String(),
		Password:   password.String(),
		Connection: c.connection,
	}
	username.Destroy()
	password.Destroy()

	c.acc = acc

	c.gatherLoop(ctx)

	return nil
}

func (*CtrlXDataLayer) Gather(telegraf.Accumulator) error {
	// Metrics are sent to the accumulator asynchronously in worker thread. So nothing to do here.
	return nil
}

func (c *CtrlXDataLayer) Stop() {
	c.cancel()
	c.wg.Wait()
	if c.connection != nil {
		c.connection.CloseIdleConnections()
	}
}

// convertTimestamp2UnixTime converts the given Data Layer timestamp of the payload to UnixTime.
func convertTimestamp2UnixTime(t int64) time.Time {
	// 1 sec=1000 millisec=1000000 microsec=1000000000 nanosec.
	// Convert from FILETIME (100-nanosecond intervals since January 1, 1601 UTC) to
	// seconds and nanoseconds since January 1, 1970 UTC.
	// Between Jan 1, 1601 and Jan 1, 1970 there are 11644473600 seconds.
	return time.Unix(0, (t-116444736000000000)*100)
}

// createSubscription uses the official 'ctrlX Data Layer API' to create the sse subscription.
func (c *CtrlXDataLayer) createSubscription(sub *subscription) (string, error) {
	sseURL := c.url + subscriptionPath

	id := "telegraf_" + uuid.New().String()
	request := sub.createRequest(id)
	payload, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to create subscription %d payload: %w", sub.index, err)
	}

	requestBody := bytes.NewBuffer(payload)
	req, err := http.NewRequest("POST", sseURL, requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to create subscription %d request: %w", sub.index, err)
	}

	req.Header.Add("Authorization", c.tokenManager.Token.String())

	resp, err := c.connection.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to do request to create sse subscription %d: %w", sub.index, err)
	}
	resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", fmt.Errorf("failed to create sse subscription %d, status: %s", sub.index, resp.Status)
	}

	return sseURL + "/" + id, nil
}

// createSubscriptionAndSseClient creates a sse subscription on the server and
// initializes a sse client to receive sse events from the server.
func (c *CtrlXDataLayer) createSubscriptionAndSseClient(sub *subscription) (*sseclient.SseClient, error) {
	t, err := c.tokenManager.RequestAuthToken()
	if err != nil {
		return nil, err
	}

	subURL, err := c.createSubscription(sub)
	if err != nil {
		return nil, err
	}

	client := sseclient.NewSseClient(subURL, t.String(), c.InsecureSkipVerify)

	return client, nil
}

// addMetric writes sse metric into accumulator.
func (c *CtrlXDataLayer) addMetric(se *sseclient.SseEvent, sub *subscription) {
	switch se.Event {
	case "update":
		// Received an updated value, that we translate into a metric
		var d sseEventData

		if err := json.Unmarshal([]byte(se.Data), &d); err != nil {
			c.acc.AddError(fmt.Errorf("received malformed data from 'update' event: %w", err))
			return
		}
		m, err := c.createMetric(&d, sub)
		if err != nil {
			c.acc.AddError(fmt.Errorf("failed to create metrics: %w", err))
			return
		}
		c.acc.AddMetric(m)
	case "error":
		// Received an error event, that we report to the accumulator
		var e sseEventError
		if err := json.Unmarshal([]byte(se.Data), &e); err != nil {
			c.acc.AddError(fmt.Errorf("received malformed data from 'error' event: %w", err))
			return
		}
		c.acc.AddError(fmt.Errorf("received 'error' event for node: %q", e.Instance))
	case "keepalive":
		// Keepalive events are ignored for the moment
		c.Log.Debug("Received keepalive event")
	default:
		// Received a yet unsupported event type
		c.Log.Debugf("Received unsupported event: %q", se.Event)
	}
}

// createMetric - create metric depending on flag 'output_json' and data type
func (c *CtrlXDataLayer) createMetric(em *sseEventData, sub *subscription) (telegraf.Metric, error) {
	t := convertTimestamp2UnixTime(em.Timestamp)
	node := sub.node(em.Node)
	if node == nil {
		return nil, errors.New("node not found")
	}

	// default tags
	tags := map[string]string{
		"node":   em.Node,
		"source": c.Server,
	}

	// add tags of subscription if user has defined
	for key, value := range sub.Tags {
		tags[key] = value
	}

	// add tags of node if user has defined
	for key, value := range node.Tags {
		tags[key] = value
	}

	// set measurement of subscription
	measurement := sub.Measurement

	// get field key from node properties
	fieldKey := node.fieldKey()

	if fieldKey == "" {
		return nil, errors.New("field key not valid")
	}

	if sub.OutputJSONString {
		b, err := json.Marshal(em.Value)
		if err != nil {
			return nil, err
		}
		fields := map[string]interface{}{fieldKey: string(b)}
		m := metric.New(measurement, tags, fields, t)
		return m, nil
	}

	switch em.Type {
	case "object":
		flattener := parsers_json.JSONFlattener{}
		err := flattener.FullFlattenJSON(fieldKey, em.Value, true, true)
		if err != nil {
			return nil, err
		}

		m := metric.New(measurement, tags, flattener.Fields, t)
		return m, nil
	case "arbool8",
		"arint8", "aruint8",
		"arint16", "aruint16",
		"arint32", "aruint32",
		"arint64", "aruint64",
		"arfloat", "ardouble",
		"arstring",
		"artimestamp":
		fields := make(map[string]interface{})
		values := em.Value.([]interface{})
		for i := 0; i < len(values); i++ {
			index := strconv.Itoa(i)
			key := fieldKey + "_" + index
			fields[key] = values[i]
		}
		m := metric.New(measurement, tags, fields, t)
		return m, nil
	case "bool8",
		"int8", "uint8",
		"int16", "uint16",
		"int32", "uint32",
		"int64", "uint64",
		"float", "double",
		"string",
		"timestamp":
		fields := map[string]interface{}{fieldKey: em.Value}
		m := metric.New(measurement, tags, fields, t)
		return m, nil
	}

	return nil, fmt.Errorf("unsupported value type: %s", em.Type)
}

// gatherLoop creates sse subscriptions on the Data Layer and requests the sse data
// the connection will be restablished if the sse subscription is broken.
func (c *CtrlXDataLayer) gatherLoop(ctx context.Context) {
	for _, sub := range c.Subscription {
		c.wg.Add(1)
		go func(sub subscription) {
			defer c.wg.Done()
			for {
				select {
				case <-ctx.Done():
					c.Log.Debugf("Gather loop for subscription %d stopped", sub.index)
					return
				default:
					client, err := c.createSubscriptionAndSseClient(&sub)
					if err != nil {
						c.Log.Errorf("Creating sse client to subscription %d: %v", sub.index, err)
						time.Sleep(time.Duration(defaultReconnectInterval))
						continue
					}
					c.Log.Debugf("Created sse client to subscription %d", sub.index)

					// Establish connection and handle events in a callback function.
					err = client.Subscribe(ctx, func(event string, data string) {
						c.addMetric(&sseclient.SseEvent{
							Event: event,
							Data:  data,
						}, &sub)
					})
					if errors.Is(err, context.Canceled) {
						// Subscription cancelled
						c.Log.Debugf("Requesting data of subscription %d cancelled", sub.index)
						return
					}
					c.Log.Errorf("Requesting data of subscription %d failed: %v", sub.index, err)
				}
			}
		}(sub)
	}
}

// init registers the plugin in telegraf.
func init() {
	inputs.Add("ctrlx_datalayer", func() telegraf.Input {
		return &CtrlXDataLayer{}
	})
}
