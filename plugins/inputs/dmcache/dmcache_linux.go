//go:build linux

package dmcache

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"

	"github.com/influxdata/telegraf"
)

const metricName = "dmcache"

type cacheStatus struct {
	device            string
	length            int64
	target            string
	metadataBlocksize int64
	metadataUsed      int64
	metadataTotal     int64
	cacheBlocksize    int64
	cacheUsed         int64
	cacheTotal        int64
	readHits          int64
	readMisses        int64
	writeHits         int64
	writeMisses       int64
	demotions         int64
	promotions        int64
	dirty             int64
}

func (c *DMCache) Gather(acc telegraf.Accumulator) error {
	outputLines, err := c.getCurrentStatus()
	if err != nil {
		return err
	}

	totalStatus := cacheStatus{}

	for _, s := range outputLines {
		status, err := parseDMSetupStatus(s)
		if err != nil {
			return err
		}

		if c.PerDevice {
			tags := map[string]string{"device": status.device}
			acc.AddFields(metricName, toFields(status), tags)
		}
		aggregateStats(&totalStatus, status)
	}

	acc.AddFields(metricName, toFields(totalStatus), map[string]string{"device": "all"})

	return nil
}

func parseDMSetupStatus(line string) (cacheStatus, error) {
	var err error
	parseError := errors.New("output from dmsetup could not be parsed")
	status := cacheStatus{}
	values := strings.Fields(line)
	if len(values) < 15 {
		return cacheStatus{}, parseError
	}

	status.device = strings.TrimRight(values[0], ":")
	status.length, err = strconv.ParseInt(values[2], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.target = values[3]
	status.metadataBlocksize, err = strconv.ParseInt(values[4], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	metadata := strings.Split(values[5], "/")
	if len(metadata) != 2 {
		return cacheStatus{}, parseError
	}
	status.metadataUsed, err = strconv.ParseInt(metadata[0], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.metadataTotal, err = strconv.ParseInt(metadata[1], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.cacheBlocksize, err = strconv.ParseInt(values[6], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	cache := strings.Split(values[7], "/")
	if len(cache) != 2 {
		return cacheStatus{}, parseError
	}
	status.cacheUsed, err = strconv.ParseInt(cache[0], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.cacheTotal, err = strconv.ParseInt(cache[1], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.readHits, err = strconv.ParseInt(values[8], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.readMisses, err = strconv.ParseInt(values[9], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.writeHits, err = strconv.ParseInt(values[10], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.writeMisses, err = strconv.ParseInt(values[11], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.demotions, err = strconv.ParseInt(values[12], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.promotions, err = strconv.ParseInt(values[13], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}
	status.dirty, err = strconv.ParseInt(values[14], 10, 64)
	if err != nil {
		return cacheStatus{}, err
	}

	return status, nil
}

func aggregateStats(totalStatus *cacheStatus, status cacheStatus) {
	totalStatus.length += status.length
	totalStatus.metadataBlocksize += status.metadataBlocksize
	totalStatus.metadataUsed += status.metadataUsed
	totalStatus.metadataTotal += status.metadataTotal
	totalStatus.cacheBlocksize += status.cacheBlocksize
	totalStatus.cacheUsed += status.cacheUsed
	totalStatus.cacheTotal += status.cacheTotal
	totalStatus.readHits += status.readHits
	totalStatus.readMisses += status.readMisses
	totalStatus.writeHits += status.writeHits
	totalStatus.writeMisses += status.writeMisses
	totalStatus.demotions += status.demotions
	totalStatus.promotions += status.promotions
	totalStatus.dirty += status.dirty
}

func toFields(status cacheStatus) map[string]interface{} {
	fields := make(map[string]interface{})
	fields["length"] = status.length
	fields["metadata_blocksize"] = status.metadataBlocksize
	fields["metadata_used"] = status.metadataUsed
	fields["metadata_total"] = status.metadataTotal
	fields["cache_blocksize"] = status.cacheBlocksize
	fields["cache_used"] = status.cacheUsed
	fields["cache_total"] = status.cacheTotal
	fields["read_hits"] = status.readHits
	fields["read_misses"] = status.readMisses
	fields["write_hits"] = status.writeHits
	fields["write_misses"] = status.writeMisses
	fields["demotions"] = status.demotions
	fields["promotions"] = status.promotions
	fields["dirty"] = status.dirty
	return fields
}

func dmSetupStatus() ([]string, error) {
	out, err := exec.Command("/bin/sh", "-c", "sudo /sbin/dmsetup status --target cache").Output()
	if err != nil {
		return nil, err
	}
	if string(out) == "No devices found\n" {
		return nil, nil
	}

	outString := strings.TrimRight(string(out), "\n")
	status := strings.Split(outString, "\n")

	return status, nil
}
