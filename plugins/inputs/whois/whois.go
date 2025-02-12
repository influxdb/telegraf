package whois

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/likexian/whois"
	"github.com/likexian/whois-parser"
)

//go:embed sample.conf
var sampleConfig string

// Whois struct holds the plugin configuration
type Whois struct {
	Domains []string        `toml:"domains"`
	Timeout config.Duration `toml:"timeout"`
	Log     telegraf.Logger `toml:"-"`
}

func (*Whois) SampleConfig() string {
	return sampleConfig
}

func (w *Whois) Gather(acc telegraf.Accumulator) error {
	if len(w.Domains) == 0 {
		w.Log.Error("No domains configured")
		return errors.New("no domains configured")
	}

	now := time.Now()

	for _, domain := range w.Domains {
		w.Log.Debugf("Fetching WHOIS data for: %s", domain)

		// Fetch WHOIS raw data
		rawWhois, err := whoisLookup(domain)
		if err != nil {
			w.Log.Errorf("WHOIS query failed for %s: %v", domain, err)
			acc.AddError(err)
			continue
		}

		w.Log.Debugf("Raw WHOIS response for %s: %s", domain, rawWhois)

		// Parse WHOIS data using whois-parser
		parsedWhois, err := parseWhoisData(rawWhois)
		if err != nil {
			w.Log.Errorf("WHOIS parsing failed for %s: %v", domain, err)
			acc.AddError(err)
			continue
		}

		w.Log.Debugf("Parsed WHOIS data for %s: %+v", domain, parsedWhois)

		// Prevent nil pointer panic
		if parsedWhois.Domain == nil {
			w.Log.Warnf("No domain info found for %s", domain)
			continue
		}

		// Extract expiration date
		expiration := parsedWhois.Domain.ExpirationDate
		if expiration == "" {
			w.Log.Warnf("No expiration date found for %s", domain)
			continue
		}

		// Try parsing expiration date
		expirationTime, err := parseDateString(expiration)
		if err != nil {
			w.Log.Warnf("Failed to parse expiration date for %s: %s", domain, expiration)
			continue
		}

		// Extract registrar name (handle nil)
		registrar := ""
		if parsedWhois.Registrar != nil {
			registrar = parsedWhois.Registrar.Name
		}

		// Extract status (handle nil)
		status := "UNKNOWN"
		if parsedWhois.Domain.Status != nil {
			status = simplifyStatus(parsedWhois.Domain.Status)
		}

		// Calculate expiry in seconds
		expiry := int(expirationTime.Sub(now).Seconds())

		// Add metrics
		fields := map[string]interface{}{
			"expiration_timestamp": float64(expirationTime.Unix()),
			"expiry":               expiry,
		}
		tags := map[string]string{
			"domain":    domain,
			"registrar": registrar,
			"status":    status,
		}
		acc.AddFields("whois", fields, tags)
	}

	return nil
}

var whoisLookup = func(domain string) (string, error) {
	return whois.Whois(domain)
}

var parseWhoisData = func(raw string) (whoisparser.WhoisInfo, error) {
	return whoisparser.Parse(raw)
}

// parseDateString attempts to parse a given date using a collection of common
// format strings. Date formats containing time components are tried first
// before attempts are made using date-only formats.
func parseDateString(datetime string) (time.Time, error) {
	datetime = strings.Trim(datetime, ".")
	datetime = strings.ReplaceAll(datetime, ". ", "-")

	formats := [...]string{
		// Date & time formats
		"2006-01-02 15:04:05",
		"2006.01.02 15:04:05",
		"02/01/2006 15:04:05",
		"02.01.2006 15:04:05",
		"02.1.2006 15:04:05",
		"2.1.2006 15:04:05",
		"02-Jan-2006 15:04:05",
		"20060102 15:04:05",
		time.ANSIC,
		time.Stamp,
		time.StampMilli,
		time.StampMicro,
		time.StampNano,

		// Date, time & time zone formats
		"2006-01-02T15:04:05Z",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05 (MST+3)",
		time.UnixDate,
		time.RubyDate,
		time.RFC822,
		time.RFC822Z,
		time.RFC850,
		time.RFC1123,
		time.RFC1123Z,
		time.RFC3339,
		time.RFC3339Nano,

		// Date only formats
		"2006-01-02",
		"02-Jan-2006",
		"02.01.2006",
		"02-01-2006",
		"January _2 2006",
		"Mon Jan _2 2006",
		"02/01/2006",
		"01/02/2006",
		"2006/01/02",
		"2006-Jan-02",
		"before Jan-2006",
		"January 2, 2006",
	}

	for i := range formats {
		format := &formats[i]
		if t, err := time.Parse(*format, datetime); err == nil {
			return t, nil
		}
	}

	return time.Now(), fmt.Errorf("could not parse %s as a date", datetime)
}

// simplifyStatus maps raw WHOIS statuses to a simpler classification
func simplifyStatus(statusList []string) string {
	for _, status := range statusList {
		s := strings.ToLower(status)

		if strings.Contains(s, "pendingdelete") {
			return "PENDING DELETE"
		}
		if strings.Contains(s, "redemptionperiod") {
			return "EXPIRED"
		}
		if strings.Contains(s, "clienttransferprohibited") || strings.Contains(s, "clientdeleteprohibited") {
			return "LOCKED"
		}
		if s == "registered" {
			return "REGISTERED"
		}
		if s == "active" {
			return "ACTIVE"
		}
	}
	return "UNKNOWN"
}

// Plugin registration
func init() {
	inputs.Add("whois", func() telegraf.Input {
		return &Whois{
			Timeout: config.Duration(5 * time.Second),
		}
	})
}
