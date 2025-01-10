//go:build windows

// Package win_eventlog Input plugin to collect Windows Event Log messages
//
//revive:disable-next-line:var-naming
package win_eventlog

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var _ unsafe.Pointer

// EvtHandle uintptr
type EvtHandle uintptr

// Do the interface allocations only once for common
// Errno values.
const (
	//revive:disable-next-line:var-naming
	errnoERROR_IO_PENDING = 997
)

var (
	//revive:disable-next-line:var-naming
	errERROR_IO_PENDING error = syscall.Errno(errnoERROR_IO_PENDING)
)

// EvtFormatMessageFlag defines the values that specify the message string from
// the event to format.
type EvtFormatMessageFlag uint32

// EVT_FORMAT_MESSAGE_FLAGS enumeration
// https://msdn.microsoft.com/en-us/library/windows/desktop/aa385525(v=vs.85).aspx
const (
	// EvtFormatMessageEvent - Format the event's message string.
	EvtFormatMessageEvent EvtFormatMessageFlag = iota + 1
	// EvtFormatMessageLevel - Format the message string of the level specified in the event.
	EvtFormatMessageLevel
	// EvtFormatMessageTask - Format the message string of the task specified in the event.
	EvtFormatMessageTask
	// EvtFormatMessageOpcode - Format the message string of the task specified in the event.
	EvtFormatMessageOpcode
	// EvtFormatMessageKeyword - Format the message string of the keywords specified in the event. If the
	// event specifies multiple keywords, the formatted string is a list of null-terminated strings.
	// Increment through the strings until your pointer points past the end of the used buffer.
	EvtFormatMessageKeyword
)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e syscall.Errno) error {
	switch e {
	case 0:
		return nil
	case errnoERROR_IO_PENDING:
		return errERROR_IO_PENDING
	}

	return e
}

var (
	modwevtapi = windows.NewLazySystemDLL("wevtapi.dll")

	procEvtSubscribe             = modwevtapi.NewProc("EvtSubscribe")
	procEvtRender                = modwevtapi.NewProc("EvtRender")
	procEvtClose                 = modwevtapi.NewProc("EvtClose")
	procEvtNext                  = modwevtapi.NewProc("EvtNext")
	procEvtFormatMessage         = modwevtapi.NewProc("EvtFormatMessage")
	procEvtOpenPublisherMetadata = modwevtapi.NewProc("EvtOpenPublisherMetadata")
	procEvtCreateBookmark        = modwevtapi.NewProc("EvtCreateBookmark")
	procEvtUpdateBookmark        = modwevtapi.NewProc("EvtUpdateBookmark")
)

//nolint:revive //argument-limit conditionally more arguments allowed
func _EvtSubscribe(
	session EvtHandle,
	signalEvent uintptr,
	channelPath *uint16,
	query *uint16,
	bookmark EvtHandle,
	context uintptr,
	callback syscall.Handle,
	flags EvtSubscribeFlag,
) (EvtHandle, error) {
	r0, _, e1 := syscall.SyscallN(
		procEvtSubscribe.Addr(),
		uintptr(session),
		signalEvent,
		uintptr(unsafe.Pointer(channelPath)), //nolint:gosec // G103: Valid use of unsafe call to pass channelPath
		uintptr(unsafe.Pointer(query)),       //nolint:gosec // G103: Valid use of unsafe call to pass query
		uintptr(bookmark),
		context,
		uintptr(callback),
		uintptr(flags),
	)

	var err error
	handle := EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return handle, err
}

//nolint:revive //argument-limit conditionally more arguments allowed
func _EvtRender(
	context EvtHandle,
	fragment EvtHandle,
	flags EvtRenderFlag,
	bufferSize uint32,
	buffer *byte,
	bufferUsed *uint32,
	propertyCount *uint32,
) error {
	r1, _, e1 := syscall.SyscallN(
		procEvtRender.Addr(),
		uintptr(context),
		uintptr(fragment),
		uintptr(flags),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(buffer)),        //nolint:gosec // G103: Valid use of unsafe call to pass buffer
		uintptr(unsafe.Pointer(bufferUsed)),    //nolint:gosec // G103: Valid use of unsafe call to pass bufferUsed
		uintptr(unsafe.Pointer(propertyCount)), //nolint:gosec // G103: Valid use of unsafe call to pass propertyCount
	)

	var err error
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return err
}

func _EvtClose(object EvtHandle) error {
	r1, _, e1 := syscall.SyscallN(procEvtClose.Addr(), uintptr(object))
	var err error
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return err
}

func _EvtNext(resultSet EvtHandle, eventArraySize uint32, eventArray *EvtHandle, timeout, flags uint32, numReturned *uint32) error {
	r1, _, e1 := syscall.SyscallN(
		procEvtNext.Addr(),
		uintptr(resultSet),
		uintptr(eventArraySize),
		uintptr(unsafe.Pointer(eventArray)), //nolint:gosec // G103: Valid use of unsafe call to pass eventArray
		uintptr(timeout),
		uintptr(flags),
		uintptr(unsafe.Pointer(numReturned)), //nolint:gosec // G103: Valid use of unsafe call to pass numReturned
	)

	var err error
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return err
}

//nolint:revive //argument-limit conditionally more arguments allowed
func _EvtFormatMessage(
	publisherMetadata EvtHandle,
	event EvtHandle,
	messageID uint32,
	valueCount uint32,
	values uintptr,
	flags EvtFormatMessageFlag,
	bufferSize uint32,
	buffer *byte,
	bufferUsed *uint32,
) error {
	r1, _, e1 := syscall.SyscallN(
		procEvtFormatMessage.Addr(),
		uintptr(publisherMetadata),
		uintptr(event),
		uintptr(messageID),
		uintptr(valueCount),
		values,
		uintptr(flags),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(buffer)),     //nolint:gosec // G103: Valid use of unsafe call to pass buffer
		uintptr(unsafe.Pointer(bufferUsed)), //nolint:gosec // G103: Valid use of unsafe call to pass bufferUsed
	)

	var err error
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return err
}

func _EvtOpenPublisherMetadata(session EvtHandle, publisherIdentity, logFilePath *uint16, locale, flags uint32) (EvtHandle, error) {
	r0, _, e1 := syscall.SyscallN(
		procEvtOpenPublisherMetadata.Addr(),
		uintptr(session),
		uintptr(unsafe.Pointer(publisherIdentity)), //nolint:gosec // G103: Valid use of unsafe call to pass publisherIdentity
		uintptr(unsafe.Pointer(logFilePath)),       //nolint:gosec // G103: Valid use of unsafe call to pass logFilePath
		uintptr(locale),
		uintptr(flags),
	)

	var err error
	handle := EvtHandle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return handle, err
}

func _EvtCreateBookmark(bookmarkXML *uint16) (EvtHandle, error) {
	//nolint:gosec // G103: Valid use of unsafe call to pass bookmarkXML
	r0, _, e1 := syscall.SyscallN(procEvtCreateBookmark.Addr(), uintptr(unsafe.Pointer(bookmarkXML)))
	handle := EvtHandle(r0)
	if handle != 0 {
		return handle, nil
	}
	if e1 != 0 {
		return handle, errnoErr(e1)
	}
	return handle, syscall.EINVAL
}

func _EvtUpdateBookmark(bookmark, event EvtHandle) error {
	r0, _, e1 := syscall.SyscallN(procEvtUpdateBookmark.Addr(), uintptr(bookmark), uintptr(event))
	if r0 != 0 {
		return nil
	}
	if e1 != 0 {
		return errnoErr(e1)
	}
	return syscall.EINVAL
}
