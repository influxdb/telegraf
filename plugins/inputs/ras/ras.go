//go:generate ../../../tools/readme_config_includer/generator
//go:build linux && (386 || amd64 || arm || arm64)

package ras

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	// Required for SQL framework driver
	_ "modernc.org/sqlite"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

//go:embed sample.conf
var sampleConfig string

const (
	mceQuery = `
		SELECT
			id, timestamp, error_msg, mcistatus_msg, socketid
		FROM mce_record
		WHERE timestamp > ?
		`
	defaultDbPath          = "/var/lib/rasdaemon/ras-mc_event.db"
	dateLayout             = "2006-01-02 15:04:05 -0700"
	memoryReadCorrected    = "memory_read_corrected_errors"
	memoryReadUncorrected  = "memory_read_uncorrectable_errors"
	memoryWriteCorrected   = "memory_write_corrected_errors"
	memoryWriteUncorrected = "memory_write_uncorrectable_errors"
	instructionCache       = "cache_l0_l1_errors"
	instructionTLB         = "tlb_instruction_errors"
	levelTwoCache          = "cache_l2_errors"
	upi                    = "upi_errors"
	processorBase          = "processor_base_errors"
	processorBus           = "processor_bus_errors"
	internalTimer          = "internal_timer_errors"
	smmHandlerCode         = "smm_handler_code_access_violation_errors"
	internalParity         = "internal_parity_errors"
	frc                    = "frc_errors"
	externalMCEBase        = "external_mce_errors"
	microcodeROMParity     = "microcode_rom_parity_errors"
	unclassifiedMCEBase    = "unclassified_mce_errors"
)

type Ras struct {
	DBPath string          `toml:"db_path"`
	Log    telegraf.Logger `toml:"-"`

	db                *sql.DB
	latestTimestamp   time.Time
	cpuSocketCounters map[int]metricCounters
	serverCounters    metricCounters
}

type machineCheckError struct {
	id           int
	timestamp    string
	socketID     int
	errorMsg     string
	mciStatusMsg string
}

type metricCounters map[string]int64

func (*Ras) SampleConfig() string {
	return sampleConfig
}

// Start initializes connection to DB, metrics are gathered in Gather
func (r *Ras) Start(telegraf.Accumulator) error {
	err := validateDbPath(r.DBPath)
	if err != nil {
		return err
	}

	r.db, err = connectToDB(r.DBPath)
	if err != nil {
		return err
	}

	return nil
}

// Gather reads the stats provided by RASDaemon and writes it to the Accumulator.
func (r *Ras) Gather(acc telegraf.Accumulator) error {
	rows, err := r.db.Query(mceQuery, r.latestTimestamp)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		mcError, err := fetchMachineCheckError(rows)
		if err != nil {
			return err
		}
		tsErr := r.updateLatestTimestamp(mcError.timestamp)
		if tsErr != nil {
			return err
		}
		r.updateCounters(mcError)
	}

	addCPUSocketMetrics(acc, r.cpuSocketCounters)
	addServerMetrics(acc, r.serverCounters)

	return nil
}

// Stop closes any existing DB connection
func (r *Ras) Stop() {
	if r.db != nil {
		err := r.db.Close()
		if err != nil {
			r.Log.Errorf("Error appeared during closing DB (%s): %v", r.DBPath, err)
		}
	}
}

func (r *Ras) updateLatestTimestamp(timestamp string) error {
	ts, err := parseDate(timestamp)
	if err != nil {
		return err
	}
	if ts.After(r.latestTimestamp) {
		r.latestTimestamp = ts
	}

	return nil
}

func (r *Ras) updateCounters(mcError *machineCheckError) {
	if strings.Contains(mcError.errorMsg, "No Error") {
		return
	}

	r.initializeCPUMetricDataIfRequired(mcError.socketID)
	r.updateSocketCounters(mcError)
	r.updateServerCounters(mcError)
}

func newMetricCounters() *metricCounters {
	return &metricCounters{
		memoryReadCorrected:    0,
		memoryReadUncorrected:  0,
		memoryWriteCorrected:   0,
		memoryWriteUncorrected: 0,
		instructionCache:       0,
		instructionTLB:         0,
		processorBase:          0,
		processorBus:           0,
		internalTimer:          0,
		smmHandlerCode:         0,
		internalParity:         0,
		frc:                    0,
		externalMCEBase:        0,
		microcodeROMParity:     0,
		unclassifiedMCEBase:    0,
	}
}

func (r *Ras) updateServerCounters(mcError *machineCheckError) {
	if strings.Contains(mcError.errorMsg, "CACHE Level-2") && strings.Contains(mcError.errorMsg, "Error") {
		r.serverCounters[levelTwoCache]++
	}

	if strings.Contains(mcError.errorMsg, "UPI:") {
		r.serverCounters[upi]++
	}
}

func validateDbPath(dbPath string) error {
	pathInfo, err := os.Stat(dbPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("provided db_path does not exist: [%s]", dbPath)
	}

	if err != nil {
		return fmt.Errorf("cannot get system information for db_path file %q: %w", dbPath, err)
	}

	if mode := pathInfo.Mode(); !mode.IsRegular() {
		return fmt.Errorf("provided db_path does not point to a regular file: [%s]", dbPath)
	}

	return nil
}

func connectToDB(dbPath string) (*sql.DB, error) {
	return sql.Open("sqlite", dbPath)
}

