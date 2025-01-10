package cookie

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	clockutil "github.com/benbjohnson/clock"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/testutil"
)

const (
	reqUser      = "testUser"
	reqPasswd    = "testPassword"
	reqBody      = "a body"
	reqHeaderKey = "hello"
	reqHeaderVal = "world"

	authEndpointNoCreds                   = "/auth"
	authEndpointWithBasicAuth             = "/authWithCreds"
	authEndpointWithBasicAuthOnlyUsername = "/authWithCredsUser"
	authEndpointWithBody                  = "/authWithBody"
	authEndpointWithHeader                = "/authWithHeader"
)

var fakeCookie = &http.Cookie{
	Name:  "test-cookie",
	Value: "this is an auth cookie",
}

var reqHeaderValSecret = config.NewSecret([]byte(reqHeaderVal))

type fakeServer struct {
	*httptest.Server
	*int32
}

func newFakeServer(t *testing.T) fakeServer {
	var c int32
	return fakeServer{
		Server: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authed := func() {
				atomic.AddInt32(&c, 1)        // increment auth counter
				http.SetCookie(w, fakeCookie) // set fake cookie
			}
			switch r.URL.Path {
			case authEndpointNoCreds:
				authed()
			case authEndpointWithHeader:
				if !cmp.Equal(r.Header.Get(reqHeaderKey), reqHeaderVal) {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				authed()
			case authEndpointWithBody:
				body, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
				if !cmp.Equal([]byte(reqBody), body) {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				authed()
			case authEndpointWithBasicAuth:
				u, p, ok := r.BasicAuth()
				if !ok || u != reqUser || p != reqPasswd {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				authed()
			case authEndpointWithBasicAuthOnlyUsername:
				u, p, ok := r.BasicAuth()
				if !ok || u != reqUser || p != "" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				authed()
			default:
				// ensure cookie exists on request
				if _, err := r.Cookie(fakeCookie.Name); err != nil {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				if _, err := w.Write([]byte("good test response")); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					t.Error(err)
					return
				}
			}
		})),
		int32: &c,
	}
}

func (s fakeServer) checkResp(t *testing.T, expCode int) {
	t.Helper()
	resp, err := s.Client().Get(s.URL + "/endpoint")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, expCode, resp.StatusCode)

	if expCode == http.StatusOK {
		require.Len(t, resp.Request.Cookies(), 1)
		require.Equal(t, "test-cookie", resp.Request.Cookies()[0].Name)
	}
}

func (s fakeServer) checkAuthCount(t *testing.T, atLeast int32) {
	t.Helper()
	require.GreaterOrEqual(t, atomic.LoadInt32(s.int32), atLeast)
}

func TestAuthConfig_Start(t *testing.T) {
	const (
		renewal      = 50 * time.Millisecond
		renewalCheck = 5 * renewal
	)
	type fields struct {
		Method   string
		Username string
		Password string
		Body     string
		Headers  map[string]*config.Secret
	}
	type args struct {
		renewal  time.Duration
		endpoint string
	}
	tests := []struct {
		name              string
		fields            fields
		args              args
		wantErr           error
		firstAuthCount    int32
		lastAuthCount     int32
		firstHTTPResponse int
		lastHTTPResponse  int
	}{
		{
			name: "success no creds, no body, default method",
			args: args{
				renewal:  renewal,
				endpoint: authEndpointNoCreds,
			},
			firstAuthCount:    1,
			lastAuthCount:     3,
			firstHTTPResponse: http.StatusOK,
			lastHTTPResponse:  http.StatusOK,
		},
		{
			name: "success no creds, no body, default method, header set",
			args: args{
				renewal:  renewal,
				endpoint: authEndpointWithHeader,
			},
			fields: fields{
				Headers: map[string]*config.Secret{reqHeaderKey: &reqHeaderValSecret},
			},
			firstAuthCount:    1,
			lastAuthCount:     3,
			firstHTTPResponse: http.StatusOK,
			lastHTTPResponse:  http.StatusOK,
		},
		{
			name: "success with creds, no body",
			fields: fields{
				Method:   http.MethodPost,
				Username: reqUser,
				Password: reqPasswd,
			},
			args: args{
				renewal:  renewal,
				endpoint: authEndpointWithBasicAuth,
			},
			firstAuthCount:    1,
			lastAuthCount:     3,
			firstHTTPResponse: http.StatusOK,
			lastHTTPResponse:  http.StatusOK,
		},
		{
			name: "failure with bad creds",
			fields: fields{
				Method:   http.MethodPost,
				Username: reqUser,
				Password: "a bad password",
			},
			args: args{
				renewal:  renewal,
				endpoint: authEndpointWithBasicAuth,
			},
			wantErr:           errors.New("cookie auth renewal received status code: 401 (Unauthorized) []"),
			firstAuthCount:    0,
			lastAuthCount:     0,
			firstHTTPResponse: http.StatusForbidden,
			lastHTTPResponse:  http.StatusForbidden,
		},
		{
			name: "success with no creds, with good body",
			fields: fields{
				Method: http.MethodPost,
				Body:   reqBody,
			},
			args: args{
				renewal:  renewal,
				endpoint: authEndpointWithBody,
			},
			firstAuthCount:    1,
			lastAuthCount:     3,
			firstHTTPResponse: http.StatusOK,
			lastHTTPResponse:  http.StatusOK,
		},
		{
			name: "failure with bad body",
			fields: fields{
				Method: http.MethodPost,
				Body:   "a bad body",
			},
			args: args{
				renewal:  renewal,
				endpoint: authEndpointWithBody,
			},
			wantErr:           errors.New("cookie auth renewal received status code: 401 (Unauthorized) []"),
			firstAuthCount:    0,
			lastAuthCount:     0,
			firstHTTPResponse: http.StatusForbidden,
			lastHTTPResponse:  http.StatusForbidden,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newFakeServer(t)
			c := &CookieAuthConfig{
				URL:      srv.URL + tt.args.endpoint,
				Method:   tt.fields.Method,
				Username: tt.fields.Username,
				Password: tt.fields.Password,
				Body:     tt.fields.Body,
				Headers:  tt.fields.Headers,
				Renewal:  config.Duration(tt.args.renewal),
			}
			if err := c.initializeClient(srv.Client()); tt.wantErr != nil {
				require.EqualError(t, err, tt.wantErr.Error())
			} else {
				require.NoError(t, err)
			}
			mock := clockutil.NewMock()
			ticker := mock.Ticker(time.Duration(c.Renewal))
			defer ticker.Stop()

			c.wg.Add(1)
			ctx, cancel := context.WithCancel(context.Background())
			go c.authRenewal(ctx, ticker, testutil.Logger{Name: "cookie_auth"})

			srv.checkAuthCount(t, tt.firstAuthCount)
			srv.checkResp(t, tt.firstHTTPResponse)
			mock.Add(renewalCheck)

			// Ensure that the auth renewal goroutine has completed
			require.Eventually(t, func() bool { return atomic.LoadInt32(srv.int32) >= tt.lastAuthCount }, time.Second, 10*time.Millisecond)

			cancel()
			c.wg.Wait()
			srv.checkAuthCount(t, tt.lastAuthCount)
			srv.checkResp(t, tt.lastHTTPResponse)

			srv.Close()
		})
	}
}
