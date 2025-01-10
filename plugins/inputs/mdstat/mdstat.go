//go:generate ../../../tools/readme_config_includer/generator
//go:build linux

// Copyright 2018 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Code has been changed since initial import.

package mdstat

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/internal"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

var (
	statusLineRE         = regexp.MustCompile(`(\d+) blocks .*\[(\d+)/(\d+)\] \[([U_]+)\]`)
	recoveryLineBlocksRE = regexp.MustCompile(`\((\d+)/\d+\)`)
	recoveryLinePctRE    = regexp.MustCompile(`= +(.+)%`)
	recoveryLineFinishRE = regexp.MustCompile(`finish=(.+)min`)
	recoveryLineSpeedRE  = regexp.MustCompile(`speed=(.+)[A-Z]`)
	componentDeviceRE    = regexp.MustCompile(`(.*)\[\d+\]`)
)

type Mdstat struct {
	FileName string `toml:"file_name"`
}

type statusLine struct {
	active int64
	total  int64
	size   int64
	down   int64
}

type recoveryLine struct {
	syncedBlocks int64
	pct          float64
	finish       float64
	speed        float64
}

func evalStatusLine(deviceLine, statusLineStr string) (statusLine, error) {
	sizeFields := strings.Fields(statusLineStr)
	if len(sizeFields) < 1 {
		return statusLine{active: 0, total: 0, down: 0, size: 0},
			fmt.Errorf("statusLine empty? %q", statusLineStr)
	}
	sizeStr := sizeFields[0]
	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return statusLine{active: 0, total: 0, down: 0, size: 0},
			fmt.Errorf("unexpected statusLine %q: %w", statusLineStr, err)
	}

	if strings.Contains(deviceLine, "raid0") || strings.Contains(deviceLine, "linear") {
		// In the device deviceLine, only disks have a number associated with them in [].
		total := int64(strings.Count(deviceLine, "["))
		return statusLine{active: total, total: total, down: 0, size: size}, nil
	}

	if strings.Contains(deviceLine, "inactive") {
		return statusLine{active: 0, total: 0, down: 0, size: size}, nil
	}

	matches := statusLineRE.FindStringSubmatch(statusLineStr)
	if len(matches) != 5 {
		return statusLine{active: 0, total: 0, down: 0, size: size},
			fmt.Errorf("couldn't find all the substring matches: %s", statusLineStr)
	}
	total, err := strconv.ParseInt(matches[2], 10, 64)
	if err != nil {
		return statusLine{active: 0, total: 0, down: 0, size: size},
			fmt.Errorf("unexpected statusLine %q: %w", statusLineStr, err)
	}
	active, err := strconv.ParseInt(matches[3], 10, 64)
	if err != nil {
		return statusLine{active: 0, total: total, down: 0, size: size},
			fmt.Errorf("unexpected statusLine %q: %w", statusLineStr, err)
	}
	down := int64(strings.Count(matches[4], "_"))

	return statusLine{active: active, total: total, size: size, down: down}, nil
}

func evalRecoveryLine(recoveryLineStr string) (recoveryLine, error) {
	// Get count of completed vs. total blocks
	matches := recoveryLineBlocksRE.FindStringSubmatch(recoveryLineStr)
	if len(matches) != 2 {
		return recoveryLine{syncedBlocks: 0, pct: 0, finish: 0, speed: 0},
			fmt.Errorf("unexpected recoveryLine matching syncedBlocks: %s", recoveryLineStr)
	}
	syncedBlocks, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return recoveryLine{syncedBlocks: 0, pct: 0, finish: 0, speed: 0},
			fmt.Errorf("error parsing int from recoveryLine %q: %w", recoveryLineStr, err)
	}

	// Get percentage complete
	matches = recoveryLinePctRE.FindStringSubmatch(recoveryLineStr)
	if len(matches) != 2 {
		return recoveryLine{syncedBlocks: syncedBlocks, pct: 0, finish: 0, speed: 0},
			fmt.Errorf("unexpected recoveryLine matching percentage: %s", recoveryLineStr)
	}
	pct, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return recoveryLine{syncedBlocks: syncedBlocks, pct: 0, finish: 0, speed: 0},
			fmt.Errorf("error parsing float from recoveryLine %q: %w", recoveryLineStr, err)
	}

	// Get time expected left to complete
	matches = recoveryLineFinishRE.FindStringSubmatch(recoveryLineStr)
	if len(matches) != 2 {
		return recoveryLine{syncedBlocks: syncedBlocks, pct: pct, finish: 0, speed: 0},
			fmt.Errorf("unexpected recoveryLine matching est. finish time: %s", recoveryLineStr)
	}
	finish, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return recoveryLine{syncedBlocks: syncedBlocks, pct: pct, finish: 0, speed: 0},
			fmt.Errorf("error parsing float from recoveryLine %q: %w", recoveryLineStr, err)
	}

	// Get recovery speed
	matches = recoveryLineSpeedRE.FindStringSubmatch(recoveryLineStr)
	if len(matches) != 2 {
		return recoveryLine{syncedBlocks: syncedBlocks, pct: pct, finish: finish, speed: 0},
			fmt.Errorf("unexpected recoveryLine matching speed: %s", recoveryLineStr)
	}
	speed, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return recoveryLine{syncedBlocks: syncedBlocks, pct: pct, finish: finish, speed: 0},
			fmt.Errorf("error parsing float from recoveryLine %q: %w", recoveryLineStr, err)
	}
	return recoveryLine{syncedBlocks: syncedBlocks, pct: pct, finish: finish, speed: speed}, nil
}

