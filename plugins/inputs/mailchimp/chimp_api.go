package mailchimp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/influxdata/telegraf"
)

const (
	reportsEndpoint         string = "/3.0/reports"
	reportsEndpointCampaign string = "/3.0/reports/%s"
)

var mailchimpDatacenter = regexp.MustCompile("[a-z]+[0-9]+$")

type chimpAPI struct {
	transport http.RoundTripper
	debug     bool

	sync.Mutex

	url *url.URL
	log telegraf.Logger
}

type reportsParams struct {
	count          string
	offset         string
	sinceSendTime  string
	beforeSendTime string
}

func (p *reportsParams) String() string {
	v := url.Values{}
	if p.count != "" {
		v.Set("count", p.count)
	}
	if p.offset != "" {
		v.Set("offset", p.offset)
	}
	if p.beforeSendTime != "" {
		v.Set("before_send_time", p.beforeSendTime)
	}
	if p.sinceSendTime != "" {
		v.Set("since_send_time", p.sinceSendTime)
	}
	return v.Encode()
}

func newChimpAPI(apiKey string, log telegraf.Logger) *chimpAPI {
	u := &url.URL{}
	u.Scheme = "https"
	u.Host = mailchimpDatacenter.FindString(apiKey) + ".api.mailchimp.com"
	u.User = url.UserPassword("", apiKey)
	return &chimpAPI{url: u, log: log}
}

type apiError struct {
	Status   int    `json:"status"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Instance string `json:"instance"`
}

func (e apiError) Error() string {
	return fmt.Sprintf("ERROR %v: %v. See %v", e.Status, e.Title, e.Type)
}

func chimpErrorCheck(body []byte) error {
	var e apiError
	if err := json.Unmarshal(body, &e); err != nil {
		return err
	}
	if e.Title != "" || e.Status != 0 {
		return e
	}
	return nil
}

func (a *chimpAPI) getReports(params reportsParams) (reportsResponse, error) {
	a.Lock()
	defer a.Unlock()
	a.url.Path = reportsEndpoint

	var response reportsResponse
	rawjson, err := a.runChimp(params)
	if err != nil {
		return response, err
	}

	err = json.Unmarshal(rawjson, &response)
	if err != nil {
		return response, err
	}

	return response, nil
}

func (a *chimpAPI) getReport(campaignID string) (report, error) {
	a.Lock()
	defer a.Unlock()
	a.url.Path = fmt.Sprintf(reportsEndpointCampaign, campaignID)

	var response report
	rawjson, err := a.runChimp(reportsParams{})
	if err != nil {
		return response, err
	}

	err = json.Unmarshal(rawjson, &response)
	if err != nil {
		return response, err
	}

	return response, nil
}

func (a *chimpAPI) runChimp(params reportsParams) ([]byte, error) {
	client := &http.Client{
		Transport: a.transport,
		Timeout:   4 * time.Second,
	}

	var b bytes.Buffer
	req, err := http.NewRequest("GET", a.url.String(), &b)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = params.String()
	req.Header.Set("User-Agent", "Telegraf-MailChimp-Plugin")
	if a.debug {
		a.log.Debugf("request URL: %s", req.URL.String())
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		//nolint:errcheck // LimitReader returns io.EOF and we're not interested in read errors.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return nil, fmt.Errorf("%s returned HTTP status %s: %q", a.url.String(), resp.Status, body)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if a.debug {
		a.log.Debugf("response Body: %q", string(body))
	}

	if err := chimpErrorCheck(body); err != nil {
		return nil, err
	}
	return body, nil
}

type reportsResponse struct {
	Reports    []report `json:"reports"`
	TotalItems int      `json:"total_items"`
}

type report struct {
	ID            string `json:"id"`
	CampaignTitle string `json:"campaign_title"`
	Type          string `json:"type"`
	EmailsSent    int    `json:"emails_sent"`
	AbuseReports  int    `json:"abuse_reports"`
	Unsubscribed  int    `json:"unsubscribed"`
	SendTime      string `json:"send_time"`

	TimeSeries    []timeSeries
	Bounces       bounces       `json:"bounces"`
	Forwards      forwards      `json:"forwards"`
	Opens         opens         `json:"opens"`
	Clicks        clicks        `json:"clicks"`
	FacebookLikes facebookLikes `json:"facebook_likes"`
	IndustryStats industryStats `json:"industry_stats"`
	ListStats     listStats     `json:"list_stats"`
}

type bounces struct {
	HardBounces  int `json:"hard_bounces"`
	SoftBounces  int `json:"soft_bounces"`
	SyntaxErrors int `json:"syntax_errors"`
}

type forwards struct {
	ForwardsCount int `json:"forwards_count"`
	ForwardsOpens int `json:"forwards_opens"`
}

type opens struct {
	OpensTotal  int     `json:"opens_total"`
	UniqueOpens int     `json:"unique_opens"`
	OpenRate    float64 `json:"open_rate"`
	LastOpen    string  `json:"last_open"`
}

type clicks struct {
	ClicksTotal            int     `json:"clicks_total"`
	UniqueClicks           int     `json:"unique_clicks"`
	UniqueSubscriberClicks int     `json:"unique_subscriber_clicks"`
	ClickRate              float64 `json:"click_rate"`
	LastClick              string  `json:"last_click"`
}

type facebookLikes struct {
	RecipientLikes int `json:"recipient_likes"`
	UniqueLikes    int `json:"unique_likes"`
	FacebookLikes  int `json:"facebook_likes"`
}

type industryStats struct {
	Type       string  `json:"type"`
	OpenRate   float64 `json:"open_rate"`
	ClickRate  float64 `json:"click_rate"`
	BounceRate float64 `json:"bounce_rate"`
	UnopenRate float64 `json:"unopen_rate"`
	UnsubRate  float64 `json:"unsub_rate"`
	AbuseRate  float64 `json:"abuse_rate"`
}

type listStats struct {
	SubRate   float64 `json:"sub_rate"`
	UnsubRate float64 `json:"unsub_rate"`
	OpenRate  float64 `json:"open_rate"`
	ClickRate float64 `json:"click_rate"`
}

type timeSeries struct {
	TimeStamp       string `json:"timestamp"`
	EmailsSent      int    `json:"emails_sent"`
	UniqueOpens     int    `json:"unique_opens"`
	RecipientsClick int    `json:"recipients_click"`
}
