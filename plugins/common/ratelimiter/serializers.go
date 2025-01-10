package ratelimiter

import (
	"bytes"
	"math"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
)

// Serializer interface abstracting the different implementations of a
// limited-size serializer
type Serializer interface {
	Serialize(metric telegraf.Metric, limit int64) ([]byte, error)
	SerializeBatch(metrics []telegraf.Metric, limit int64) ([]byte, error)
}

// Individual serializers do serialize each metric individually using the
// serializer's Serialize() function and add the resulting output to the buffer
// until the limit is reached. This only works for serializers NOT requiring
// the serialization of a batch as-a-whole.
type IndividualSerializer struct {
	serializer telegraf.Serializer
	buffer     *bytes.Buffer
}

func NewIndividualSerializer(s telegraf.Serializer) *IndividualSerializer {
	return &IndividualSerializer{
		serializer: s,
		buffer:     &bytes.Buffer{},
	}
}

func (s *IndividualSerializer) Serialize(metric telegraf.Metric, limit int64) ([]byte, error) {
	// Do the serialization
	buf, err := s.serializer.Serialize(metric)
	if err != nil {
		return nil, err
	}

	// The serialized metric fits into the limit, so output it
	if buflen := int64(len(buf)); buflen <= limit {
		return buf, nil
	}

	// The serialized metric exceeds the limit
	return nil, internal.ErrSizeLimitReached
}

func (s *IndividualSerializer) SerializeBatch(metrics []telegraf.Metric, limit int64) ([]byte, error) {
	// Grow the buffer so it can hold at least the required size. This will
	// save us from reallocate often
	s.buffer.Reset()
	if limit > 0 && limit < int64(math.MaxInt) {
		s.buffer.Grow(int(limit))
	}

	// Prepare a potential write error and be optimistic
	werr := &internal.PartialWriteError{
		MetricsAccept: make([]int, 0, len(metrics)),
	}

	// Iterate through the metrics, serialize them and add them to the output
	// buffer if they are within the size limit.
	var used int64
	for i, m := range metrics {
		buf, err := s.serializer.Serialize(m)
		if err != nil {
			// Failing serialization is a fatal error so mark the metric as such
			werr.Err = internal.ErrSerialization
			werr.MetricsReject = append(werr.MetricsReject, i)
			werr.MetricsRejectErrors = append(werr.MetricsRejectErrors, err)
			continue
		}

		// The serialized metric fits into the limit, so add it to the output
		if usedAdded := used + int64(len(buf)); usedAdded <= limit {
			if _, err := s.buffer.Write(buf); err != nil {
				return nil, err
			}
			werr.MetricsAccept = append(werr.MetricsAccept, i)
			used = usedAdded
			continue
		}

		// Return only the size-limit-reached error if all metrics failed.
		if used == 0 {
			return nil, internal.ErrSizeLimitReached
		}

		// Adding the serialized metric would exceed the limit so exit with an
		// WriteError and fill in the required information
		werr.Err = internal.ErrSizeLimitReached
		break
	}
	if werr.Err != nil {
		return s.buffer.Bytes(), werr
	}
	return s.buffer.Bytes(), nil
}
