//go:generate ../../../tools/readme_config_includer/generator
//go:build windows

// Package win_eventlog Input plugin to collect Windows Event Log messages
//
//revive:disable-next-line:var-naming
package win_eventlog

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/xml"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/filter"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

// WinEventLog config
type WinEventLog struct {
	Locale                 uint32          `toml:"locale"`
	EventlogName           string          `toml:"eventlog_name"`
	Query                  string          `toml:"xpath_query"`
	FromBeginning          bool            `toml:"from_beginning"`
	BatchSize              uint32          `toml:"event_batch_size"`
	ProcessUserData        bool            `toml:"process_userdata"`
	ProcessEventData       bool            `toml:"process_eventdata"`
	Separator              string          `toml:"separator"`
	OnlyFirstLineOfMessage bool            `toml:"only_first_line_of_message"`
	TimeStampFromEvent     bool            `toml:"timestamp_from_event"`
	EventTags              []string        `toml:"event_tags"`
	EventFields            []string        `toml:"event_fields"`
	ExcludeFields          []string        `toml:"exclude_fields"`
	ExcludeEmpty           []string        `toml:"exclude_empty"`
	Log                    telegraf.Logger `toml:"-"`

	subscription     EvtHandle
	subscriptionFlag EvtSubscribeFlag
	bookmark         EvtHandle
	tagFilter        filter.Filter
	fieldFilter      filter.Filter
	fieldEmptyFilter filter.Filter
}

const bufferSize = 1 << 14

func (*WinEventLog) SampleConfig() string {
	return sampleConfig
}

func (w *WinEventLog) Init() error {
	// Set default for batch-size
	if w.BatchSize < 1 {
		w.BatchSize = 5
	}

	w.subscriptionFlag = EvtSubscribeToFutureEvents
	if w.FromBeginning {
		w.subscriptionFlag = EvtSubscribeStartAtOldestRecord
	}

	if w.Query == "" {
		w.Query = "*"
	}

	bookmark, err := _EvtCreateBookmark(nil)
	if err != nil {
		return err
	}
	w.bookmark = bookmark

	if w.tagFilter, err = filter.Compile(w.EventTags); err != nil {
		return fmt.Errorf("creating tag filter failed: %w", err)
	}

	if w.fieldFilter, err = filter.NewIncludeExcludeFilter(w.EventFields, w.ExcludeFields); err != nil {
		return fmt.Errorf("creating field filter failed: %w", err)
	}

	if w.fieldEmptyFilter, err = filter.Compile(w.ExcludeEmpty); err != nil {
		return fmt.Errorf("creating empty fields filter failed: %w", err)
	}

	return nil
}

func (w *WinEventLog) Start(_ telegraf.Accumulator) error {
	subscription, err := w.evtSubscribe()
	if err != nil {
		return fmt.Errorf("subscription of Windows Event Log failed: %w", err)
	}
	w.subscription = subscription
	w.Log.Debug("Subscription handle id:", w.subscription)

	return nil
}

func (w *WinEventLog) Stop() {
	//nolint:errcheck // ending the subscription, error can be ignored
	_ = _EvtClose(w.subscription)
}

func (w *WinEventLog) GetState() interface{} {
	bookmarkXML, err := w.renderBookmark(w.bookmark)
	if err != nil {
		w.Log.Errorf("State-persistence failed, cannot render bookmark: %v", err)
		return ""
	}
	return bookmarkXML
}

func (w *WinEventLog) SetState(state interface{}) error {
	bookmarkXML, ok := state.(string)
	if !ok {
		return fmt.Errorf("invalid type %T for state", state)
	}

	ptr, err := syscall.UTF16PtrFromString(bookmarkXML)
	if err != nil {
		return fmt.Errorf("conversion to pointer failed: %w", err)
	}

	bookmark, err := _EvtCreateBookmark(ptr)
	if err != nil {
		return fmt.Errorf("creating bookmark failed: %w", err)
	}
	w.bookmark = bookmark
	w.subscriptionFlag = EvtSubscribeStartAfterBookmark

	return nil
}

