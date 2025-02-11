//go:generate ../../../tools/readme_config_includer/generator
package webhooks

import (
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"time"

	"github.com/gorilla/mux"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/inputs/webhooks/artifactory"
	"github.com/influxdata/telegraf/plugins/inputs/webhooks/filestack"
	"github.com/influxdata/telegraf/plugins/inputs/webhooks/github"
	"github.com/influxdata/telegraf/plugins/inputs/webhooks/mandrill"
	"github.com/influxdata/telegraf/plugins/inputs/webhooks/papertrail"
	"github.com/influxdata/telegraf/plugins/inputs/webhooks/particle"
	"github.com/influxdata/telegraf/plugins/inputs/webhooks/rollbar"
)

//go:embed sample.conf
var sampleConfig string

const (
	defaultReadTimeout  = 10 * time.Second
	defaultWriteTimeout = 10 * time.Second
)

type Webhooks struct {
	ServiceAddress string          `toml:"service_address"`
	ReadTimeout    config.Duration `toml:"read_timeout"`
	WriteTimeout   config.Duration `toml:"write_timeout"`

	Artifactory *artifactory.Webhook `toml:"artifactory"`
	Filestack   *filestack.Webhook   `toml:"filestack"`
	Github      *github.Webhook      `toml:"github"`
	Mandrill    *mandrill.Webhook    `toml:"mandrill"`
	Papertrail  *papertrail.Webhook  `toml:"papertrail"`
	Particle    *particle.Webhook    `toml:"particle"`
	Rollbar     *rollbar.Webhook     `toml:"rollbar"`

	Log telegraf.Logger `toml:"-"`

	srv *http.Server
}

// Webhook is an interface that all webhooks must implement
type Webhook interface {
	// Register registers the webhook with the provided router
	Register(router *mux.Router, acc telegraf.Accumulator, log telegraf.Logger)
}

func (*Webhooks) SampleConfig() string {
	return sampleConfig
}

func (wb *Webhooks) Start(acc telegraf.Accumulator) error {
	if wb.ReadTimeout < config.Duration(time.Second) {
		wb.ReadTimeout = config.Duration(defaultReadTimeout)
	}
	if wb.WriteTimeout < config.Duration(time.Second) {
		wb.WriteTimeout = config.Duration(defaultWriteTimeout)
	}

	r := mux.NewRouter()

	for _, webhook := range wb.availableWebhooks() {
		webhook.Register(r, acc, wb.Log)
	}

	wb.srv = &http.Server{
		Handler:      r,
		ReadTimeout:  time.Duration(wb.ReadTimeout),
		WriteTimeout: time.Duration(wb.WriteTimeout),
	}

	ln, err := net.Listen("tcp", wb.ServiceAddress)
	if err != nil {
		return fmt.Errorf("error starting server: %w", err)
	}

	go func() {
		if err := wb.srv.Serve(ln); err != nil {
			if err != http.ErrServerClosed {
				acc.AddError(fmt.Errorf("error listening: %w", err))
			}
		}
	}()

	wb.Log.Infof("Started the webhooks service on %s", wb.ServiceAddress)

	return nil
}

func (*Webhooks) Gather(telegraf.Accumulator) error {
	return nil
}

func (wb *Webhooks) Stop() {
	wb.srv.Close()
	wb.Log.Infof("Stopping the Webhooks service")
}

// availableWebhooks Looks for fields which implement Webhook interface
func (wb *Webhooks) availableWebhooks() []Webhook {
	webhooks := make([]Webhook, 0)
	s := reflect.ValueOf(wb).Elem()
	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)

		if !f.CanInterface() {
			continue
		}

		if wbPlugin, ok := f.Interface().(Webhook); ok {
			if !reflect.ValueOf(wbPlugin).IsNil() {
				webhooks = append(webhooks, wbPlugin)
			}
		}
	}

	return webhooks
}

func newWebhooks() *Webhooks {
	return &Webhooks{}
}

func init() {
	inputs.Add("webhooks", func() telegraf.Input { return newWebhooks() })
}
