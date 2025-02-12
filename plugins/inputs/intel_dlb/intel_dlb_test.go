//go:build linux
// +build linux

package intel_dlb

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs/dpdk/mocks"
	"github.com/influxdata/telegraf/testutil"
)

func TestDLB_Init(t *testing.T) {
	t.Run("when SocketPath is empty, then set default value", func(t *testing.T) {
		dlb := IntelDLB{
			SocketPath: "",
			Log:        testutil.Logger{},
		}
		require.Equal(t, "", dlb.SocketPath)

		//nolint:errcheck // we are just testing that socket path gets set to default, not that default is valid
		dlb.Init()

		require.Equal(t, defaultSocketPath, dlb.SocketPath)
	})

	t.Run("invalid socket path throws error in Init method when UnreachableSocketBehavior is set to 'error'", func(t *testing.T) {
		dlb := IntelDLB{
			SocketPath:                "/this/is/wrong/path",
			Log:                       testutil.Logger{},
			UnreachableSocketBehavior: "error",
		}
		err := dlb.Init()

		require.Error(t, err)
		require.Contains(t, err.Error(), "provided path does not exist")
	})

	t.Run("not-existing socket path doesn't throw error in Init method when UnreachableSocketBehavior is set to 'ignore'", func(t *testing.T) {
		dlb := IntelDLB{
			SocketPath:                "/socket/is/not/there/yet",
			Log:                       testutil.Logger{},
			UnreachableSocketBehavior: "ignore",
		}
		err := dlb.Init()

		require.Error(t, err)
		require.NotContains(t, err.Error(), "provided path does not exist")
	})

	t.Run("wrong UnreachableSocketBehavior option throws error in Init method", func(t *testing.T) {
		pathToSocket, socket := createSocketForTest(t)
		defer socket.Close()
		dlb := IntelDLB{
			SocketPath:                pathToSocket,
			UnreachableSocketBehavior: "DAS BOOT",
			Log:                       testutil.Logger{},
		}
		err := dlb.Init()

		require.Error(t, err)
		require.Contains(t, err.Error(), "unreachable_socket_behavior: unknown choice DAS BOOT")
	})

	t.Run("wrong eventdev command throws error in Init method", func(t *testing.T) {
		pathToSocket, socket := createSocketForTest(t)
		defer socket.Close()
		dlb := IntelDLB{
			SocketPath:       pathToSocket,
			EventdevCommands: []string{"/noteventdev/dev_xstats"},
			Log:              testutil.Logger{},
		}
		err := dlb.Init()

		require.Error(t, err)
		require.Contains(t, err.Error(), "provided command is not valid - ")
	})

	t.Run("wrong eventdev command throws error", func(t *testing.T) {
		dlb := IntelDLB{
			EventdevCommands: []string{"/noteventdev/dev_xstats"},
		}
		err := validateEventdevCommands(dlb.EventdevCommands)

		require.Error(t, err)
		require.Contains(t, err.Error(), "provided command is not valid - ")
	})

	t.Run("validate eventdev command", func(t *testing.T) {
		dlb := IntelDLB{
			EventdevCommands: []string{"/eventdev/dev_xstats"},
		}
		err := validateEventdevCommands(dlb.EventdevCommands)

		require.NoError(t, err)
	})

	t.Run("successfully initialize intel_dlb struct", func(t *testing.T) {
		pathToSocket, socket := createSocketForTest(t)
		fileMock := &mockRasReader{}
		defer socket.Close()
		dlb := IntelDLB{
			SocketPath: pathToSocket,
			Log:        testutil.Logger{},
			rasReader:  fileMock,
		}
		const globPath = "/sys/devices/pci0000:00/0000:00:00.0/device"
		fileMock.On("gatherPaths", mock.Anything).Return([]string{globPath}, nil).Once().
			On("readFromFile", mock.Anything).Return([]byte("0x2710"), nil).Once()

		err := dlb.Init()
		require.NoError(t, err)
		require.Equal(t, []string{"/eventdev/dev_xstats", "/eventdev/port_xstats", "/eventdev/queue_xstats", "/eventdev/queue_links"}, dlb.EventdevCommands)
		fileMock.AssertExpectations(t)
	})

	t.Run("throw error while initializing dlb plugin when theres no dlb device", func(t *testing.T) {
		fileMock := &mockRasReader{}
		pathToSocket, socket := createSocketForTest(t)
		defer socket.Close()
		dlb := IntelDLB{
			rasReader:  fileMock,
			SocketPath: pathToSocket,
			Log:        testutil.Logger{},
		}
		const emptyPath = ""
		fileMock.On("gatherPaths", mock.Anything).Return([]string{emptyPath}, errors.New("can't find device folder")).Once()
		err := dlb.Init()
		require.Error(t, err)
		require.Contains(t, err.Error(), "can't find device folder")
		fileMock.AssertExpectations(t)
	})
}