// Gather Windows Event Log entries
func (w *WinEventLog) Gather(acc telegraf.Accumulator) error {
	for {
		events, err := w.fetchEvents(w.subscription)
		if err != nil {
			if errors.Is(err, ERROR_NO_MORE_ITEMS) {
				break
			}
			w.Log.Errorf("Error getting events: %v", err)
			return err
		}

		for i := range events {
			// Prepare fields names usage counter
			fieldsUsage := make(map[string]int)

			tags := make(map[string]string)
			fields := make(map[string]interface{})
			event := events[i]
			evt := reflect.ValueOf(&event).Elem()
			timeStamp := time.Now()
			// Walk through all fields of Event struct to process System tags or fields
			for i := 0; i < evt.NumField(); i++ {
				fieldName := evt.Type().Field(i).Name
				fieldType := evt.Field(i).Type().String()
				fieldValue := evt.Field(i).Interface()
				computedValues := make(map[string]interface{})
				switch fieldName {
				case "Source":
					fieldValue = event.Source.Name
					fieldType = reflect.TypeOf(fieldValue).String()
				case "Execution":
					fieldValue := event.Execution.ProcessID
					fieldType = reflect.TypeOf(fieldValue).String()
					fieldName = "ProcessID"
					// Look up Process Name from pid
					if should, _ := w.shouldProcessField("ProcessName"); should {
						processName, err := GetFromSnapProcess(fieldValue)
						if err == nil {
							computedValues["ProcessName"] = processName
						}
					}
				case "TimeCreated":
					fieldValue = event.TimeCreated.SystemTime
					fieldType = reflect.TypeOf(fieldValue).String()
					if w.TimeStampFromEvent {
						timeStamp, err = time.Parse(time.RFC3339Nano, fmt.Sprintf("%v", fieldValue))
						if err != nil {
							w.Log.Warnf("Error parsing timestamp %q: %v", fieldValue, err)
						}
					}
				case "Correlation":
					if should, _ := w.shouldProcessField("ActivityID"); should {
						activityID := event.Correlation.ActivityID
						if len(activityID) > 0 {
							computedValues["ActivityID"] = activityID
						}
					}
					if should, _ := w.shouldProcessField("RelatedActivityID"); should {
						relatedActivityID := event.Correlation.RelatedActivityID
						if len(relatedActivityID) > 0 {
							computedValues["RelatedActivityID"] = relatedActivityID
						}
					}
				case "Security":
					computedValues["UserID"] = event.Security.UserID
					// Look up UserName and Domain from SID
					if should, _ := w.shouldProcessField("UserName"); should {
						sid := event.Security.UserID
						usid, err := syscall.StringToSid(sid)
						if err == nil {
							username, domain, _, err := usid.LookupAccount("")
							if err == nil {
								computedValues["UserName"] = fmt.Sprint(domain, "\\", username)
							}
						}
					}
				}
				if should, where := w.shouldProcessField(fieldName); should {
					if where == "tags" {
						strValue := fmt.Sprintf("%v", fieldValue)
						if !w.shouldExcludeEmptyField(fieldName, "string", strValue) {
							tags[fieldName] = strValue
							fieldsUsage[fieldName]++
						}
					} else if where == "fields" {
						if !w.shouldExcludeEmptyField(fieldName, fieldType, fieldValue) {
							fields[fieldName] = fieldValue
							fieldsUsage[fieldName]++
						}
					}
				}

				// Insert computed fields
				for computedKey, computedValue := range computedValues {
					if should, where := w.shouldProcessField(computedKey); should {
						if where == "tags" {
							tags[computedKey] = fmt.Sprintf("%v", computedValue)
							fieldsUsage[computedKey]++
						} else if where == "fields" {
							fields[computedKey] = computedValue
							fieldsUsage[computedKey]++
						}
					}
				}
			}

			// Unroll additional XML
			var xmlFields []EventField
			if w.ProcessUserData {
				fieldsUserData, xmlFieldsUsage := UnrollXMLFields(event.UserData.InnerXML, fieldsUsage, w.Separator)
				xmlFields = append(xmlFields, fieldsUserData...)
				fieldsUsage = xmlFieldsUsage
			}
			if w.ProcessEventData {
				fieldsEventData, xmlFieldsUsage := UnrollXMLFields(event.EventData.InnerXML, fieldsUsage, w.Separator)
				xmlFields = append(xmlFields, fieldsEventData...)
				fieldsUsage = xmlFieldsUsage
			}
			uniqueXMLFields := UniqueFieldNames(xmlFields, fieldsUsage, w.Separator)
			for _, xmlField := range uniqueXMLFields {
				should, where := w.shouldProcessField(xmlField.Name)
				if !should {
					continue
				}
				if where == "tags" {
					tags[xmlField.Name] = xmlField.Value
				} else {
					fields[xmlField.Name] = xmlField.Value
				}
			}

			// Pass collected metrics
			acc.AddFields("win_eventlog", fields, tags, timeStamp)
		}
	}

	return nil
}

func (w *WinEventLog) shouldProcessField(field string) (should bool, list string) {
	if w.tagFilter != nil && w.tagFilter.Match(field) {
		return true, "tags"
	}

	if w.fieldFilter.Match(field) {
		return true, "fields"
	}

	return false, "excluded"
}

