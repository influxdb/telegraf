//go:generate ../../../tools/readme_config_includer/generator
package salesforce

import (
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	defaultVersion     = "39.0"
	defaultEnvironment = "production"
)

type Salesforce struct {
	Username      string `toml:"username"`
	Password      string `toml:"password"`
	SecurityToken string `toml:"security_token"`
	Environment   string `toml:"environment"`
	Version       string `toml:"version"`

	sessionID      string
	serverURL      *url.URL
	organizationID string

	client *http.Client
}

type limit struct {
	Max       int
	Remaining int
}

type limits map[string]limit

func (*Salesforce) SampleConfig() string {
	return sampleConfig
}

func (s *Salesforce) Gather(acc telegraf.Accumulator) error {
	limits, err := s.fetchLimits()
	if err != nil {
		return err
	}

	tags := map[string]string{
		"organization_id": s.organizationID,
		"host":            s.serverURL.Host,
	}

	fields := make(map[string]interface{})
	for k, v := range limits {
		key := internal.SnakeCase(k)
		fields[key+"_max"] = v.Max
		fields[key+"_remaining"] = v.Remaining
	}

	acc.AddFields("salesforce", fields, tags)
	return nil
}

// query the limits endpoint
func (s *Salesforce) queryLimits() (*http.Response, error) {
	endpoint := fmt.Sprintf("%s://%s/services/data/v%s/limits", s.serverURL.Scheme, s.serverURL.Host, s.Version)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "encoding/json")
	req.Header.Add("Authorization", "Bearer "+s.sessionID)
	return s.client.Do(req)
}

func (s *Salesforce) isAuthenticated() bool {
	return s.sessionID != ""
}

func (s *Salesforce) fetchLimits() (limits, error) {
	var l limits
	if !s.isAuthenticated() {
		if err := s.login(); err != nil {
			return l, err
		}
	}

	resp, err := s.queryLimits()
	if err != nil {
		return l, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		if err := s.login(); err != nil {
			return l, err
		}
		resp, err = s.queryLimits()
		if err != nil {
			return l, err
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		return l, fmt.Errorf("salesforce responded with unexpected status code %d", resp.StatusCode)
	}

	l = make(limits)
	err = json.NewDecoder(resp.Body).Decode(&l)
	return l, err
}

func (s *Salesforce) getLoginEndpoint() (string, error) {
	switch s.Environment {
	case "sandbox":
		return fmt.Sprintf("https://test.salesforce.com/services/Soap/c/%s/", s.Version), nil
	case "production":
		return fmt.Sprintf("https://login.salesforce.com/services/Soap/c/%s/", s.Version), nil
	default:
		return "", fmt.Errorf("unknown environment type: %s", s.Environment)
	}
}

// Authenticate with Salesforce
func (s *Salesforce) login() error {
	if s.Username == "" || s.Password == "" {
		return errors.New("missing username or password")
	}

	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
		<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/"
		  xmlns:urn="urn:enterprise.soap.sforce.com">
		  <soapenv:Body>
			<urn:login>
			  <urn:username>%s</urn:username>
			  <urn:password>%s%s</urn:password>
			</urn:login>
		  </soapenv:Body>
		</soapenv:Envelope>`,
		s.Username, s.Password, s.SecurityToken)

	loginEndpoint, err := s.getLoginEndpoint()
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, loginEndpoint, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "text/xml")
	req.Header.Add("SOAPAction", "login")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck // LimitReader returns io.EOF and we're not interested in read errors.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("%s returned HTTP status %s: %q", loginEndpoint, resp.Status, body)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	soapFault := struct {
		Code    string `xml:"Body>Fault>faultcode"`
		Message string `xml:"Body>Fault>faultstring"`
	}{}

	err = xml.Unmarshal(respBody, &soapFault)
	if err != nil {
		return err
	}

	if soapFault.Code != "" {
		return fmt.Errorf("login failed: %s", soapFault.Message)
	}

	loginResult := struct {
		ServerURL      string `xml:"Body>loginResponse>result>serverUrl"`
		SessionID      string `xml:"Body>loginResponse>result>sessionId"`
		OrganizationID string `xml:"Body>loginResponse>result>userInfo>organizationId"`
	}{}

	err = xml.Unmarshal(respBody, &loginResult)
	if err != nil {
		return err
	}

	s.sessionID = loginResult.SessionID
	s.organizationID = loginResult.OrganizationID
	s.serverURL, err = url.Parse(loginResult.ServerURL)

	return err
}

func newSalesforce() *Salesforce {
	tr := &http.Transport{
		ResponseHeaderTimeout: 5 * time.Second,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}
	return &Salesforce{
		client:      client,
		Version:     defaultVersion,
		Environment: defaultEnvironment}
}

func init() {
	inputs.Add("salesforce", func() telegraf.Input {
		return newSalesforce()
	})
}
