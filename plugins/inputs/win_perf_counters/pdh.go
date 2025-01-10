// Copyright (c) 2010 The win Authors. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
// 3. The names of the authors may not be used to endorse or promote products
//    derived from this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHORS ``AS IS'' AND ANY EXPRESS OR
// IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES
// OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED.
// IN NO EVENT SHALL THE AUTHORS BE LIABLE FOR ANY DIRECT, INDIRECT,
// INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT
// NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF
// THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//
// This is the official list of 'win' authors for copyright purposes.
//
// Alexander Neumann <an2048@googlemail.com>
// Joseph Watson <jtwatson@linux-consulting.us>
// Kevin Pors <krpors@gmail.com>

//go:build windows

package win_perf_counters

import (
	"fmt"
	"syscall"
	"unsafe"

	"time"

	"golang.org/x/sys/windows"
)

// Error codes
const (
	ErrorSuccess                = 0
	ErrorFailure                = 1
	ErrorInvalidFunction        = 1
	EpochDifferenceMicros int64 = 11644473600000000
)

type (
	HANDLE uintptr
)

// PDH error codes, which can be returned by all Pdh* functions. Taken from mingw-w64 pdhmsg.h

const (
	PdhCstatusValidData                   = 0x00000000 // The returned data is valid.
	PdhCstatusNewData                     = 0x00000001 // The return data value is valid and different from the last sample.
	PdhCstatusNoMachine                   = 0x800007D0 // Unable to connect to the specified computer, or the computer is offline.
	PdhCstatusNoInstance                  = 0x800007D1
	PdhMoreData                           = 0x800007D2 // The PdhGetFormattedCounterArray* function can return this if there's 'more data to be displayed'.
	PdhCstatusItemNotValidated            = 0x800007D3
	PdhRetry                              = 0x800007D4
	PdhNoData                             = 0x800007D5 // The query does not currently contain any counters (for example, limited access)
	PdhCalcNegativeDenominator            = 0x800007D6
	PdhCalcNegativeTimebase               = 0x800007D7
	PdhCalcNegativeValue                  = 0x800007D8
	PdhDialogCancelled                    = 0x800007D9
	PdhEndOfLogFile                       = 0x800007DA
	PdhAsyncQueryTimeout                  = 0x800007DB
	PdhCannotSetDefaultRealtimeDatasource = 0x800007DC
	PdhCstatusNoObject                    = 0xC0000BB8
	PdhCstatusNoCounter                   = 0xC0000BB9 // The specified counter could not be found.
	PdhCstatusInvalidData                 = 0xC0000BBA // The counter was successfully found, but the data returned is not valid.
	PdhMemoryAllocationFailure            = 0xC0000BBB
	PdhInvalidHandle                      = 0xC0000BBC
	PdhInvalidArgument                    = 0xC0000BBD // Required argument is missing or incorrect.
	PdhFunctionNotFound                   = 0xC0000BBE
	PdhCstatusNoCountername               = 0xC0000BBF
	PdhCstatusBadCountername              = 0xC0000BC0 // Unable to parse the counter path. Check the format and syntax of the specified path.
	PdhInvalidBuffer                      = 0xC0000BC1
	PdhInsufficientBuffer                 = 0xC0000BC2
	PdhCannotConnectMachine               = 0xC0000BC3
	PdhInvalidPath                        = 0xC0000BC4
	PdhInvalidInstance                    = 0xC0000BC5
	PdhInvalidData                        = 0xC0000BC6 // specified counter does not contain valid data or a successful status code.
	PdhNoDialogData                       = 0xC0000BC7
	PdhCannotReadNameStrings              = 0xC0000BC8
	PdhLogFileCreateError                 = 0xC0000BC9
	PdhLogFileOpenError                   = 0xC0000BCA
	PdhLogTypeNotFound                    = 0xC0000BCB
	PdhNoMoreData                         = 0xC0000BCC
	PdhEntryNotInLogFile                  = 0xC0000BCD
	PdhDataSourceIsLogFile                = 0xC0000BCE
	PdhDataSourceIsRealTime               = 0xC0000BCF
	PdhUnableReadLogHeader                = 0xC0000BD0
	PdhFileNotFound                       = 0xC0000BD1
	PdhFileAlreadyExists                  = 0xC0000BD2
	PdhNotImplemented                     = 0xC0000BD3
	PdhStringNotFound                     = 0xC0000BD4
	PdhUnableMapNameFiles                 = 0x80000BD5
	PdhUnknownLogFormat                   = 0xC0000BD6
	PdhUnknownLogsvcCommand               = 0xC0000BD7
	PdhLogsvcQueryNotFound                = 0xC0000BD8
	PdhLogsvcNotOpened                    = 0xC0000BD9
	PdhWbemError                          = 0xC0000BDA
	PdhAccessDenied                       = 0xC0000BDB
	PdhLogFileTooSmall                    = 0xC0000BDC
	PdhInvalidDatasource                  = 0xC0000BDD
	PdhInvalidSqldb                       = 0xC0000BDE
	PdhNoCounters                         = 0xC0000BDF
	PdhSQLAllocFailed                     = 0xC0000BE0
	PdhSQLAllocconFailed                  = 0xC0000BE1
	PdhSQLExecDirectFailed                = 0xC0000BE2
	PdhSQLFetchFailed                     = 0xC0000BE3
	PdhSQLRowcountFailed                  = 0xC0000BE4
	PdhSQLMoreResultsFailed               = 0xC0000BE5
	PdhSQLConnectFailed                   = 0xC0000BE6
	PdhSQLBindFailed                      = 0xC0000BE7
	PdhCannotConnectWmiServer             = 0xC0000BE8
	PdhPlaCollectionAlreadyRunning        = 0xC0000BE9
	PdhPlaErrorScheduleOverlap            = 0xC0000BEA
	PdhPlaCollectionNotFound              = 0xC0000BEB
	PdhPlaErrorScheduleElapsed            = 0xC0000BEC
	PdhPlaErrorNostart                    = 0xC0000BED
	PdhPlaErrorAlreadyExists              = 0xC0000BEE
	PdhPlaErrorTypeMismatch               = 0xC0000BEF
	PdhPlaErrorFilepath                   = 0xC0000BF0
	PdhPlaServiceError                    = 0xC0000BF1
	PdhPlaValidationError                 = 0xC0000BF2
	PdhPlaValidationWarning               = 0x80000BF3
	PdhPlaErrorNameTooLong                = 0xC0000BF4
	PdhInvalidSQLLogFormat                = 0xC0000BF5
	PdhCounterAlreadyInQuery              = 0xC0000BF6
	PdhBinaryLogCorrupt                   = 0xC0000BF7
	PdhLogSampleTooSmall                  = 0xC0000BF8
	PdhOsLaterVersion                     = 0xC0000BF9
	PdhOsEarlierVersion                   = 0xC0000BFA
	PdhIncorrectAppendTime                = 0xC0000BFB
	PdhUnmatchedAppendCounter             = 0xC0000BFC
	PdhSQLAlterDetailFailed               = 0xC0000BFD
	PdhQueryPerfDataTimeout               = 0xC0000BFE
)

