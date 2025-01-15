// Go API over pdh syscalls
//go:build windows

package win_perf_counters

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

// Initial buffer size for return buffers
const initialBufferSize = uint32(1024) // 1kB

var errBufferLimitReached = errors.New("buffer limit reached")

// counterValue is abstraction for pdhFmtCountervalueItemDouble
type counterValue struct {
	InstanceName string
	Value        interface{}
}

// PerformanceQuery provides wrappers around Windows performance counters API for easy usage in GO
//
//nolint:interfacebloat // conditionally allow to contain more methods
type PerformanceQuery interface {
	Open() error
	Close() error
	AddCounterToQuery(counterPath string) (pdhCounterHandle, error)
	AddEnglishCounterToQuery(counterPath string) (pdhCounterHandle, error)
	GetCounterPath(counterHandle pdhCounterHandle) (string, error)
	ExpandWildCardPath(counterPath string) ([]string, error)
	GetFormattedCounterValueDouble(hCounter pdhCounterHandle) (float64, error)
	GetRawCounterValue(hCounter pdhCounterHandle) (int64, error)
	GetFormattedCounterArrayDouble(hCounter pdhCounterHandle) ([]counterValue, error)
	GetRawCounterArray(hCounter pdhCounterHandle) ([]counterValue, error)
	CollectData() error
	CollectDataWithTime() (time.Time, error)
	IsVistaOrNewer() bool
}

type PerformanceQueryCreator interface {
	NewPerformanceQuery(string, uint32) PerformanceQuery
}

// pdhError represents error returned from Performance Counters API
type pdhError struct {
	ErrorCode uint32
	errorText string
}

func (m *pdhError) Error() string {
	return m.errorText
}

func NewPdhError(code uint32) error {
	return &pdhError{
		ErrorCode: code,
		errorText: PdhFormatError(code),
	}
}

// performanceQueryImpl is implementation of PerformanceQuery interface, which calls phd.dll functions
type performanceQueryImpl struct {
	maxBufferSize uint32
	query         pdhQueryHandle
}

type performanceQueryCreatorImpl struct{}

func (performanceQueryCreatorImpl) NewPerformanceQuery(_ string, maxBufferSize uint32) PerformanceQuery {
	return &performanceQueryImpl{maxBufferSize: maxBufferSize}
}

// Open creates a new counterPath that is used to manage the collection of performance data.
// It returns counterPath handle used for subsequent calls for adding counters and querying data
func (m *performanceQueryImpl) Open() error {
	if m.query != 0 {
		err := m.Close()
		if err != nil {
			return err
		}
	}
	var handle pdhQueryHandle

	if ret := PdhOpenQuery(0, 0, &handle); ret != ErrorSuccess {
		return NewPdhError(ret)
	}
	m.query = handle
	return nil
}

// Close closes the counterPath, releases associated counter handles and frees resources
func (m *performanceQueryImpl) Close() error {
	if m.query == 0 {
		return errors.New("uninitialized query")
	}

	if ret := PdhCloseQuery(m.query); ret != ErrorSuccess {
		return NewPdhError(ret)
	}
	m.query = 0
	return nil
}

func (m *performanceQueryImpl) AddCounterToQuery(counterPath string) (pdhCounterHandle, error) {
	var counterHandle pdhCounterHandle
	if m.query == 0 {
		return 0, errors.New("uninitialized query")
	}

	if ret := PdhAddCounter(m.query, counterPath, 0, &counterHandle); ret != ErrorSuccess {
		return 0, NewPdhError(ret)
	}
	return counterHandle, nil
}

func (m *performanceQueryImpl) AddEnglishCounterToQuery(counterPath string) (pdhCounterHandle, error) {
	var counterHandle pdhCounterHandle
	if m.query == 0 {
		return 0, errors.New("uninitialized query")
	}
	if ret := PdhAddEnglishCounter(m.query, counterPath, 0, &counterHandle); ret != ErrorSuccess {
		return 0, NewPdhError(ret)
	}
	return counterHandle, nil
}

