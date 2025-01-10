//go:generate ../../../tools/readme_config_includer/generator
package tacacs

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/nwaples/tacplus"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
)

type Tacacs struct {
	Servers         []string        `toml:"servers"`
	Username        config.Secret   `toml:"username"`
	Password        config.Secret   `toml:"password"`
	Secret          config.Secret   `toml:"secret"`
	RequestAddr     string          `toml:"request_ip"`
	ResponseTimeout config.Duration `toml:"response_timeout"`
	Log             telegraf.Logger `toml:"-"`
	clients         []tacplus.Client
	authStart       tacplus.AuthenStart
}

//go:embed sample.conf
var sampleConfig string

func (*Tacacs) SampleConfig() string {
	return sampleConfig
}

func (t *Tacacs) Init() error {
	if len(t.Servers) == 0 {
		t.Servers = []string{"127.0.0.1:49"}
	}

	if t.Username.Empty() || t.Password.Empty() || t.Secret.Empty() {
		return errors.New("empty credentials were provided (username, password or secret)")
	}

	if t.RequestAddr == "" {
		t.RequestAddr = "127.0.0.1"
	}
	if net.ParseIP(t.RequestAddr) == nil {
		return fmt.Errorf("invalid ip address provided for request_ip: %s", t.RequestAddr)
	}

	t.clients = make([]tacplus.Client, 0, len(t.Servers))
	for _, server := range t.Servers {
		t.clients = append(t.clients, tacplus.Client{
			Addr:       server,
			ConnConfig: tacplus.ConnConfig{},
		})
	}

	t.authStart = tacplus.AuthenStart{
		Action:        tacplus.AuthenActionLogin,
		AuthenType:    tacplus.AuthenTypeASCII,
		AuthenService: tacplus.AuthenServiceLogin,
		PrivLvl:       1,
		Port:          "heartbeat",
		RemAddr:       t.RequestAddr,
	}

	return nil
}

func AuthenReplyToString(code uint8) string {
	switch code {
	case tacplus.AuthenStatusPass:
		return `AuthenStatusPass`
	case tacplus.AuthenStatusFail:
		return `AuthenStatusFail`
	case tacplus.AuthenStatusGetData:
		return `AuthenStatusGetData`
	case tacplus.AuthenStatusGetUser:
		return `AuthenStatusGetUser`
	case tacplus.AuthenStatusGetPass:
		return `AuthenStatusGetPass`
	case tacplus.AuthenStatusRestart:
		return `AuthenStatusRestart`
	case tacplus.AuthenStatusError:
		return `AuthenStatusError`
	case tacplus.AuthenStatusFollow:
		return `AuthenStatusFollow`
	}
	return "AuthenStatusUnknown(" + strconv.FormatUint(uint64(code), 10) + ")"
}

func (t *Tacacs) Gather(acc telegraf.Accumulator) error {
	var wg sync.WaitGroup

	for idx := range t.clients {
		wg.Add(1)
		go func(client *tacplus.Client) {
			defer wg.Done()
			acc.AddError(t.pollServer(acc, client))
		}(&t.clients[idx])
	}

	wg.Wait()
	return nil
}

func (t *Tacacs) pollServer(acc telegraf.Accumulator, client *tacplus.Client) error {
	// Create the fields for this metric
	tags := map[string]string{"source": client.Addr}
	fields := make(map[string]interface{})

	secret, err := t.Secret.Get()
	if err != nil {
		return fmt.Errorf("getting secret failed: %w", err)
	}
	defer secret.Destroy()

	client.ConnConfig.Secret = secret.Bytes()

	username, err := t.Username.Get()
	if err != nil {
		return fmt.Errorf("getting username failed: %w", err)
	}
	defer username.Destroy()

	password, err := t.Password.Get()
	if err != nil {
		return fmt.Errorf("getting password failed: %w", err)
	}
	defer password.Destroy()

	ctx := context.Background()
	if t.ResponseTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(t.ResponseTimeout))
		defer cancel()
	}

	startTime := time.Now()
	reply, session, err := client.SendAuthenStart(ctx, &t.authStart)
	if err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, os.ErrDeadlineExceeded) {
			return fmt.Errorf("error on new tacacs authentication start request to %s : %w", client.Addr, err)
		}
		fields["responsetime_ms"] = time.Since(startTime).Milliseconds()
		fields["response_status"] = "Timeout"
		acc.AddFields("tacacs", fields, tags)
		return nil
	}
	defer session.Close()
	if reply.Status != tacplus.AuthenStatusGetUser {
		fields["responsetime_ms"] = time.Since(startTime).Milliseconds()
		fields["response_status"] = AuthenReplyToString(reply.Status)
		acc.AddFields("tacacs", fields, tags)
		return nil
	}

	reply, err = session.Continue(ctx, username.String())
	if err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, os.ErrDeadlineExceeded) {
			return fmt.Errorf("error on tacacs authentication continue username request to %s : %w", client.Addr, err)
		}
		fields["responsetime_ms"] = time.Since(startTime).Milliseconds()
		fields["response_status"] = "Timeout"
		acc.AddFields("tacacs", fields, tags)
		return nil
	}
	if reply.Status != tacplus.AuthenStatusGetPass {
		fields["responsetime_ms"] = time.Since(startTime).Milliseconds()
		fields["response_status"] = AuthenReplyToString(reply.Status)
		acc.AddFields("tacacs", fields, tags)
		return nil
	}

	reply, err = session.Continue(ctx, password.String())
	if err != nil {
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, os.ErrDeadlineExceeded) {
			return fmt.Errorf("error on tacacs authentication continue password request to %s : %w", client.Addr, err)
		}
		fields["responsetime_ms"] = time.Since(startTime).Milliseconds()
		fields["response_status"] = "Timeout"
		acc.AddFields("tacacs", fields, tags)
		return nil
	}
	if reply.Status != tacplus.AuthenStatusPass {
		fields["responsetime_ms"] = time.Since(startTime).Milliseconds()
		fields["response_status"] = AuthenReplyToString(reply.Status)
		acc.AddFields("tacacs", fields, tags)
		return nil
	}

	fields["responsetime_ms"] = time.Since(startTime).Milliseconds()
	fields["response_status"] = AuthenReplyToString(reply.Status)
	acc.AddFields("tacacs", fields, tags)
	return nil
}

func init() {
	inputs.Add("tacacs", func() telegraf.Input {
		return &Tacacs{ResponseTimeout: config.Duration(time.Second * 5)}
	})
}