var PDHErrors = map[uint32]string{
	PdhCstatusValidData:                   "PDH_CSTATUS_VALID_DATA",
	PdhCstatusNewData:                     "PDH_CSTATUS_NEW_DATA",
	PdhCstatusNoMachine:                   "PDH_CSTATUS_NO_MACHINE",
	PdhCstatusNoInstance:                  "PDH_CSTATUS_NO_INSTANCE",
	PdhMoreData:                           "PDH_MORE_DATA",
	PdhCstatusItemNotValidated:            "PDH_CSTATUS_ITEM_NOT_VALIDATED",
	PdhRetry:                              "PDH_RETRY",
	PdhNoData:                             "PDH_NO_DATA",
	PdhCalcNegativeDenominator:            "PDH_CALC_NEGATIVE_DENOMINATOR",
	PdhCalcNegativeTimebase:               "PDH_CALC_NEGATIVE_TIMEBASE",
	PdhCalcNegativeValue:                  "PDH_CALC_NEGATIVE_VALUE",
	PdhDialogCancelled:                    "PDH_DIALOG_CANCELLED",
	PdhEndOfLogFile:                       "PDH_END_OF_LOG_FILE",
	PdhAsyncQueryTimeout:                  "PDH_ASYNC_QUERY_TIMEOUT",
	PdhCannotSetDefaultRealtimeDatasource: "PDH_CANNOT_SET_DEFAULT_REALTIME_DATASOURCE",
	PdhCstatusNoObject:                    "PDH_CSTATUS_NO_OBJECT",
	PdhCstatusNoCounter:                   "PDH_CSTATUS_NO_COUNTER",
	PdhCstatusInvalidData:                 "PDH_CSTATUS_INVALID_DATA",
	PdhMemoryAllocationFailure:            "PDH_MEMORY_ALLOCATION_FAILURE",
	PdhInvalidHandle:                      "PDH_INVALID_HANDLE",
	PdhInvalidArgument:                    "PDH_INVALID_ARGUMENT",
	PdhFunctionNotFound:                   "PDH_FUNCTION_NOT_FOUND",
	PdhCstatusNoCountername:               "PDH_CSTATUS_NO_COUNTERNAME",
	PdhCstatusBadCountername:              "PDH_CSTATUS_BAD_COUNTERNAME",
	PdhInvalidBuffer:                      "PDH_INVALID_BUFFER",
	PdhInsufficientBuffer:                 "PDH_INSUFFICIENT_BUFFER",
	PdhCannotConnectMachine:               "PDH_CANNOT_CONNECT_MACHINE",
	PdhInvalidPath:                        "PDH_INVALID_PATH",
	PdhInvalidInstance:                    "PDH_INVALID_INSTANCE",
	PdhInvalidData:                        "PDH_INVALID_DATA",
	PdhNoDialogData:                       "PDH_NO_DIALOG_DATA",
	PdhCannotReadNameStrings:              "PDH_CANNOT_READ_NAME_STRINGS",
	PdhLogFileCreateError:                 "PDH_LOG_FILE_CREATE_ERROR",
	PdhLogFileOpenError:                   "PDH_LOG_FILE_OPEN_ERROR",
	PdhLogTypeNotFound:                    "PDH_LOG_TYPE_NOT_FOUND",
	PdhNoMoreData:                         "PDH_NO_MORE_DATA",
	PdhEntryNotInLogFile:                  "PDH_ENTRY_NOT_IN_LOG_FILE",
	PdhDataSourceIsLogFile:                "PDH_DATA_SOURCE_IS_LOG_FILE",
	PdhDataSourceIsRealTime:               "PDH_DATA_SOURCE_IS_REAL_TIME",
	PdhUnableReadLogHeader:                "PDH_UNABLE_READ_LOG_HEADER",
	PdhFileNotFound:                       "PDH_FILE_NOT_FOUND",
	PdhFileAlreadyExists:                  "PDH_FILE_ALREADY_EXISTS",
	PdhNotImplemented:                     "PDH_NOT_IMPLEMENTED",
	PdhStringNotFound:                     "PDH_STRING_NOT_FOUND",
	PdhUnableMapNameFiles:                 "PDH_UNABLE_MAP_NAME_FILES",
	PdhUnknownLogFormat:                   "PDH_UNKNOWN_LOG_FORMAT",
	PdhUnknownLogsvcCommand:               "PDH_UNKNOWN_LOGSVC_COMMAND",
	PdhLogsvcQueryNotFound:                "PDH_LOGSVC_QUERY_NOT_FOUND",
	PdhLogsvcNotOpened:                    "PDH_LOGSVC_NOT_OPENED",
	PdhWbemError:                          "PDH_WBEM_ERROR",
	PdhAccessDenied:                       "PDH_ACCESS_DENIED",
	PdhLogFileTooSmall:                    "PDH_LOG_FILE_TOO_SMALL",
	PdhInvalidDatasource:                  "PDH_INVALID_DATASOURCE",
	PdhInvalidSqldb:                       "PDH_INVALID_SQLDB",
	PdhNoCounters:                         "PDH_NO_COUNTERS",
	PdhSQLAllocFailed:                     "PDH_SQL_ALLOC_FAILED",
	PdhSQLAllocconFailed:                  "PDH_SQL_ALLOCCON_FAILED",
	PdhSQLExecDirectFailed:                "PDH_SQL_EXEC_DIRECT_FAILED",
	PdhSQLFetchFailed:                     "PDH_SQL_FETCH_FAILED",
	PdhSQLRowcountFailed:                  "PDH_SQL_ROWCOUNT_FAILED",
	PdhSQLMoreResultsFailed:               "PDH_SQL_MORE_RESULTS_FAILED",
	PdhSQLConnectFailed:                   "PDH_SQL_CONNECT_FAILED",
	PdhSQLBindFailed:                      "PDH_SQL_BIND_FAILED",
	PdhCannotConnectWmiServer:             "PDH_CANNOT_CONNECT_WMI_SERVER",
	PdhPlaCollectionAlreadyRunning:        "PDH_PLA_COLLECTION_ALREADY_RUNNING",
	PdhPlaErrorScheduleOverlap:            "PDH_PLA_ERROR_SCHEDULE_OVERLAP",
	PdhPlaCollectionNotFound:              "PDH_PLA_COLLECTION_NOT_FOUND",
	PdhPlaErrorScheduleElapsed:            "PDH_PLA_ERROR_SCHEDULE_ELAPSED",
	PdhPlaErrorNostart:                    "PDH_PLA_ERROR_NOSTART",
	PdhPlaErrorAlreadyExists:              "PDH_PLA_ERROR_ALREADY_EXISTS",
	PdhPlaErrorTypeMismatch:               "PDH_PLA_ERROR_TYPE_MISMATCH",
	PdhPlaErrorFilepath:                   "PDH_PLA_ERROR_FILEPATH",
	PdhPlaServiceError:                    "PDH_PLA_SERVICE_ERROR",
	PdhPlaValidationError:                 "PDH_PLA_VALIDATION_ERROR",
	PdhPlaValidationWarning:               "PDH_PLA_VALIDATION_WARNING",
	PdhPlaErrorNameTooLong:                "PDH_PLA_ERROR_NAME_TOO_LONG",
	PdhInvalidSQLLogFormat:                "PDH_INVALID_SQL_LOG_FORMAT",
	PdhCounterAlreadyInQuery:              "PDH_COUNTER_ALREADY_IN_QUERY",
	PdhBinaryLogCorrupt:                   "PDH_BINARY_LOG_CORRUPT",
	PdhLogSampleTooSmall:                  "PDH_LOG_SAMPLE_TOO_SMALL",
	PdhOsLaterVersion:                     "PDH_OS_LATER_VERSION",
	PdhOsEarlierVersion:                   "PDH_OS_EARLIER_VERSION",
	PdhIncorrectAppendTime:                "PDH_INCORRECT_APPEND_TIME",
	PdhUnmatchedAppendCounter:             "PDH_UNMATCHED_APPEND_COUNTER",
	PdhSQLAlterDetailFailed:               "PDH_SQL_ALTER_DETAIL_FAILED",
	PdhQueryPerfDataTimeout:               "PDH_QUERY_PERF_DATA_TIMEOUT",
}