// GetCounterPath return counter information for given handle
func (m *performanceQueryImpl) GetCounterPath(counterHandle pdhCounterHandle) (string, error) {
	for buflen := initialBufferSize; buflen <= m.maxBufferSize; buflen *= 2 {
		buf := make([]byte, buflen)

		// Get the info with the current buffer size
		size := buflen
		ret := PdhGetCounterInfo(counterHandle, 0, &size, &buf[0])
		if ret == ErrorSuccess {
			ci := (*pdhCounterInfo)(unsafe.Pointer(&buf[0])) //nolint:gosec // G103: Valid use of unsafe call to create PDH_COUNTER_INFO
			return UTF16PtrToString(ci.SzFullPath), nil
		}

		// Use the size as a hint if it exceeds the current buffer size
		if size > buflen {
			buflen = size
		}

		// We got a non-recoverable error so exit here
		if ret != PdhMoreData {
			return "", NewPdhError(ret)
		}
	}

	return "", errBufferLimitReached
}

// ExpandWildCardPath  examines local computer and returns those counter paths that match the given counter path which contains wildcard characters.
func (m *performanceQueryImpl) ExpandWildCardPath(counterPath string) ([]string, error) {
	for buflen := initialBufferSize; buflen <= m.maxBufferSize; buflen *= 2 {
		buf := make([]uint16, buflen)

		// Get the info with the current buffer size
		size := buflen
		ret := PdhExpandWildCardPath(counterPath, &buf[0], &size)
		if ret == ErrorSuccess {
			return UTF16ToStringArray(buf), nil
		}

		// Use the size as a hint if it exceeds the current buffer size
		if size > buflen {
			buflen = size
		}

		// We got a non-recoverable error so exit here
		if ret != PdhMoreData {
			return nil, NewPdhError(ret)
		}
	}

	return nil, errBufferLimitReached
}

// GetFormattedCounterValueDouble computes a displayable value for the specified counter
func (*performanceQueryImpl) GetFormattedCounterValueDouble(hCounter pdhCounterHandle) (float64, error) {
	var counterType uint32
	var value pdhFmtCountervalueDouble

	if ret := PdhGetFormattedCounterValueDouble(hCounter, &counterType, &value); ret != ErrorSuccess {
		return 0, NewPdhError(ret)
	}
	if value.CStatus == PdhCstatusValidData || value.CStatus == PdhCstatusNewData {
		return value.DoubleValue, nil
	}
	return 0, NewPdhError(value.CStatus)
}

func (m *performanceQueryImpl) GetFormattedCounterArrayDouble(hCounter pdhCounterHandle) ([]counterValue, error) {
	for buflen := initialBufferSize; buflen <= m.maxBufferSize; buflen *= 2 {
		buf := make([]byte, buflen)

		// Get the info with the current buffer size
		var itemCount uint32
		size := buflen
		ret := PdhGetFormattedCounterArrayDouble(hCounter, &size, &itemCount, &buf[0])
		if ret == ErrorSuccess {
			//nolint:gosec // G103: Valid use of unsafe call to create PDH_FMT_COUNTERVALUE_ITEM_DOUBLE
			items := (*[1 << 20]pdhFmtCountervalueItemDouble)(unsafe.Pointer(&buf[0]))[:itemCount]
			values := make([]counterValue, 0, itemCount)
			for _, item := range items {
				if item.FmtValue.CStatus == PdhCstatusValidData || item.FmtValue.CStatus == PdhCstatusNewData {
					val := counterValue{UTF16PtrToString(item.SzName), item.FmtValue.DoubleValue}
					values = append(values, val)
				}
			}
			return values, nil
		}

		// Use the size as a hint if it exceeds the current buffer size
		if size > buflen {
			buflen = size
		}

		// We got a non-recoverable error so exit here
		if ret != PdhMoreData {
			return nil, NewPdhError(ret)
		}
	}

	return nil, errBufferLimitReached
}

