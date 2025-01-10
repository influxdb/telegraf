//go:generate ../../../tools/readme_config_includer/generator
package stomp

import (
	"crypto/tls"
	_ "embed"
	"fmt"
	"net"
	"time"

	"github.com/go-stomp/stomp"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	common_tls "github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

type STOMP struct {
	Host      string          `toml:"host"`
	Username  config.Secret   `toml:"username"`
	Password  config.Secret   `toml:"password"`
	QueueName string          `toml:"queueName"`
	Log       telegraf.Logger `toml:"-"`

	HeartBeatSend config.Duration `toml:"heartbeat_timeout_send"`
	HeartBeatRec  config.Duration `toml:"heartbeat_timeout_receive"`

	common_tls.ClientConfig

	conn  net.Conn
	stomp *stomp.Conn

	serialize telegraf.Serializer
}

func (q *STOMP) Connect() error {
	tlsConfig, err := q.ClientConfig.TLSConfig()
	if err != nil {
		return err
	}
	if tlsConfig != nil {
		q.conn, err = tls.Dial("tcp", q.Host, tlsConfig)
		if err != nil {
			return err
		}
	} else {
		q.conn, err = net.Dial("tcp", q.Host)
		if err != nil {
			return err
		}
	}

	authOption, err := q.getAuthOption()
	if err != nil {
		return err
	}
	heartbeatOption := stomp.ConnOpt.HeartBeat(
		time.Duration(q.HeartBeatSend),
		time.Duration(q.HeartBeatRec),
	)
	q.stomp, err = stomp.Connect(q.conn, heartbeatOption, authOption)
	if err != nil {
		return err
	}
	q.Log.Debug("STOMP Connected...")
	return nil
}

func (q *STOMP) SetSerializer(serializer telegraf.Serializer) {
	q.serialize = serializer
}

func (q *STOMP) Write(metrics []telegraf.Metric) error {
	for _, metric := range metrics {
		values, err := q.serialize.Serialize(metric)
		if err != nil {
			q.Log.Errorf("Serializing metric %v failed: %s", metric, err)
			continue
		}
		err = q.stomp.Send(q.QueueName, "text/plain", values, nil)
		if err != nil {
			return fmt.Errorf("sending metric failed: %w", err)
		}
	}
	return nil
}
func (q *STOMP) SampleConfig() string {
	return sampleConfig
}
func (q *STOMP) Close() error {
	return q.stomp.Disconnect()
}

func (q *STOMP) getAuthOption() (func(*stomp.Conn) error, error) {
	username, err := q.Username.Get()
	if err != nil {
		return nil, fmt.Errorf("getting username failed: %w", err)
	}
	defer username.Destroy()
	password, err := q.Password.Get()
	if err != nil {
		return nil, fmt.Errorf("getting password failed: %w", err)
	}
	defer password.Destroy()
	return stomp.ConnOpt.Login(username.String(), password.String()), nil
}

func init() {
	outputs.Add("stomp", func() telegraf.Output {
		return &STOMP{
			Host:      "localhost:61613",
			QueueName: "telegraf",
		}
	})
}