func (w *WinEventLog) shouldExcludeEmptyField(field, fieldType string, fieldValue interface{}) (should bool) {
	if w.fieldEmptyFilter == nil || !w.fieldEmptyFilter.Match(field) {
		return false
	}

	switch fieldType {
	case "string":
		return len(fieldValue.(string)) < 1
	case "int":
		return fieldValue.(int) == 0
	case "uint32":
		return fieldValue.(uint32) == 0
	}

	return false
}

func (w *WinEventLog) evtSubscribe() (EvtHandle, error) {
	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(sigEvent)

	logNamePtr, err := syscall.UTF16PtrFromString(w.EventlogName)
	if err != nil {
		return 0, err
	}

	xqueryPtr, err := syscall.UTF16PtrFromString(w.Query)
	if err != nil {
		return 0, err
	}

	var bookmark EvtHandle
	if w.subscriptionFlag == EvtSubscribeStartAfterBookmark {
		bookmark = w.bookmark
	}
	subsHandle, err := _EvtSubscribe(0, uintptr(sigEvent), logNamePtr, xqueryPtr, bookmark, 0, 0, w.subscriptionFlag)
	if err != nil {
		return 0, err
	}

	return subsHandle, nil
}

func (w *WinEventLog) fetchEventHandles(subsHandle EvtHandle) ([]EvtHandle, error) {
	var evtReturned uint32

	eventHandles := make([]EvtHandle, w.BatchSize)
	if err := _EvtNext(subsHandle, w.BatchSize, &eventHandles[0], 0, 0, &evtReturned); err != nil {
		if errors.Is(err, ERROR_INVALID_OPERATION) && evtReturned == 0 {
			return nil, ERROR_NO_MORE_ITEMS
		}
		return nil, err
	}

	return eventHandles[:evtReturned], nil
}

func (w *WinEventLog) fetchEvents(subsHandle EvtHandle) ([]Event, error) {
	var events []Event

	eventHandles, err := w.fetchEventHandles(subsHandle)
	if err != nil {
		return nil, err
	}

	var evterr error
	for _, eventHandle := range eventHandles {
		if eventHandle == 0 {
			continue
		}
		if event, err := w.renderEvent(eventHandle); err == nil {
			events = append(events, event)
		}
		if err := _EvtUpdateBookmark(w.bookmark, eventHandle); err != nil && evterr == nil {
			evterr = err
		}

		if err := _EvtClose(eventHandle); err != nil && evterr == nil {
			evterr = err
		}
	}
	return events, evterr
}

func (w *WinEventLog) renderBookmark(bookmark EvtHandle) (string, error) {
	var bufferUsed, propertyCount uint32

	buf := make([]byte, bufferSize)
	err := _EvtRender(0, bookmark, EvtRenderBookmark, uint32(len(buf)), &buf[0], &bufferUsed, &propertyCount)
	if err != nil {
		return "", err
	}

	x, err := DecodeUTF16(buf[:bufferUsed])
	if err != nil {
		return "", err
	}
	if x[len(x)-1] == 0 {
		x = x[:len(x)-1]
	}
	return string(x), err
}

func (w *WinEventLog) renderEvent(eventHandle EvtHandle) (Event, error) {
	var bufferUsed, propertyCount uint32

	buf := make([]byte, bufferSize)
	event := Event{}
	err := _EvtRender(0, eventHandle, EvtRenderEventXML, uint32(len(buf)), &buf[0], &bufferUsed, &propertyCount)
	if err != nil {
		return event, err
	}

	eventXML, err := DecodeUTF16(buf[:bufferUsed])
	if err != nil {
		return event, err
	}

	err = xml.Unmarshal(eventXML, &event)
	if err != nil {
		//nolint:nilerr // We can return event without most text values, that way we will not lose information
		// This can happen when processing Forwarded Events
		return event, nil
	}

	// Do resolve local messages the usual way, while using built-in information for events forwarded by WEC.
	// This is a safety measure as the underlying Windows-internal EvtFormatMessage might segfault in cases
	// where the publisher (i.e. the remote machine which forwarded the event) is unavailable e.g. due to
	// a reboot. See https://github.com/influxdata/telegraf/issues/12328 for the full story.
	if event.RenderingInfo == nil {
		return w.renderLocalMessage(event, eventHandle)
	}

	// We got 'RenderInfo' elements, so try to apply them in the following function
	return w.renderRemoteMessage(event)
}

