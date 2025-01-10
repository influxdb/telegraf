package json_v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/influxdata/telegraf/plugins/inputs/zipkin/codec"
	"github.com/influxdata/telegraf/plugins/inputs/zipkin/codec/thrift/gen-go/zipkincore"
)

// JSON decodes spans from  bodies `POST`ed to the spans endpoint
type JSON struct{}

// Decode unmarshals and validates the JSON body
func (*JSON) Decode(octets []byte) ([]codec.Span, error) {
	var spans []span
	err := json.Unmarshal(octets, &spans)
	if err != nil {
		return nil, err
	}

	res := make([]codec.Span, 0, len(spans))
	for i := range spans {
		if err := spans[i].Validate(); err != nil {
			return nil, err
		}
		res = append(res, &spans[i])
	}
	return res, nil
}

type span struct {
	TraceID  string             `json:"traceId"`
	SpanName string             `json:"name"`
	ParentID string             `json:"parentId,omitempty"`
	ID       string             `json:"id"`
	Time     *int64             `json:"timestamp,omitempty"`
	Dur      *int64             `json:"duration,omitempty"`
	Debug    bool               `json:"debug,omitempty"`
	Anno     []annotation       `json:"annotations"`
	BAnno    []binaryAnnotation `json:"binaryAnnotations"`
}

func (s *span) Validate() error {
	var err error
	check := func(f func() (string, error)) {
		if err != nil {
			return
		}
		_, err = f()
	}

	check(s.Trace)
	check(s.SpanID)
	check(s.Parent)
	if err != nil {
		return err
	}

	_, err = s.BinaryAnnotations()
	return err
}

func (s *span) Trace() (string, error) {
	if s.TraceID == "" {
		return "", errors.New("trace ID cannot be null")
	}
	return TraceIDFromString(s.TraceID)
}

func (s *span) SpanID() (string, error) {
	if s.ID == "" {
		return "", errors.New("span ID cannot be null")
	}
	return IDFromString(s.ID)
}

func (s *span) Parent() (string, error) {
	if s.ParentID == "" {
		return "", nil
	}
	return IDFromString(s.ParentID)
}

func (s *span) Name() string {
	return s.SpanName
}

func (s *span) Annotations() []codec.Annotation {
	res := make([]codec.Annotation, 0, len(s.Anno))
	for i := range s.Anno {
		res = append(res, &s.Anno[i])
	}
	return res
}

func (s *span) BinaryAnnotations() ([]codec.BinaryAnnotation, error) {
	res := make([]codec.BinaryAnnotation, 0, len(s.BAnno))
	for i, a := range s.BAnno {
		if a.Key() != "" && a.Value() == "" {
			return nil, fmt.Errorf("no value for key %s at binaryAnnotations[%d]", a.K, i)
		}
		if a.Value() != "" && a.Key() == "" {
			return nil, fmt.Errorf("no key at binaryAnnotations[%d]", i)
		}
		res = append(res, &s.BAnno[i])
	}
	return res, nil
}

func (s *span) Timestamp() time.Time {
	if s.Time == nil {
		return time.Time{}
	}
	return codec.MicroToTime(*s.Time)
}

func (s *span) Duration() time.Duration {
	if s.Dur == nil {
		return 0
	}
	return time.Duration(*s.Dur) * time.Microsecond
}

type annotation struct {
	Endpoint *endpoint `json:"endpoint,omitempty"`
	Time     int64     `json:"timestamp"`
	Val      string    `json:"value,omitempty"`
}

func (a *annotation) Timestamp() time.Time {
	return codec.MicroToTime(a.Time)
}

func (a *annotation) Value() string {
	return a.Val
}

func (a *annotation) Host() codec.Endpoint {
	return a.Endpoint
}

type binaryAnnotation struct {
	K        string          `json:"key"`
	V        json.RawMessage `json:"value"`
	Type     string          `json:"type"`
	Endpoint *endpoint       `json:"endpoint,omitempty"`
}

func (b *binaryAnnotation) Key() string {
	return b.K
}

func (b *binaryAnnotation) Value() string {
	t, err := zipkincore.AnnotationTypeFromString(b.Type)
	// Assume this is a string if we cannot tell the type
	if err != nil {
		t = zipkincore.AnnotationType_STRING
	}

	switch t {
	case zipkincore.AnnotationType_BOOL:
		var v bool
		err := json.Unmarshal(b.V, &v)
		if err == nil {
			return strconv.FormatBool(v)
		}
	case zipkincore.AnnotationType_BYTES:
		return string(b.V)
	case zipkincore.AnnotationType_I16, zipkincore.AnnotationType_I32, zipkincore.AnnotationType_I64:
		var v int64
		err := json.Unmarshal(b.V, &v)
		if err == nil {
			return strconv.FormatInt(v, 10)
		}
	case zipkincore.AnnotationType_DOUBLE:
		var v float64
		err := json.Unmarshal(b.V, &v)
		if err == nil {
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	case zipkincore.AnnotationType_STRING:
		var v string
		err := json.Unmarshal(b.V, &v)
		if err == nil {
			return v
		}
	}

	return ""
}

func (b *binaryAnnotation) Host() codec.Endpoint {
	return b.Endpoint
}

type endpoint struct {
	ServiceName string `json:"serviceName"`
	Ipv4        string `json:"ipv4"`
	Ipv6        string `json:"ipv6,omitempty"`
	Port        int    `json:"port"`
}

func (e *endpoint) Host() string {
	if e.Port != 0 {
		return fmt.Sprintf("%s:%d", e.Ipv4, e.Port)
	}
	return e.Ipv4
}

func (e *endpoint) Name() string {
	return e.ServiceName
}

// TraceIDFromString creates a TraceID from a hexadecimal string
func TraceIDFromString(s string) (string, error) {
	var hi, lo uint64
	var err error
	if len(s) > 32 {
		return "", fmt.Errorf("length of TraceID cannot be greater than 32 hex characters: %s", s)
	} else if len(s) > 16 {
		hiLen := len(s) - 16
		if hi, err = strconv.ParseUint(s[0:hiLen], 16, 64); err != nil {
			return "", err
		}
		if lo, err = strconv.ParseUint(s[hiLen:], 16, 64); err != nil {
			return "", err
		}
	} else {
		if lo, err = strconv.ParseUint(s, 16, 64); err != nil {
			return "", err
		}
	}
	if hi == 0 {
		return strconv.FormatUint(lo, 16), nil
	}
	return fmt.Sprintf("%x%016x", hi, lo), nil
}

// IDFromString validates the ID and returns it in hexadecimal format.
func IDFromString(s string) (string, error) {
	if len(s) > 16 {
		return "", fmt.Errorf("length of ID cannot be greater than 16 hex characters: %s", s)
	}
	id, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return "", err
	}
	return strconv.FormatUint(id, 16), nil
}