func evalComponentDevices(deviceFields []string) string {
	mdComponentDevices := make([]string, 0)
	if len(deviceFields) > 3 {
		for _, field := range deviceFields[4:] {
			match := componentDeviceRE.FindStringSubmatch(field)
			if match == nil {
				continue
			}
			mdComponentDevices = append(mdComponentDevices, match[1])
		}
	}

	// Ensure no churn on tag ordering change
	sort.Strings(mdComponentDevices)
	return strings.Join(mdComponentDevices, ",")
}

func (*Mdstat) SampleConfig() string {
	return sampleConfig
}

func (k *Mdstat) Gather(acc telegraf.Accumulator) error {
	data, err := k.getProcMdstat()
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	// empty file should return nothing
	if len(lines) < 3 {
		return nil
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == "" || line[0] == ' ' || strings.HasPrefix(line, "Personalities") || strings.HasPrefix(line, "unused") {
			continue
		}
		deviceFields := strings.Fields(line)
		if len(deviceFields) < 3 || len(lines) <= i+3 {
			return fmt.Errorf("not enough fields in mdline (expected at least 3): %s", line)
		}
		mdName := deviceFields[0] // mdx
		state := deviceFields[2]  // active or inactive

		/*
			Failed disks have the suffix (F) & Spare disks have the suffix (S).
			Failed disks may also not be marked separately...
		*/
		fail := int64(strings.Count(line, "(F)"))
		spare := int64(strings.Count(line, "(S)"))

		sts, err := evalStatusLine(lines[i], lines[i+1])
		if err != nil {
			return fmt.Errorf("error parsing md device lines: %w", err)
		}

		syncLineIdx := i + 2
		if strings.Contains(lines[i+2], "bitmap") { // skip bitmap line
			syncLineIdx++
		}

		var rcvry recoveryLine
		// If device is syncing at the moment, get the number of currently
		// synced bytes, otherwise that number equals the size of the device.
		rcvry.syncedBlocks = sts.size
		recovering := strings.Contains(lines[syncLineIdx], "recovery")
		resyncing := strings.Contains(lines[syncLineIdx], "resync")
		checking := strings.Contains(lines[syncLineIdx], "check")

		// Append recovery and resyncing state info.
		if recovering || resyncing || checking {
			if recovering {
				state = "recovering"
			} else if checking {
				state = "checking"
			} else {
				state = "resyncing"
			}

			// Handle case when resync=PENDING or resync=DELAYED.
			if strings.Contains(lines[syncLineIdx], "PENDING") || strings.Contains(lines[syncLineIdx], "DELAYED") {
				rcvry.syncedBlocks = 0
			} else {
				var err error
				rcvry, err = evalRecoveryLine(lines[syncLineIdx])
				if err != nil {
					return fmt.Errorf("error parsing sync line in md device %q: %w", mdName, err)
				}
			}
		}
		fields := map[string]interface{}{
			"DisksActive":            sts.active,
			"DisksFailed":            fail,
			"DisksSpare":             spare,
			"DisksTotal":             sts.total,
			"DisksDown":              sts.down,
			"BlocksTotal":            sts.size,
			"BlocksSynced":           rcvry.syncedBlocks,
			"BlocksSyncedPct":        rcvry.pct,
			"BlocksSyncedFinishTime": rcvry.finish,
			"BlocksSyncedSpeed":      rcvry.speed,
		}
		tags := map[string]string{
			"Name":          mdName,
			"ActivityState": state,
			"Devices":       evalComponentDevices(deviceFields),
		}
		acc.AddFields("mdstat", fields, tags)
	}

	return nil
}

func (k *Mdstat) getProcMdstat() ([]byte, error) {
	var mdStatFile string
	if k.FileName == "" {
		mdStatFile = internal.GetProcPath() + "/mdstat"
	} else {
		mdStatFile = k.FileName
	}
	if _, err := os.Stat(mdStatFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("mdstat: %s does not exist", mdStatFile)
	} else if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(mdStatFile)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func init() {
	inputs.Add("mdstat", func() telegraf.Input { return &Mdstat{} })
}
