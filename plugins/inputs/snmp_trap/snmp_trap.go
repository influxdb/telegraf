//go:generate ../../../tools/readme_config_includer/generator
package snmp_trap

import (
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gosnmp/gosnmp"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal/snmp"
	"github.com/influxdata/telegraf/plugins/inputs"
)

var defaultTimeout = config.Duration(time.Second * 5)

//go:embed sample.conf
var sampleConfig string

type SnmpTrap struct {
	ServiceAddress string          `toml:"service_address"`
	Timeout        config.Duration `toml:"timeout"`
	Version        string          `toml:"version"`
	Path           []string        `toml:"path"`
	// Settings for version 3
	// Values: "noAuthNoPriv", "authNoPriv", "authPriv"
	SecLevel string `toml:"sec_level"`

	SecName config.Secret `toml:"sec_name"`
	// Values: "MD5", "SHA", "". Default: ""
	AuthProtocol string        `toml:"auth_protocol"`
	AuthPassword config.Secret `toml:"auth_password"`
	// Values: "DES", "AES", "". Default: ""
	PrivProtocol string        `toml:"priv_protocol"`
	PrivPassword config.Secret `toml:"priv_password"`

	Translator string          `toml:"-"`
	Log        telegraf.Logger `toml:"-"`

	acc      telegraf.Accumulator
	listener *gosnmp.TrapListener
	timeFunc func() time.Time
	errCh    chan error

	makeHandlerWrapper func(gosnmp.TrapHandlerFunc) gosnmp.TrapHandlerFunc
	transl             translator
}

type wrapLog struct {
	telegraf.Logger
}

type translator interface {
	lookup(oid string) (snmp.MibEntry, error)
}

func (*SnmpTrap) SampleConfig() string {
	return sampleConfig
}

func (s *SnmpTrap) SetTranslator(name string) {
	s.Translator = name
}

func (s *SnmpTrap) Init() error {
	var err error
	switch s.Translator {
	case "gosmi":
		s.transl, err = newGosmiTranslator(s.Path, s.Log)
		if err != nil {
			return err
		}
	case "netsnmp":
		s.transl = newNetsnmpTranslator(s.Timeout)
	default:
		return errors.New("invalid translator value")
	}

	if err != nil {
		s.Log.Errorf("Could not get path %v", err)
	}
	return nil
}

