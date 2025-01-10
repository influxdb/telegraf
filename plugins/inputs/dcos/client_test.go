package dcos

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

var privateKey = testutil.NewPKI("../../../testutil/pki").ReadServerKey()

func TestLogin(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	var tests = []struct {
		name          string
		responseCode  int
		responseBody  string
		expectedError error
		expectedToken string
	}{
		{
			name:          "Login successful",
			responseCode:  http.StatusOK,
			responseBody:  `{"token": "XXX.YYY.ZZZ"}`,
			expectedError: nil,
			expectedToken: "XXX.YYY.ZZZ",
		},
		{
			name:         "Unauthorized Error",
			responseCode: http.StatusUnauthorized,
			responseBody: `{"title": "x", "description": "y"}`,
			expectedError: &apiError{
				url:         ts.URL + "/acs/api/v1/auth/login",
				statusCode:  http.StatusUnauthorized,
				title:       "x",
				description: "y",
			},
			expectedToken: "",
		},
	}

	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privateKey))
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.responseCode)
				fmt.Fprintln(w, tt.responseBody)
			})

			u, err := url.Parse(ts.URL)
			require.NoError(t, err)

			ctx := context.Background()
			sa := &serviceAccount{
				accountID:  "telegraf",
				privateKey: key,
			}
			client := newClusterClient(u, defaultResponseTimeout, 1, nil)
			auth, err := client.login(ctx, sa)

			require.Equal(t, tt.expectedError, err)

			if tt.expectedToken != "" {
				require.Equal(t, tt.expectedToken, auth.Text)
			} else {
				require.Nil(t, auth)
			}
		})
	}
}

func TestGetSummary(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	var tests = []struct {
		name          string
		responseCode  int
		responseBody  string
		expectedValue *summary
		expectedError error
	}{
		{
			name:          "No nodes",
			responseCode:  http.StatusOK,
			responseBody:  `{"cluster": "a", "slaves": []}`,
			expectedValue: &summary{Cluster: "a", Slaves: make([]slave, 0)},
			expectedError: nil,
		},
		{
			name:          "Unauthorized Error",
			responseCode:  http.StatusUnauthorized,
			responseBody:  `<html></html>`,
			expectedValue: nil,
			expectedError: &apiError{
				url:        ts.URL + "/mesos/master/state-summary",
				statusCode: http.StatusUnauthorized,
				title:      "401 Unauthorized",
			},
		},
		{
			name:         "Has nodes",
			responseCode: http.StatusOK,
			responseBody: `{"cluster": "a", "slaves": [{"id": "a"}, {"id": "b"}]}`,
			expectedValue: &summary{
				Cluster: "a",
				Slaves: []slave{
					{ID: "a"},
					{ID: "b"},
				},
			},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// check the path
				w.WriteHeader(tt.responseCode)
				fmt.Fprintln(w, tt.responseBody)
			})

			u, err := url.Parse(ts.URL)
			require.NoError(t, err)

			ctx := context.Background()
			client := newClusterClient(u, defaultResponseTimeout, 1, nil)
			summary, err := client.getSummary(ctx)

			require.Equal(t, tt.expectedError, err)
			require.Equal(t, tt.expectedValue, summary)
		})
	}
}

func TestGetNodeMetrics(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	var tests = []struct {
		name          string
		responseCode  int
		responseBody  string
		expectedValue *metrics
		expectedError error
	}{
		{
			name:          "Empty Body",
			responseCode:  http.StatusOK,
			responseBody:  `{}`,
			expectedValue: &metrics{},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// check the path
				w.WriteHeader(tt.responseCode)
				fmt.Fprintln(w, tt.responseBody)
			})

			u, err := url.Parse(ts.URL)
			require.NoError(t, err)

			ctx := context.Background()
			client := newClusterClient(u, defaultResponseTimeout, 1, nil)
			m, err := client.getNodeMetrics(ctx, "foo")

			require.Equal(t, tt.expectedError, err)
			require.Equal(t, tt.expectedValue, m)
		})
	}
}

func TestGetContainerMetrics(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	var tests = []struct {
		name          string
		responseCode  int
		responseBody  string
		expectedValue *metrics
		expectedError error
	}{
		{
			name:          "204 No Content",
			responseCode:  http.StatusNoContent,
			responseBody:  ``,
			expectedValue: &metrics{},
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// check the path
				w.WriteHeader(tt.responseCode)
				fmt.Fprintln(w, tt.responseBody)
			})

			u, err := url.Parse(ts.URL)
			require.NoError(t, err)

			ctx := context.Background()
			client := newClusterClient(u, defaultResponseTimeout, 1, nil)
			m, err := client.getContainerMetrics(ctx, "foo", "bar")

			require.Equal(t, tt.expectedError, err)
			require.Equal(t, tt.expectedValue, m)
		})
	}
}