func TestDLB_writeReadSocketMessage(t *testing.T) {
	t.Run("throws custom error message when write error occur", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection: mockConn,
			Log:        testutil.Logger{},
		}
		mockConn.On("Write", make([]byte, 0)).Return(0, errors.New("write error")).Once().
			On("Close").Return(nil).Once()

		_, _, err := dlb.writeReadSocketMessage("")

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to send command to socket: 'write error'")
		mockConn.AssertExpectations(t)
	})

	t.Run("throws custom error message when read error occur", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection: mockConn,
			Log:        testutil.Logger{},
		}
		simulateResponse(mockConn, "", errors.New("read error"))

		_, _, err := dlb.writeReadSocketMessage("")

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to read response of from socket: 'read error'")
		mockConn.AssertExpectations(t)
	})

	t.Run("throws custom error message when write error occur", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection: mockConn,
			Log:        testutil.Logger{},
		}
		mockConn.On("Write", make([]byte, 0)).Return(0, nil).Once().
			On("Read", mock.Anything).Return(0, nil).
			On("Close").Return(nil).Once()

		_, _, err := dlb.writeReadSocketMessage("")

		require.Error(t, err)
		require.Contains(t, err.Error(), "got empty response from socket: 'message length is empty'")
		mockConn.AssertExpectations(t)
	})
}

func TestDLB_parseJSON(t *testing.T) {
	var tests = []struct {
		testName    string
		socketReply []byte
		replyMsgLen int
		errMsg      string
	}{
		{"wrong json format", []byte("/wrong/json"), 10, "invalid character '/' looking for beginning of value"},
		{"socket reply length equal to 0 throws error", []byte("/wrong/json"), 0, "socket reply message is empty"},
		{"invalid reply length throws error", []byte("/wrong/json"), 20, "socket reply length is bigger than it should be"},
		{"nil socket reply throws error", nil, 0, "socket reply is empty"},
	}
	for _, testCase := range tests {
		t.Run(testCase.testName, func(t *testing.T) {
			mockConn := &mocks.Conn{}
			dlb := IntelDLB{
				connection: mockConn,
				Log:        testutil.Logger{},
			}
			mockConn.On("Close").Return(nil).Once()

			err := dlb.parseJSON(testCase.replyMsgLen, testCase.socketReply, make(map[string]interface{}))

			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.errMsg)
			mockConn.AssertExpectations(t)
		})
	}
}

func TestDLB_getInitMessageLength(t *testing.T) {
	t.Run("trying to unmarshal invalid JSON throws error", func(t *testing.T) {
		fileMock := &mockRasReader{}
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection: mockConn,
			Log:        testutil.Logger{},
			rasReader:  fileMock,
		}
		mockConn.On("Read", mock.Anything).Run(func(arg mock.Arguments) {
			elem := arg.Get(0).([]byte)
			copy(elem, "")
		}).Return(len(""), nil).Once().On("Close").Return(nil).Once()

		err := dlb.setInitMessageLength()
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse json")
		fileMock.AssertExpectations(t)
	})

	t.Run("when init message equals 0 throw error", func(t *testing.T) {
		fileMock := &mockRasReader{}
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection: mockConn,
			Log:        testutil.Logger{},
			rasReader:  fileMock,
		}
		dlb.maxInitMessageLength = 1024
		const initMsgResponse = "{\"version\":\"DPDK 20.11.3\",\"pid\":208361,\"max_output_len\":0}"
		mockConn.On("Read", mock.Anything).Run(func(arg mock.Arguments) {
			elem := arg.Get(0).([]byte)
			copy(elem, initMsgResponse)
		}).Return(len(initMsgResponse), nil).Once().On("Close").Return(nil).Once()

		err := dlb.setInitMessageLength()
		require.Error(t, err)
		require.Contains(t, err.Error(), "got empty response from socket")
		fileMock.AssertExpectations(t)
	})
}

func TestDLB_gatherCommandsResult(t *testing.T) {
	t.Run("trying connecting to wrong socket throw error", func(t *testing.T) {
		pathToSocket := "/tmp/dpdk-test-socket"
		socket, err := net.Listen("unix", pathToSocket)
		fileMock := &mockRasReader{}
		defer socket.Close()
		dlb := IntelDLB{
			SocketPath: pathToSocket,
			Log:        testutil.Logger{},
			rasReader:  fileMock,
		}
		require.NoError(t, err)

		err = dlb.gatherCommandsResult("", nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "connect: protocol wrong type for socket")
		fileMock.AssertExpectations(t)
	})
}

