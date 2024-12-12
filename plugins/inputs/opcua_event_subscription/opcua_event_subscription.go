package opcua_event_subscription

import (
	"context"
	"fmt"
	"time"
    "sync"

	"github.com/gopcua/opcua"
	opcuaclient "github.com/influxdata/telegraf/plugins/common/opcua"
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/config"
)

type OpcuaEventSubscription struct {
	Endpoint            string          `toml:"endpoint"`
	Interval            config.Duration `toml:"interval"`
	EventType           NodeIDWrapper   `toml:"event_type"`
	NodeIDs             []NodeIDWrapper `toml:"node_ids"`
	SourceNames         []string        `toml:"source_names"`
	Fields              []string        `toml:"fields"`
	SecurityMode        string          `toml:"security_mode"`
	SecurityPolicy      string          `toml:"security_policy"`
	Certificate         string          `toml:"certificate"`
	PrivateKey          string          `toml:"private_key"`
    ConnectionTimeout   config.Duration `toml:"connection_timeout"`
    RequestTimeout      config.Duration `toml:"request_timeout"`

	Client               *opcuaclient.OpcUAClient
	SubscriptionManager  *SubscriptionManager
	NotificationHandler  *NotificationHandler
	Cancel               context.CancelFunc
	Log                  telegraf.Logger
	ClientHandleToNodeId sync.Map
}

func (o *OpcuaEventSubscription) SampleConfig() string {
	return `
        ## OPC UA Server Endpoint
        endpoint = "opc.tcp://opcua.demo-this.com:62544/Quickstarts/AlarmConditionServer"

        ## Polling interval
        interval = "10s"

        ## Event Type Filter
        event_type = "ns=0;i=2041"

        ## Node IDs to subscribe to
        node_ids = ["ns=2;s=0:East/Blue"]

        ## Source Name Filter (optional)
        source_names = ["SourceName1", "SourceName2"]

        ## Fields to be returned
        fields = ["Severity", "Message"]

        ## Security mode and policy (optional)
        security_mode = "None"
        security_policy = "None"

        ## Client certificate and key (optional)
        certificate = ""
        private_key = ""

        ## Connection and Request Timeout (optional)
        connection_timeout = "10s"
        request_timeout = "10s"
    `
}

func (o *OpcuaEventSubscription) Start(acc telegraf.Accumulator) error {
	o.Log.Info("******************START******************")

	if o.Endpoint == "" {
		return fmt.Errorf("missing mandatory field: endpoint")
	}

	if o.Interval <= 0 {
		return fmt.Errorf("missing or invalid mandatory field: interval")
	}

	if len(o.NodeIDs) == 0 {
		return fmt.Errorf("missing mandatory field: node_ids")
	}

	if o.EventType.ID == nil {
		return fmt.Errorf("missing mandatory field: event_type")
	}

	if len(o.Fields) == 0 {
		return fmt.Errorf("missing mandatory field: fields")
	}

    if o.ConnectionTimeout == 0 {
        o.Log.Debug("ConnectionTimeout not set. Set to default value of 10s")
        o.ConnectionTimeout = config.Duration(10 * time.Second) // Default to 10 seconds
    }
    if o.RequestTimeout == 0 {
        o.Log.Debug("RequestTimeout not set. Set to default value of 10s")
        o.RequestTimeout = config.Duration(10 * time.Second) // Default to 10 seconds
    }

	clientConfig := &opcuaclient.OpcUAClientConfig{
		Endpoint:       o.Endpoint,
		SecurityPolicy: o.SecurityPolicy,
		SecurityMode:   o.SecurityMode,
		Certificate:    o.Certificate,
		PrivateKey:     o.PrivateKey,
		ConnectTimeout: config.Duration(o.ConnectionTimeout),
        RequestTimeout: config.Duration(o.RequestTimeout),
	}

	client, err := clientConfig.CreateClient(o.Log)
	if err != nil {
		return fmt.Errorf("failed to create OPC UA client: %v", err)
	}
	o.Client = client

	err = o.Client.Connect(context.Background())
	if err != nil {
		return fmt.Errorf("failed to connect to OPC UA server: %v", err)
	}

	o.SubscriptionManager = &SubscriptionManager{
		Client:                 o.Client.Client,
		EventType:              o.EventType,
		NodeIDs:                o.NodeIDs,
		Fields:                 o.Fields,
		SourceNames:            o.SourceNames,
		Log:                    o.Log,
		Interval:               time.Duration(o.Interval),
		ClientHandleToNodeId:   &o.ClientHandleToNodeId,
	}

	o.NotificationHandler = &NotificationHandler{
		Fields:                 o.Fields,
		Log:                    o.Log,
		Endpoint:               o.Endpoint,
		ClientHandleToNodeId:   &o.ClientHandleToNodeId,
	}

	return nil
}

func (o *OpcuaEventSubscription) Gather(acc telegraf.Accumulator) error {
	if o.Client == nil {
		return fmt.Errorf("OPC UA Client is not initialized")
	}

	if len(o.SubscriptionManager.subscriptions) == 0 {
		ctx := context.Background()
		notifyCh := make(chan *opcua.PublishNotificationData)

		if err := o.SubscriptionManager.CreateSubscription(ctx, notifyCh); err != nil {
			return fmt.Errorf("failed to create subscription: %v", err)
		}

		if err := o.SubscriptionManager.Subscribe(ctx, notifyCh); err != nil {
			return fmt.Errorf("failed to subscribe: %v", err)
		}

		go func() {
			for {
				select {
				case <-ctx.Done():
					o.Log.Warn("Context cancelled, stopping Goroutine")
					return
				case notification := <-notifyCh:
					if notification.Error != nil {
						o.Log.Errorf("Notification error: %v", notification.Error)
						continue
					}
					o.NotificationHandler.HandleNotification(notification, acc)
				}
			}
		}()
	}

	return nil
}

func (o *OpcuaEventSubscription) Stop() {
	o.Log.Info("******************STOP******************")
	if o.Cancel != nil {
		o.Cancel()
	}
	if o.Client != nil {
		for _, sub := range o.SubscriptionManager.subscriptions {
			sub.Cancel(context.Background())
		}
		o.Client.Disconnect(context.Background())
	}
}

func init() {
	inputs.Add("opcua_event_subscription", func() telegraf.Input {
		return &OpcuaEventSubscription{}
	})
}