func (w *WinEventLog) renderLocalMessage(event Event, eventHandle EvtHandle) (Event, error) {
	publisherHandle, err := openPublisherMetadata(0, event.Source.Name, w.Locale)
	if err != nil {
		return event, nil
	}
	defer _EvtClose(publisherHandle) //nolint:errcheck // Ignore error returned during Close

	// Populating text values
	keywords, err := formatEventString(EvtFormatMessageKeyword, eventHandle, publisherHandle)
	if err == nil {
		event.Keywords = keywords
	}
	message, err := formatEventString(EvtFormatMessageEvent, eventHandle, publisherHandle)
	if err == nil {
		if w.OnlyFirstLineOfMessage {
			scanner := bufio.NewScanner(strings.NewReader(message))
			scanner.Scan()
			message = scanner.Text()
		}
		event.Message = message
	}
	level, err := formatEventString(EvtFormatMessageLevel, eventHandle, publisherHandle)
	if err == nil {
		event.LevelText = level
	}
	task, err := formatEventString(EvtFormatMessageTask, eventHandle, publisherHandle)
	if err == nil {
		event.TaskText = task
	}
	opcode, err := formatEventString(EvtFormatMessageOpcode, eventHandle, publisherHandle)
	if err == nil {
		event.OpcodeText = opcode
	}
	return event, nil
}

func (w *WinEventLog) renderRemoteMessage(event Event) (Event, error) {
	// Populating text values from RenderingInfo part of the XML
	if len(event.RenderingInfo.Keywords) > 0 {
		event.Keywords = strings.Join(event.RenderingInfo.Keywords, ",")
	}
	if event.RenderingInfo.Message != "" {
		message := event.RenderingInfo.Message
		if w.OnlyFirstLineOfMessage {
			scanner := bufio.NewScanner(strings.NewReader(message))
			scanner.Scan()
			message = scanner.Text()
		}
		event.Message = message
	}
	if event.RenderingInfo.Level != "" {
		event.LevelText = event.RenderingInfo.Level
	}
	if event.RenderingInfo.Task != "" {
		event.TaskText = event.RenderingInfo.Task
	}
	if event.RenderingInfo.Opcode != "" {
		event.OpcodeText = event.RenderingInfo.Opcode
	}
	return event, nil
}

func formatEventString(messageFlag EvtFormatMessageFlag, eventHandle, publisherHandle EvtHandle) (string, error) {
	var bufferUsed uint32
	err := _EvtFormatMessage(publisherHandle, eventHandle, 0, 0, 0, messageFlag,
		0, nil, &bufferUsed)
	if err != nil && !errors.Is(err, ERROR_INSUFFICIENT_BUFFER) {
		return "", err
	}

	// Handle empty elements
	if bufferUsed < 1 {
		return "", nil
	}

	bufferUsed *= 2
	buffer := make([]byte, bufferUsed)
	bufferUsed = 0

	err = _EvtFormatMessage(publisherHandle, eventHandle, 0, 0, 0, messageFlag,
		uint32(len(buffer)/2), &buffer[0], &bufferUsed)
	bufferUsed *= 2
	if err != nil {
		return "", err
	}

	result, err := DecodeUTF16(buffer[:bufferUsed])
	if err != nil {
		return "", err
	}

	var out string
	if messageFlag == EvtFormatMessageKeyword {
		// Keywords are returned as array of a zero-terminated strings
		splitZero := func(c rune) bool { return c == '\x00' }
		eventKeywords := strings.FieldsFunc(string(result), splitZero)
		// So convert them to comma-separated string
		out = strings.Join(eventKeywords, ",")
	} else {
		result := bytes.Trim(result, "\x00")
		out = string(result)
	}
	return out, nil
}

// openPublisherMetadata opens a handle to the publisher's metadata. Close must
// be called on returned EvtHandle when finished with the handle.
func openPublisherMetadata(
	session EvtHandle,
	publisherName string,
	lang uint32,
) (EvtHandle, error) {
	p, err := syscall.UTF16PtrFromString(publisherName)
	if err != nil {
		return 0, err
	}

	h, err := _EvtOpenPublisherMetadata(session, p, nil, lang, 0)
	if err != nil {
		return 0, err
	}

	return h, nil
}

func init() {
	inputs.Add("win_eventlog", func() telegraf.Input {
		return &WinEventLog{
			ProcessUserData:        true,
			ProcessEventData:       true,
			Separator:              "_",
			OnlyFirstLineOfMessage: true,
			TimeStampFromEvent:     true,
			EventTags:              []string{"Source", "EventID", "Level", "LevelText", "Keywords", "Channel", "Computer"},
			ExcludeEmpty:           []string{"Task", "Opcode", "*ActivityID", "UserID"},
		}
	})
}
