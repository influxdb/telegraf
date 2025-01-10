//go:generate ../../../tools/readme_config_includer/generator
package openldap

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-ldap/ldap/v3"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/common/tls"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	searchBase    = "cn=Monitor"
	searchFilter  = "(|(objectClass=monitorCounterObject)(objectClass=monitorOperation)(objectClass=monitoredObject))"
	searchAttrs   = []string{"monitorCounter", "monitorOpInitiated", "monitorOpCompleted", "monitoredInfo"}
	attrTranslate = map[string]string{
		"monitorCounter":     "",
		"monitoredInfo":      "",
		"monitorOpInitiated": "_initiated",
		"monitorOpCompleted": "_completed",
		"olmMDBPagesMax":     "_mdb_pages_max",
		"olmMDBPagesUsed":    "_mdb_pages_used",
		"olmMDBPagesFree":    "_mdb_pages_free",
		"olmMDBReadersMax":   "_mdb_readers_max",
		"olmMDBReadersUsed":  "_mdb_readers_used",
		"olmMDBEntries":      "_mdb_entries",
	}
)

type Openldap struct {
	Host               string `toml:"host"`
	Port               int    `toml:"port"`
	SSL                string `toml:"ssl" deprecated:"1.7.0;1.35.0;use 'tls' instead"`
	TLS                string `toml:"tls"`
	InsecureSkipVerify bool   `toml:"insecure_skip_verify"`
	SSLCA              string `toml:"ssl_ca" deprecated:"1.7.0;1.35.0;use 'tls_ca' instead"`
	TLSCA              string `toml:"tls_ca"`
	BindDn             string `toml:"bind_dn"`
	BindPassword       string `toml:"bind_password"`
	ReverseMetricNames bool   `toml:"reverse_metric_names"`
}

func (*Openldap) SampleConfig() string {
	return sampleConfig
}

func (o *Openldap) Gather(acc telegraf.Accumulator) error {
	if o.TLS == "" {
		o.TLS = o.SSL
	}
	if o.TLSCA == "" {
		o.TLSCA = o.SSLCA
	}

	var err error
	var l *ldap.Conn
	if o.TLS != "" {
		// build tls config
		clientTLSConfig := tls.ClientConfig{
			TLSCA:              o.TLSCA,
			InsecureSkipVerify: o.InsecureSkipVerify,
		}
		tlsConfig, err := clientTLSConfig.TLSConfig()
		if err != nil {
			acc.AddError(err)
			return nil
		}

		switch o.TLS {
		case "ldaps":
			l, err = ldap.DialURL(fmt.Sprintf("ldaps://%s:%d", o.Host, o.Port), ldap.DialWithTLSConfig(tlsConfig))
			if err != nil {
				acc.AddError(err)
				return nil
			}
		case "starttls":
			l, err = ldap.DialURL(fmt.Sprintf("ldap://%s:%d", o.Host, o.Port))
			if err != nil {
				acc.AddError(err)
				return nil
			}
			err = l.StartTLS(tlsConfig)
			if err != nil {
				acc.AddError(err)
				return nil
			}
		default:
			acc.AddError(fmt.Errorf("invalid setting for ssl: %s", o.TLS))
			return nil
		}
	} else {
		l, err = ldap.DialURL(fmt.Sprintf("ldap://%s:%d", o.Host, o.Port))
	}

	if err != nil {
		acc.AddError(err)
		return nil
	}
	defer l.Close()

	// username/password bind
	if o.BindDn != "" && o.BindPassword != "" {
		err = l.Bind(o.BindDn, o.BindPassword)
		if err != nil {
			acc.AddError(err)
			return nil
		}
	}

	searchRequest := ldap.NewSearchRequest(
		searchBase,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		searchFilter,
		searchAttrs,
		nil,
	)

	sr, err := l.Search(searchRequest)
	if err != nil {
		acc.AddError(err)
		return nil
	}

	gatherSearchResult(sr, o, acc)

	return nil
}

func gatherSearchResult(sr *ldap.SearchResult, o *Openldap, acc telegraf.Accumulator) {
	fields := make(map[string]interface{})
	tags := map[string]string{
		"server": o.Host,
		"port":   strconv.Itoa(o.Port),
	}
	for _, entry := range sr.Entries {
		metricName := dnToMetric(entry.DN, o)
		for _, attr := range entry.Attributes {
			if len(attr.Values[0]) >= 1 {
				if v, err := strconv.ParseInt(attr.Values[0], 10, 64); err == nil {
					fields[metricName+attrTranslate[attr.Name]] = v
				}
			}
		}
	}
	acc.AddFields("openldap", fields, tags)
}

// Convert a DN to metric name, eg cn=Read,cn=Waiters,cn=Monitor becomes waiters_read
// Assumes the last part of the DN is cn=Monitor and we want to drop it
func dnToMetric(dn string, o *Openldap) string {
	if o.ReverseMetricNames {
		var metricParts []string

		dn = strings.Trim(dn, " ")
		dn = strings.ReplaceAll(dn, " ", "_")
		dn = strings.ReplaceAll(dn, "cn=", "")
		dn = strings.ToLower(dn)
		metricParts = strings.Split(dn, ",")
		for i, j := 0, len(metricParts)-1; i < j; i, j = i+1, j-1 {
			metricParts[i], metricParts[j] = metricParts[j], metricParts[i]
		}
		return strings.Join(metricParts[1:], "_")
	}

	metricName := strings.Trim(dn, " ")
	metricName = strings.ReplaceAll(metricName, " ", "_")
	metricName = strings.ToLower(metricName)
	metricName = strings.TrimPrefix(metricName, "cn=")
	metricName = strings.ReplaceAll(metricName, strings.ToLower("cn=Monitor"), "")
	metricName = strings.ReplaceAll(metricName, "cn=", "_")
	return strings.ReplaceAll(metricName, ",", "")
}

func newOpenldap() *Openldap {
	return &Openldap{
		Host:               "localhost",
		Port:               389,
		SSL:                "",
		TLS:                "",
		InsecureSkipVerify: false,
		SSLCA:              "",
		TLSCA:              "",
		BindDn:             "",
		BindPassword:       "",
		ReverseMetricNames: false,
	}
}

func init() {
	inputs.Add("openldap", func() telegraf.Input { return newOpenldap() })
}
