package snmp

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

type mockedCommandResult struct {
	stdout    string
	stderr    string
	exitError bool
}

func mockExecCommand(arg0 string, args ...string) *exec.Cmd {
	args = append([]string{"-test.run=TestMockExecCommand", "--", arg0}, args...)
	cmd := exec.Command(os.Args[0], args...)
	cmd.Stderr = os.Stderr // so the test output shows errors
	return cmd
}

// This is not a real test. This is just a way of mocking out commands.
//
// Idea based on https://github.com/golang/go/blob/7c31043/src/os/exec/exec_test.go#L568
func TestMockExecCommand(_ *testing.T) {
	var cmd []string //nolint:prealloc // Pre-allocated this slice would break the algorithm
	for _, arg := range os.Args {
		if arg == "--" {
			cmd = make([]string, 0)
			continue
		}
		if cmd == nil {
			continue
		}
		cmd = append(cmd, arg)
	}
	if cmd == nil {
		return
	}

	cmd0 := strings.Join(cmd, "\000")
	mcr, ok := mockedCommandResults[cmd0]
	if !ok {
		cv := fmt.Sprintf("%#v", cmd)[8:] // trim `[]string` prefix
		fmt.Fprintf(
			os.Stderr,
			"Unmocked command. Please add the following to `mockedCommands` in snmp_mocks_generate.go, and then run `go generate`:\n\t%s,\n",
			cv,
		)
		//nolint:revive // error code is important for this "test"
		os.Exit(1)
	}
	fmt.Printf("%s", mcr.stdout)
	fmt.Fprintf(os.Stderr, "%s", mcr.stderr)
	if mcr.exitError {
		//nolint:revive // error code is important for this "test"
		os.Exit(1)
	}
	//nolint:revive // error code is important for this "test"
	os.Exit(0)
}

func init() {
	execCommand = mockExecCommand
}