func TestDLB_gatherCommandsWithDeviceIndex(t *testing.T) {
	t.Run("process wrong commands should throw error", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection:       mockConn,
			Log:              testutil.Logger{},
			EventdevCommands: []string{"/eventdev/dev_xstats"},
		}
		response := "/wrong/JSON"
		dlb.maxInitMessageLength = 1024
		simulateResponse(mockConn, response, nil)
		mockConn.On("Write", mock.Anything).Return(0, nil)
		mockConn.On("Close").Return(nil).Once()

		_, err := dlb.gatherCommandsWithDeviceIndex()

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse json")
		mockConn.AssertExpectations(t)
	})

	t.Run("process commands should return array with command and device id", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection:           mockConn,
			Log:                  testutil.Logger{},
			maxInitMessageLength: 1024,
			EventdevCommands:     []string{"/eventdev/dev_xstats"},
		}
		response := fmt.Sprintf(`{%q: [0, 1]}`, eventdevListCommand)
		simulateResponse(mockConn, response, nil)

		expectedCommands := []string{"/eventdev/dev_xstats,0", "/eventdev/dev_xstats,1"}

		commands, err := dlb.gatherCommandsWithDeviceIndex()

		require.NoError(t, err)
		require.Equal(t, expectedCommands, commands)
		mockConn.AssertExpectations(t)
	})

	t.Run("process commands should return array with queue and device id", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection:           mockConn,
			Log:                  testutil.Logger{},
			maxInitMessageLength: 1024,
			EventdevCommands:     []string{"/eventdev/queue_links"},
		}
		responseDevList := fmt.Sprintf(`{%q: [0]}`, eventdevListCommand)
		simulateResponse(mockConn, responseDevList, nil)
		responseQueueLinks := `{"0": [0]}`
		simulateResponse(mockConn, responseQueueLinks, nil)

		expectedCommands := []string{"/eventdev/queue_links,0,0"}

		commands, err := dlb.gatherCommandsWithDeviceIndex()

		require.NoError(t, err)
		require.Equal(t, expectedCommands, commands)
		mockConn.AssertExpectations(t)
	})

	t.Run("process wrong commands should throw error", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection:           mockConn,
			Log:                  testutil.Logger{},
			maxInitMessageLength: 1024,
			EventdevCommands:     []string{"/eventdev/dev_xstats", "/eventdev/wrong"},
		}
		response := fmt.Sprintf(`{%q: [0, 1]}`, eventdevListCommand)
		mockConn.On("Write", mock.Anything).Return(0, nil).Once()
		mockConn.On("Read", mock.Anything).Run(func(arg mock.Arguments) {
			elem := arg.Get(0).([]byte)
			copy(elem, response)
		}).Return(len(response), nil).Once().On("Close").Return(nil).Once()

		_, err := dlb.gatherCommandsWithDeviceIndex()

		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot split command")
		mockConn.AssertExpectations(t)
	})
}

func TestDLB_gatherSecondDeviceIndex(t *testing.T) {
	t.Run("process wrong commands should return error", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection:       mockConn,
			Log:              testutil.Logger{},
			EventdevCommands: []string{"/eventdev/wrong"},
		}
		mockConn.On("Close").Return(nil).Once()
		_, err := dlb.gatherSecondDeviceIndex(0, dlb.EventdevCommands[0])

		require.Error(t, err)
		require.Contains(t, err.Error(), "cannot split command -")
		mockConn.AssertExpectations(t)
	})

	t.Run("process wrong response commands should throw error", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection:       mockConn,
			Log:              testutil.Logger{},
			EventdevCommands: []string{"/eventdev/port_xstats"},
		}
		response := "/wrong/JSON"

		simulateResponse(mockConn, response, nil)
		mockConn.On("Close").Return(nil).Once()

		_, err := dlb.gatherSecondDeviceIndex(0, dlb.EventdevCommands[0])

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse json")
		mockConn.AssertExpectations(t)
	})

	t.Run("process wrong response commands should throw error and close socket, after second function call should connect to socket", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		pathToSocket, socket := createSocketForTest(t)
		defer socket.Close()
		dlb := IntelDLB{
			connection:           mockConn,
			Log:                  testutil.Logger{},
			maxInitMessageLength: 1024,
			EventdevCommands:     []string{"/eventdev/port_xstats"},
		}

		response := "/wrong/JSON"

		simulateResponse(mockConn, response, nil)
		mockConn.On("Close").Return(nil).Once()

		_, err := dlb.gatherSecondDeviceIndex(0, dlb.EventdevCommands[0])
		require.Nil(t, dlb.connection)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse json")
		dlb.SocketPath = pathToSocket
		go simulateSocketResponseForGather(socket, t)
		commandDeviceIndexes, err := dlb.gatherSecondDeviceIndex(0, dlb.EventdevCommands[0])
		require.NoError(t, err)

		expectedCommands := []string{"/eventdev/port_xstats,0,0", "/eventdev/port_xstats,0,1"}
		commands := commandDeviceIndexes

		require.Equal(t, expectedCommands, commands)
		mockConn.AssertExpectations(t)
	})

	t.Run("process commands should return array with command and second device id", func(t *testing.T) {
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection:           mockConn,
			Log:                  testutil.Logger{},
			maxInitMessageLength: 1024,
			EventdevCommands:     []string{"/eventdev/port_xstats"},
		}
		eventdevListWithSecondIndex := []string{"/eventdev/port_list", "/eventdev/queue_list"}
		response := fmt.Sprintf(`{%q: [0, 1]}`, eventdevListWithSecondIndex[0])
		simulateResponse(mockConn, response, nil)

		expectedCommands := []string{"/eventdev/port_xstats,0,0", "/eventdev/port_xstats,0,1"}

		commandDeviceIndexes, err := dlb.gatherSecondDeviceIndex(0, dlb.EventdevCommands[0])

		commands := commandDeviceIndexes

		require.NoError(t, err)
		require.Equal(t, expectedCommands, commands)
		mockConn.AssertExpectations(t)
	})
}

