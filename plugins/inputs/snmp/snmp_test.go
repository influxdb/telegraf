package snmp

import (
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/snmp"
	"github.com/influxdata/telegraf/testutil"
)

type testSNMPConnection struct {
	host   string
	values map[string]interface{}
}

func (tsc *testSNMPConnection) Host() string {
	return tsc.host
}

func (tsc *testSNMPConnection) Get(oids []string) (*gosnmp.SnmpPacket, error) {
	sp := &gosnmp.SnmpPacket{}
	for _, oid := range oids {
		v, ok := tsc.values[oid]
		if !ok {
			sp.Variables = append(sp.Variables, gosnmp.SnmpPDU{
				Name: oid,
				Type: gosnmp.NoSuchObject,
			})
			continue
		}
		sp.Variables = append(sp.Variables, gosnmp.SnmpPDU{
			Name:  oid,
			Value: v,
		})
	}
	return sp, nil
}

func (tsc *testSNMPConnection) Walk(oid string, wf gosnmp.WalkFunc) error {
	for void, v := range tsc.values {
		if void == oid || (len(void) > len(oid) && void[:len(oid)+1] == oid+".") {
			if err := wf(gosnmp.SnmpPDU{
				Name:  void,
				Value: v,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (*testSNMPConnection) Reconnect() error {
	return nil
}

var tsc = &testSNMPConnection{
	host: "tsc",
	values: map[string]interface{}{
		".1.0.0.0.1.1.0":         "foo",
		".1.0.0.0.1.1.1":         []byte("bar"),
		".1.0.0.0.1.1.2":         []byte(""),
		".1.0.0.0.1.102":         "bad",
		".1.0.0.0.1.2.0":         1,
		".1.0.0.0.1.2.1":         2,
		".1.0.0.0.1.2.2":         0,
		".1.0.0.0.1.3.0":         "0.123",
		".1.0.0.0.1.3.1":         "0.456",
		".1.0.0.0.1.3.2":         "0.000",
		".1.0.0.0.1.3.3":         "9.999",
		".1.0.0.0.1.5.0":         123456,
		".1.0.0.0.1.6.0":         ".1.0.0.0.1.7",
		".1.0.0.1.1":             "baz",
		".1.0.0.1.2":             234,
		".1.0.0.1.3":             []byte("byte slice"),
		".1.0.0.2.1.5.0.9.9":     11,
		".1.0.0.2.1.5.1.9.9":     22,
		".1.0.0.3.1.1.10":        "instance",
		".1.0.0.3.1.1.11":        "instance2",
		".1.0.0.3.1.1.12":        "instance3",
		".1.0.0.3.1.2.10":        10,
		".1.0.0.3.1.2.11":        20,
		".1.0.0.3.1.2.12":        20,
		".1.0.0.3.1.3.10":        1,
		".1.0.0.3.1.3.11":        2,
		".1.0.0.3.1.3.12":        3,
		".1.3.6.1.2.1.3.1.1.1.0": "foo",
		".1.3.6.1.2.1.3.1.1.1.1": []byte("bar"),
		".1.3.6.1.2.1.3.1.1.1.2": []byte(""),
		".1.3.6.1.2.1.3.1.1.102": "bad",
		".1.3.6.1.2.1.3.1.1.2.0": 1,
		".1.3.6.1.2.1.3.1.1.2.1": 2,
		".1.3.6.1.2.1.3.1.1.2.2": 0,
		".1.3.6.1.2.1.3.1.1.3.0": "1.3.6.1.2.1.3.1.1.3",
		".1.3.6.1.2.1.3.1.1.5.0": 123456,
	},
}

func TestSnmpInit(t *testing.T) {
	s := &Snmp{
		ClientConfig: snmp.ClientConfig{
			Translator: "netsnmp",
		},
	}

	require.NoError(t, s.Init())
}

func TestSnmpInit_noTranslate(t *testing.T) {
	s := &Snmp{
		Fields: []snmp.Field{
			{Oid: ".1.1.1.1", Name: "one", IsTag: true},
			{Oid: ".1.1.1.2", Name: "two"},
			{Oid: ".1.1.1.3"},
		},
		Tables: []snmp.Table{
			{Name: "testing",
				Fields: []snmp.Field{
					{Oid: ".1.1.1.4", Name: "four", IsTag: true},
					{Oid: ".1.1.1.5", Name: "five"},
					{Oid: ".1.1.1.6"},
				}},
		},
		ClientConfig: snmp.ClientConfig{
			Translator: "netsnmp",
		},
		Log: testutil.Logger{Name: "inputs.snmp"},
	}

	err := s.Init()
	require.NoError(t, err)

	require.Equal(t, ".1.1.1.1", s.Fields[0].Oid)
	require.Equal(t, "one", s.Fields[0].Name)
	require.True(t, s.Fields[0].IsTag)

	require.Equal(t, ".1.1.1.2", s.Fields[1].Oid)
	require.Equal(t, "two", s.Fields[1].Name)
	require.False(t, s.Fields[1].IsTag)

	require.Equal(t, ".1.1.1.3", s.Fields[2].Oid)
	require.Equal(t, ".1.1.1.3", s.Fields[2].Name)
	require.False(t, s.Fields[2].IsTag)

	require.Equal(t, ".1.1.1.4", s.Tables[0].Fields[0].Oid)
	require.Equal(t, "four", s.Tables[0].Fields[0].Name)
	require.True(t, s.Tables[0].Fields[0].IsTag)

	require.Equal(t, ".1.1.1.5", s.Tables[0].Fields[1].Oid)
	require.Equal(t, "five", s.Tables[0].Fields[1].Name)
	require.False(t, s.Tables[0].Fields[1].IsTag)

	require.Equal(t, ".1.1.1.6", s.Tables[0].Fields[2].Oid)
	require.Equal(t, ".1.1.1.6", s.Tables[0].Fields[2].Name)
	require.False(t, s.Tables[0].Fields[2].IsTag)
}

func TestSnmpInit_noName_noOid(t *testing.T) {
	s := &Snmp{
		Tables: []snmp.Table{
			{Fields: []snmp.Field{
				{Oid: ".1.1.1.4", Name: "four", IsTag: true},
				{Oid: ".1.1.1.5", Name: "five"},
				{Oid: ".1.1.1.6"},
			}},
		},
	}

	require.Error(t, s.Init())
}

func TestGetSNMPConnection_v2(t *testing.T) {
	s := &Snmp{
		Agents: []string{"1.2.3.4:567", "1.2.3.4", "udp://127.0.0.1"},
		ClientConfig: snmp.ClientConfig{
			Timeout:    config.Duration(3 * time.Second),
			Retries:    4,
			Version:    2,
			Community:  "foo",
			Translator: "netsnmp",
		},
	}
	require.NoError(t, s.Init())

	gsc, err := s.getConnection(0)
	require.NoError(t, err)
	gs := gsc.(snmp.GosnmpWrapper)
	require.Equal(t, "1.2.3.4", gs.Target)
	require.EqualValues(t, 567, gs.Port)
	require.Equal(t, gosnmp.Version2c, gs.Version)
	require.Equal(t, "foo", gs.Community)
	require.Equal(t, "udp", gs.Transport)

	gsc, err = s.getConnection(1)
	require.NoError(t, err)
	gs = gsc.(snmp.GosnmpWrapper)
	require.Equal(t, "1.2.3.4", gs.Target)
	require.EqualValues(t, 161, gs.Port)
	require.Equal(t, "udp", gs.Transport)

	gsc, err = s.getConnection(2)
	require.NoError(t, err)
	gs = gsc.(snmp.GosnmpWrapper)
	require.Equal(t, "127.0.0.1", gs.Target)
	require.EqualValues(t, 161, gs.Port)
	require.Equal(t, "udp", gs.Transport)
}

func TestGetSNMPConnectionTCP(t *testing.T) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	tcpServer, err := net.ListenTCP("tcp", tcpAddr)
	require.NoError(t, err)
	defer tcpServer.Close()

	s := &Snmp{
		Agents: []string{fmt.Sprintf("tcp://%s", tcpServer.Addr())},
		ClientConfig: snmp.ClientConfig{
			Translator: "netsnmp",
		},
	}
	require.NoError(t, s.Init())

	gsc, err := s.getConnection(0)
	require.NoError(t, err)
	gs := gsc.(snmp.GosnmpWrapper)
	require.Equal(t, "127.0.0.1", gs.Target)
	require.Equal(t, "tcp", gs.Transport)
}

func TestGetSNMPConnection_v3(t *testing.T) {
	s := &Snmp{
		Agents: []string{"1.2.3.4"},
		ClientConfig: snmp.ClientConfig{
			Version:        3,
			MaxRepetitions: 20,
			ContextName:    "mycontext",
			SecLevel:       "authPriv",
			SecName:        "myuser",
			AuthProtocol:   "md5",
			AuthPassword:   config.NewSecret([]byte("password123")),
			PrivProtocol:   "des",
			PrivPassword:   config.NewSecret([]byte("321drowssap")),
			EngineID:       "myengineid",
			EngineBoots:    1,
			EngineTime:     2,
			Translator:     "netsnmp",
		},
	}
	err := s.Init()
	require.NoError(t, err)

	gsc, err := s.getConnection(0)
	require.NoError(t, err)
	gs := gsc.(snmp.GosnmpWrapper)
	require.Equal(t, gosnmp.Version3, gs.Version)
	sp := gs.SecurityParameters.(*gosnmp.UsmSecurityParameters)
	require.Equal(t, "1.2.3.4", gsc.Host())
	require.EqualValues(t, 20, gs.MaxRepetitions)
	require.Equal(t, "mycontext", gs.ContextName)
	require.Equal(t, gosnmp.AuthPriv, gs.MsgFlags&gosnmp.AuthPriv)
	require.Equal(t, "myuser", sp.UserName)
	require.Equal(t, gosnmp.MD5, sp.AuthenticationProtocol)
	require.Equal(t, "password123", sp.AuthenticationPassphrase)
	require.Equal(t, gosnmp.DES, sp.PrivacyProtocol)
	require.Equal(t, "321drowssap", sp.PrivacyPassphrase)
	require.Equal(t, "myengineid", sp.AuthoritativeEngineID)
	require.EqualValues(t, 1, sp.AuthoritativeEngineBoots)
	require.EqualValues(t, 2, sp.AuthoritativeEngineTime)
}

func TestGetSNMPConnection_v3_blumenthal(t *testing.T) {
	testCases := []struct {
		Name      string
		Algorithm gosnmp.SnmpV3PrivProtocol
		Config    *Snmp
	}{
		{
			Name:      "AES192",
			Algorithm: gosnmp.AES192,
			Config: &Snmp{
				Agents: []string{"1.2.3.4"},
				ClientConfig: snmp.ClientConfig{
					Version:        3,
					MaxRepetitions: 20,
					ContextName:    "mycontext",
					SecLevel:       "authPriv",
					SecName:        "myuser",
					AuthProtocol:   "md5",
					AuthPassword:   config.NewSecret([]byte("password123")),
					PrivProtocol:   "AES192",
					PrivPassword:   config.NewSecret([]byte("password123")),
					EngineID:       "myengineid",
					EngineBoots:    1,
					EngineTime:     2,
					Translator:     "netsnmp",
				},
			},
		},
		{
			Name:      "AES192C",
			Algorithm: gosnmp.AES192C,
			Config: &Snmp{
				Agents: []string{"1.2.3.4"},
				ClientConfig: snmp.ClientConfig{
					Version:        3,
					MaxRepetitions: 20,
					ContextName:    "mycontext",
					SecLevel:       "authPriv",
					SecName:        "myuser",
					AuthProtocol:   "md5",
					AuthPassword:   config.NewSecret([]byte("password123")),
					PrivProtocol:   "AES192C",
					PrivPassword:   config.NewSecret([]byte("password123")),
					EngineID:       "myengineid",
					EngineBoots:    1,
					EngineTime:     2,
					Translator:     "netsnmp",
				},
			},
		},
		{
			Name:      "AES256",
			Algorithm: gosnmp.AES256,
			Config: &Snmp{
				Agents: []string{"1.2.3.4"},
				ClientConfig: snmp.ClientConfig{
					Version:        3,
					MaxRepetitions: 20,
					ContextName:    "mycontext",
					SecLevel:       "authPriv",
					SecName:        "myuser",
					AuthProtocol:   "md5",
					AuthPassword:   config.NewSecret([]byte("password123")),
					PrivProtocol:   "AES256",
					PrivPassword:   config.NewSecret([]byte("password123")),
					EngineID:       "myengineid",
					EngineBoots:    1,
					EngineTime:     2,
					Translator:     "netsnmp",
				},
			},
		},
		{
			Name:      "AES256C",
			Algorithm: gosnmp.AES256C,
			Config: &Snmp{
				Agents: []string{"1.2.3.4"},
				ClientConfig: snmp.ClientConfig{
					Version:        3,
					MaxRepetitions: 20,
					ContextName:    "mycontext",
					SecLevel:       "authPriv",
					SecName:        "myuser",
					AuthProtocol:   "md5",
					AuthPassword:   config.NewSecret([]byte("password123")),
					PrivProtocol:   "AES256C",
					PrivPassword:   config.NewSecret([]byte("password123")),
					EngineID:       "myengineid",
					EngineBoots:    1,
					EngineTime:     2,
					Translator:     "netsnmp",
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			s := tc.Config
			err := s.Init()
			require.NoError(t, err)

			gsc, err := s.getConnection(0)
			require.NoError(t, err)
			gs := gsc.(snmp.GosnmpWrapper)
			require.Equal(t, gosnmp.Version3, gs.Version)
			sp := gs.SecurityParameters.(*gosnmp.UsmSecurityParameters)
			require.Equal(t, "1.2.3.4", gsc.Host())
			require.EqualValues(t, 20, gs.MaxRepetitions)
			require.Equal(t, "mycontext", gs.ContextName)
			require.Equal(t, gosnmp.AuthPriv, gs.MsgFlags&gosnmp.AuthPriv)
			require.Equal(t, "myuser", sp.UserName)
			require.Equal(t, gosnmp.MD5, sp.AuthenticationProtocol)
			require.Equal(t, "password123", sp.AuthenticationPassphrase)
			require.Equal(t, tc.Algorithm, sp.PrivacyProtocol)
			require.Equal(t, "password123", sp.PrivacyPassphrase)
			require.Equal(t, "myengineid", sp.AuthoritativeEngineID)
			require.EqualValues(t, 1, sp.AuthoritativeEngineBoots)
			require.EqualValues(t, 2, sp.AuthoritativeEngineTime)
		})
	}
}

func TestGetSNMPConnection_caching(t *testing.T) {
	s := &Snmp{
		Agents: []string{"1.2.3.4", "1.2.3.5", "1.2.3.5"},
		ClientConfig: snmp.ClientConfig{
			Translator: "netsnmp",
		},
	}
	err := s.Init()
	require.NoError(t, err)
	gs1, err := s.getConnection(0)
	require.NoError(t, err)
	gs2, err := s.getConnection(0)
	require.NoError(t, err)
	gs3, err := s.getConnection(1)
	require.NoError(t, err)
	gs4, err := s.getConnection(2)
	require.NoError(t, err)
	require.Equal(t, gs1, gs2)
	require.NotEqual(t, gs2, gs3)
	require.NotEqual(t, gs3, gs4)
}

func TestGosnmpWrapper_walk_retry(t *testing.T) {
	t.Skip("Skipping test due to random failures.")

	srvr, err := net.ListenUDP("udp4", &net.UDPAddr{})
	require.NoError(t, err)
	defer srvr.Close()
	reqCount := 0
	// Set up a WaitGroup to wait for the server goroutine to exit and protect
	// reqCount.
	// Even though simultaneous access is impossible because the server will be
	// blocked on ReadFrom, without this the race detector gets unhappy.
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 256)
		for {
			_, addr, err := srvr.ReadFrom(buf)
			if err != nil {
				return
			}
			reqCount++

			// will cause decoding error
			if _, err := srvr.WriteTo([]byte{'X'}, addr); err != nil {
				return
			}
		}
	}()

	gs := &gosnmp.GoSNMP{
		Target:    srvr.LocalAddr().(*net.UDPAddr).IP.String(),
		Port:      uint16(srvr.LocalAddr().(*net.UDPAddr).Port),
		Version:   gosnmp.Version2c,
		Community: "public",
		Timeout:   time.Millisecond * 10,
		Retries:   1,
	}
	err = gs.Connect()
	require.NoError(t, err)
	conn := gs.Conn

	gsw := snmp.GosnmpWrapper{
		GoSNMP: gs,
	}
	err = gsw.Walk(".1.0.0", func(gosnmp.SnmpPDU) error { return nil })
	require.NoError(t, srvr.Close())
	wg.Wait()
	require.Error(t, err)
	require.NotEqual(t, gs.Conn, conn)
	require.Equal(t, (gs.Retries+1)*2, reqCount)
}

func TestGosnmpWrapper_get_retry(t *testing.T) {
	// TODO: Fix this test
	t.Skip("Test failing too often, skip for now and revisit later.")
	srvr, err := net.ListenUDP("udp4", &net.UDPAddr{})
	require.NoError(t, err)
	defer srvr.Close()
	reqCount := 0
	// Set up a WaitGroup to wait for the server goroutine to exit and protect
	// reqCount.
	// Even though simultaneous access is impossible because the server will be
	// blocked on ReadFrom, without this the race detector gets unhappy.
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 256)
		for {
			_, addr, err := srvr.ReadFrom(buf)
			if err != nil {
				return
			}
			reqCount++

			// will cause decoding error
			if _, err := srvr.WriteTo([]byte{'X'}, addr); err != nil {
				return
			}
		}
	}()

	gs := &gosnmp.GoSNMP{
		Target:    srvr.LocalAddr().(*net.UDPAddr).IP.String(),
		Port:      uint16(srvr.LocalAddr().(*net.UDPAddr).Port),
		Version:   gosnmp.Version2c,
		Community: "public",
		Timeout:   time.Millisecond * 10,
		Retries:   1,
	}
	err = gs.Connect()
	require.NoError(t, err)
	conn := gs.Conn

	gsw := snmp.GosnmpWrapper{
		GoSNMP: gs,
	}
	_, err = gsw.Get([]string{".1.0.0"})
	require.NoError(t, srvr.Close())
	wg.Wait()
	require.Error(t, err)
	require.NotEqual(t, gs.Conn, conn)
	require.Equal(t, (gs.Retries+1)*2, reqCount)
}

func TestGather(t *testing.T) {
	s := &Snmp{
		Agents: []string{"TestGather"},
		Name:   "mytable",
		Fields: []snmp.Field{
			{
				Name:  "myfield1",
				Oid:   ".1.0.0.1.1",
				IsTag: true,
			},
			{
				Name: "myfield2",
				Oid:  ".1.0.0.1.2",
			},
			{
				Name: "myfield3",
				Oid:  "1.0.0.1.1",
			},
		},
		Tables: []snmp.Table{
			{
				Name:        "myOtherTable",
				InheritTags: []string{"myfield1"},
				Fields: []snmp.Field{
					{
						Name: "myOtherField",
						Oid:  ".1.0.0.0.1.5",
					},
				},
			},
		},

		connectionCache: []snmp.Connection{
			tsc,
		},
	}
	acc := &testutil.Accumulator{}

	tstart := time.Now()
	require.NoError(t, s.Gather(acc))
	tstop := time.Now()

	require.Len(t, acc.Metrics, 2)

	m := acc.Metrics[0]
	require.Equal(t, "mytable", m.Measurement)
	require.Equal(t, "tsc", m.Tags[s.AgentHostTag])
	require.Equal(t, "baz", m.Tags["myfield1"])
	require.Len(t, m.Fields, 2)
	require.Equal(t, 234, m.Fields["myfield2"])
	require.Equal(t, "baz", m.Fields["myfield3"])
	require.WithinRange(t, m.Time, tstart, tstop)

	m2 := acc.Metrics[1]
	require.Equal(t, "myOtherTable", m2.Measurement)
	require.Equal(t, "tsc", m2.Tags[s.AgentHostTag])
	require.Equal(t, "baz", m2.Tags["myfield1"])
	require.Len(t, m2.Fields, 1)
	require.Equal(t, 123456, m2.Fields["myOtherField"])
}

func TestGather_host(t *testing.T) {
	s := &Snmp{
		Agents: []string{"TestGather"},
		Name:   "mytable",
		Fields: []snmp.Field{
			{
				Name:  "host",
				Oid:   ".1.0.0.1.1",
				IsTag: true,
			},
			{
				Name: "myfield2",
				Oid:  ".1.0.0.1.2",
			},
		},

		connectionCache: []snmp.Connection{
			tsc,
		},
	}

	acc := &testutil.Accumulator{}

	require.NoError(t, s.Gather(acc))

	require.Len(t, acc.Metrics, 1)
	m := acc.Metrics[0]
	require.Equal(t, "baz", m.Tags["host"])
}

func TestSnmpInitGosmi(t *testing.T) {
	testDataPath, err := filepath.Abs("../../../internal/snmp/testdata/gosmi")
	require.NoError(t, err)

	s := &Snmp{
		Tables: []snmp.Table{
			{Oid: "RFC1213-MIB::atTable"},
		},
		Fields: []snmp.Field{
			{Oid: "RFC1213-MIB::atPhysAddress"},
		},
		ClientConfig: snmp.ClientConfig{
			Path:       []string{testDataPath},
			Translator: "gosmi",
		},
	}

	require.NoError(t, s.Init())

	require.Len(t, s.Tables[0].Fields, 3)

	require.Equal(t, ".1.3.6.1.2.1.3.1.1.1", s.Tables[0].Fields[0].Oid)
	require.Equal(t, "atIfIndex", s.Tables[0].Fields[0].Name)
	require.True(t, s.Tables[0].Fields[0].IsTag)
	require.Empty(t, s.Tables[0].Fields[0].Conversion)

	require.Equal(t, ".1.3.6.1.2.1.3.1.1.2", s.Tables[0].Fields[1].Oid)
	require.Equal(t, "atPhysAddress", s.Tables[0].Fields[1].Name)
	require.False(t, s.Tables[0].Fields[1].IsTag)
	require.Equal(t, "displayhint", s.Tables[0].Fields[1].Conversion)

	require.Equal(t, ".1.3.6.1.2.1.3.1.1.3", s.Tables[0].Fields[2].Oid)
	require.Equal(t, "atNetAddress", s.Tables[0].Fields[2].Name)
	require.True(t, s.Tables[0].Fields[2].IsTag)
	require.Empty(t, s.Tables[0].Fields[2].Conversion)

	require.Equal(t, ".1.3.6.1.2.1.3.1.1.2", s.Fields[0].Oid)
	require.Equal(t, "atPhysAddress", s.Fields[0].Name)
	require.False(t, s.Fields[0].IsTag)
	require.Equal(t, "displayhint", s.Fields[0].Conversion)
}

func TestSnmpInit_noTranslateGosmi(t *testing.T) {
	s := &Snmp{
		Fields: []snmp.Field{
			{Oid: ".9.1.1.1.1", Name: "one", IsTag: true},
			{Oid: ".9.1.1.1.2", Name: "two"},
			{Oid: ".9.1.1.1.3"},
		},
		Tables: []snmp.Table{
			{Name: "testing",
				Fields: []snmp.Field{
					{Oid: ".9.1.1.1.4", Name: "four", IsTag: true},
					{Oid: ".9.1.1.1.5", Name: "five"},
					{Oid: ".9.1.1.1.6"},
				}},
		},
		ClientConfig: snmp.ClientConfig{
			Translator: "gosmi",
		},
	}

	require.NoError(t, s.Init())

	require.Equal(t, ".9.1.1.1.1", s.Fields[0].Oid)
	require.Equal(t, "one", s.Fields[0].Name)
	require.True(t, s.Fields[0].IsTag)

	require.Equal(t, ".9.1.1.1.2", s.Fields[1].Oid)
	require.Equal(t, "two", s.Fields[1].Name)
	require.False(t, s.Fields[1].IsTag)

	require.Equal(t, ".9.1.1.1.3", s.Fields[2].Oid)
	require.Equal(t, ".9.1.1.1.3", s.Fields[2].Name)
	require.False(t, s.Fields[2].IsTag)

	require.Equal(t, ".9.1.1.1.4", s.Tables[0].Fields[0].Oid)
	require.Equal(t, "four", s.Tables[0].Fields[0].Name)
	require.True(t, s.Tables[0].Fields[0].IsTag)

	require.Equal(t, ".9.1.1.1.5", s.Tables[0].Fields[1].Oid)
	require.Equal(t, "five", s.Tables[0].Fields[1].Name)
	require.False(t, s.Tables[0].Fields[1].IsTag)

	require.Equal(t, ".9.1.1.1.6", s.Tables[0].Fields[2].Oid)
	require.Equal(t, ".9.1.1.1.6", s.Tables[0].Fields[2].Name)
	require.False(t, s.Tables[0].Fields[2].IsTag)
}

func TestGatherGosmi(t *testing.T) {
	s := &Snmp{
		Agents: []string{"TestGather"},
		Name:   "mytable",
		Fields: []snmp.Field{
			{
				Name:  "myfield1",
				Oid:   ".1.0.0.1.1",
				IsTag: true,
			},
			{
				Name: "myfield2",
				Oid:  ".1.0.0.1.2",
			},
			{
				Name: "myfield3",
				Oid:  "1.0.0.1.1",
			},
		},
		Tables: []snmp.Table{
			{
				Name:        "myOtherTable",
				InheritTags: []string{"myfield1"},
				Fields: []snmp.Field{
					{
						Name: "myOtherField",
						Oid:  ".1.0.0.0.1.5",
					},
				},
			},
		},

		connectionCache: []snmp.Connection{tsc},

		ClientConfig: snmp.ClientConfig{
			Translator: "gosmi",
		},
	}
	acc := &testutil.Accumulator{}

	tstart := time.Now()
	require.NoError(t, s.Gather(acc))
	tstop := time.Now()

	require.Len(t, acc.Metrics, 2)

	m := acc.Metrics[0]
	require.Equal(t, "mytable", m.Measurement)
	require.Equal(t, "tsc", m.Tags[s.AgentHostTag])
	require.Equal(t, "baz", m.Tags["myfield1"])
	require.Len(t, m.Fields, 2)
	require.Equal(t, 234, m.Fields["myfield2"])
	require.Equal(t, "baz", m.Fields["myfield3"])
	require.WithinRange(t, m.Time, tstart, tstop)

	m2 := acc.Metrics[1]
	require.Equal(t, "myOtherTable", m2.Measurement)
	require.Equal(t, "tsc", m2.Tags[s.AgentHostTag])
	require.Equal(t, "baz", m2.Tags["myfield1"])
	require.Len(t, m2.Fields, 1)
	require.Equal(t, 123456, m2.Fields["myOtherField"])
}

func TestGather_hostGosmi(t *testing.T) {
	s := &Snmp{
		Agents: []string{"TestGather"},
		Name:   "mytable",
		Fields: []snmp.Field{
			{
				Name:  "host",
				Oid:   ".1.0.0.1.1",
				IsTag: true,
			},
			{
				Name: "myfield2",
				Oid:  ".1.0.0.1.2",
			},
		},

		connectionCache: []snmp.Connection{tsc},
	}

	acc := &testutil.Accumulator{}

	require.NoError(t, s.Gather(acc))

	require.Len(t, acc.Metrics, 1)
	m := acc.Metrics[0]
	require.Equal(t, "baz", m.Tags["host"])
}
