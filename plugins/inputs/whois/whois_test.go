package whois

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/likexian/whois-parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	// "github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/testutil"
)

// Make sure Whois implements telegraf.Input
var _ telegraf.Input = &Whois{}

func TestParseDate(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2025-08-13", "2025-08-13"},
		{"2025-08-13 04:00:00", "2025-08-13"},
		{"2025-08-13T04:00:00Z", "2025-08-13"},
		{"06-Aug-2025", "2025-08-06"},
		{"06/08/2025", "2025-08-06"},
		{"August 6, 2025", "2025-08-06"},
		{"Mon Aug 6 23:59:29 UTC 2025", "2025-08-06"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsedTime, err := parseDateString(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, parsedTime.Format("2006-01-02"))
		})
	}
}

func TestSimplifyStatus(t *testing.T) {
	tests := []struct {
		input    []string
		expected string
	}{
		{[]string{"clientTransferProhibited"}, "LOCKED"},
		{[]string{"pendingDelete"}, "PENDING DELETE"},
		{[]string{"redemptionPeriod"}, "EXPIRED"},
		{[]string{"active"}, "ACTIVE"},
		{[]string{"registered"}, "REGISTERED"},
		{[]string{"unknownStatus"}, "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := simplifyStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWhoisConfigInitialization(t *testing.T) {
	tests := []struct {
		name      string
		domains   []string
		timeout   config.Duration
		expectErr bool
	}{
		{
			name:      "Valid Configuration",
			domains:   []string{"example.com", "google.com"},
			timeout:   config.Duration(10 * time.Second),
			expectErr: false,
		},
		{
			name:      "No Domains Configured",
			domains:   nil,
			timeout:   config.Duration(5 * time.Second),
			expectErr: true,
		},
		{
			name:      "Invalid Timeout (Zero Value)",
			domains:   []string{"example.com"},
			timeout:   config.Duration(0),
			expectErr: false, // Should still work, default timeout can be applied
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plugin := &Whois{
				Domains: test.domains,
				Timeout: test.timeout,
				Log:     testutil.Logger{},
			}

			err := plugin.Gather(&testutil.Accumulator{})

			if test.expectErr {
				require.Error(t, err, "Expected error but got none")
			} else {
				require.NoError(t, err, "Unexpected error during configuration setup")
			}
		})
	}
}

func TestWhoisGatherStaticMockResponses(t *testing.T) {
	plugin := &Whois{
		Domains: []string{"example.com"},
	}

	plugin.Log = testutil.Logger{}

	acc := &testutil.Accumulator{}

	// Static mock WHOIS responses
	mockResponses := map[string]whoisparser.WhoisInfo{
		"example.com": {
			Domain: &whoisparser.Domain{
				ExpirationDate: "2025-08-13T04:00:00Z",
				Status:         []string{"clientTransferProhibited"},
			},
			Registrar: &whoisparser.Contact{
				Name: "RESERVED-Internet Assigned Numbers Authority",
			},
		},
	}

	// Mock `whoisLookup()` and `parseWhoisData()`
	originalWhois := whoisLookup
	originalParse := parseWhoisData
	defer func() {
		whoisLookup = originalWhois
		parseWhoisData = originalParse
	}()

	whoisLookup = func(domain string) (string, error) {
		return "WHOIS mock response for " + domain, nil
	}

	parseWhoisData = func(raw string) (whoisparser.WhoisInfo, error) {
		for domain, info := range mockResponses {
			if strings.Contains(raw, domain) { // Match requested domain
				return info, nil
			}
		}

		return whoisparser.WhoisInfo{}, errors.New("mock WHOIS data not found")
	}

	err := plugin.Gather(acc)
	require.NoError(t, err)

	assert.Equal(t, "example.com", acc.TagValue("whois", "domain"))
	assert.Equal(t, "LOCKED", acc.TagValue("whois", "status"))

	// Validate `expiration_timestamp` field (2025-08-13T04:00:00Z → Unix)
	expectedExpiration := float64(1755057600)
	expirationValue, found := acc.FloatField("whois", "expiration_timestamp")
	require.True(t, found, "expiration_timestamp field missing")
	assert.InDelta(t, expectedExpiration, expirationValue, 1)

	// Validate `expiry` field
	now := time.Now()
	expectedExpiry := int(expectedExpiration - float64(now.Unix()))
	expiryValue, found := acc.IntField("whois", "expiry")
	require.True(t, found, "expiry field missing")
	assert.InDelta(t, expectedExpiry, expiryValue, 10) // Allow small delta due to execution time
}

// Test WHOIS Handling for an Invalid Domain
func TestWhoisGatherInvalidDomain(t *testing.T) {
	plugin := &Whois{
		Domains: []string{"invalid-domain.xyz"},
	}

	plugin.Log = testutil.Logger{}

	acc := &testutil.Accumulator{}

	originalWhois := whoisLookup
	originalParse := parseWhoisData
	defer func() {
		whoisLookup = originalWhois
		parseWhoisData = originalParse
	}()

	whoisLookup = func(_ string) (string, error) {
		return "", errors.New("whois lookup failed")
	}

	parseWhoisData = func(_ string) (whoisparser.WhoisInfo, error) {
		return whoisparser.WhoisInfo{}, errors.New("mock WHOIS data not found")
	}

	err := plugin.Gather(acc)
	require.NoError(t, err)

	assert.Empty(t, acc.Metrics)
}
