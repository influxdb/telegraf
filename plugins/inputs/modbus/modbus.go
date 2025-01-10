//go:generate ../../../tools/readme_config_includer/generator
package modbus

import (
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	mb "github.com/grid-x/modbus"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample_general_begin.conf
var sampleConfigStart string

//go:embed sample_general_end.conf
var sampleConfigEnd string

var errAddressOverflow = errors.New("address overflow")

const (
	cDiscreteInputs   = "discrete_input"
	cCoils            = "coil"
	cHoldingRegisters = "holding_register"
	cInputRegisters   = "input_register"
)

type Modbus struct {
	Name                   string          `toml:"name"`
	Controller             string          `toml:"controller"`
	TransmissionMode       string          `toml:"transmission_mode"`
	BaudRate               int             `toml:"baud_rate"`
	DataBits               int             `toml:"data_bits"`
	Parity                 string          `toml:"parity"`
	StopBits               int             `toml:"stop_bits"`
	RS485                  *rs485Config    `toml:"rs485"`
	Timeout                config.Duration `toml:"timeout"`
	Retries                int             `toml:"busy_retries"`
	RetriesWaitTime        config.Duration `toml:"busy_retries_wait"`
	DebugConnection        bool            `toml:"debug_connection" deprecated:"1.35.0;use 'log_level' 'trace' instead"`
	Workarounds            workarounds     `toml:"workarounds"`
	ConfigurationType      string          `toml:"configuration_type"`
	ExcludeRegisterTypeTag bool            `toml:"exclude_register_type_tag"`
	Log                    telegraf.Logger `toml:"-"`

	// configuration type specific settings
	configurationOriginal
	configurationPerRequest
	configurationPerMetric

	// Connection handling
	client      mb.Client
	handler     mb.ClientHandler
	isConnected bool
	// Request handling
	requests map[byte]requestSet
}

type workarounds struct {
	AfterConnectPause       config.Duration `toml:"pause_after_connect"`
	PollPause               config.Duration `toml:"pause_between_requests"`
	CloseAfterGather        bool            `toml:"close_connection_after_gather"`
	OnRequestPerField       bool            `toml:"one_request_per_field"`
	ReadCoilsStartingAtZero bool            `toml:"read_coils_starting_at_zero"`
	StringRegisterLocation  string          `toml:"string_register_location"`
}

// According to github.com/grid-x/serial
type rs485Config struct {
	DelayRtsBeforeSend config.Duration `toml:"delay_rts_before_send"`
	DelayRtsAfterSend  config.Duration `toml:"delay_rts_after_send"`
	RtsHighDuringSend  bool            `toml:"rts_high_during_send"`
	RtsHighAfterSend   bool            `toml:"rts_high_after_send"`
	RxDuringTx         bool            `toml:"rx_during_tx"`
}

type fieldConverterFunc func(bytes []byte) interface{}

type requestSet struct {
	coil     []request
	discrete []request
	holding  []request
	input    []request
}

func (r requestSet) empty() bool {
	l := len(r.coil)
	l += len(r.discrete)
	l += len(r.holding)
	l += len(r.input)
	return l == 0
}

type field struct {
	measurement string
	name        string
	address     uint16
	length      uint16
	omit        bool
	converter   fieldConverterFunc
	value       interface{}
	tags        map[string]string
}

func (m *Modbus) SampleConfig() string {
	configs := []configuration{
		&m.configurationOriginal,
		&m.configurationPerRequest,
		&m.configurationPerMetric,
	}

	totalConfig := sampleConfigStart
	for _, c := range configs {
		totalConfig += c.sampleConfigPart() + "\n"
	}
	totalConfig += "\n"
	totalConfig += sampleConfigEnd
	return totalConfig
}

func (m *Modbus) Init() error {
	// check device name
	if m.Name == "" {
		return errors.New("device name is empty")
	}

	if m.Retries < 0 {
		return fmt.Errorf("retries cannot be negative in device %q", m.Name)
	}

	// Determine the configuration style
	var cfg configuration
	switch m.ConfigurationType {
	case "", "register":
		m.configurationOriginal.workarounds = m.Workarounds
		m.configurationOriginal.logger = m.Log
		cfg = &m.configurationOriginal
	case "request":
		m.configurationPerRequest.workarounds = m.Workarounds
		m.configurationPerRequest.excludeRegisterType = m.ExcludeRegisterTypeTag
		m.configurationPerRequest.logger = m.Log
		cfg = &m.configurationPerRequest
	case "metric":
		m.configurationPerMetric.workarounds = m.Workarounds
		m.configurationPerMetric.excludeRegisterType = m.ExcludeRegisterTypeTag
		m.configurationPerMetric.logger = m.Log
		cfg = &m.configurationPerMetric
	default:
		return fmt.Errorf("unknown configuration type %q in device %q", m.ConfigurationType, m.Name)
	}

	// Check and process the configuration
	if err := cfg.check(); err != nil {
		return fmt.Errorf("configuration invalid for device %q: %w", m.Name, err)
	}

	r, err := cfg.process()
	if err != nil {
		return fmt.Errorf("cannot process configuration for device %q: %w", m.Name, err)
	}
	m.requests = r

	// Setup client
	if err := m.initClient(); err != nil {
		return fmt.Errorf("initializing client failed for controller %q: %w", m.Controller, err)
	}
	for slaveID, rqs := range m.requests {
		var nHoldingRegs, nInputsRegs, nDiscreteRegs, nCoilRegs uint16
		var nHoldingFields, nInputsFields, nDiscreteFields, nCoilFields int

		for _, r := range rqs.holding {
			nHoldingRegs += r.length
			nHoldingFields += len(r.fields)
		}
		for _, r := range rqs.input {
			nInputsRegs += r.length
			nInputsFields += len(r.fields)
		}
		for _, r := range rqs.discrete {
			nDiscreteRegs += r.length
			nDiscreteFields += len(r.fields)
		}
		for _, r := range rqs.coil {
			nCoilRegs += r.length
			nCoilFields += len(r.fields)
		}
		m.Log.Infof("Got %d request(s) touching %d holding registers for %d fields (slave %d) on device %q",
			len(rqs.holding), nHoldingRegs, nHoldingFields, slaveID, m.Name)
		for i, r := range rqs.holding {
			m.Log.Debugf("    #%d: @%d with length %d", i+1, r.address, r.length)
		}
		m.Log.Infof("Got %d request(s) touching %d inputs registers for %d fields (slave %d) on device %q",
			len(rqs.input), nInputsRegs, nInputsFields, slaveID, m.Name)
		for i, r := range rqs.input {
			m.Log.Debugf("    #%d: @%d with length %d", i+1, r.address, r.length)
		}
		m.Log.Infof("Got %d request(s) touching %d discrete registers for %d fields (slave %d) on device %q",
			len(rqs.discrete), nDiscreteRegs, nDiscreteFields, slaveID, m.Name)
		for i, r := range rqs.discrete {
			m.Log.Debugf("    #%d: @%d with length %d", i+1, r.address, r.length)
		}
		m.Log.Infof("Got %d request(s) touching %d coil registers for %d fields (slave %d) on device %q",
			len(rqs.coil), nCoilRegs, nCoilFields, slaveID, m.Name)
		for i, r := range rqs.coil {
			m.Log.Debugf("    #%d: @%d with length %d", i+1, r.address, r.length)
		}
	}
	return nil
}

func (m *Modbus) Gather(acc telegraf.Accumulator) error {
	if !m.isConnected {
		if err := m.connect(); err != nil {
			return err
		}
	}

	for slaveID, requests := range m.requests {
		m.Log.Debugf("Reading slave %d for %s...", slaveID, m.Controller)
		if err := m.readSlaveData(slaveID, requests); err != nil {
			acc.AddError(fmt.Errorf("slave %d on controller %q: %w", slaveID, m.Controller, err))
			var mbErr *mb.Error
			if !errors.As(err, &mbErr) || mbErr.ExceptionCode != mb.ExceptionCodeServerDeviceBusy {
				m.Log.Debugf("Reconnecting to %s...", m.Controller)
				if err := m.disconnect(); err != nil {
					return fmt.Errorf("disconnecting failed for controller %q: %w", m.Controller, err)
				}
				if err := m.connect(); err != nil {
					return fmt.Errorf("slave %d on controller %q: connecting failed: %w", slaveID, m.Controller, err)
				}
			}
			continue
		}
		timestamp := time.Now()

		grouper := metric.NewSeriesGrouper()
		tags := map[string]string{
			"name":     m.Name,
			"slave_id": strconv.Itoa(int(slaveID)),
		}

		if !m.ExcludeRegisterTypeTag {
			tags["type"] = cCoils
		}
		collectFields(grouper, timestamp, tags, requests.coil)

		if !m.ExcludeRegisterTypeTag {
			tags["type"] = cDiscreteInputs
		}
		collectFields(grouper, timestamp, tags, requests.discrete)

		if !m.ExcludeRegisterTypeTag {
			tags["type"] = cHoldingRegisters
		}
		collectFields(grouper, timestamp, tags, requests.holding)

		if !m.ExcludeRegisterTypeTag {
			tags["type"] = cInputRegisters
		}
		collectFields(grouper, timestamp, tags, requests.input)

		// Add the metrics grouped by series to the accumulator
		for _, x := range grouper.Metrics() {
			acc.AddMetric(x)
		}
	}

	// Disconnect after read if configured
	if m.Workarounds.CloseAfterGather {
		return m.disconnect()
	}

	return nil
}

func (m *Modbus) initClient() error {
	u, err := url.Parse(m.Controller)
	if err != nil {
		return err
	}

	var tracelog mb.Logger
	if m.Log.Level().Includes(telegraf.Trace) || m.DebugConnection { // for backward compatibility
		tracelog = m
	}

	switch u.Scheme {
	case "tcp":
		host, port, err := net.SplitHostPort(u.Host)
		if err != nil {
			return err
		}
		switch m.TransmissionMode {
		case "", "auto", "TCP":
			handler := mb.NewTCPClientHandler(host + ":" + port)
			handler.Timeout = time.Duration(m.Timeout)
			handler.Logger = tracelog
			m.handler = handler
		case "RTUoverTCP":
			handler := mb.NewRTUOverTCPClientHandler(host + ":" + port)
			handler.Timeout = time.Duration(m.Timeout)
			handler.Logger = tracelog
			m.handler = handler
		case "ASCIIoverTCP":
			handler := mb.NewASCIIOverTCPClientHandler(host + ":" + port)
			handler.Timeout = time.Duration(m.Timeout)
			handler.Logger = tracelog
			m.handler = handler
		default:
			return fmt.Errorf("invalid transmission mode %q for %q on device %q", m.TransmissionMode, u.Scheme, m.Name)
		}
	case "", "file":
		path := filepath.Join(u.Host, u.Path)
		if path == "" {
			return fmt.Errorf("invalid path for controller %q", m.Controller)
		}
		switch m.TransmissionMode {
		case "", "auto", "RTU":
			handler := mb.NewRTUClientHandler(path)
			handler.Timeout = time.Duration(m.Timeout)
			handler.BaudRate = m.BaudRate
			handler.DataBits = m.DataBits
			handler.Parity = m.Parity
			handler.StopBits = m.StopBits
			handler.Logger = tracelog
			if m.RS485 != nil {
				handler.RS485.Enabled = true
				handler.RS485.DelayRtsBeforeSend = time.Duration(m.RS485.DelayRtsBeforeSend)
				handler.RS485.DelayRtsAfterSend = time.Duration(m.RS485.DelayRtsAfterSend)
				handler.RS485.RtsHighDuringSend = m.RS485.RtsHighDuringSend
				handler.RS485.RtsHighAfterSend = m.RS485.RtsHighAfterSend
				handler.RS485.RxDuringTx = m.RS485.RxDuringTx
			}
			m.handler = handler
		case "ASCII":
			handler := mb.NewASCIIClientHandler(path)
			handler.Timeout = time.Duration(m.Timeout)
			handler.BaudRate = m.BaudRate
			handler.DataBits = m.DataBits
			handler.Parity = m.Parity
			handler.StopBits = m.StopBits
			handler.Logger = tracelog
			if m.RS485 != nil {
				handler.RS485.Enabled = true
				handler.RS485.DelayRtsBeforeSend = time.Duration(m.RS485.DelayRtsBeforeSend)
				handler.RS485.DelayRtsAfterSend = time.Duration(m.RS485.DelayRtsAfterSend)
				handler.RS485.RtsHighDuringSend = m.RS485.RtsHighDuringSend
				handler.RS485.RtsHighAfterSend = m.RS485.RtsHighAfterSend
				handler.RS485.RxDuringTx = m.RS485.RxDuringTx
			}
			m.handler = handler
		default:
			return fmt.Errorf("invalid transmission mode %q for %q on device %q", m.TransmissionMode, u.Scheme, m.Name)
		}
	default:
		return fmt.Errorf("invalid controller %q", m.Controller)
	}

	m.client = mb.NewClient(m.handler)
	m.isConnected = false

	return nil
}

// Connect to a MODBUS Slave device via Modbus/[TCP|RTU|ASCII]
func (m *Modbus) connect() error {
	err := m.handler.Connect()
	m.isConnected = err == nil
	if m.isConnected && m.Workarounds.AfterConnectPause != 0 {
		nextRequest := time.Now().Add(time.Duration(m.Workarounds.AfterConnectPause))
		time.Sleep(time.Until(nextRequest))
	}
	return err
}

func (m *Modbus) disconnect() error {
	err := m.handler.Close()
	m.isConnected = false
	return err
}

func (m *Modbus) readSlaveData(slaveID byte, requests requestSet) error {
	m.handler.SetSlave(slaveID)

	for retry := 0; retry < m.Retries; retry++ {
		err := m.gatherFields(requests)
		if err == nil {
			// Reading was successful
			return nil
		}

		// Exit in case a non-recoverable error occurred
		var mbErr *mb.Error
		if !errors.As(err, &mbErr) || mbErr.ExceptionCode != mb.ExceptionCodeServerDeviceBusy {
			return err
		}

		// Wait some time and try again reading the slave.
		m.Log.Infof("Device busy! Retrying %d more time(s) on controller %q...", m.Retries-retry, m.Controller)
		time.Sleep(time.Duration(m.RetriesWaitTime))
	}
	return m.gatherFields(requests)
}

func (m *Modbus) gatherFields(requests requestSet) error {
	if err := m.gatherRequestsCoil(requests.coil); err != nil {
		return err
	}
	if err := m.gatherRequestsDiscrete(requests.discrete); err != nil {
		return err
	}
	if err := m.gatherRequestsHolding(requests.holding); err != nil {
		return err
	}
	return m.gatherRequestsInput(requests.input)
}

func (m *Modbus) gatherRequestsCoil(requests []request) error {
	for _, request := range requests {
		m.Log.Debugf("trying to read coil@%v[%v]...", request.address, request.length)
		bytes, err := m.client.ReadCoils(request.address, request.length)
		if err != nil {
			return err
		}
		nextRequest := time.Now().Add(time.Duration(m.Workarounds.PollPause))
		m.Log.Debugf("got coil@%v[%v]: %v", request.address, request.length, bytes)

		// Bit value handling
		for i, field := range request.fields {
			offset := field.address - request.address
			idx := offset / 8
			bit := offset % 8

			v := (bytes[idx] >> bit) & 0x01
			request.fields[i].value = field.converter([]byte{v})
			m.Log.Debugf("  field %s with bit %d @ byte %d: %v --> %v", field.name, bit, idx, v, request.fields[i].value)
		}

		// Some (serial) devices require a pause between requests...
		time.Sleep(time.Until(nextRequest))
	}
	return nil
}

func (m *Modbus) gatherRequestsDiscrete(requests []request) error {
	for _, request := range requests {
		m.Log.Debugf("trying to read discrete@%v[%v]...", request.address, request.length)
		bytes, err := m.client.ReadDiscreteInputs(request.address, request.length)
		if err != nil {
			return err
		}
		nextRequest := time.Now().Add(time.Duration(m.Workarounds.PollPause))
		m.Log.Debugf("got discrete@%v[%v]: %v", request.address, request.length, bytes)

		// Bit value handling
		for i, field := range request.fields {
			offset := field.address - request.address
			idx := offset / 8
			bit := offset % 8

			v := (bytes[idx] >> bit) & 0x01
			request.fields[i].value = field.converter([]byte{v})
			m.Log.Debugf("  field %s with bit %d @ byte %d: %v --> %v", field.name, bit, idx, v, request.fields[i].value)
		}

		// Some (serial) devices require a pause between requests...
		time.Sleep(time.Until(nextRequest))
	}
	return nil
}

func (m *Modbus) gatherRequestsHolding(requests []request) error {
	for _, request := range requests {
		m.Log.Debugf("trying to read holding@%v[%v]...", request.address, request.length)
		bytes, err := m.client.ReadHoldingRegisters(request.address, request.length)
		if err != nil {
			return err
		}
		nextRequest := time.Now().Add(time.Duration(m.Workarounds.PollPause))
		m.Log.Debugf("got holding@%v[%v]: %v", request.address, request.length, bytes)

		// Non-bit value handling
		for i, field := range request.fields {
			// Determine the offset of the field values in the read array
			offset := 2 * uint32(field.address-request.address) // registers are 16bit = 2 byte
			length := 2 * uint32(field.length)                  // field length is in registers a 16bit

			// Convert the actual value
			request.fields[i].value = field.converter(bytes[offset : offset+length])
			m.Log.Debugf("  field %s with offset %d with len %d: %v --> %v", field.name, offset, length, bytes[offset:offset+length], request.fields[i].value)
		}

		// Some (serial) devices require a pause between requests...
		time.Sleep(time.Until(nextRequest))
	}
	return nil
}

func (m *Modbus) gatherRequestsInput(requests []request) error {
	for _, request := range requests {
		m.Log.Debugf("trying to read input@%v[%v]...", request.address, request.length)
		bytes, err := m.client.ReadInputRegisters(request.address, request.length)
		if err != nil {
			return err
		}
		nextRequest := time.Now().Add(time.Duration(m.Workarounds.PollPause))
		m.Log.Debugf("got input@%v[%v]: %v", request.address, request.length, bytes)

		// Non-bit value handling
		for i, field := range request.fields {
			// Determine the offset of the field values in the read array
			offset := 2 * uint32(field.address-request.address) // registers are 16bit = 2 byte
			length := 2 * uint32(field.length)                  // field length is in registers a 16bit

			// Convert the actual value
			request.fields[i].value = field.converter(bytes[offset : offset+length])
			m.Log.Debugf("  field %s with offset %d with len %d: %v --> %v", field.name, offset, length, bytes[offset:offset+length], request.fields[i].value)
		}

		// Some (serial) devices require a pause between requests...
		time.Sleep(time.Until(nextRequest))
	}
	return nil
}

func collectFields(grouper *metric.SeriesGrouper, timestamp time.Time, tags map[string]string, requests []request) {
	for _, request := range requests {
		for _, field := range request.fields {
			// Collect tags from global and per-request
			ftags := make(map[string]string, len(tags)+len(field.tags))
			for k, v := range tags {
				ftags[k] = v
			}
			for k, v := range field.tags {
				ftags[k] = v
			}
			// In case no measurement was specified we use "modbus" as default
			measurement := "modbus"
			if field.measurement != "" {
				measurement = field.measurement
			}

			// Group the data by series
			grouper.Add(measurement, ftags, timestamp, field.name, field.value)
		}
	}
}

// Printf implements the logger interface of the modbus client
func (m *Modbus) Printf(format string, v ...interface{}) {
	m.Log.Tracef(format, v...)
}

// Add this plugin to telegraf
func init() {
	inputs.Add("modbus", func() telegraf.Input { return &Modbus{} })
}