// BEGIN GO GENERATE CONTENT
var mockedCommandResults = map[string]mockedCommandResult{
	"snmptranslate\x00-Td\x00-Ob\x00-m\x00all\x00.1.0.0.0": {
		stdout:    "TEST::testTable\ntestTable OBJECT-TYPE\n  -- FROM\tTEST\n  MAX-ACCESS\tnot-accessible\n  STATUS\tcurrent\n::= { iso(1) 0 testOID(0) 0 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00-m\x00all\x00.1.0.0.1.1": {
		stdout: "TEST::hostname\nhostname OBJECT-TYPE\n  -- FROM\tTEST\n  SYNTAX\tOCTET STRING\n  MAX-ACCESS\tread-only\n  " +
			"STATUS\tcurrent\n::= { iso(1) 0 testOID(0) 1 1 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00-m\x00all\x00.1.0.0.1.2": {
		stdout:    "TEST::1.2\nanonymous#1 OBJECT-TYPE\n  -- FROM\tTEST\n::= { iso(1) 0 testOID(0) 1 2 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00-m\x00all\x001.0.0.1.1": {
		stdout: "TEST::hostname\nhostname OBJECT-TYPE\n  -- FROM\tTEST\n  SYNTAX\tOCTET STRING\n  MAX-ACCESS\tread-only\n  " +
			"STATUS\tcurrent\n::= { iso(1) 0 testOID(0) 1 1 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00-m\x00all\x00.1.0.0.0.1.1": {
		stdout: "TEST::server\nserver OBJECT-TYPE\n  -- FROM\tTEST\n  SYNTAX\tOCTET STRING\n  MAX-ACCESS\tread-only\n  " +
			"STATUS\tcurrent\n::= { iso(1) 0 testOID(0) testTable(0) testTableEntry(1) 1 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00-m\x00all\x00.1.0.0.0.1.1.0": {
		stdout: "TEST::server.0\nserver OBJECT-TYPE\n  -- FROM\tTEST\n  SYNTAX\tOCTET STRING\n  MAX-ACCESS\tread-only\n  " +
			"STATUS\tcurrent\n::= { iso(1) 0 testOID(0) testTable(0) testTableEntry(1) server(1) 0 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00-m\x00all\x00.1.0.0.0.1.5": {
		stdout: "TEST::testTableEntry.5\ntestTableEntry OBJECT-TYPE\n  -- FROM\tTEST\n  MAX-ACCESS\tnot-accessible\n  " +
			"STATUS\tcurrent\n  INDEX\t\t{ server }\n::= { iso(1) 0 testOID(0) testTable(0) testTableEntry(1) 5 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00-m\x00all\x00.1.2.3": {
		stdout:    "iso.2.3\niso OBJECT-TYPE\n  -- FROM\t#-1\n::= { iso(1) 2 3 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00-m\x00all\x00.1.0.0.0.1.7": {
		stdout: "TEST::testTableEntry.7\ntestTableEntry OBJECT-TYPE\n  -- FROM\tTEST\n  MAX-ACCESS\tnot-accessible\n  " +
			"STATUS\tcurrent\n  INDEX\t\t{ server }\n::= { iso(1) std(0) testOID(0) testTable(0) testTableEntry(1) 7 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00.iso.2.3": {
		stdout:    "iso.2.3\niso OBJECT-TYPE\n  -- FROM\t#-1\n::= { iso(1) 2 3 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00-m\x00all\x00.999": {stdout: ".999\n [TRUNCATED]\n", stderr: "", exitError: false},
	"snmptranslate\x00-Td\x00-Ob\x00TEST::server": {
		stdout: "TEST::server\nserver OBJECT-TYPE\n  -- FROM\tTEST\n  SYNTAX\tOCTET STRING\n  MAX-ACCESS\tread-only\n  " +
			"STATUS\tcurrent\n::= { iso(1) 0 testOID(0) testTable(0) testTableEntry(1) 1 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00TEST::server.0": {
		stdout: "TEST::server.0\nserver OBJECT-TYPE\n  -- FROM\tTEST\n  SYNTAX\tOCTET STRING\n  MAX-ACCESS\tread-only\n  " +
			"STATUS\tcurrent\n::= { iso(1) 0 testOID(0) testTable(0) testTableEntry(1) server(1) 0 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00TEST::testTable": {
		stdout:    "TEST::testTable\ntestTable OBJECT-TYPE\n  -- FROM\tTEST\n  MAX-ACCESS\tnot-accessible\n  STATUS\tcurrent\n::= { iso(1) 0 testOID(0) 0 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00TEST::connections": {
		stdout: "TEST::connections\nconnections OBJECT-TYPE\n  -- FROM\tTEST\n  SYNTAX\tINTEGER\n  MAX-ACCESS\tread-only\n  " +
			"STATUS\tcurrent\n::= { iso(1) 0 testOID(0) testTable(0) testTableEntry(1) 2 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00TEST::latency": {
		stdout: "TEST::latency\nlatency OBJECT-TYPE\n  -- FROM\tTEST\n  SYNTAX\tOCTET STRING\n  MAX-ACCESS\tread-only\n  " +
			"STATUS\tcurrent\n::= { iso(1) 0 testOID(0) testTable(0) testTableEntry(1) 3 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00TEST::description": {
		stdout: "TEST::description\ndescription OBJECT-TYPE\n  -- FROM\tTEST\n  SYNTAX\tOCTET STRING\n  MAX-ACCESS\tread-only\n  " +
			"STATUS\tcurrent\n::= { iso(1) 0 testOID(0) testTable(0) testTableEntry(1) 4 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00TEST::hostname": {
		stdout: "TEST::hostname\nhostname OBJECT-TYPE\n  -- FROM\tTEST\n  SYNTAX\tOCTET STRING\n  MAX-ACCESS\tread-only\n  " +
			"STATUS\tcurrent\n::= { iso(1) 0 testOID(0) 1 1 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00IF-MIB::ifPhysAddress.1": {
		stdout: "IF-MIB::ifPhysAddress.1\nifPhysAddress OBJECT-TYPE\n  -- FROM\tIF-MIB\n  -- TEXTUAL CONVENTION PhysAddress\n  SYNTAX\tOCTET STRING\n  " +
			"DISPLAY-HINT\t\"1x:\"\n  MAX-ACCESS\tread-only\n  STATUS\tcurrent\n  DESCRIPTION\t\"The interface's address at its protocol sub-layer.  " +
			"For\n            example, for an 802.x interface, this object normally\n            contains a MAC address.  " +
			"The interface's media-specific MIB\n            must define the bit and byte ordering and the format of the\n            " +
			"value of this object.  For interfaces which do not have such\n            an address (e.g., a serial line), " +
			"this object should contain\n            an octet string of zero length.\"\n::= " +
			"{ iso(1) org(3) dod(6) internet(1) mgmt(2) mib-2(1) interfaces(2) ifTable(2) ifEntry(1) ifPhysAddress(6) 1 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00BRIDGE-MIB::dot1dTpFdbAddress.1": {
		stdout: "BRIDGE-MIB::dot1dTpFdbAddress.1\ndot1dTpFdbAddress OBJECT-TYPE\n  -- FROM\tBRIDGE-MIB\n  -- TEXTUAL CONVENTION MacAddress\n  " +
			"SYNTAX\tOCTET STRING (6) \n  DISPLAY-HINT\t\"1x:\"\n  MAX-ACCESS\tread-only\n  STATUS\tcurrent\n  DESCRIPTION\t\"" +
			"A unicast MAC address for which the bridge has\n        forwarding and/or filtering information.\"\n::= " +
			"{ iso(1) org(3) dod(6) internet(1) mgmt(2) mib-2(1) dot1dBridge(17) dot1dTp(4) dot1dTpFdbTable(3) dot1dTpFdbEntry(1) dot1dTpFdbAddress(1) 1 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00-Ob\x00TCP-MIB::tcpConnectionLocalAddress.1": {
		stdout: "TCP-MIB::tcpConnectionLocalAddress.1\ntcpConnectionLocalAddress OBJECT-TYPE\n  -- FROM\tTCP-MIB\n  -- " +
			"TEXTUAL CONVENTION InetAddress\n  SYNTAX\tOCTET STRING (0..255) \n  MAX-ACCESS\tnot-accessible\n  " +
			"STATUS\tcurrent\n  DESCRIPTION\t\"The local IP address for this TCP connection.  " +
			"The type\n            of this address is determined by the value of\n            tcpConnectionLocalAddressType.\n\n            " +
			"As this object is used in the index for the\n            tcpConnectionTable, implementors should be\n            " +
			"careful not to create entries that would result in OIDs\n            with more than 128 subidentifiers; " +
			"otherwise the information\n            cannot be accessed by using SNMPv1, SNMPv2c, or SNMPv3.\"\n" +
			"::= { iso(1) org(3) dod(6) internet(1) mgmt(2) mib-2(1) tcp(6) tcpConnectionTable(19) tcpConnectionEntry(1) tcpConnectionLocalAddress(2) 1 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptranslate\x00-Td\x00TEST::testTable.1": {
		stdout: "TEST::testTableEntry\ntestTableEntry OBJECT-TYPE\n  -- FROM\tTEST\n  MAX-ACCESS\tnot-accessible\n  " +
			"STATUS\tcurrent\n  INDEX\t\t{ server }\n::= { iso(1) 0 testOID(0) testTable(0) 1 }\n",
		stderr:    "",
		exitError: false,
	},
	"snmptable\x00-Ch\x00-Cl\x00-c\x00public\x00127.0.0.1\x00TEST::testTable": {
		stdout:    "server connections latency description \nTEST::testTable: No entries\n",
		stderr:    "",
		exitError: false,
	},
}