func TestDLB_processCommandResult(t *testing.T) {
	t.Run("gather xstats info with valid values", func(t *testing.T) {
		mockAcc := &testutil.Accumulator{}
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection:           mockConn,
			Log:                  testutil.Logger{},
			maxInitMessageLength: 1024,
			EventdevCommands:     []string{"/eventdev/dev_xstats"},
		}
		response := fmt.Sprintf(`{%q: [0]}`, eventdevListCommand)
		simulateResponse(mockConn, response, nil)

		response = `{"/eventdev/dev_xstats": {"dev_rx_ok": 0}}`
		simulateResponse(mockConn, response, nil)
		err := dlb.gatherMetricsFromSocket(mockAcc)
		require.NoError(t, err)

		expected := []telegraf.Metric{
			testutil.MustMetric(
				"intel_dlb",
				map[string]string{
					"command": "/eventdev/dev_xstats,0",
				},
				map[string]interface{}{
					"dev_rx_ok": int64(0),
				},
				time.Unix(0, 0),
			),
		}
		actual := mockAcc.GetTelegrafMetrics()

		testutil.RequireMetricsEqual(t, expected, actual, testutil.IgnoreTime())
		mockConn.AssertExpectations(t)
	})

	t.Run("successfully gather xstats and aer metrics", func(t *testing.T) {
		mockAcc := &testutil.Accumulator{}
		mockConn := &mocks.Conn{}
		fileMock := &mockRasReader{}
		dlb := IntelDLB{
			connection:           mockConn,
			Log:                  testutil.Logger{},
			EventdevCommands:     []string{"/eventdev/dev_xstats"},
			devicesDir:           []string{"/sys/devices/pci0000:00/0000:00:00.0/device"},
			rasReader:            fileMock,
			maxInitMessageLength: 1024,
		}
		responseGather := fmt.Sprintf(`{%q: [0]}`, eventdevListCommand)
		mockConn.On("Write", mock.Anything).Return(0, nil).Twice()
		mockConn.On("Read", mock.Anything).Run(func(arg mock.Arguments) {
			elem := arg.Get(0).([]byte)
			copy(elem, responseGather)
		}).Return(len(responseGather), nil).Once()
		response := `{"/eventdev/dev_xstats": {"dev_rx_ok": 0}}`
		mockConn.On("Read", mock.Anything).Run(func(arg mock.Arguments) {
			elem := arg.Get(0).([]byte)
			copy(elem, response)
		}).Return(len(response), nil).Once()
		fileMock.On("readFromFile", mock.AnythingOfType("string")).Return([]byte(aerCorrectableData), nil).Once().
			On("readFromFile", mock.AnythingOfType("string")).Return([]byte(aerFatalData), nil).Once().
			On("readFromFile", mock.AnythingOfType("string")).Return([]byte(aerNonFatalData), nil).Once()
		err := dlb.Gather(mockAcc)
		require.NoError(t, err)
		actual := mockAcc.GetTelegrafMetrics()
		testutil.SortMetrics()
		ex := expectedTelegrafMetrics
		testutil.RequireMetricsEqual(t, ex, actual, testutil.IgnoreTime())
		mockConn.AssertExpectations(t)
	})

	t.Run("invalid JSON throws error in process result function", func(t *testing.T) {
		mockAcc := &testutil.Accumulator{}
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection:       mockConn,
			Log:              testutil.Logger{},
			EventdevCommands: []string{"/eventdev/dev_xstats"},
		}
		response := fmt.Sprintf(`{%q: [0]}`, eventdevListCommand)
		simulateResponse(mockConn, response, nil)

		simulateResponse(mockConn, "/wrong/json", nil)
		mockConn.On("Close").Return(nil).Once()

		err := dlb.gatherMetricsFromSocket(mockAcc)

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse json")
		mockConn.AssertExpectations(t)
	})

	t.Run("throw error when reply message is empty", func(t *testing.T) {
		mockAcc := &testutil.Accumulator{}
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection: mockConn,
			Log:        testutil.Logger{},
		}
		const response = ""
		mockConn.On("Write", mock.Anything).Return(0, nil)
		mockConn.On("Read", mock.Anything).Run(func(arg mock.Arguments) {
			elem := arg.Get(0).([]byte)
			copy(elem, response)
		}).Return(len(response), nil).Once()
		mockConn.On("Close").Return(nil)

		err := dlb.gatherMetricsFromSocket(mockAcc)
		require.Error(t, err)
		require.Contains(t, err.Error(), "got empty response from socket")
		mockConn.AssertExpectations(t)
	})

	t.Run("throw error when can't read socket reply", func(t *testing.T) {
		mockAcc := &testutil.Accumulator{}
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection: mockConn,
			Log:        testutil.Logger{},
		}
		const response = ""
		mockConn.On("Write", mock.Anything).Return(0, nil)
		mockConn.On("Read", mock.Anything).Run(func(arg mock.Arguments) {
			elem := arg.Get(0).([]byte)
			copy(elem, response)
		}).Return(len(response), errors.New("read error")).Once()
		mockConn.On("Close").Return(nil)

		err := dlb.gatherMetricsFromSocket(mockAcc)
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to read response of from socket")
		mockConn.AssertExpectations(t)
	})

	t.Run("throw error when invalid reply was provided", func(t *testing.T) {
		mockAcc := &testutil.Accumulator{}
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection:           mockConn,
			maxInitMessageLength: 1024,
			Log:                  testutil.Logger{},
		}
		simulateResponse(mockConn, "\"string reply\"", nil)
		mockConn.On("Close").Return(nil).Once()
		err := dlb.gatherMetricsFromSocket(mockAcc)

		require.Error(t, err)
		require.Contains(t, err.Error(), "json: cannot unmarshal string into Go value of type")
		mockConn.AssertExpectations(t)
	})

	t.Run("throw error while processing xstats", func(t *testing.T) {
		mockAcc := &testutil.Accumulator{}
		mockConn := &mocks.Conn{}
		fileMock := &mockRasReader{}
		dlb := IntelDLB{
			connection:           mockConn,
			Log:                  testutil.Logger{},
			EventdevCommands:     []string{"/eventdev/dev_xstats"},
			rasReader:            fileMock,
			maxInitMessageLength: 1024,
		}
		mockConn.On("Close").Return(nil)

		responseGather := fmt.Sprintf(`{%q: [0]}`, eventdevListCommand)
		mockConn.On("Write", mock.Anything).Return(0, nil).Once().
			On("Read", mock.Anything).Run(func(arg mock.Arguments) {
			elem := arg.Get(0).([]byte)
			copy(elem, responseGather)
		}).Return(len(responseGather), nil).Once()

		wrongResponse := "/wrong/json"
		mockConn.On("Write", mock.Anything).Return(0, nil).Once().
			On("Read", mock.Anything).Run(func(arg mock.Arguments) {
			elem := arg.Get(0).([]byte)
			copy(elem, wrongResponse)
		}).Return(len(wrongResponse), nil).Once()

		err := dlb.gatherMetricsFromSocket(mockAcc)

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to parse json:")
		mockConn.AssertExpectations(t)
	})
}