// Formatting options for GetFormattedCounterValue().
const (
	PdhFmtRaw          = 0x00000010
	PdhFmtAnsi         = 0x00000020
	PdhFmtUnicode      = 0x00000040
	PdhFmtLong         = 0x00000100 // Return data as a long int.
	PdhFmtDouble       = 0x00000200 // Return data as a double precision floating point real.
	PdhFmtLarge        = 0x00000400 // Return data as a 64 bit integer.
	PdhFmtNoscale      = 0x00001000 // can be OR-ed: Do not apply the counter's default scaling factor.
	PdhFmt1000         = 0x00002000 // can be OR-ed: multiply the actual value by 1,000.
	PdhFmtNodata       = 0x00004000 // can be OR-ed: unknown what this is for, MSDN says nothing.
	PdhFmtNocap100     = 0x00008000 // can be OR-ed: do not cap values > 100.
	PerfDetailCostly   = 0x00010000
	PerfDetailStandard = 0x0000FFFF
)

type (
	pdhQueryHandle   HANDLE // query handle
	pdhCounterHandle HANDLE // counter handle
)

var (
	// Library
	libPdhDll *syscall.DLL

	// Functions
	pdhAddCounterW               *syscall.Proc
	pdhAddEnglishCounterW        *syscall.Proc
	pdhCloseQuery                *syscall.Proc
	pdhCollectQueryData          *syscall.Proc
	pdhCollectQueryDataWithTime  *syscall.Proc
	pdhGetFormattedCounterValue  *syscall.Proc
	pdhGetFormattedCounterArrayW *syscall.Proc
	pdhOpenQuery                 *syscall.Proc
	pdhValidatePathW             *syscall.Proc
	pdhExpandWildCardPathW       *syscall.Proc
	pdhGetCounterInfoW           *syscall.Proc
	pdhGetRawCounterValue        *syscall.Proc
	pdhGetRawCounterArrayW       *syscall.Proc
)

