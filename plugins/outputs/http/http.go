//go:generate ../../../tools/readme_config_includer/generator
package http

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	aws_signer "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	common_aws "github.com/influxdata/telegraf/plugins/common/aws"
	common_http "github.com/influxdata/telegraf/plugins/common/http"
	"github.com/influxdata/telegraf/plugins/outputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	maxErrMsgLen = 1024
	defaultURL   = "http://127.0.0.1:8080/telegraf"
)

const (
	defaultContentType    = "text/plain; charset=utf-8"
	defaultMethod         = http.MethodPost
	defaultUseBatchFormat = true
)

type HTTP struct {
	URL                     string                    `toml:"url"`
	Method                  string                    `toml:"method"`
	Username                config.Secret             `toml:"username"`
	Password                config.Secret             `toml:"password"`
	Headers                 map[string]*config.Secret `toml:"headers"`
	ContentEncoding         string                    `toml:"content_encoding"`
	UseBatchFormat          bool                      `toml:"use_batch_format"`
	AwsService              string                    `toml:"aws_service"`
	NonRetryableStatusCodes []int                     `toml:"non_retryable_statuscodes"`
	common_http.HTTPClientConfig
	Log telegraf.Logger `toml:"-"`

	client     *http.Client
	serializer telegraf.Serializer

	awsCfg *aws.Config
	common_aws.CredentialConfig

	// Google API Auth
	CredentialsFile string `toml:"google_application_credentials"`
	oauth2Token     *oauth2.Token
}

func (*HTTP) SampleConfig() string {
	return sampleConfig
}

func (h *HTTP) SetSerializer(serializer telegraf.Serializer) {
	h.serializer = serializer
}

func (h *HTTP) Connect() error {
	if h.AwsService != "" {
		cfg, err := h.CredentialConfig.Credentials()
		if err == nil {
			h.awsCfg = &cfg
		}
	}

	if h.Method == "" {
		h.Method = http.MethodPost
	}
	h.Method = strings.ToUpper(h.Method)
	if h.Method != http.MethodPost && h.Method != http.MethodPut && h.Method != http.MethodPatch {
		return fmt.Errorf("invalid method [%s] %s", h.URL, h.Method)
	}

	ctx := context.Background()
	client, err := h.HTTPClientConfig.CreateClient(ctx, h.Log)
	if err != nil {
		return err
	}

	h.client = client

	return nil
}

func (h *HTTP) Close() error {
	if h.client != nil {
		h.client.CloseIdleConnections()
	}

	return nil
}

func (h *HTTP) Write(metrics []telegraf.Metric) error {
	if h.UseBatchFormat {
		reqBody, err := h.serializer.SerializeBatch(metrics)
		if err != nil {
			return err
		}

		return h.writeMetric(reqBody)
	}

	for _, metric := range metrics {
		reqBody, err := h.serializer.Serialize(metric)
		if err != nil {
			return err
		}

		if err := h.writeMetric(reqBody); err != nil {
			return err
		}
	}
	return nil
}

func (h *HTTP) writeMetric(reqBody []byte) error {
	var reqBodyBuffer io.Reader = bytes.NewBuffer(reqBody)

	var err error
	if h.ContentEncoding == "gzip" {
		rc := internal.CompressWithGzip(reqBodyBuffer)
		defer rc.Close()
		reqBodyBuffer = rc
	}

	var payloadHash *string
	if h.awsCfg != nil {
		// We need a local copy of the full buffer, the signature scheme requires a sha256 of the request body.
		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, reqBodyBuffer)
		if err != nil {
			return err
		}

		sum := sha256.Sum256(buf.Bytes())
		reqBodyBuffer = buf

		// sha256 is hex encoded
		hash := hex.EncodeToString(sum[:])
		payloadHash = &hash
	}

	req, err := http.NewRequest(h.Method, h.URL, reqBodyBuffer)
	if err != nil {
		return err
	}

	if h.awsCfg != nil {
		signer := aws_signer.NewSigner()
		ctx := context.Background()

		credentials, err := h.awsCfg.Credentials.Retrieve(ctx)
		if err != nil {
			return err
		}

		err = signer.SignHTTP(ctx, credentials, req, *payloadHash, h.AwsService, h.Region, time.Now().UTC())
		if err != nil {
			return err
		}
	}

	if !h.Username.Empty() || !h.Password.Empty() {
		username, err := h.Username.Get()
		if err != nil {
			return fmt.Errorf("getting username failed: %w", err)
		}
		password, err := h.Password.Get()
		if err != nil {
			username.Destroy()
			return fmt.Errorf("getting password failed: %w", err)
		}
		req.SetBasicAuth(username.String(), password.String())
		username.Destroy()
		password.Destroy()
	}

	// google api auth
	if h.CredentialsFile != "" {
		token, err := h.getAccessToken(context.Background(), h.URL)
		if err != nil {
			return err
		}
		token.SetAuthHeader(req)
	}

	req.Header.Set("User-Agent", internal.ProductToken())
	req.Header.Set("Content-Type", defaultContentType)
	if h.ContentEncoding == "gzip" {
		req.Header.Set("Content-Encoding", "gzip")
	}

	for k, v := range h.Headers {
		secret, err := v.Get()
		if err != nil {
			return err
		}

		headerVal := secret.String()
		if strings.EqualFold(k, "host") {
			req.Host = headerVal
		}
		req.Header.Set(k, headerVal)

		secret.Destroy()
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		for _, nonRetryableStatusCode := range h.NonRetryableStatusCodes {
			if resp.StatusCode == nonRetryableStatusCode {
				h.Log.Errorf("Received non-retryable status %v. Metrics are lost.", resp.StatusCode)
				return nil
			}
		}

		errorLine := ""
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxErrMsgLen))
		if scanner.Scan() {
			errorLine = scanner.Text()
		}

		return fmt.Errorf("when writing to [%s] received status code: %d. body: %s", h.URL, resp.StatusCode, errorLine)
	}

	_, err = io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("when writing to [%s] received error: %w", h.URL, err)
	}

	return nil
}

func init() {
	outputs.Add("http", func() telegraf.Output {
		return &HTTP{
			Method:         defaultMethod,
			URL:            defaultURL,
			UseBatchFormat: defaultUseBatchFormat,
		}
	})
}

func (h *HTTP) getAccessToken(ctx context.Context, audience string) (*oauth2.Token, error) {
	if h.oauth2Token.Valid() {
		return h.oauth2Token, nil
	}

	ts, err := idtoken.NewTokenSource(ctx, audience, idtoken.WithCredentialsFile(h.CredentialsFile))
	if err != nil {
		return nil, fmt.Errorf("error creating oauth2 token source: %w", err)
	}

	token, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("error fetching oauth2 token: %w", err)
	}

	h.oauth2Token = token

	return token, nil
}
