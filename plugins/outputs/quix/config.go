package quix

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type brokerConfig struct {
	BootstrapServers string `json:"bootstrap.servers"`
	SaslMechanism    string `json:"sasl.mechanism"`
	SaslUsername     string `json:"sasl.username"`
	SaslPassword     string `json:"sasl.password"`
	SecurityProtocol string `json:"security.protocol"`
	SSLCertBase64    string `json:"ssl.ca.cert"`

	cert []byte
}

func (q *Quix) fetchBrokerConfig() (*brokerConfig, error) {
	// Create request
	endpoint := fmt.Sprintf("%s/workspaces/%s/broker/librdkafka", q.APIURL, q.Workspace)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request failed: %w", err)
	}

	// Setup authentication
	token, err := q.Token.Get()
	if err != nil {
		return nil, fmt.Errorf("getting token failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.String())
	req.Header.Set("Accept", "application/json")
	token.Destroy()

	// Query the broker configuration from the Quix API
	client, err := q.HTTPClientConfig.CreateClient(context.Background(), q.Log)
	if err != nil {
		return nil, fmt.Errorf("creating client failed: %w", err)
	}
	defer client.CloseIdleConnections()

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the body as we need it both in case of an error as well as for
	// decoding the config in case of success
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		q.Log.Errorf("Reading message body failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response %q (%d): %s",
			http.StatusText(resp.StatusCode),
			resp.StatusCode,
			string(body),
		)
	}

	// Decode the broker and the returned certificate
	var cfg brokerConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("decoding body failed: %w", err)
	}

	cert, err := base64.StdEncoding.DecodeString(cfg.SSLCertBase64)
	if err != nil {
		return nil, fmt.Errorf("decoding certificate failed: %w", err)
	}
	cfg.cert = cert

	return &cfg, nil
}