func (s *SnmpTrap) Start(acc telegraf.Accumulator) error {
	s.acc = acc
	s.listener = gosnmp.NewTrapListener()
	s.listener.OnNewTrap = makeTrapHandler(s)

	// gosnmp.Default is a pointer, using this more than once
	// has side effects
	defaults := *gosnmp.Default
	s.listener.Params = &defaults
	s.listener.Params.Logger = gosnmp.NewLogger(wrapLog{s.Log})

	switch s.Version {
	case "3":
		s.listener.Params.Version = gosnmp.Version3
	case "2c":
		s.listener.Params.Version = gosnmp.Version2c
	case "1":
		s.listener.Params.Version = gosnmp.Version1
	default:
		s.listener.Params.Version = gosnmp.Version2c
	}

	if s.listener.Params.Version == gosnmp.Version3 {
		s.listener.Params.SecurityModel = gosnmp.UserSecurityModel

		switch strings.ToLower(s.SecLevel) {
		case "noauthnopriv", "":
			s.listener.Params.MsgFlags = gosnmp.NoAuthNoPriv
		case "authnopriv":
			s.listener.Params.MsgFlags = gosnmp.AuthNoPriv
		case "authpriv":
			s.listener.Params.MsgFlags = gosnmp.AuthPriv
		default:
			return fmt.Errorf("unknown security level %q", s.SecLevel)
		}

		var authenticationProtocol gosnmp.SnmpV3AuthProtocol
		switch strings.ToLower(s.AuthProtocol) {
		case "md5":
			authenticationProtocol = gosnmp.MD5
		case "sha":
			authenticationProtocol = gosnmp.SHA
		case "sha224":
			authenticationProtocol = gosnmp.SHA224
		case "sha256":
			authenticationProtocol = gosnmp.SHA256
		case "sha384":
			authenticationProtocol = gosnmp.SHA384
		case "sha512":
			authenticationProtocol = gosnmp.SHA512
		case "":
			authenticationProtocol = gosnmp.NoAuth
		default:
			return fmt.Errorf("unknown authentication protocol %q", s.AuthProtocol)
		}

		var privacyProtocol gosnmp.SnmpV3PrivProtocol
		switch strings.ToLower(s.PrivProtocol) {
		case "aes":
			privacyProtocol = gosnmp.AES
		case "des":
			privacyProtocol = gosnmp.DES
		case "aes192":
			privacyProtocol = gosnmp.AES192
		case "aes192c":
			privacyProtocol = gosnmp.AES192C
		case "aes256":
			privacyProtocol = gosnmp.AES256
		case "aes256c":
			privacyProtocol = gosnmp.AES256C
		case "":
			privacyProtocol = gosnmp.NoPriv
		default:
			return fmt.Errorf("unknown privacy protocol %q", s.PrivProtocol)
		}

		secnameSecret, err := s.SecName.Get()
		if err != nil {
			return fmt.Errorf("getting secname failed: %w", err)
		}
		secname := secnameSecret.String()
		secnameSecret.Destroy()

		privPasswdSecret, err := s.PrivPassword.Get()
		if err != nil {
			return fmt.Errorf("getting secname failed: %w", err)
		}
		privPasswd := privPasswdSecret.String()
		privPasswdSecret.Destroy()

		authPasswdSecret, err := s.AuthPassword.Get()
		if err != nil {
			return fmt.Errorf("getting secname failed: %w", err)
		}
		authPasswd := authPasswdSecret.String()
		authPasswdSecret.Destroy()

		s.listener.Params.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 secname,
			PrivacyProtocol:          privacyProtocol,
			PrivacyPassphrase:        privPasswd,
			AuthenticationPassphrase: authPasswd,
			AuthenticationProtocol:   authenticationProtocol,
		}
	}

	// wrap the handler, used in unit tests
	if nil != s.makeHandlerWrapper {
		s.listener.OnNewTrap = s.makeHandlerWrapper(s.listener.OnNewTrap)
	}

	split := strings.SplitN(s.ServiceAddress, "://", 2)
	if len(split) != 2 {
		return fmt.Errorf("invalid service address: %s", s.ServiceAddress)
	}

	protocol := split[0]
	addr := split[1]

	// gosnmp.TrapListener currently supports udp only.  For forward
	// compatibility, require udp in the service address
	if protocol != "udp" {
		return fmt.Errorf("unknown protocol %q in %q", protocol, s.ServiceAddress)
	}

	// If (*TrapListener).Listen immediately returns an error we need
	// to return it from this function.  Use a channel to get it here
	// from the goroutine.  Buffer one in case Listen returns after
	// Listening but before our Close is called.
	s.errCh = make(chan error, 1)
	go func() {
		s.errCh <- s.listener.Listen(addr)
	}()

	select {
	case <-s.listener.Listening():
		s.Log.Infof("Listening on %s", s.ServiceAddress)
	case err := <-s.errCh:
		return err
	}

	return nil
}

func (*SnmpTrap) Gather(telegraf.Accumulator) error {
	return nil
}

func (s *SnmpTrap) Stop() {
	s.listener.Close()
	err := <-s.errCh
	if nil != err {
		s.Log.Errorf("Error stopping trap listener %v", err)
	}
}

func setTrapOid(tags map[string]string, oid string, e snmp.MibEntry) {
	tags["oid"] = oid
	tags["name"] = e.OidText
	tags["mib"] = e.MibName
}

