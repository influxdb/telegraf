package tacacs

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/nwaples/tacplus"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

type testRequestHandler map[string]struct {
	password string
	args     []string
}

func (t testRequestHandler) HandleAuthenStart(_ context.Context, a *tacplus.AuthenStart, s *tacplus.ServerSession) *tacplus.AuthenReply {
	user := a.User
	for user == "" {
		c, err := s.GetUser(context.Background(), "Username:")
		if err != nil || c.Abort {
			return nil
		}
		user = c.Message
	}
	pass := ""
	for pass == "" {
		c, err := s.GetPass(context.Background(), "Password:")
		if err != nil || c.Abort {
			return nil
		}
		pass = c.Message
	}
	if u, ok := t[user]; ok && u.password == pass {
		return &tacplus.AuthenReply{Status: tacplus.AuthenStatusPass}
	}
	return &tacplus.AuthenReply{Status: tacplus.AuthenStatusFail}
}

func (t testRequestHandler) HandleAuthorRequest(_ context.Context, a *tacplus.AuthorRequest, _ *tacplus.ServerSession) *tacplus.AuthorResponse {
	if u, ok := t[a.User]; ok {
		return &tacplus.AuthorResponse{Status: tacplus.AuthorStatusPassAdd, Arg: u.args}
	}
	return &tacplus.AuthorResponse{Status: tacplus.AuthorStatusFail}
}

func (t testRequestHandler) HandleAcctRequest(_ context.Context, _ *tacplus.AcctRequest, _ *tacplus.ServerSession) *tacplus.AcctReply {
	return &tacplus.AcctReply{Status: tacplus.AcctStatusSuccess}
}

