package firehose

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/influxdata/telegraf/config"
)

// Firehose request data-structures according to
// https://docs.aws.amazon.com/firehose/latest/dev/httpdeliveryrequestresponse.html#requestformat
type record struct {
	EncodedData string `json:"data"`
}

type requestBody struct {
	RequestID string   `json:"requestId"`
	Timestamp int64    `json:"timestamp"`
	Records   []record `json:"records"`
}

// Required response data structure according to
// https://docs.aws.amazon.com/firehose/latest/dev/httpdeliveryrequestresponse.html#responseformat
type responseBody struct {
	RequestID    string `json:"requestId"`
	Timestamp    int64  `json:"timestamp"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

type message struct {
	id           string
	responseCode int
}

func (m *message) authenticate(req *http.Request, expected config.Secret) error {
	// We completely switch off authentication if no 'access_key' was provided in the config, it's intended!
	if expected.Empty() {
		return nil
	}
	key := req.Header.Get("x-amz-firehose-access-key")
	match, err := expected.EqualTo([]byte(key))
	if err != nil {
		m.responseCode = http.StatusInternalServerError
		return fmt.Errorf("comparing keys failed: %w", err)
	}
	if !match {
		m.responseCode = http.StatusUnauthorized
		return fmt.Errorf("unauthorized request from %v", req.RemoteAddr)
	}

	return nil
}

func (m *message) decodeData(r *requestBody) ([][]byte, error) {
	// Decode base64-encoded data and return them as a slice of byte slices
	decodedData := make([][]byte, 0)
	for _, record := range r.Records {
		data, err := base64.StdEncoding.DecodeString(record.EncodedData)
		if err != nil {
			m.responseCode = http.StatusBadRequest
			return nil, err
		}
		decodedData = append(decodedData, data)
	}
	return decodedData, nil
}

func (m *message) extractTagsFromCommonAttributes(req *http.Request, tagkeys []string) (map[string]string, error) {
	tags := make(map[string]string, len(tagkeys))

	h := req.Header.Get("x-amz-firehose-common-attributes")
	if len(tagkeys) == 0 || h == "" {
		return tags, nil
	}

	var params map[string]interface{}
	if err := json.Unmarshal([]byte(h), &params); err != nil {
		m.responseCode = http.StatusBadRequest
		return nil, fmt.Errorf("decoding x-amz-firehose-common-attributes header failed: %w", err)
	}

	raw, ok := params["commonAttributes"]
	if !ok {
		m.responseCode = http.StatusBadRequest
		return nil, errors.New("commonAttributes not found in x-amz-firehose-common-attributes header")
	}

	attributes, ok := raw.(map[string]interface{})
	if !ok {
		m.responseCode = http.StatusBadRequest
		return nil, errors.New("parse parameters data failed")
	}
	for _, k := range tagkeys {
		if v, found := attributes[k]; found {
			tags[k] = v.(string)
		}
	}
	return tags, nil
}

func (m *message) sendResponse(w http.ResponseWriter) error {
	var errorMessage string
	if m.responseCode != http.StatusOK {
		errorMessage = http.StatusText(m.responseCode)
	}

	response, err := json.Marshal(responseBody{
		RequestID:    m.id,
		Timestamp:    time.Now().Unix(),
		ErrorMessage: errorMessage,
	})
	if err != nil {
		return err
	}
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(m.responseCode)
	if _, err := w.Write(response); err != nil {
		return fmt.Errorf("writing response to request %s failed: %w", m.id, err)
	}
	return nil
}