func (m *performanceQueryImpl) GetRawCounterArray(hCounter pdhCounterHandle) ([]counterValue, error) {
	for buflen := initialBufferSize; buflen <= m.maxBufferSize; buflen *= 2 {
		buf := make([]byte, buflen)

		// Get the info with the current buffer size
		var itemCount uint32
		size := buflen
		ret := PdhGetRawCounterArray(hCounter, &size, &itemCount, &buf[0])
		if ret == ErrorSuccess {
			//nolint:gosec // G103: Valid use of unsafe call to create PDH_RAW_COUNTER_ITEM
			items := (*[1 << 20]pdhRawCounterItem)(unsafe.Pointer(&buf[0]))[:itemCount]
			values := make([]counterValue, 0, itemCount)
			for _, item := range items {
				if item.RawValue.CStatus == PdhCstatusValidData || item.RawValue.CStatus == PdhCstatusNewData {
					val := counterValue{UTF16PtrToString(item.SzName), item.RawValue.FirstValue}
					values = append(values, val)
				}
			}
			return values, nil
		}

		// Use the size as a hint if it exceeds the current buffer size
		if size > buflen {
			buflen = size
		}

		// We got a non-recoverable error so exit here
		if ret != PdhMoreData {
			return nil, NewPdhError(ret)
		}
	}

	return nil, errBufferLimitReached
}

func (m *performanceQueryImpl) CollectData() error {
	var ret uint32
	if m.query == 0 {
		return errors.New("uninitialized query")
	}

	if ret = PdhCollectQueryData(m.query); ret != ErrorSuccess {
		return NewPdhError(ret)
	}
	return nil
}

func (m *performanceQueryImpl) CollectDataWithTime() (time.Time, error) {
	if m.query == 0 {
		return time.Now(), errors.New("uninitialized query")
	}
	ret, mtime := PdhCollectQueryDataWithTime(m.query)
	if ret != ErrorSuccess {
		return time.Now(), NewPdhError(ret)
	}
	return mtime, nil
}

func (*performanceQueryImpl) IsVistaOrNewer() bool {
	return PdhAddEnglishCounterSupported()
}

func (m *performanceQueryImpl) GetRawCounterValue(hCounter pdhCounterHandle) (int64, error) {
	if m.query == 0 {
		return 0, errors.New("uninitialised query")
	}

	var counterType uint32
	var value pdhRawCounter
	var ret uint32

	if ret = PdhGetRawCounterValue(hCounter, &counterType, &value); ret == ErrorSuccess {
		if value.CStatus == PdhCstatusValidData || value.CStatus == PdhCstatusNewData {
			return value.FirstValue, nil
		}
		return 0, NewPdhError(value.CStatus)
	}
	return 0, NewPdhError(ret)
}

// UTF16PtrToString converts Windows API LPTSTR (pointer to string) to go string
func UTF16PtrToString(s *uint16) string {
	if s == nil {
		return ""
	}
	//nolint:gosec // G103: Valid use of unsafe call to create string from Windows API LPTSTR (pointer to string)
	return syscall.UTF16ToString((*[1 << 29]uint16)(unsafe.Pointer(s))[0:])
}

// UTF16ToStringArray converts list of Windows API NULL terminated strings  to go string array
func UTF16ToStringArray(buf []uint16) []string {
	var strings []string
	nextLineStart := 0
	stringLine := UTF16PtrToString(&buf[0])
	for stringLine != "" {
		strings = append(strings, stringLine)
		nextLineStart += len([]rune(stringLine)) + 1
		remainingBuf := buf[nextLineStart:]
		stringLine = UTF16PtrToString(&remainingBuf[0])
	}
	return strings
}