func (r *Ras) initializeCPUMetricDataIfRequired(socketID int) {
	if _, ok := r.cpuSocketCounters[socketID]; !ok {
		r.cpuSocketCounters[socketID] = *newMetricCounters()
	}
}

func (r *Ras) updateSocketCounters(mcError *machineCheckError) {
	r.updateMemoryCounters(mcError)
	r.updateProcessorBaseCounters(mcError)

	if strings.Contains(mcError.errorMsg, "Instruction TLB") && strings.Contains(mcError.errorMsg, "Error") {
		r.cpuSocketCounters[mcError.socketID][instructionTLB]++
	}

	if strings.Contains(mcError.errorMsg, "BUS") && strings.Contains(mcError.errorMsg, "Error") {
		r.cpuSocketCounters[mcError.socketID][processorBus]++
	}

	if (strings.Contains(mcError.errorMsg, "CACHE Level-0") ||
		strings.Contains(mcError.errorMsg, "CACHE Level-1")) &&
		strings.Contains(mcError.errorMsg, "Error") {
		r.cpuSocketCounters[mcError.socketID][instructionCache]++
	}
}

func (r *Ras) updateProcessorBaseCounters(mcError *machineCheckError) {
	if strings.Contains(mcError.errorMsg, "Internal Timer error") {
		r.cpuSocketCounters[mcError.socketID][internalTimer]++
		r.cpuSocketCounters[mcError.socketID][processorBase]++
	}

	if strings.Contains(mcError.errorMsg, "SMM Handler Code Access Violation") {
		r.cpuSocketCounters[mcError.socketID][smmHandlerCode]++
		r.cpuSocketCounters[mcError.socketID][processorBase]++
	}

	if strings.Contains(mcError.errorMsg, "Internal parity error") {
		r.cpuSocketCounters[mcError.socketID][internalParity]++
		r.cpuSocketCounters[mcError.socketID][processorBase]++
	}

	if strings.Contains(mcError.errorMsg, "FRC error") {
		r.cpuSocketCounters[mcError.socketID][frc]++
		r.cpuSocketCounters[mcError.socketID][processorBase]++
	}

	if strings.Contains(mcError.errorMsg, "External error") {
		r.cpuSocketCounters[mcError.socketID][externalMCEBase]++
		r.cpuSocketCounters[mcError.socketID][processorBase]++
	}

	if strings.Contains(mcError.errorMsg, "Microcode ROM parity error") {
		r.cpuSocketCounters[mcError.socketID][microcodeROMParity]++
		r.cpuSocketCounters[mcError.socketID][processorBase]++
	}

	if strings.Contains(mcError.errorMsg, "Unclassified") || strings.Contains(mcError.errorMsg, "Internal unclassified") {
		r.cpuSocketCounters[mcError.socketID][unclassifiedMCEBase]++
		r.cpuSocketCounters[mcError.socketID][processorBase]++
	}
}

func (r *Ras) updateMemoryCounters(mcError *machineCheckError) {
	if strings.Contains(mcError.errorMsg, "Memory read error") {
		if strings.Contains(mcError.mciStatusMsg, "Corrected_error") {
			r.cpuSocketCounters[mcError.socketID][memoryReadCorrected]++
		} else {
			r.cpuSocketCounters[mcError.socketID][memoryReadUncorrected]++
		}
	}
	if strings.Contains(mcError.errorMsg, "Memory write error") {
		if strings.Contains(mcError.mciStatusMsg, "Corrected_error") {
			r.cpuSocketCounters[mcError.socketID][memoryWriteCorrected]++
		} else {
			r.cpuSocketCounters[mcError.socketID][memoryWriteUncorrected]++
		}
	}
}

func addCPUSocketMetrics(acc telegraf.Accumulator, cpuSocketCounters map[int]metricCounters) {
	for socketID, data := range cpuSocketCounters {
		tags := map[string]string{
			"socket_id": strconv.Itoa(socketID),
		}
		fields := make(map[string]interface{})

		for errorName, count := range data {
			fields[errorName] = count
		}

		acc.AddCounter("ras", fields, tags)
	}
}

func addServerMetrics(acc telegraf.Accumulator, counters map[string]int64) {
	fields := make(map[string]interface{})
	for errorName, count := range counters {
		fields[errorName] = count
	}

	acc.AddCounter("ras", fields, make(map[string]string))
}

func fetchMachineCheckError(rows *sql.Rows) (*machineCheckError, error) {
	mcError := &machineCheckError{}
	err := rows.Scan(&mcError.id, &mcError.timestamp, &mcError.errorMsg, &mcError.mciStatusMsg, &mcError.socketID)

	if err != nil {
		return nil, err
	}

	return mcError, nil
}

func parseDate(date string) (time.Time, error) {
	return time.Parse(dateLayout, date)
}

func init() {
	inputs.Add("ras", func() telegraf.Input {
		//nolint:errcheck // known timestamp
		defaultTimestamp, _ := parseDate("1970-01-01 00:00:01 -0700")
		return &Ras{
			DBPath:          defaultDbPath,
			latestTimestamp: defaultTimestamp,
			cpuSocketCounters: map[int]metricCounters{
				0: *newMetricCounters(),
			},
			serverCounters: map[string]int64{
				levelTwoCache: 0,
				upi:           0,
			},
		}
	})
}
