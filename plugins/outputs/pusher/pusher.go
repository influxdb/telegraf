package pusher

import (
	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/outputs"
	"github.com/influxdata/telegraf/plugins/serializers"
	"github.com/pusher/pusher-http-go"
)

type Pusher struct {
	AppId       string `toml:"app_id"`
	AppKey      string `toml:"app_key"`
	AppSecret   string `toml:"app_secret"`
	ChannelName string `toml:"channel_name"`

	Host string `toml:"host"`

	Secure bool `toml:"secure"`

	client *pusher.Client

	serializer serializers.Serializer
}

var sampleConfig = `
  ## Pusher Credentials
  #app_id = ""
  #app_key = ""
  #app_secret = ""
  #channel_name = ""
  secure = true
  host = "api.pusherapp.com"

  data_format = "json"
`

func (p *Pusher) SampleConfig() string {
	return sampleConfig
}

func (p *Pusher) Description() string {
	return "Configuration for Pusher output."
}

func (p *Pusher) SetSerializer(serializer serializers.Serializer) {
	p.serializer = serializer
}

func (p *Pusher) Write(metrics []telegraf.Metric) error {
	for _, m := range metrics {
		err := p.WriteSinglePoint(m)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Pusher) WriteSinglePoint(point telegraf.Metric) error {
	values, err := p.serializer.Serialize(point)

	if err != nil {
		return err
	}

	if _, err = p.client.Trigger(p.ChannelName, point.Name(), values); err != nil {
		return err
	}

	return nil
}

func (p *Pusher) Connect() error {
	client := pusher.Client{
		AppId:  p.AppId,
		Key:    p.AppKey,
		Secret: p.AppSecret,
		Secure: p.Secure,
		Host:   p.Host,
	}
	p.client = &client

	return nil
}

func (p *Pusher) Close() error {
	return nil
}

func init() {
	outputs.Add("pusher", func() telegraf.Output {
		return &Pusher{}
	})
}
