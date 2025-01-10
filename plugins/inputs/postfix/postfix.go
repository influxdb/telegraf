//go:generate ../../../tools/readme_config_includer/generator
//go:build !windows

// postfix doesn't aim for Windows

package postfix

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

type Postfix struct {
	QueueDirectory string `toml:"queue_directory"`
}

func (*Postfix) SampleConfig() string {
	return sampleConfig
}

func (p *Postfix) Gather(acc telegraf.Accumulator) error {
	if p.QueueDirectory == "" {
		var err error
		p.QueueDirectory, err = getQueueDirectory()
		if err != nil {
			return fmt.Errorf("unable to determine queue directory: %w", err)
		}
	}

	for _, q := range []string{"active", "hold", "incoming", "maildrop", "deferred"} {
		fields, err := qScan(filepath.Join(p.QueueDirectory, q), acc)
		if err != nil {
			acc.AddError(fmt.Errorf("error scanning queue %q: %w", q, err))
			continue
		}

		acc.AddFields("postfix_queue", fields, map[string]string{"queue": q})
	}

	return nil
}

func getQueueDirectory() (string, error) {
	qd, err := exec.Command("postconf", "-h", "queue_directory").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(qd)), nil
}

func qScan(path string, acc telegraf.Accumulator) (map[string]interface{}, error) {
	var length, size int64
	var oldest time.Time

	err := filepath.Walk(path, func(_ string, finfo os.FileInfo, err error) error {
		if err != nil {
			acc.AddError(fmt.Errorf("error scanning %q: %w", path, err))
			return nil
		}
		if finfo.IsDir() {
			return nil
		}

		length++
		size += finfo.Size()

		ctime := statCTime(finfo.Sys())
		if ctime.IsZero() {
			return nil
		}
		if oldest.IsZero() || ctime.Before(oldest) {
			oldest = ctime
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	var age int64
	if !oldest.IsZero() {
		age = int64(time.Since(oldest) / time.Second)
	} else if length != 0 {
		// system doesn't support ctime
		age = -1
	}

	fields := map[string]interface{}{"length": length, "size": size}
	if age != -1 {
		fields["age"] = age
	}

	return fields, nil
}

func init() {
	inputs.Add("postfix", func() telegraf.Input {
		return &Postfix{
			QueueDirectory: "/var/spool/postfix",
		}
	})
}
