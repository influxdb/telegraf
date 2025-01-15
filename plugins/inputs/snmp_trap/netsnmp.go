package snmp_trap

import (
	"bufio"
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/influxdata/telegraf/config"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/internal/snmp"
)

type execer func(config.Duration, string, ...string) ([]byte, error)

func realExecCmd(timeout config.Duration, arg0 string, args ...string) ([]byte, error) {
	cmd := exec.Command(arg0, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := internal.RunTimeout(cmd, time.Duration(timeout))
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type netsnmpTranslator struct {
	// Each translator has its own cache and each plugin instance has
	// its own translator. This is different than the snmp plugin
	// which has one global cache.
	//
	// We may want to change snmp_trap to
	// have a global cache although it's not as important for
	// snmp_trap to be global because there is usually only one
	// instance, while it's common to configure many snmp instances.
	cacheLock sync.Mutex
	cache     map[string]snmp.MibEntry
	execCmd   execer
	timeout   config.Duration
}

func (s *netsnmpTranslator) lookup(oid string) (e snmp.MibEntry, err error) {
	s.cacheLock.Lock()
	defer s.cacheLock.Unlock()
	var ok bool
	if e, ok = s.cache[oid]; !ok {
		// cache miss.  exec snmptranslate
		e, err = s.snmptranslate(oid)
		if err == nil {
			s.cache[oid] = e
		}
		return e, err
	}
	return e, nil
}

func (s *netsnmpTranslator) snmptranslate(oid string) (e snmp.MibEntry, err error) {
	var out []byte
	out, err = s.execCmd(s.timeout, "snmptranslate", "-Td", "-Ob", "-m", "all", oid)

	if err != nil {
		return e, err
	}

	scanner := bufio.NewScanner(bytes.NewBuffer(out))
	ok := scanner.Scan()
	if err = scanner.Err(); !ok && err != nil {
		return e, err
	}

	e.OidText = scanner.Text()

	i := strings.Index(e.OidText, "::")
	if i == -1 {
		return e, errors.New("not found")
	}
	e.MibName = e.OidText[:i]
	e.OidText = e.OidText[i+2:]
	return e, nil
}

func newNetsnmpTranslator(timeout config.Duration) *netsnmpTranslator {
	return &netsnmpTranslator{
		execCmd: realExecCmd,
		cache:   make(map[string]snmp.MibEntry),
		timeout: timeout,
	}
}