func Test_checkAndAddDLBDevice(t *testing.T) {
	t.Run("throw error when dlb validation can't find device folder", func(t *testing.T) {
		fileMock := &mockRasReader{}
		dlb := IntelDLB{
			rasReader: fileMock,
			Log:       testutil.Logger{},
		}
		fileMock.On("gatherPaths", mock.AnythingOfType("string")).Return(nil, errors.New("can't find device folder")).Once()

		err := dlb.checkAndAddDLBDevice()

		require.Error(t, err)
		require.Contains(t, err.Error(), "can't find device folder")
		fileMock.AssertExpectations(t)
	})

	t.Run("reading file throws error", func(t *testing.T) {
		fileMock := &mockRasReader{}
		dlb := IntelDLB{
			rasReader:  fileMock,
			Log:        testutil.Logger{},
			devicesDir: []string{"/sys/devices/pci0000:00/0000:00:00.0/device"},
		}
		const globPath = "/sys/devices/pci0000:00/0000:00:00.0/device"
		fileMock.On("gatherPaths", mock.Anything).Return([]string{globPath}, nil).Once().
			On("readFromFile", mock.Anything).Return([]byte("0x2710"), errors.New("read error while getting device folders")).Once()

		err := dlb.checkAndAddDLBDevice()

		require.Error(t, err)
		require.Contains(t, err.Error(), "read error while getting device folders")
		fileMock.AssertExpectations(t)
	})

	t.Run("reading file with empty rasreader throws error", func(t *testing.T) {
		fileMock := &mockRasReader{}
		dlb := IntelDLB{
			Log: testutil.Logger{},
		}
		err := dlb.checkAndAddDLBDevice()

		require.Error(t, err)
		require.Contains(t, err.Error(), "rasreader was not initialized")
		fileMock.AssertExpectations(t)
	})

	t.Run("reading file with unused device IDs throws error", func(t *testing.T) {
		fileMock := &mockRasReader{}
		dlb := IntelDLB{
			rasReader:    fileMock,
			Log:          testutil.Logger{},
			devicesDir:   []string{"/sys/devices/pci0000:00/0000:00:00.0/device"},
			DLBDeviceIDs: []string{"0x2710"},
		}
		const globPath = "/sys/devices/pci0000:00/0000:00:00.0/device"
		fileMock.On("gatherPaths", mock.Anything).Return([]string{globPath}, nil).Once().
			On("readFromFile", mock.Anything).Return([]byte("0x2710"), errors.New("read error while getting device folders")).Once()

		err := dlb.checkAndAddDLBDevice()

		require.Error(t, err)
		require.Contains(t, err.Error(), "read error while getting device folders")
		fileMock.AssertExpectations(t)
	})

	t.Run("no errors when dlb device was found while validating", func(t *testing.T) {
		fileMock := &mockRasReader{}
		dlb := IntelDLB{
			rasReader:    fileMock,
			Log:          testutil.Logger{},
			DLBDeviceIDs: []string{"0x2710"},
		}
		const globPath = "/sys/devices/pci0000:00/0000:00:00.0/device"
		fileMock.On("gatherPaths", mock.Anything).Return([]string{globPath}, nil).Once().
			On("readFromFile", mock.Anything).Return([]byte("0x2710"), nil).Once()

		err := dlb.checkAndAddDLBDevice()

		require.NoError(t, err)

		expected := []string{"/sys/devices/pci0000:00/0000:00:00.0"}
		require.Equal(t, expected, dlb.devicesDir)
		fileMock.AssertExpectations(t)
	})

	t.Run("no errors when found unused dlb device", func(t *testing.T) {
		fileMock := &mockRasReader{}
		dlb := IntelDLB{
			rasReader:    fileMock,
			Log:          testutil.Logger{},
			DLBDeviceIDs: []string{"0x2710", "0x0000"},
		}
		const globPath = "/sys/devices/pci0000:00/0000:00:00.0/device"
		fileMock.On("gatherPaths", mock.Anything).Return([]string{globPath}, nil).Once().
			On("readFromFile", mock.Anything).Return([]byte("0x2710"), nil).Once()

		err := dlb.checkAndAddDLBDevice()

		require.NoError(t, err)

		expected := []string{"/sys/devices/pci0000:00/0000:00:00.0"}
		require.Equal(t, expected, dlb.devicesDir)
		fileMock.AssertExpectations(t)
	})

	t.Run("error when dlb device was not found while validating", func(t *testing.T) {
		fileMock := &mockRasReader{}
		mockConn := &mocks.Conn{}
		dlb := IntelDLB{
			connection: mockConn,
			rasReader:  fileMock,
			Log:        testutil.Logger{},
		}
		const globPath = "/sys/devices/pci0000:00/0000:00:00.0/device"
		fileMock.On("gatherPaths", mock.Anything).Return([]string{globPath}, nil).Once().
			On("readFromFile", mock.Anything).Return([]byte("0x7100"), nil).Once()

		err := dlb.checkAndAddDLBDevice()

		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("cannot find any of provided IDs on the system - %+q", dlb.DLBDeviceIDs))
		fileMock.AssertExpectations(t)
		mockConn.AssertExpectations(t)
	})
}

