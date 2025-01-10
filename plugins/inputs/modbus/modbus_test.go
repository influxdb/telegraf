package modbus

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	mb "github.com/grid-x/modbus"
	"github.com/stretchr/testify/require"
	"github.com/tbrandon/mbserver"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/metric"
	"github.com/influxdata/telegraf/plugins/inputs"
	"github.com/influxdata/telegraf/plugins/parsers/influx"
	"github.com/influxdata/telegraf/testutil"
)

func TestControllers(t *testing.T) {
	var tests = []struct {
		name       string
		controller string
		mode       string
		errmsg     string
	}{
		{
			name:       "TCP host",
			controller: "tcp://localhost:502",
		},
		{
			name:       "TCP mode auto",
			controller: "tcp://localhost:502",
			mode:       "auto",
		},
		{
			name:       "TCP mode TCP",
			controller: "tcp://localhost:502",
			mode:       "TCP",
		},
		{
			name:       "TCP mode RTUoverTCP",
			controller: "tcp://localhost:502",
			mode:       "RTUoverTCP",
		},
		{
			name:       "TCP mode ASCIIoverTCP",
			controller: "tcp://localhost:502",
			mode:       "ASCIIoverTCP",
		},
		{
			name:       "TCP invalid host",
			controller: "tcp://localhost",
			errmsg:     "address localhost: missing port in address",
		},
		{
			name:       "TCP invalid mode RTU",
			controller: "tcp://localhost:502",
			mode:       "RTU",
			errmsg:     "invalid transmission mode",
		},
		{
			name:       "TCP invalid mode ASCII",
			controller: "tcp://localhost:502",
			mode:       "ASCII",
			errmsg:     "invalid transmission mode",
		},
		{
			name:       "absolute file path",
			controller: "file:///dev/ttyUSB0",
		},
		{
			name:       "relative file path",
			controller: "file://dev/ttyUSB0",
		},
		{
			name:       "relative file path with dot",
			controller: "file://./dev/ttyUSB0",
		},
		{
			name:       "Windows COM-port",
			controller: "COM2",
		},
		{
			name:       "Windows COM-port file path",
			controller: "file://com2",
		},
		{
			name:       "serial mode auto",
			controller: "file:///dev/ttyUSB0",
			mode:       "auto",
		},
		{
			name:       "serial mode RTU",
			controller: "file:///dev/ttyUSB0",
			mode:       "RTU",
		},
		{
			name:       "serial mode ASCII",
			controller: "file:///dev/ttyUSB0",
			mode:       "ASCII",
		},
		{
			name:       "empty file path",
			controller: "file://",
			errmsg:     "invalid path for controller",
		},
		{
			name:       "empty controller",
			controller: "",
			errmsg:     "invalid path for controller",
		},
		{
			name:       "invalid scheme",
			controller: "foo://bar",
			errmsg:     "invalid controller",
		},
		{
			name:       "serial invalid mode TCP",
			controller: "file:///dev/ttyUSB0",
			mode:       "TCP",
			errmsg:     "invalid transmission mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := Modbus{
				Name:             "dummy",
				Controller:       tt.controller,
				TransmissionMode: tt.mode,
				Log:              testutil.Logger{Quiet: true},
			}
			err := plugin.Init()
			if tt.errmsg != "" {
				require.ErrorContains(t, err, tt.errmsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRetrySuccessful(t *testing.T) {
	retries := 0
	maxretries := 2
	value := 1

	serv := mbserver.NewServer()
	require.NoError(t, serv.ListenTCP("localhost:1502"))
	defer serv.Close()

	// Make read on coil-registers fail for some trials by making the device to appear busy
	serv.RegisterFunctionHandler(1,
		func(*mbserver.Server, mbserver.Framer) ([]byte, *mbserver.Exception) {
			data := make([]byte, 2)
			data[0] = byte(1)
			data[1] = byte(value)

			except := &mbserver.SlaveDeviceBusy
			if retries >= maxretries {
				except = &mbserver.Success
			}
			retries++

			return data, except
		})

	modbus := Modbus{
		Name:       "TestRetry",
		Controller: "tcp://localhost:1502",
		Retries:    maxretries,
		Log:        testutil.Logger{Quiet: true},
	}
	modbus.SlaveID = 1
	modbus.Coils = []fieldDefinition{
		{
			Name:    "retry_success",
			Address: []uint16{0},
		},
	}

	expected := []telegraf.Metric{
		testutil.MustMetric(
			"modbus",
			map[string]string{
				"type":     cCoils,
				"slave_id": strconv.Itoa(int(modbus.SlaveID)),
				"name":     modbus.Name,
			},
			map[string]interface{}{"retry_success": uint16(value)},
			time.Unix(0, 0),
		),
	}

	var acc testutil.Accumulator
	require.NoError(t, modbus.Init())
	require.NotEmpty(t, modbus.requests)
	require.NoError(t, modbus.Gather(&acc))
	acc.Wait(len(expected))

	testutil.RequireMetricsEqual(t, expected, acc.GetTelegrafMetrics(), testutil.IgnoreTime())
}

func TestRetryFailExhausted(t *testing.T) {
	maxretries := 2

	serv := mbserver.NewServer()
	require.NoError(t, serv.ListenTCP("localhost:1502"))
	defer serv.Close()

	// Make the read on coils fail with busy
	serv.RegisterFunctionHandler(1,
		func(*mbserver.Server, mbserver.Framer) ([]byte, *mbserver.Exception) {
			data := make([]byte, 2)
			data[0] = byte(1)
			data[1] = byte(0)

			return data, &mbserver.SlaveDeviceBusy
		})

	modbus := Modbus{
		Name:       "TestRetryFailExhausted",
		Controller: "tcp://localhost:1502",
		Retries:    maxretries,
		Log:        testutil.Logger{Quiet: true},
	}
	modbus.SlaveID = 1
	modbus.Coils = []fieldDefinition{
		{
			Name:    "retry_fail",
			Address: []uint16{0},
		},
	}

	var acc testutil.Accumulator
	require.NoError(t, modbus.Init())
	require.NotEmpty(t, modbus.requests)

	require.NoError(t, modbus.Gather(&acc))
	require.Len(t, acc.Errors, 1)
	require.ErrorContains(t, acc.FirstError(), `slave 1 on controller "tcp://localhost:1502": modbus: exception '6' (server device busy)`)
}

func TestRetryFailIllegal(t *testing.T) {
	maxretries := 2

	serv := mbserver.NewServer()
	require.NoError(t, serv.ListenTCP("localhost:1502"))
	defer serv.Close()

	// Make the read on coils fail with illegal function preventing retry
	counter := 0
	serv.RegisterFunctionHandler(1,
		func(*mbserver.Server, mbserver.Framer) ([]byte, *mbserver.Exception) {
			counter++
			data := make([]byte, 2)
			data[0] = byte(1)
			data[1] = byte(0)

			return data, &mbserver.IllegalFunction
		},
	)

	modbus := Modbus{
		Name:       "TestRetryFailExhausted",
		Controller: "tcp://localhost:1502",
		Retries:    maxretries,
		Log:        testutil.Logger{Quiet: true},
	}
	modbus.SlaveID = 1
	modbus.Coils = []fieldDefinition{
		{
			Name:    "retry_fail",
			Address: []uint16{0},
		},
	}

	var acc testutil.Accumulator
	require.NoError(t, modbus.Init())
	require.NotEmpty(t, modbus.requests)

	require.NoError(t, modbus.Gather(&acc))
	require.Len(t, acc.Errors, 1)
	require.ErrorContains(t, acc.FirstError(), `slave 1 on controller "tcp://localhost:1502": modbus: exception '1' (illegal function)`)
	require.Equal(t, 1, counter)
}

func TestCases(t *testing.T) {
	// Get all directories in testdata
	folders, err := os.ReadDir("testcases")
	require.NoError(t, err)

	// Prepare the influx parser for expectations
	parser := &influx.Parser{}
	require.NoError(t, parser.Init())

	// Compare options
	options := []cmp.Option{
		testutil.IgnoreTime(),
		testutil.SortMetrics(),
	}

	// Register the plugin
	inputs.Add("modbus", func() telegraf.Input { return &Modbus{} })

	// Define a function to return the register value as data
	readFunc := func(_ *mbserver.Server, frame mbserver.Framer) ([]byte, *mbserver.Exception) {
		data := frame.GetData()
		register := binary.BigEndian.Uint16(data[0:2])
		numRegs := binary.BigEndian.Uint16(data[2:4])

		// Add the length in bytes and the register to the returned data
		buf := make([]byte, 2*numRegs+1)
		buf[0] = byte(2 * numRegs)
		switch numRegs {
		case 1: // 16-bit
			binary.BigEndian.PutUint16(buf[1:], register)
		case 2: // 32-bit
			binary.BigEndian.PutUint32(buf[1:], uint32(register))
		case 4: // 64-bit
			binary.BigEndian.PutUint64(buf[1:], uint64(register))
		}
		return buf, &mbserver.Success
	}

	// Setup a Modbus server to test against
	serv := mbserver.NewServer()
	serv.RegisterFunctionHandler(mb.FuncCodeReadInputRegisters, readFunc)
	serv.RegisterFunctionHandler(mb.FuncCodeReadHoldingRegisters, readFunc)
	require.NoError(t, serv.ListenTCP("localhost:1502"))
	defer serv.Close()

	// Run the test cases
	for _, f := range folders {
		// Only handle folders
		if !f.IsDir() {
			continue
		}
		testcasePath := filepath.Join("testcases", f.Name())
		configFilename := filepath.Join(testcasePath, "telegraf.conf")
		expectedOutputFilename := filepath.Join(testcasePath, "expected.out")
		expectedErrorFilename := filepath.Join(testcasePath, "expected.err")
		initErrorFilename := filepath.Join(testcasePath, "init.err")

		t.Run(f.Name(), func(t *testing.T) {
			// Read the expected error for the init call if any
			var expectedInitError string
			if _, err := os.Stat(initErrorFilename); err == nil {
				e, err := testutil.ParseLinesFromFile(initErrorFilename)
				require.NoError(t, err)
				require.Len(t, e, 1)
				expectedInitError = e[0]
			}

			// Read the expected output if any
			var expected []telegraf.Metric
			if _, err := os.Stat(expectedOutputFilename); err == nil {
				var err error
				expected, err = testutil.ParseMetricsFromFile(expectedOutputFilename, parser)
				require.NoError(t, err)
			}

			// Read the expected error if any
			var expectedErrors []string
			if _, err := os.Stat(expectedErrorFilename); err == nil {
				e, err := testutil.ParseLinesFromFile(expectedErrorFilename)
				require.NoError(t, err)
				require.NotEmpty(t, e)
				expectedErrors = e
			}

			// Configure the plugin
			cfg := config.NewConfig()
			require.NoError(t, cfg.LoadConfig(configFilename))
			require.Len(t, cfg.Inputs, 1)

			// Extract the plugin and make sure it connects to our dummy
			// server
			plugin := cfg.Inputs[0].Input.(*Modbus)
			plugin.Controller = "tcp://localhost:1502"

			// Init the plugin.
			err := plugin.Init()
			if expectedInitError != "" {
				require.ErrorContains(t, err, expectedInitError)
				return
			}
			require.NoError(t, err)

			// Gather data
			var acc testutil.Accumulator
			require.NoError(t, plugin.Gather(&acc))
			if len(acc.Errors) > 0 {
				var actualErrorMsgs []string
				for _, err := range acc.Errors {
					actualErrorMsgs = append(actualErrorMsgs, err.Error())
				}
				require.ElementsMatch(t, actualErrorMsgs, expectedErrors)
			}

			// Check the metric nevertheless as we might get some metrics despite errors.
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, expected, actual, options...)
		})
	}
}

type rangeDefinition struct {
	start     uint16
	count     uint16
	increment uint16
	length    uint16
	dtype     string
	omit      bool
}

type requestExpectation struct {
	fields []rangeDefinition
	req    request
}

func generateRequestDefinitions(ranges []rangeDefinition) []requestFieldDefinition {
	var fields []requestFieldDefinition

	id := 0
	for _, r := range ranges {
		if r.increment == 0 {
			r.increment = r.length
		}
		for i := uint16(0); i < r.count; i++ {
			f := requestFieldDefinition{
				Name:      fmt.Sprintf("holding-%d", id),
				Address:   r.start + i*r.increment,
				InputType: r.dtype,
				Omit:      r.omit,
			}
			fields = append(fields, f)
			id++
		}
	}
	return fields
}

func generateExpectation(defs []requestExpectation) []request {
	requests := make([]request, 0, len(defs))
	for _, def := range defs {
		r := def.req
		r.fields = make([]field, 0)
		for _, d := range def.fields {
			if d.increment == 0 {
				d.increment = d.length
			}
			for i := uint16(0); i < d.count; i++ {
				f := field{
					address: d.start + i*d.increment,
					length:  d.length,
				}
				r.fields = append(r.fields, f)
			}
		}
		requests = append(requests, r)
	}
	return requests
}

func requireEqualRequests(t *testing.T, expected, actual []request) {
	require.Equal(t, len(expected), len(actual), "request size mismatch")

	for i, e := range expected {
		a := actual[i]
		require.Equalf(t, e.address, a.address, "address mismatch in request %d", i)
		require.Equalf(t, e.length, a.length, "length mismatch in request %d", i)
		require.Equalf(t, len(e.fields), len(a.fields), "no. fields mismatch in request %d", i)
		for j, ef := range e.fields {
			af := a.fields[j]
			require.Equalf(t, ef.address, af.address, "address mismatch in field %d of request %d", j, i)
			require.Equalf(t, ef.length, af.length, "length mismatch in field %d of request %d", j, i)
		}
	}
}

func TestRegisterWorkaroundsOneRequestPerField(t *testing.T) {
	plugin := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "register",
		Log:               testutil.Logger{Quiet: true},
		Workarounds:       workarounds{OnRequestPerField: true},
	}
	plugin.SlaveID = 1
	plugin.HoldingRegisters = []fieldDefinition{
		{
			ByteOrder: "AB",
			DataType:  "INT16",
			Name:      "holding-1",
			Address:   []uint16{1},
			Scale:     1.0,
		},
		{
			ByteOrder: "AB",
			DataType:  "INT16",
			Name:      "holding-2",
			Address:   []uint16{2},
			Scale:     1.0,
		},
		{
			ByteOrder: "AB",
			DataType:  "INT16",
			Name:      "holding-3",
			Address:   []uint16{3},
			Scale:     1.0,
		},
		{
			ByteOrder: "AB",
			DataType:  "INT16",
			Name:      "holding-4",
			Address:   []uint16{4},
			Scale:     1.0,
		},
		{
			ByteOrder: "AB",
			DataType:  "INT16",
			Name:      "holding-5",
			Address:   []uint16{5},
			Scale:     1.0,
		},
	}
	require.NoError(t, plugin.Init())
	require.Len(t, plugin.requests[1].holding, len(plugin.HoldingRegisters))
}

func TestRequestsWorkaroundsReadCoilsStartingAtZeroRegister(t *testing.T) {
	plugin := Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "register",
		Log:               testutil.Logger{Quiet: true},
		Workarounds:       workarounds{ReadCoilsStartingAtZero: true},
	}
	plugin.SlaveID = 1
	plugin.Coils = []fieldDefinition{
		{
			Name:    "coil-8",
			Address: []uint16{8},
		},
		{
			Name:    "coil-new-group",
			Address: []uint16{maxQuantityCoils},
		},
	}
	require.NoError(t, plugin.Init())
	require.Len(t, plugin.requests[1].coil, 2)

	// First group should now start at zero and have the cumulated length
	require.Equal(t, uint16(0), plugin.requests[1].coil[0].address)
	require.Equal(t, uint16(9), plugin.requests[1].coil[0].length)

	// The second field should form a new group as the previous request
	// is now too large (beyond max-coils-per-read) after zero enforcement.
	require.Equal(t, maxQuantityCoils, plugin.requests[1].coil[1].address)
	require.Equal(t, uint16(1), plugin.requests[1].coil[1].length)
}

func TestWorkaroundsStringRegisterLocation(t *testing.T) {
	tests := []struct {
		name     string
		location string
		order    string
		content  []byte
		expected string
	}{
		{
			name:  "default big-endian",
			order: "ABCD",
			content: []byte{
				0x4d, 0x6f, 0x64, 0x62, 0x75, 0x73, 0x20, 0x53,
				0x74, 0x72, 0x69, 0x6e, 0x67, 0x00,
			},
			expected: "Modbus String",
		},
		{
			name:  "default little-endian",
			order: "DCBA",
			content: []byte{
				0x6f, 0x4d, 0x62, 0x64, 0x73, 0x75, 0x53, 0x20,
				0x72, 0x74, 0x6e, 0x69, 0x00, 0x67,
			},
			expected: "Modbus String",
		},
		{
			name:     "both big-endian",
			location: "both",
			order:    "ABCD",
			content: []byte{
				0x4d, 0x6f, 0x64, 0x62, 0x75, 0x73, 0x20, 0x53,
				0x74, 0x72, 0x69, 0x6e, 0x67, 0x00,
			},
			expected: "Modbus String",
		},
		{
			name:     "both little-endian",
			location: "both",
			order:    "DCBA",
			content: []byte{
				0x6f, 0x4d, 0x62, 0x64, 0x73, 0x75, 0x53, 0x20,
				0x72, 0x74, 0x6e, 0x69, 0x00, 0x67,
			},
			expected: "Modbus String",
		},
		{
			name:     "lower big-endian",
			location: "lower",
			order:    "ABCD",
			content: []byte{
				0x00, 0x4d, 0x00, 0x6f, 0x00, 0x64, 0x00, 0x62,
				0x00, 0x75, 0x00, 0x73, 0x00, 0x20, 0x00, 0x53,
				0x00, 0x74, 0x00, 0x72, 0x00, 0x69, 0x00, 0x6e,
				0x00, 0x67, 0x00, 0x00,
			},
			expected: "Modbus String",
		},
		{
			name:     "lower little-endian",
			location: "lower",
			order:    "DCBA",
			content: []byte{
				0x4d, 0x00, 0x6f, 0x00, 0x64, 0x00, 0x62, 0x00,
				0x75, 0x00, 0x73, 0x00, 0x20, 0x00, 0x53, 0x00,
				0x74, 0x00, 0x72, 0x00, 0x69, 0x00, 0x6e, 0x00,
				0x67, 0x00, 0x00, 0x00,
			},
			expected: "Modbus String",
		},
		{
			name:     "upper big-endian",
			location: "upper",
			order:    "ABCD",
			content: []byte{
				0x4d, 0x00, 0x6f, 0x00, 0x64, 0x00, 0x62, 0x00,
				0x75, 0x00, 0x73, 0x00, 0x20, 0x00, 0x53, 0x00,
				0x74, 0x00, 0x72, 0x00, 0x69, 0x00, 0x6e, 0x00,
				0x67, 0x00, 0x00, 0x00,
			},
			expected: "Modbus String",
		},
		{
			name:     "upper little-endian",
			location: "upper",
			order:    "DCBA",
			content: []byte{
				0x00, 0x4d, 0x00, 0x6f, 0x00, 0x64, 0x00, 0x62,
				0x00, 0x75, 0x00, 0x73, 0x00, 0x20, 0x00, 0x53,
				0x00, 0x74, 0x00, 0x72, 0x00, 0x69, 0x00, 0x6e,
				0x00, 0x67, 0x00, 0x00,
			},
			expected: "Modbus String",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const addr = uint16(8)
			length := uint16(len(tt.content) / 2)

			expected := []telegraf.Metric{
				metric.New(
					"modbus",
					map[string]string{
						"name":     "Test",
						"slave_id": "1",
						"type":     "holding_register",
					},
					map[string]interface{}{
						"value": tt.expected,
					},
					time.Unix(0, 0),
				),
			}

			plugin := &Modbus{
				Name:              "Test",
				Controller:        "tcp://localhost:1502",
				ConfigurationType: "request",
				Log:               testutil.Logger{Quiet: true},
				Workarounds:       workarounds{StringRegisterLocation: tt.location},
				configurationPerRequest: configurationPerRequest{
					Requests: []requestDefinition{
						{
							SlaveID:      1,
							ByteOrder:    tt.order,
							RegisterType: "holding",
							Fields: []requestFieldDefinition{
								{
									Address:   addr,
									Name:      "value",
									InputType: "STRING",
									Length:    length,
								},
							},
						},
					},
				},
			}
			require.NoError(t, plugin.Init())

			// Create a mock server and fill in the data
			serv := mbserver.NewServer()
			require.NoError(t, serv.ListenTCP("localhost:1502"))
			defer serv.Close()

			handler := mb.NewTCPClientHandler("localhost:1502")
			require.NoError(t, handler.Connect())
			defer handler.Close()
			client := mb.NewClient(handler)
			_, err := client.WriteMultipleRegisters(addr, length, tt.content)
			require.NoError(t, err)

			// Gather the data
			var acc testutil.Accumulator
			require.NoError(t, plugin.Gather(&acc))

			// Compare
			actual := acc.GetTelegrafMetrics()
			testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
		})
	}
}

func TestWorkaroundsStringRegisterLocationInvalid(t *testing.T) {
	plugin := &Modbus{
		Name:              "Test",
		Controller:        "tcp://localhost:1502",
		ConfigurationType: "request",
		Log:               testutil.Logger{Quiet: true},
		Workarounds:       workarounds{StringRegisterLocation: "foo"},
	}
	require.ErrorContains(t, plugin.Init(), `invalid 'string_register_location'`)
}