func init() {
	// Library
	libPdhDll = syscall.MustLoadDLL("pdh.dll")

	// Functions
	pdhAddCounterW = libPdhDll.MustFindProc("PdhAddCounterW")
	pdhAddEnglishCounterW, _ = libPdhDll.FindProc("PdhAddEnglishCounterW") // XXX: only supported on versions > Vista.
	pdhCloseQuery = libPdhDll.MustFindProc("PdhCloseQuery")
	pdhCollectQueryData = libPdhDll.MustFindProc("PdhCollectQueryData")
	pdhCollectQueryDataWithTime, _ = libPdhDll.FindProc("PdhCollectQueryDataWithTime")
	pdhGetFormattedCounterValue = libPdhDll.MustFindProc("PdhGetFormattedCounterValue")
	pdhGetFormattedCounterArrayW = libPdhDll.MustFindProc("PdhGetFormattedCounterArrayW")
	pdhOpenQuery = libPdhDll.MustFindProc("PdhOpenQuery")
	pdhValidatePathW = libPdhDll.MustFindProc("PdhValidatePathW")
	pdhExpandWildCardPathW = libPdhDll.MustFindProc("PdhExpandWildCardPathW")
	pdhGetCounterInfoW = libPdhDll.MustFindProc("PdhGetCounterInfoW")
	pdhGetRawCounterValue = libPdhDll.MustFindProc("PdhGetRawCounterValue")
	pdhGetRawCounterArrayW = libPdhDll.MustFindProc("PdhGetRawCounterArrayW")
}