func makeTrapHandler(s *SnmpTrap) gosnmp.TrapHandlerFunc {
	return func(packet *gosnmp.SnmpPacket, addr *net.UDPAddr) {
		tm := s.timeFunc()
		fields := make(map[string]interface{}, len(packet.Variables)+1)
		tags := map[string]string{
			"version": packet.Version.String(),
			"source":  addr.IP.String(),
		}

		if packet.Version == gosnmp.Version1 {
			// Follow the procedure described in RFC 2576 3.1 to
			// translate a v1 trap to v2.
			var trapOid string

			if packet.GenericTrap >= 0 && packet.GenericTrap < 6 {
				trapOid = ".1.3.6.1.6.3.1.1.5." + strconv.Itoa(packet.GenericTrap+1)
			} else if packet.GenericTrap == 6 {
				trapOid = packet.Enterprise + ".0." + strconv.Itoa(packet.SpecificTrap)
			}

			if trapOid != "" {
				e, err := s.transl.lookup(trapOid)
				if err != nil {
					s.Log.Errorf("Error resolving V1 OID, oid=%s, source=%s: %v", trapOid, tags["source"], err)
					return
				}
				setTrapOid(tags, trapOid, e)
			}

			if packet.AgentAddress != "" {
				tags["agent_address"] = packet.AgentAddress
			}

			fields["sysUpTimeInstance"] = packet.Timestamp
		}

		for _, v := range packet.Variables {
			// Use system mibs to resolve oids.  Don't fall back to
			// numeric oid because it's not useful enough to the end
			// user and can be difficult to translate or remove from
			// the database later.

			var value interface{}

			// todo: format the pdu value based on its snmp type and
			// the mib's textual convention.  The snmp input plugin
			// only handles textual convention for ip and mac
			// addresses

			switch v.Type {
			case gosnmp.ObjectIdentifier:
				val, ok := v.Value.(string)
				if !ok {
					s.Log.Errorf("Error getting value OID")
					return
				}

				var e snmp.MibEntry
				var err error
				e, err = s.transl.lookup(val)
				if nil != err {
					s.Log.Errorf("Error resolving value OID, oid=%s, source=%s: %v", val, tags["source"], err)
					return
				}

				value = e.OidText

				// 1.3.6.1.6.3.1.1.4.1.0 is SNMPv2-MIB::snmpTrapOID.0.
				// If v.Name is this oid, set a tag of the trap name.
				if v.Name == ".1.3.6.1.6.3.1.1.4.1.0" {
					setTrapOid(tags, val, e)
					continue
				}
			case gosnmp.OctetString:
				// OctetStrings may contain hex data that needs its own conversion
				if !utf8.Valid(v.Value.([]byte)[:]) {
					value = hex.EncodeToString(v.Value.([]byte))
				} else {
					value = v.Value
				}
			default:
				value = v.Value
			}

			e, err := s.transl.lookup(v.Name)
			if nil != err {
				s.Log.Errorf("Error resolving OID oid=%s, source=%s: %v", v.Name, tags["source"], err)
				return
			}

			name := e.OidText

			fields[name] = value
		}

		if packet.Version == gosnmp.Version3 {
			if packet.ContextName != "" {
				tags["context_name"] = packet.ContextName
			}
			if packet.ContextEngineID != "" {
				// SNMP RFCs like 3411 and 5343 show engine ID as a hex string
				tags["engine_id"] = fmt.Sprintf("%x", packet.ContextEngineID)
			}
		} else {
			if packet.Community != "" {
				tags["community"] = packet.Community
			}
		}

		s.acc.AddFields("snmp_trap", fields, tags, tm)
	}
}

func (l wrapLog) Printf(format string, args ...interface{}) {
	l.Debugf(format, args...)
}

func (l wrapLog) Print(args ...interface{}) {
	l.Debug(args...)
}

func init() {
	inputs.Add("snmp_trap", func() telegraf.Input {
		return &SnmpTrap{
			timeFunc:       time.Now,
			ServiceAddress: "udp://:162",
			Timeout:        defaultTimeout,
			Path:           []string{"/usr/share/snmp/mibs"},
			Version:        "2c",
		}
	})
}
