package jolokia2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/influxdata/telegraf/plugins/common/tls"
)

type Client struct {
	URL    string
	client *http.Client
	config *ClientConfig
}

type ClientConfig struct {
	ResponseTimeout time.Duration
	Username        string
	Password        string
	Origin          string
	ProxyConfig     *ProxyConfig
	tls.ClientConfig
}

type ProxyConfig struct {
	DefaultTargetUsername string
	DefaultTargetPassword string
	Targets               []ProxyTargetConfig
}

type ProxyTargetConfig struct {
	Username string
	Password string
	URL      string
}

type ReadRequest struct {
	Mbean      string
	Attributes []string
	Path       string
}

type ReadResponse struct {
	Status            int
	Value             interface{}
	RequestMbean      string
	RequestAttributes []string
	RequestPath       string
	RequestTarget     string
}

//	Jolokia JSON request object. Example: {
//	  "type": "read",
//	  "mbean: "java.lang:type="Runtime",
//	  "attribute": "Uptime",
//	  "target": {
//	    "url: "service:jmx:rmi:///jndi/rmi://target:9010/jmxrmi"
//	  }
//	}
type jolokiaRequest struct {
	Type      string         `json:"type"`
	Mbean     string         `json:"mbean"`
	Attribute interface{}    `json:"attribute,omitempty"`
	Path      string         `json:"path,omitempty"`
	Target    *jolokiaTarget `json:"target,omitempty"`
}

type jolokiaTarget struct {
	URL      string `json:"url"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

//	Jolokia JSON response object. Example: {
//	  "request": {
//	    "type": "read"
//	    "mbean": "java.lang:type=Runtime",
//	    "attribute": "Uptime",
//	    "target": {
//	      "url": "service:jmx:rmi:///jndi/rmi://target:9010/jmxrmi"
//	    }
//	  },
//	  "value": 1214083,
//	  "timestamp": 1488059309,
//	  "status": 200
//	}
type jolokiaResponse struct {
	Request jolokiaRequest `json:"request"`
	Value   interface{}    `json:"value"`
	Status  int            `json:"status"`
}

func NewClient(address string, config *ClientConfig) (*Client, error) {
	tlsConfig, err := config.ClientConfig.TLSConfig()
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		ResponseHeaderTimeout: config.ResponseTimeout,
		TLSClientConfig:       tlsConfig,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   config.ResponseTimeout,
	}

	return &Client{
		URL:    address,
		config: config,
		client: client,
	}, nil
}

func (c *Client) read(requests []ReadRequest) ([]ReadResponse, error) {
	jRequests := makeJolokiaRequests(requests, c.config.ProxyConfig)
	requestBody, err := json.Marshal(jRequests)
	if err != nil {
		return nil, err
	}

	requestURL, err := formatReadURL(c.URL, c.config.Username, c.config.Password)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", requestURL, bytes.NewBuffer(requestBody))
	if err != nil {
		// err is not contained in returned error - it may contain sensitive data (password) which should not be logged
		return nil, fmt.Errorf("unable to create new request for: %q", c.URL)
	}

	req.Header.Add("Content-type", "application/json")
	if c.config.Origin != "" {
		req.Header.Add("Origin", c.config.Origin)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response from url %q has status code %d (%s), expected %d (%s)",
			c.URL, resp.StatusCode, http.StatusText(resp.StatusCode), http.StatusOK, http.StatusText(http.StatusOK))
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jResponses []jolokiaResponse
	if err = json.Unmarshal(responseBody, &jResponses); err != nil {
		return nil, fmt.Errorf("decoding JSON response: %w: %s", err, responseBody)
	}

	return makeReadResponses(jResponses), nil
}

func makeJolokiaRequests(rrequests []ReadRequest, proxyConfig *ProxyConfig) []jolokiaRequest {
	jrequests := make([]jolokiaRequest, 0)
	if proxyConfig == nil {
		for _, rr := range rrequests {
			jrequests = append(jrequests, makeJolokiaRequest(rr, nil))
		}
	} else {
		for _, t := range proxyConfig.Targets {
			if t.Username == "" {
				t.Username = proxyConfig.DefaultTargetUsername
			}
			if t.Password == "" {
				t.Password = proxyConfig.DefaultTargetPassword
			}

			for _, rr := range rrequests {
				jtarget := &jolokiaTarget{
					URL:      t.URL,
					User:     t.Username,
					Password: t.Password,
				}

				jrequests = append(jrequests, makeJolokiaRequest(rr, jtarget))
			}
		}
	}

	return jrequests
}

func makeJolokiaRequest(rrequest ReadRequest, jtarget *jolokiaTarget) jolokiaRequest {
	jrequest := jolokiaRequest{
		Type:   "read",
		Mbean:  rrequest.Mbean,
		Path:   rrequest.Path,
		Target: jtarget,
	}

	if len(rrequest.Attributes) == 1 {
		jrequest.Attribute = rrequest.Attributes[0]
	}
	if len(rrequest.Attributes) > 1 {
		jrequest.Attribute = rrequest.Attributes
	}

	return jrequest
}

func makeReadResponses(jresponses []jolokiaResponse) []ReadResponse {
	rresponses := make([]ReadResponse, 0)

	for _, jr := range jresponses {
		rrequest := ReadRequest{
			Mbean:      jr.Request.Mbean,
			Path:       jr.Request.Path,
			Attributes: make([]string, 0),
		}

		attrValue := jr.Request.Attribute
		if attrValue != nil {
			attribute, ok := attrValue.(string)
			if ok {
				rrequest.Attributes = []string{attribute}
			} else {
				attributes, _ := attrValue.([]interface{})
				rrequest.Attributes = make([]string, 0, len(attributes))
				for _, attr := range attributes {
					rrequest.Attributes = append(rrequest.Attributes, attr.(string))
				}
			}
		}
		rresponse := ReadResponse{
			Value:             jr.Value,
			Status:            jr.Status,
			RequestMbean:      rrequest.Mbean,
			RequestAttributes: rrequest.Attributes,
			RequestPath:       rrequest.Path,
		}
		if jtarget := jr.Request.Target; jtarget != nil {
			rresponse.RequestTarget = jtarget.URL
		}

		rresponses = append(rresponses, rresponse)
	}

	return rresponses
}

func formatReadURL(configURL, username, password string) (string, error) {
	parsedURL, err := url.Parse(configURL)
	if err != nil {
		return "", err
	}

	readURL := url.URL{
		Host:   parsedURL.Host,
		Scheme: parsedURL.Scheme,
	}

	if username != "" || password != "" {
		readURL.User = url.UserPassword(username, password)
	}

	readURL.Path = path.Join(parsedURL.Path, "read")
	readURL.Query().Add("ignoreErrors", "true")
	return readURL.String(), nil
}