// PdhAddCounter adds the specified counter to the query. This is the internationalized version. Preferably, use the
// function PdhAddEnglishCounter instead. hQuery is the query handle, which has been fetched by PdhOpenQuery.
// szFullCounterPath is a full, internationalized counter path (this will differ per Windows language version).
// dwUserData is a 'user-defined value', which becomes part of the counter information. To retrieve this value
// later, call PdhGetCounterInfo() and access dwQueryUserData of the pdhCounterInfo structure.
//
// Examples of szFullCounterPath (in an English version of Windows):
//
//	\\Processor(_Total)\\% Idle Time
//	\\Processor(_Total)\\% Processor Time
//	\\LogicalDisk(C:)\% Free Space
//
// To view all (internationalized...) counters on a system, there are three non-programmatic ways: perfmon utility,
// the typeperf command, and the registry editor. perfmon.exe is perhaps the easiest way, because it's basically a
// full implementation of the pdh.dll API, except with a GUI and all that. The registry setting also provides an
// interface to the available counters, and can be found at the following key:
//
//	HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Perflib\CurrentLanguage
//
// This registry key contains several values as follows:
//
//	1
//	1847
//	2
//	System
//	4
//	Memory
//	6
//	% Processor Time
//	... many, many more
//
// Somehow, these numeric values can be used as szFullCounterPath too:
//
//	\2\6 will correspond to \\System\% Processor Time
//
// The typeperf command may also be pretty easy. To find all performance counters, simply execute:
//
//	typeperf -qx
func PdhAddCounter(hQuery pdhQueryHandle, szFullCounterPath string, dwUserData uintptr, phCounter *pdhCounterHandle) uint32 {
	ptxt, _ := syscall.UTF16PtrFromString(szFullCounterPath)
	ret, _, _ := pdhAddCounterW.Call(
		uintptr(hQuery),
		uintptr(unsafe.Pointer(ptxt)), //nolint:gosec // G103: Valid use of unsafe call to pass ptxt
		dwUserData,
		uintptr(unsafe.Pointer(phCounter))) //nolint:gosec // G103: Valid use of unsafe call to pass phCounter

	return uint32(ret)
}

// PdhAddEnglishCounterSupported returns true if PdhAddEnglishCounterW Win API function was found in pdh.dll.
// PdhAddEnglishCounterW function is not supported on pre-Windows Vista systems
func PdhAddEnglishCounterSupported() bool {
	return pdhAddEnglishCounterW != nil
}

// PdhAddEnglishCounter adds the specified language-neutral counter to the query. See the PdhAddCounter function. This function only exists on
// Windows versions higher than Vista.
func PdhAddEnglishCounter(hQuery pdhQueryHandle, szFullCounterPath string, dwUserData uintptr, phCounter *pdhCounterHandle) uint32 {
	if pdhAddEnglishCounterW == nil {
		return ErrorInvalidFunction
	}

	ptxt, _ := syscall.UTF16PtrFromString(szFullCounterPath)
	ret, _, _ := pdhAddEnglishCounterW.Call(
		uintptr(hQuery),
		uintptr(unsafe.Pointer(ptxt)), //nolint:gosec // G103: Valid use of unsafe call to pass ptxt
		dwUserData,
		uintptr(unsafe.Pointer(phCounter))) //nolint:gosec // G103: Valid use of unsafe call to pass phCounter

	return uint32(ret)
}

// PdhCloseQuery closes all counters contained in the specified query, closes all handles related to the query,
// and frees all memory associated with the query.
func PdhCloseQuery(hQuery pdhQueryHandle) uint32 {
	ret, _, _ := pdhCloseQuery.Call(uintptr(hQuery))

	return uint32(ret)
}

// PdhCollectQueryData collects the current raw data value for all counters in the specified query and updates the status
// code of each counter. With some counters, this function needs to be repeatedly called before the value
// of the counter can be extracted with PdhGetFormattedCounterValue(). For example, the following code
// requires at least two calls:
//
//	var handle win.PDH_HQUERY
//	var counterHandle win.PDH_HCOUNTER
//	ret := win.PdhOpenQuery(0, 0, &handle)
//	ret = win.PdhAddEnglishCounter(handle, "\\Processor(_Total)\\% Idle Time", 0, &counterHandle)
//	var derp win.PDH_FMT_COUNTERVALUE_DOUBLE
//
//	ret = win.PdhCollectQueryData(handle)
//	fmt.Printf("Collect return code is %x\n", ret) // return code will be PDH_CSTATUS_INVALID_DATA
//	ret = win.PdhGetFormattedCounterValueDouble(counterHandle, 0, &derp)
//
//	ret = win.PdhCollectQueryData(handle)
//	fmt.Printf("Collect return code is %x\n", ret) // return code will be ERROR_SUCCESS
//	ret = win.PdhGetFormattedCounterValueDouble(counterHandle, 0, &derp)
//
// The PdhCollectQueryData will return an error in the first call because it needs two values for
// displaying the correct data for the processor idle time. The second call will have a 0 return code.
func PdhCollectQueryData(hQuery pdhQueryHandle) uint32 {
	ret, _, _ := pdhCollectQueryData.Call(uintptr(hQuery))

	return uint32(ret)
}