func TestTacacsLocal(t *testing.T) {
	testHandler := tacplus.ServerConnHandler{
		Handler: &testRequestHandler{
			"testusername": {
				password: "testpassword",
			},
		},
		ConnConfig: tacplus.ConnConfig{
			Secret: []byte(`testsecret`),
			Mux:    true,
		},
	}
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err, "local net listen failed to start listening")

	srvLocal := l.Addr().String()

	srv := &tacplus.Server{
		ServeConn: func(nc net.Conn) {
			testHandler.Serve(nc)
		},
	}

	go func() {
		err = srv.Serve(l)
		require.NoError(t, err, "local srv.Serve failed to start serving on "+srvLocal)
	}()

	// Define the testset
	var testset = []struct {
		name                 string
		testingTimeout       config.Duration
		serverToTest         []string
		usedUsername         config.Secret
		usedPassword         config.Secret
		usedSecret           config.Secret
		requestAddr          string
		expectNoGatherErrors bool
		expectNoInitErrors   bool
	}{
		{
			name:                 "success_timeout_0s",
			testingTimeout:       config.Duration(0),
			serverToTest:         []string{srvLocal},
			usedUsername:         config.NewSecret([]byte(`testusername`)),
			usedPassword:         config.NewSecret([]byte(`testpassword`)),
			usedSecret:           config.NewSecret([]byte(`testsecret`)),
			requestAddr:          "127.0.0.1",
			expectNoGatherErrors: true,
			expectNoInitErrors:   true,
		},
		{
			name:                 "wrongpw",
			testingTimeout:       config.Duration(time.Second * 5),
			serverToTest:         []string{srvLocal},
			usedUsername:         config.NewSecret([]byte(`testusername`)),
			usedPassword:         config.NewSecret([]byte(`WRONGPASSWORD`)),
			usedSecret:           config.NewSecret([]byte(`testsecret`)),
			requestAddr:          "127.0.0.1",
			expectNoGatherErrors: true,
			expectNoInitErrors:   true,
		},
		{
			name:                 "wrongsecret",
			testingTimeout:       config.Duration(time.Second * 5),
			serverToTest:         []string{srvLocal},
			usedUsername:         config.NewSecret([]byte(`testusername`)),
			usedPassword:         config.NewSecret([]byte(`testpassword`)),
			usedSecret:           config.NewSecret([]byte(`WRONGSECRET`)),
			requestAddr:          "127.0.0.1",
			expectNoGatherErrors: false,
			expectNoInitErrors:   true,
		},
		{
			name:                 "unreachable",
			testingTimeout:       config.Duration(time.Nanosecond * 1000),
			serverToTest:         []string{"unreachable.hostname.com:404"},
			usedUsername:         config.NewSecret([]byte(`testusername`)),
			usedPassword:         config.NewSecret([]byte(`testpassword`)),
			usedSecret:           config.NewSecret([]byte(`testsecret`)),
			requestAddr:          "127.0.0.1",
			expectNoGatherErrors: false,
			expectNoInitErrors:   true,
		},
		{
			name:                 "empty_creds",
			testingTimeout:       config.Duration(time.Second * 5),
			serverToTest:         []string{srvLocal},
			usedUsername:         config.NewSecret([]byte(``)),
			usedPassword:         config.NewSecret([]byte(`testpassword`)),
			usedSecret:           config.NewSecret([]byte(`testsecret`)),
			requestAddr:          "127.0.0.1",
			expectNoGatherErrors: false,
			expectNoInitErrors:   false,
		},
		{
			name:                 "wrong_reqaddress",
			testingTimeout:       config.Duration(time.Second * 5),
			serverToTest:         []string{srvLocal},
			usedUsername:         config.NewSecret([]byte(`testusername`)),
			usedPassword:         config.NewSecret([]byte(`testpassword`)),
			usedSecret:           config.NewSecret([]byte(`testsecret`)),
			requestAddr:          "257.257.257.257",
			expectNoGatherErrors: false,
			expectNoInitErrors:   false,
		},
		{
			name:                 "no_reqaddress",
			testingTimeout:       config.Duration(time.Second * 5),
			serverToTest:         []string{srvLocal},
			usedUsername:         config.NewSecret([]byte(`testusername`)),
			usedPassword:         config.NewSecret([]byte(`testpassword`)),
			usedSecret:           config.NewSecret([]byte(`testsecret`)),
			expectNoGatherErrors: true,
			expectNoInitErrors:   true,
		},
	}

	for _, tt := range testset {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Tacacs{
				ResponseTimeout: tt.testingTimeout,
				Servers:         tt.serverToTest,
				Username:        tt.usedUsername,
				Password:        tt.usedPassword,
				Secret:          tt.usedSecret,
				RequestAddr:     tt.requestAddr,
				Log:             testutil.Logger{},
			}

			var acc testutil.Accumulator

			if tt.expectNoInitErrors {
				require.NoError(t, plugin.Init())
				require.NoError(t, plugin.Gather(&acc))
			} else {
				initErr := plugin.Init()
				require.Error(t, initErr)
				if tt.name == "empty_creds" {
					require.ErrorContains(t, initErr, "empty credentials were provided (username, password or secret)")
				}
				if tt.name == "wrong_reqaddress" {
					require.ErrorContains(t, initErr, "invalid ip address provided for request_ip")
				}
			}

			if tt.expectNoGatherErrors {
				require.Len(t, acc.Errors, 0)
				if !acc.HasMeasurement("tacacs") {
					t.Errorf("acc.HasMeasurement: expected tacacs")
				}
				require.Equal(t, true, acc.HasTag("tacacs", "source"))
				require.Equal(t, srvLocal, acc.TagValue("tacacs", "source"))
				require.Equal(t, true, acc.HasInt64Field("tacacs", "responsetime_ms"))
				require.Equal(t, true, acc.HasTag("tacacs", "response_code"))
			} else {
				if tt.expectNoInitErrors {
					require.Len(t, acc.Errors, 1)
				}
				require.Equal(t, false, acc.HasTag("tacacs", "source"))
				require.Equal(t, false, acc.HasInt64Field("tacacs", "responsetime_ms"))
				require.Equal(t, false, acc.HasTag("tacacs", "response_code"))
			}
			if tt.name == "success_timeout_0s" || tt.name == "no_reqaddress" {
				require.Equal(t, strconv.FormatUint(uint64(tacplus.AuthenStatusPass), 10), acc.TagValue("tacacs", "response_code"))
			}
			if tt.name == "wrongpw" {
				require.Equal(t, strconv.FormatUint(uint64(tacplus.AuthenStatusFail), 10), acc.TagValue("tacacs", "response_code"))
				require.Equal(t, time.Duration(plugin.ResponseTimeout).Milliseconds(), acc.Metrics[0].Fields["responsetime_ms"])
			}
			if tt.name == "wrongsecret" {
				require.ErrorContains(t, acc.Errors[0], "error on new tacacs authentication start request to "+srvLocal+" : bad secret or packet")
			}
			if tt.name == "unreachable" {
				require.ErrorContains(t, acc.Errors[0], "error on new tacacs authentication start request to unreachable.hostname.com:404 : dial tcp")
			}
			if tt.name == "no_reqaddress" {
				require.Equal(t, "127.0.0.1", plugin.RequestAddr)
			}
			if tt.name == "no_timeout" {
				require.Equal(t, config.Duration(time.Second*5), plugin.ResponseTimeout)
			}
		})
	}
}

func TestTacacsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	container := testutil.Container{
		Image:        "dchidell/docker-tacacs",
		ExposedPorts: []string{"49/tcp"},
		WaitingFor: wait.ForAll(
			wait.ForLog("Starting server..."),
		),
	}
	err := container.Start()
	require.NoError(t, err, "failed to start container")
	defer container.Terminate()

	port := container.Ports["49"]

	// Define the testset
	var testset = []struct {
		name           string
		testingTimeout config.Duration
		serverToTest   string
		expectSuccess  bool
		usedPassword   string
	}{
		{
			name:           "timeout_3s",
			testingTimeout: config.Duration(time.Second * 3),
			serverToTest:   container.Address + ":" + port,
			expectSuccess:  true,
			usedPassword:   "cisco",
		},
		{
			name:           "timeout_0s",
			testingTimeout: config.Duration(0),
			serverToTest:   container.Address + ":" + port,
			expectSuccess:  true,
			usedPassword:   "cisco",
		},
		{
			name:           "wrong_pw",
			testingTimeout: config.Duration(time.Second * 5),
			serverToTest:   container.Address + ":" + port,
			expectSuccess:  false,
			usedPassword:   "wrongpass",
		},
	}

	for _, tt := range testset {
		t.Run(tt.name, func(t *testing.T) {
			// Setup the plugin-under-test
			plugin := &Tacacs{
				ResponseTimeout: tt.testingTimeout,
				Servers:         []string{tt.serverToTest},
				Username:        config.NewSecret([]byte(`iosadmin`)),
				Password:        config.NewSecret([]byte(tt.usedPassword)),
				Secret:          config.NewSecret([]byte(`ciscotacacskey`)),
				RequestAddr:     "127.0.0.1",
				Log:             testutil.Logger{},
			}
			var acc testutil.Accumulator

			// Startup the plugin
			require.NoError(t, plugin.Init())

			// Gather
			require.NoError(t, plugin.Gather(&acc))

			if tt.expectSuccess {
				if len(acc.Errors) > 0 {
					t.Errorf("error occured in test where should be none, error was: %s", acc.Errors[0].Error())
				}
				if !acc.HasMeasurement("tacacs") {
					t.Errorf("acc.HasMeasurement: expected tacacs")
				}
				require.Equal(t, true, acc.HasTag("tacacs", "source"))
				require.Equal(t, tt.serverToTest, acc.TagValue("tacacs", "source"))
				require.Equal(t, true, acc.HasInt64Field("tacacs", "responsetime_ms"), true)
				require.Len(t, acc.Errors, 0)
			}

			if tt.name == "wrong_pw" {
				require.Len(t, acc.Errors, 0)
				require.Equal(t, true, acc.HasTag("tacacs", "response_code"))
				require.Equal(t, strconv.FormatUint(uint64(tacplus.AuthenStatusFail), 10), acc.TagValue("tacacs", "response_code"))
				require.Equal(t, true, acc.HasTag("tacacs", "source"))
				require.Equal(t, tt.serverToTest, acc.TagValue("tacacs", "source"))
				require.Equal(t, true, acc.HasInt64Field("tacacs", "responsetime_ms"))
				require.Equal(t, time.Duration(plugin.ResponseTimeout).Milliseconds(), acc.Metrics[0].Fields["responsetime_ms"])
			}
		})
	}
}