func Test_readRasMetrics(t *testing.T) {
	var errorTests = []struct {
		name           string
		returnResponse []byte
		err            error
		errMsg         string
	}{
		{"error when reading fails", []byte(aerCorrectableData), errors.New("read error"), "read error"},
		{"error when empty data is given", []byte(""), nil, "no value to parse"},
		{"error when trying to split empty data", []byte("x1 x2"), nil, "failed to parse value"},
	}

	for _, test := range errorTests {
		t.Run(test.name, func(t *testing.T) {
			fileMock := &mockRasReader{}
			mockConn := &mocks.Conn{}
			dlb := IntelDLB{
				connection: mockConn,
				rasReader:  fileMock,
				Log:        testutil.Logger{},
			}
			mockConn.On("Close").Return(nil).Once()
			fileMock.On("readFromFile", mock.AnythingOfType("string")).Return(test.returnResponse, test.err).Once()

			_, err := dlb.readRasMetrics("/dlb", "device")

			require.Error(t, err)
			require.Contains(t, err.Error(), test.errMsg)
			fileMock.AssertExpectations(t)
		})
	}

	t.Run("no error when reading countable error file", func(t *testing.T) {
		fileMock := &mockRasReader{}
		dlb := IntelDLB{
			rasReader: fileMock,
			Log:       testutil.Logger{},
		}

		fileMock.On("readFromFile", mock.AnythingOfType("string")).Return([]byte(aerCorrectableData), nil).Once()

		_, err := dlb.readRasMetrics("/dlb", "device")

		require.NoError(t, err)
		fileMock.AssertExpectations(t)
	})
}