// PdhCollectQueryDataWithTime queries data from perfmon, retrieving the device/windows timestamp from the node it was collected on.
// Converts the filetime structure to a GO time class and returns the native time.
func PdhCollectQueryDataWithTime(hQuery pdhQueryHandle) (uint32, time.Time) {
	var localFileTime fileTime
	//nolint:gosec // G103: Valid use of unsafe call to pass localFileTime
	ret, _, _ := pdhCollectQueryDataWithTime.Call(uintptr(hQuery), uintptr(unsafe.Pointer(&localFileTime)))

	if ret == ErrorSuccess {
		var utcFileTime fileTime
		ret, _, _ := kernelLocalFileTimeToFileTime.Call(
			uintptr(unsafe.Pointer(&localFileTime)), //nolint:gosec // G103: Valid use of unsafe call to pass localFileTime
			uintptr(unsafe.Pointer(&utcFileTime)))   //nolint:gosec // G103: Valid use of unsafe call to pass utcFileTime

		if ret == 0 {
			return uint32(ErrorFailure), time.Now()
		}

		// First convert 100-ns intervals to microseconds, then adjust for the
		// epoch difference
		var totalMicroSeconds int64
		totalMicroSeconds = ((int64(utcFileTime.dwHighDateTime) << 32) | int64(utcFileTime.dwLowDateTime)) / 10
		totalMicroSeconds -= EpochDifferenceMicros

		retTime := time.Unix(0, totalMicroSeconds*1000)

		return uint32(ErrorSuccess), retTime
	}

	return uint32(ret), time.Now()
}

// PdhGetFormattedCounterValueDouble formats the given hCounter using a 'double'. The result is set into the specialized union struct pValue.
// This function does not directly translate to a Windows counterpart due to union specialization tricks.
func PdhGetFormattedCounterValueDouble(hCounter pdhCounterHandle, lpdwType *uint32, pValue *pdhFmtCountervalueDouble) uint32 {
	ret, _, _ := pdhGetFormattedCounterValue.Call(
		uintptr(hCounter),
		uintptr(PdhFmtDouble|PdhFmtNocap100),
		uintptr(unsafe.Pointer(lpdwType)), //nolint:gosec // G103: Valid use of unsafe call to pass lpdwType
		uintptr(unsafe.Pointer(pValue)))   //nolint:gosec // G103: Valid use of unsafe call to pass pValue

	return uint32(ret)
}

// PdhGetFormattedCounterArrayDouble returns an array of formatted counter values. Use this function when you want to format the counter values of a
// counter that contains a wildcard character for the instance name. The itemBuffer must a slice of type pdhFmtCountervalueItemDouble.
// An example of how this function can be used:
//
//	okPath := "\\Process(*)\\% Processor Time" // notice the wildcard * character
//
//	// omitted all necessary stuff ...
//
//	var bufSize uint32
//	var bufCount uint32
//	var size uint32 = uint32(unsafe.Sizeof(win.PDH_FMT_COUNTERVALUE_ITEM_DOUBLE{}))
//	var emptyBuf [1]win.PDH_FMT_COUNTERVALUE_ITEM_DOUBLE // need at least 1 addressable null ptr.
//
//	for {
//		// collect
//		ret := win.PdhCollectQueryData(queryHandle)
//		if ret == win.ERROR_SUCCESS {
//			ret = win.PdhGetFormattedCounterArrayDouble(counterHandle, &bufSize, &bufCount, &emptyBuf[0]) // uses null ptr here according to MSDN.
//			if ret == win.PDH_MORE_DATA {
//				filledBuf := make([]win.PDH_FMT_COUNTERVALUE_ITEM_DOUBLE, bufCount*size)
//				ret = win.PdhGetFormattedCounterArrayDouble(counterHandle, &bufSize, &bufCount, &filledBuf[0])
//				for i := 0; i < int(bufCount); i++ {
//					c := filledBuf[i]
//					var s string = win.UTF16PtrToString(c.SzName)
//					fmt.Printf("Index %d -> %s, value %v\n", i, s, c.FmtValue.DoubleValue)
//				}
//
//				filledBuf = nil
//				// Need to at least set bufSize to zero, because if not, the function will not
//				// return PDH_MORE_DATA and will not set the bufSize.
//				bufCount = 0
//				bufSize = 0
//			}
//
//			time.Sleep(2000 * time.Millisecond)
//		}
//	}
func PdhGetFormattedCounterArrayDouble(hCounter pdhCounterHandle, lpdwBufferSize, lpdwBufferCount *uint32, itemBuffer *byte) uint32 {
	ret, _, _ := pdhGetFormattedCounterArrayW.Call(
		uintptr(hCounter),
		uintptr(PdhFmtDouble|PdhFmtNocap100),
		uintptr(unsafe.Pointer(lpdwBufferSize)),  //nolint:gosec // G103: Valid use of unsafe call to pass lpdwBufferSize
		uintptr(unsafe.Pointer(lpdwBufferCount)), //nolint:gosec // G103: Valid use of unsafe call to pass lpdwBufferCount
		uintptr(unsafe.Pointer(itemBuffer)))      //nolint:gosec // G103: Valid use of unsafe call to pass itemBuffer

	return uint32(ret)
}

