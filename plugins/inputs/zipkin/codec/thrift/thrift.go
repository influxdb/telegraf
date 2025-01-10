package thrift

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/apache/thrift/lib/go/thrift"

	"github.com/influxdata/telegraf/plugins/inputs/zipkin/codec"
	"github.com/influxdata/telegraf/plugins/inputs/zipkin/codec/thrift/gen-go/zipkincore"
)

// UnmarshalThrift converts raw bytes in thrift format to a slice of spans
func UnmarshalThrift(body []byte) ([]*zipkincore.Span, error) {
	buffer := thrift.NewTMemoryBuffer()
	buffer.Write(body)

	transport := thrift.NewTBinaryProtocolConf(buffer, nil)
	_, size, err := transport.ReadListBegin(context.Background())
	if err != nil {
		return nil, err
	}

	spans := make([]*zipkincore.Span, 0, size)
	for i := 0; i < size; i++ {
		zs := &zipkincore.Span{}
		if err := zs.Read(context.Background(), transport); err != nil {
			return nil, err
		}
		spans = append(spans, zs)
	}

	if err := transport.ReadListEnd(context.Background()); err != nil {
		return nil, err
	}
	return spans, nil
}

// Thrift decodes binary data to create a Trace
type Thrift struct{}

// Decode unmarshals and validates bytes in thrift format
func (*Thrift) Decode(octets []byte) ([]codec.Span, error) {
	spans, err := UnmarshalThrift(octets)
	if err != nil {
		return nil, err
	}

	res := make([]codec.Span, 0, len(spans))
	for _, s := range spans {
		res = append(res, &span{s})
	}
	return res, nil
}

var _ codec.Endpoint = &endpoint{}

type endpoint struct {
	*zipkincore.Endpoint
}

func (e *endpoint) Host() string {
	ipv4 := func(addr int32) string {
		buf := make([]byte, 4)
		binary.BigEndian.PutUint32(buf, uint32(addr))
		return net.IP(buf).String()
	}

	if e.Endpoint == nil {
		return ipv4(int32(0))
	}
	if e.Endpoint.GetPort() == 0 {
		return ipv4(e.Endpoint.GetIpv4())
	}
	// Zipkin uses a signed int16 for the port, but, warns us that they actually treat it
	// as an unsigned int16. So, we convert from int16 to int32 followed by taking & 0xffff
	// to convert from signed to unsigned
	// https://github.com/openzipkin/zipkin/blob/57dc2ec9c65fe6144e401c0c933b4400463a69df/zipkin/src/main/java/zipkin/Endpoint.java#L44
	return ipv4(e.Endpoint.GetIpv4()) + ":" + strconv.FormatInt(int64(int(e.Endpoint.GetPort())&0xffff), 10)
}

func (e *endpoint) Name() string {
	if e.Endpoint == nil {
		return codec.DefaultServiceName
	}
	return e.Endpoint.GetServiceName()
}

var _ codec.BinaryAnnotation = &binaryAnnotation{}

type binaryAnnotation struct {
	*zipkincore.BinaryAnnotation
}

func (b *binaryAnnotation) Key() string {
	return b.BinaryAnnotation.GetKey()
}

func (b *binaryAnnotation) Value() string {
	return string(b.BinaryAnnotation.GetValue())
}

func (b *binaryAnnotation) Host() codec.Endpoint {
	if b.BinaryAnnotation.Host == nil {
		return nil
	}
	return &endpoint{b.BinaryAnnotation.Host}
}

var _ codec.Annotation = &annotation{}

type annotation struct {
	*zipkincore.Annotation
}

func (a *annotation) Timestamp() time.Time {
	ts := a.Annotation.GetTimestamp()
	if ts == 0 {
		return time.Time{}
	}
	return codec.MicroToTime(ts)
}

func (a *annotation) Value() string {
	return a.Annotation.GetValue()
}

func (a *annotation) Host() codec.Endpoint {
	if a.Annotation.Host == nil {
		return nil
	}
	return &endpoint{a.Annotation.Host}
}

var _ codec.Span = &span{}

type span struct {
	*zipkincore.Span
}

func (s *span) Trace() (string, error) {
	if s.Span.GetTraceIDHigh() == 0 && s.Span.GetTraceID() == 0 {
		return "", errors.New("span does not have a trace ID")
	}

	if s.Span.GetTraceIDHigh() == 0 {
		return fmt.Sprintf("%x", s.Span.GetTraceID()), nil
	}
	return fmt.Sprintf("%x%016x", s.Span.GetTraceIDHigh(), s.Span.GetTraceID()), nil
}

func (s *span) SpanID() (string, error) {
	return formatID(s.Span.GetID()), nil
}

func (s *span) Parent() (string, error) {
	id := s.Span.GetParentID()
	if id != 0 {
		return formatID(id), nil
	}
	return "", nil
}

func (s *span) Name() string {
	return s.Span.GetName()
}

func (s *span) Annotations() []codec.Annotation {
	res := make([]codec.Annotation, 0, len(s.Span.Annotations))
	for _, ann := range s.Span.Annotations {
		res = append(res, &annotation{ann})
	}
	return res
}

func (s *span) BinaryAnnotations() ([]codec.BinaryAnnotation, error) {
	res := make([]codec.BinaryAnnotation, 0, len(s.Span.BinaryAnnotations))
	for _, ann := range s.Span.BinaryAnnotations {
		res = append(res, &binaryAnnotation{ann})
	}
	return res, nil
}

func (s *span) Timestamp() time.Time {
	ts := s.Span.GetTimestamp()
	if ts == 0 {
		return time.Time{}
	}
	return codec.MicroToTime(ts)
}

func (s *span) Duration() time.Duration {
	return time.Duration(s.Span.GetDuration()) * time.Microsecond
}

func formatID(id int64) string {
	return strconv.FormatInt(id, 16)
}