func Test_gatherRasMetrics(t *testing.T) {
	var errorTests = []struct {
		name           string
		returnResponse []byte
		err            error
		errMsg         string
	}{
		{"throw error when data in file is invalid", nil, nil, "no value to parse"},
		{"throw error when data in file is invalid", []byte("x1 x2"), nil, "failed to parse value"},
	}
	for _, test := range errorTests {
		t.Run(test.name, func(t *testing.T) {
			fileMock := &mockRasReader{}
			mockAcc := &testutil.Accumulator{}
			mockConn := &mocks.Conn{}
			dlb := IntelDLB{
				connection: mockConn,
				rasReader:  fileMock,
				devicesDir: []string{"/sys/devices/pci0000:00/0000:00:00.0/device"},
				Log:        testutil.Logger{},
			}
			mockConn.On("Close").Return(nil).Once()
			fileMock.On("readFromFile", mock.AnythingOfType("string")).Return(test.returnResponse, test.err).Once()

			err := dlb.gatherRasMetrics(mockAcc)

			require.Error(t, err)
			require.Contains(t, err.Error(), test.errMsg)
			fileMock.AssertExpectations(t)
		})
	}

	t.Run("gather ras metrics and add to accumulator", func(t *testing.T) {
		fileMock := &mockRasReader{}
		mockAcc := &testutil.Accumulator{}
		dlb := IntelDLB{
			rasReader:  fileMock,
			devicesDir: []string{"/sys/devices/pci0000:00/0000:00:00.0/device"},
			Log:        testutil.Logger{},
		}
		fileMock.On("readFromFile", mock.AnythingOfType("string")).Return([]byte(aerCorrectableData), nil).Once().
			On("readFromFile", mock.AnythingOfType("string")).Return([]byte(aerFatalData), nil).Once().
			On("readFromFile", mock.AnythingOfType("string")).Return([]byte(aerNonFatalData), nil).Once()

		err := dlb.gatherRasMetrics(mockAcc)

		require.NoError(t, err)

		actual := mockAcc.GetTelegrafMetrics()
		testutil.SortMetrics()
		testutil.RequireMetricsEqual(t, expectedRasMetrics, actual, testutil.IgnoreTime())
		fileMock.AssertExpectations(t)
	})
}

func Test_rasReader(t *testing.T) {
	file := rasReaderImpl{}
	// Create unique temporary file
	fileobj, err := os.CreateTemp(t.TempDir(), "qat")
	require.NoError(t, err)

	t.Run("tests with existing file", func(t *testing.T) {
		// Remove the temporary file after this test
		defer os.Remove(fileobj.Name())

		_, err = fileobj.WriteString(testFileContent)
		require.NoError(t, err)
		err = fileobj.Close()
		require.NoError(t, err)

		// Check that content returned by read is equal to provided file.
		data, err := file.readFromFile(fileobj.Name())
		require.NoError(t, err)
		require.Equal(t, []byte(testFileContent), data)

		// Error if path is malformed.
		_, err = file.readFromFile(fileobj.Name() + "/../..")
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a directory")
	})

	var errorTests = []struct {
		name           string
		filePath       string
		expectedErrMsg string
	}{
		{"error if file does not exist", fileobj.Name(), "no such file or directory"},
		{"error if path does not point to regular file", os.TempDir(), "is a directory"},
		{"error if file does not exist", "/not/path/unreal/path", "no such file or directory"},
	}

	for _, test := range errorTests {
		t.Run(test.name, func(t *testing.T) {
			_, err = file.readFromFile(test.filePath)
			require.Error(t, err)
			require.Contains(t, err.Error(), test.expectedErrMsg)
		})
	}
}

func simulateResponse(mockConn *mocks.Conn, response string, readErr error) {
	mockConn.On("Write", mock.Anything).Return(0, nil).Once().
		On("Read", mock.Anything).Run(func(arg mock.Arguments) {
		elem := arg.Get(0).([]byte)
		copy(elem, response)
	}).Return(len(response), readErr).Once()

	if readErr != nil {
		mockConn.On("Close").Return(nil).Once()
	}
}

func simulateSocketResponseForGather(socket net.Listener, t *testing.T) {
	conn, err := socket.Accept()
	if err != nil {
		t.Error(err)
		return
	}

	type initMessage struct {
		Version      string `json:"version"`
		Pid          int    `json:"pid"`
		MaxOutputLen uint32 `json:"max_output_len"`
	}
	initMsg, err := json.Marshal(initMessage{
		Version:      "",
		Pid:          1,
		MaxOutputLen: 1024,
	})
	if err != nil {
		t.Error(err)
		return
	}

	if _, err = conn.Write(initMsg); err != nil {
		t.Error(err)
		return
	}

	eventdevListWithSecondIndex := []string{"/eventdev/port_list", "/eventdev/queue_list"}
	if _, err = fmt.Fprintf(conn, `{%q: [0, 1]}`, eventdevListWithSecondIndex[0]); err != nil {
		t.Error(err)
		return
	}
}