// PdhOpenQuery creates a new query that is used to manage the collection of performance data.
// szDataSource is a null terminated string that specifies the name of the log file from which to
// retrieve the performance data. If 0, performance data is collected from a real-time data source.
// dwUserData is a user-defined value to associate with this query. To retrieve the user data later,
// call PdhGetCounterInfo and access dwQueryUserData of the pdhCounterInfo structure. phQuery is
// the handle to the query, and must be used in subsequent calls. This function returns a PDH_
// constant error code, or ErrorSuccess if the call succeeded.
func PdhOpenQuery(szDataSource, dwUserData uintptr, phQuery *pdhQueryHandle) uint32 {
	ret, _, _ := pdhOpenQuery.Call(
		szDataSource,
		dwUserData,
		uintptr(unsafe.Pointer(phQuery))) //nolint:gosec // G103: Valid use of unsafe call to pass phQuery

	return uint32(ret)
}

// PdhExpandWildCardPath examines the specified computer or log file and returns those counter paths that match the given counter path
// which contains wildcard characters. The general counter path format is as follows:
//
// \\computer\object(parent/instance#index)\counter
//
// The parent, instance, index, and counter components of the counter path may contain either a valid name or a wildcard character.
// The computer, parent, instance, and index components are not necessary for all counters.
//
// The following is a list of the possible formats:
//
// \\computer\object(parent/instance#index)\counter
// \\computer\object(parent/instance)\counter
// \\computer\object(instance#index)\counter
// \\computer\object(instance)\counter
// \\computer\object\counter
// \object(parent/instance#index)\counter
// \object(parent/instance)\counter
// \object(instance#index)\counter
// \object(instance)\counter
// \object\counter
// Use an asterisk (*) as the wildcard character, for example, \object(*)\counter.
//
// If a wildcard character is specified in the parent name, all instances of the specified object
// that match the specified instance and counter fields will be returned.
// For example, \object(*/instance)\counter.
//
// If a wildcard character is specified in the instance name, all instances of the specified object and parent object will be returned if all instance names
// corresponding to the specified index match the wildcard character. For example, \object(parent/*)\counter.
// If the object does not contain an instance, an error occurs.
//
// If a wildcard character is specified in the counter name, all counters of the specified object are returned.
//
// Partial counter path string matches (for example, "pro*") are supported.
func PdhExpandWildCardPath(szWildCardPath string, mszExpandedPathList *uint16, pcchPathListLength *uint32) uint32 {
	ptxt, _ := syscall.UTF16PtrFromString(szWildCardPath)
	flags := uint32(0) // expand instances and counters
	ret, _, _ := pdhExpandWildCardPathW.Call(
		0,                             // search counters on local computer
		uintptr(unsafe.Pointer(ptxt)), //nolint:gosec // G103: Valid use of unsafe call to pass ptxt
		uintptr(unsafe.Pointer(mszExpandedPathList)), //nolint:gosec // G103: Valid use of unsafe call to pass mszExpandedPathList
		uintptr(unsafe.Pointer(pcchPathListLength)),  //nolint:gosec // G103: Valid use of unsafe call to pass pcchPathListLength
		uintptr(unsafe.Pointer(&flags)))              //nolint:gosec // G103: Valid use of unsafe call to pass flags

	return uint32(ret)
}

// PdhValidatePath validates a path. Will return ErrorSuccess when ok, or PdhCstatusBadCountername when the path is erroneous.
func PdhValidatePath(path string) uint32 {
	ptxt, _ := syscall.UTF16PtrFromString(path)
	ret, _, _ := pdhValidatePathW.Call(uintptr(unsafe.Pointer(ptxt))) //nolint:gosec // G103: Valid use of unsafe call to pass ptxt

	return uint32(ret)
}

func PdhFormatError(msgID uint32) string {
	var flags uint32 = windows.FORMAT_MESSAGE_FROM_HMODULE | windows.FORMAT_MESSAGE_ARGUMENT_ARRAY | windows.FORMAT_MESSAGE_IGNORE_INSERTS
	buf := make([]uint16, 300)
	_, err := windows.FormatMessage(flags, uintptr(libPdhDll.Handle), msgID, 0, buf, nil)
	if err == nil {
		return UTF16PtrToString(&buf[0])
	}
	return fmt.Sprintf("(pdhErr=%d) %s", msgID, err.Error())
}

