//go:build windows

package exec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/kballard/go-shellquote"

	"github.com/influxdata/telegraf/internal"
)

func (c *commandRunner) run(command string) ([]byte, []byte, error) {
	splitCmd, err := shellquote.Split(command)
	if err != nil || len(splitCmd) == 0 {
		return nil, nil, fmt.Errorf("exec: unable to parse command: %w", err)
	}

	cmd := exec.Command(splitCmd[0], splitCmd[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}

	if len(c.environment) > 0 {
		cmd.Env = append(os.Environ(), c.environment...)
	}

	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	runErr := internal.RunTimeout(cmd, c.timeout)

	out = removeWindowsCarriageReturns(out)
	stderr = removeWindowsCarriageReturns(stderr)
	if stderr.Len() > 0 && !c.debug {
		truncate(&stderr)
	}

	return out.Bytes(), stderr.Bytes(), runErr
}

func removeWindowsCarriageReturns(b bytes.Buffer) bytes.Buffer {
	var buf bytes.Buffer
	for {
		byt, err := b.ReadBytes(0x0D)
		byt = bytes.TrimRight(byt, "\x0d")
		if len(byt) > 0 {
			buf.Write(byt)
		}
		if errors.Is(err, io.EOF) {
			return buf
		}
	}
}