func createSocketForTest(t *testing.T) (string, net.Listener) {
	pathToSocket := "/tmp/dpdk-test-socket"
	socket, err := net.Listen("unixpacket", pathToSocket)
	require.NoError(t, err)
	return pathToSocket, socket
}

const (
	testFileContent = `
line1
line2 2
line3
line4
line5
`
	aerCorrectableData = `
RxErr 1
BadTLP 0
BadDLLP 0
Rollover 1
Timeout 0
NonFatalErr 0
CorrIntErr 0
HeaderOF 0
TOTAL_ERR_COR 0`
	aerFatalData = `
Undefined 0
DLP 1
SDES 0
TLP 0
FCP 0
CmpltTO 0
CmpltAbrt 0
UnxCmplt 0
RxOF 0
MalfTLP 0
ECRC 0
UnsupReq 0
ACSViol 0
UncorrIntErr 0
BlockedTLP 0
AtomicOpBlocked 0
TLPBlockedErr 0
PoisonTLPBlocked 0
TOTAL_ERR_FATAL 3`
	aerNonFatalData = `
Undefined 0
DLP 0
SDES 0
TLP 0
FCP 0
CmpltTO 2
CmpltAbrt 0
UnxCmplt 0
RxOF 0
MalfTLP 0
ECRC 0
UnsupReq 0
ACSViol 0
UncorrIntErr 0
BlockedTLP 0
AtomicOpBlocked 0
TLPBlockedErr 0
PoisonTLPBlocked 0
TOTAL_ERR_NONFATAL 9`
)

var (
	expectedRasMetrics = []telegraf.Metric{
		testutil.MustMetric(
			"intel_dlb_ras",
			map[string]string{
				"device":      "0000:00:00.0",
				"metric_file": aerCorrectableFileName,
			},
			map[string]interface{}{
				"RxErr":         uint64(1),
				"BadTLP":        uint64(0),
				"BadDLLP":       uint64(0),
				"Rollover":      uint64(1),
				"Timeout":       uint64(0),
				"NonFatalErr":   uint64(0),
				"CorrIntErr":    uint64(0),
				"HeaderOF":      uint64(0),
				"TOTAL_ERR_COR": uint64(0),
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"intel_dlb_ras",
			map[string]string{
				"device":      "0000:00:00.0",
				"metric_file": aerFatalFileName,
			},
			map[string]interface{}{
				"Undefined":        uint64(0),
				"DLP":              uint64(1),
				"SDES":             uint64(0),
				"TLP":              uint64(0),
				"FCP":              uint64(0),
				"CmpltTO":          uint64(0),
				"CmpltAbrt":        uint64(0),
				"UnxCmplt":         uint64(0),
				"RxOF":             uint64(0),
				"MalfTLP":          uint64(0),
				"ECRC":             uint64(0),
				"UnsupReq":         uint64(0),
				"ACSViol":          uint64(0),
				"UncorrIntErr":     uint64(0),
				"BlockedTLP":       uint64(0),
				"AtomicOpBlocked":  uint64(0),
				"TLPBlockedErr":    uint64(0),
				"PoisonTLPBlocked": uint64(0),
				"TOTAL_ERR_FATAL":  uint64(3),
			},
			time.Unix(0, 0),
		),
		testutil.MustMetric(
			"intel_dlb_ras",
			map[string]string{
				"device":      "0000:00:00.0",
				"metric_file": aerNonFatalFileName,
			},
			map[string]interface{}{
				"Undefined":          uint64(0),
				"DLP":                uint64(0),
				"SDES":               uint64(0),
				"TLP":                uint64(0),
				"FCP":                uint64(0),
				"CmpltTO":            uint64(2),
				"CmpltAbrt":          uint64(0),
				"UnxCmplt":           uint64(0),
				"RxOF":               uint64(0),
				"MalfTLP":            uint64(0),
				"ECRC":               uint64(0),
				"UnsupReq":           uint64(0),
				"ACSViol":            uint64(0),
				"UncorrIntErr":       uint64(0),
				"BlockedTLP":         uint64(0),
				"AtomicOpBlocked":    uint64(0),
				"TLPBlockedErr":      uint64(0),
				"PoisonTLPBlocked":   uint64(0),
				"TOTAL_ERR_NONFATAL": uint64(9),
			},
			time.Unix(0, 0),
		),
	}

	expectedTelegrafMetrics = []telegraf.Metric{
		testutil.MustMetric(
			"intel_dlb",
			map[string]string{
				"command": "/eventdev/dev_xstats,0",
			},
			map[string]interface{}{
				"dev_rx_ok": int64(0),
			},
			time.Unix(0, 0),
		),
		expectedRasMetrics[0],
		expectedRasMetrics[1],
		expectedRasMetrics[2],
	}
)