// PdhGetCounterInfo retrieves information about a counter, such as data size, counter type, path, and user-supplied data values
// hCounter [in]
// Handle of the counter from which you want to retrieve information. The PdhAddCounter function returns this handle.
//
// bRetrieveExplainText [in]
// Determines whether explain text is retrieved. If you set this parameter to TRUE, the explain text for the counter is retrieved.
// If you set this parameter to FALSE, the field in the returned buffer is NULL.
//
// pdwBufferSize [in, out]
// Size of the lpBuffer buffer, in bytes. If zero on input, the function returns PdhMoreData and sets this parameter to the required buffer size.
// If the buffer is larger than the required size, the function sets this parameter to the actual size of the buffer that was used.
// If the specified size on input is greater than zero but less than the required size, you should not rely on the returned size to reallocate the buffer.
//
// lpBuffer [out]
// Caller-allocated buffer that receives a pdhCounterInfo structure.
// The structure is variable-length, because the string data is appended to the end of the fixed-format portion of the structure.
// This is done so that all data is returned in a single buffer allocated by the caller. Set to NULL if pdwBufferSize is zero.
func PdhGetCounterInfo(hCounter pdhCounterHandle, bRetrieveExplainText int, pdwBufferSize *uint32, lpBuffer *byte) uint32 {
	ret, _, _ := pdhGetCounterInfoW.Call(
		uintptr(hCounter),
		uintptr(bRetrieveExplainText),
		uintptr(unsafe.Pointer(pdwBufferSize)), //nolint:gosec // G103: Valid use of unsafe call to pass pdwBufferSize
		uintptr(unsafe.Pointer(lpBuffer)))      //nolint:gosec // G103: Valid use of unsafe call to pass lpBuffer

	return uint32(ret)
}

// PdhGetRawCounterValue returns the current raw value of the counter.
// If the specified counter instance does not exist, this function will return ErrorSuccess
// and the CStatus member of the pdhRawCounter structure will contain PdhCstatusNoInstance.
//
// hCounter [in]
// Handle of the counter from which to retrieve the current raw value. The PdhAddCounter function returns this handle.
//
// lpdwType [out]
// Receives the counter type. For a list of counter types, see the Counter Types section of the Windows Server 2003 Deployment Kit.
// This parameter is optional.
//
// pValue [out]
// A pdhRawCounter structure that receives the counter value.
func PdhGetRawCounterValue(hCounter pdhCounterHandle, lpdwType *uint32, pValue *pdhRawCounter) uint32 {
	ret, _, _ := pdhGetRawCounterValue.Call(
		uintptr(hCounter),
		uintptr(unsafe.Pointer(lpdwType)), //nolint:gosec // G103: Valid use of unsafe call to pass lpdwType
		uintptr(unsafe.Pointer(pValue)))   //nolint:gosec // G103: Valid use of unsafe call to pass pValue

	return uint32(ret)
}

// PdhGetRawCounterArray returns an array of raw values from the specified counter. Use this function when you want to retrieve the raw counter values
// of a counter that contains a wildcard character for the instance name.
// hCounter
// Handle of the counter for whose current raw instance values you want to retrieve. The PdhAddCounter function returns this handle.
//
// lpdwBufferSize
// Size of the ItemBuffer buffer, in bytes. If zero on input, the function returns PdhMoreData and sets this parameter to the required buffer size.
// If the buffer is larger than the required size, the function sets this parameter to the actual size of the buffer that was used.
// If the specified size on input is greater than zero but less than the required size, you should not rely on the returned size to reallocate the buffer.
//
// lpdwItemCount
// Number of raw counter values in the ItemBuffer buffer.
//
// ItemBuffer
// Caller-allocated buffer that receives the array of pdhRawCounterItem structures; the structures contain the raw instance counter values.
// Set to NULL if lpdwBufferSize is zero.
func PdhGetRawCounterArray(hCounter pdhCounterHandle, lpdwBufferSize, lpdwBufferCount *uint32, itemBuffer *byte) uint32 {
	ret, _, _ := pdhGetRawCounterArrayW.Call(
		uintptr(hCounter),
		uintptr(unsafe.Pointer(lpdwBufferSize)),  //nolint:gosec // G103: Valid use of unsafe call to pass lpdwBufferSize
		uintptr(unsafe.Pointer(lpdwBufferCount)), //nolint:gosec // G103: Valid use of unsafe call to pass lpdwBufferCount
		uintptr(unsafe.Pointer(itemBuffer)))      //nolint:gosec // G103: Valid use of unsafe call to pass itemBuffer
	return uint32(ret)
}
