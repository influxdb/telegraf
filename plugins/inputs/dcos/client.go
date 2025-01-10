package dcos

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// How long to stayed logged in for
	loginDuration = 65 * time.Minute
)

// client is an interface for communicating with the DC/OS API.
type client interface {
	setToken(token string)

	login(ctx context.Context, sa *serviceAccount) (*authToken, error)
	getSummary(ctx context.Context) (*summary, error)
	getContainers(ctx context.Context, node string) ([]container, error)
	getNodeMetrics(ctx context.Context, node string) (*metrics, error)
	getContainerMetrics(ctx context.Context, node, container string) (*metrics, error)
	getAppMetrics(ctx context.Context, node, container string) (*metrics, error)
}

type apiError struct {
	url         string
	statusCode  int
	title       string
	description string
}

// login is request data for logging in.
type login struct {
	UID   string `json:"uid"`
	Exp   int64  `json:"exp"`
	Token string `json:"token"`
}

// loginError is the response when login fails.
type loginError struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// loginAuth is the response to a successful login.
type loginAuth struct {
	Token string `json:"token"`
}

// slave is a node in the cluster.
type slave struct {
	ID string `json:"id"`
}

// summary provides high level cluster wide information.
type summary struct {
	Cluster string
	Slaves  []slave
}

// container is a container on a node.
type container struct {
	ID string
}

type dataPoint struct {
	Name  string            `json:"name"`
	Tags  map[string]string `json:"tags"`
	Unit  string            `json:"unit"`
	Value float64           `json:"value"`
}

// metrics are the DCOS metrics
type metrics struct {
	Datapoints []dataPoint            `json:"datapoints"`
	Dimensions map[string]interface{} `json:"dimensions"`
}

// authToken is the authentication token.
type authToken struct {
	Text   string
	Expire time.Time
}

// clusterClient is a client that uses the cluster URL.
type clusterClient struct {
	clusterURL *url.URL
	httpClient *http.Client
	token      string
	semaphore  chan struct{}
}

type claims struct {
	UID string `json:"uid"`
	jwt.RegisteredClaims
}

func (e apiError) Error() string {
	if e.description != "" {
		return fmt.Sprintf("[%s] %s: %s", e.url, e.title, e.description)
	}
	return fmt.Sprintf("[%s] %s", e.url, e.title)
}

func newClusterClient(clusterURL *url.URL, timeout time.Duration, maxConns int, tlsConfig *tls.Config) *clusterClient {
	httpClient := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:    maxConns,
			TLSClientConfig: tlsConfig,
		},
		Timeout: timeout,
	}
	semaphore := make(chan struct{}, maxConns)

	c := &clusterClient{
		clusterURL: clusterURL,
		httpClient: httpClient,
		semaphore:  semaphore,
	}
	return c
}

func (c *clusterClient) setToken(token string) {
	c.token = token
}

func (c *clusterClient) login(ctx context.Context, sa *serviceAccount) (*authToken, error) {
	token, err := createLoginToken(sa)
	if err != nil {
		return nil, err
	}

	exp := time.Now().Add(loginDuration)

	body := &login{
		UID:   sa.accountID,
		Exp:   exp.Unix(),
		Token: token,
	}

	octets, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	loc := c.toURL("/acs/api/v1/auth/login")
	req, err := http.NewRequest("POST", loc, bytes.NewBuffer(octets))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")

	req = req.WithContext(ctx)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		auth := &loginAuth{}
		dec := json.NewDecoder(resp.Body)
		err = dec.Decode(auth)
		if err != nil {
			return nil, err
		}

		token := &authToken{
			Text:   auth.Token,
			Expire: exp,
		}
		return token, nil
	}

	loginError := &loginError{}
	dec := json.NewDecoder(resp.Body)
	err = dec.Decode(loginError)
	if err != nil {
		err := &apiError{
			url:        loc,
			statusCode: resp.StatusCode,
			title:      resp.Status,
		}
		return nil, err
	}

	err = &apiError{
		url:         loc,
		statusCode:  resp.StatusCode,
		title:       loginError.Title,
		description: loginError.Description,
	}
	return nil, err
}

func (c *clusterClient) getSummary(ctx context.Context) (*summary, error) {
	summary := &summary{}
	err := c.doGet(ctx, c.toURL("/mesos/master/state-summary"), summary)
	if err != nil {
		return nil, err
	}

	return summary, nil
}

func (c *clusterClient) getContainers(ctx context.Context, node string) ([]container, error) {
	list := make([]string, 0)
	err := c.doGet(ctx, c.toURL(fmt.Sprintf("/system/v1/agent/%s/metrics/v0/containers", node)), &list)
	if err != nil {
		return nil, err
	}

	containers := make([]container, 0, len(list))
	for _, c := range list {
		containers = append(containers, container{ID: c})
	}

	return containers, nil
}

func (c *clusterClient) getMetrics(ctx context.Context, address string) (*metrics, error) {
	metrics := &metrics{}

	err := c.doGet(ctx, address, metrics)
	if err != nil {
		return nil, err
	}

	return metrics, nil
}

func (c *clusterClient) getNodeMetrics(ctx context.Context, node string) (*metrics, error) {
	path := fmt.Sprintf("/system/v1/agent/%s/metrics/v0/node", node)
	return c.getMetrics(ctx, c.toURL(path))
}

func (c *clusterClient) getContainerMetrics(ctx context.Context, node, container string) (*metrics, error) {
	path := fmt.Sprintf("/system/v1/agent/%s/metrics/v0/containers/%s", node, container)
	return c.getMetrics(ctx, c.toURL(path))
}

func (c *clusterClient) getAppMetrics(ctx context.Context, node, container string) (*metrics, error) {
	path := fmt.Sprintf("/system/v1/agent/%s/metrics/v0/containers/%s/app", node, container)
	return c.getMetrics(ctx, c.toURL(path))
}

func createGetRequest(address, token string) (*http.Request, error) {
	req, err := http.NewRequest("GET", address, nil)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Add("Authorization", "token="+token)
	}
	req.Header.Add("Accept", "application/json")

	return req, nil
}

func (c *clusterClient) doGet(ctx context.Context, address string, v interface{}) error {
	req, err := createGetRequest(address, c.token)
	if err != nil {
		return err
	}

	select {
	case c.semaphore <- struct{}{}:
		break
	case <-ctx.Done():
		return ctx.Err()
	}

	resp, err := c.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		<-c.semaphore
		return err
	}
	defer func() {
		resp.Body.Close()
		<-c.semaphore
	}()

	// Clear invalid token if unauthorized
	if resp.StatusCode == http.StatusUnauthorized {
		c.token = ""
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{
			url:        address,
			statusCode: resp.StatusCode,
			title:      resp.Status,
		}
	}

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	err = json.NewDecoder(resp.Body).Decode(v)
	return err
}

func (c *clusterClient) toURL(path string) string {
	clusterURL := *c.clusterURL
	clusterURL.Path = path
	return clusterURL.String()
}

func createLoginToken(sa *serviceAccount) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims{
		UID: sa.accountID,
		RegisteredClaims: jwt.RegisteredClaims{
			// How long we have to login with this token
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute * 5)),
		},
	})
	return token.SignedString(sa.privateKey)
}
