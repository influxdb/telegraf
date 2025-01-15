//go:build linux && (386 || amd64 || arm || arm64)

package ras

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestUpdateCounters(t *testing.T) {
	ras := newRas()
	for i := range testData {
		ras.updateCounters(&testData[i])
	}

	require.Len(t, ras.cpuSocketCounters, 1, "Should contain counters only for single socket")

	for metric, value := range ras.cpuSocketCounters[0] {
		if metric == processorBase {
			// processor_base_errors is sum of other seven errors: internal_timer_errors, smm_handler_code_access_violation_errors,
			// internal_parity_errors, frc_errors, external_mce_errors, microcode_rom_parity_errors and unclassified_mce_errors
			require.Equal(t, int64(7), value, processorBase+" should have value of 7")
		} else {
			require.Equal(t, int64(1), value, metric+" should have value of 1")
		}
	}

	for metric, value := range ras.serverCounters {
		require.Equal(t, int64(1), value, metric+" should have value of 1")
	}
}

func TestUpdateLatestTimestamp(t *testing.T) {
	ras := newRas()
	ts := "2020-08-01 15:13:27 +0200"
	testData = append(testData, []machineCheckError{
		{
			timestamp:    "2019-05-20 08:25:55 +0200",
			socketID:     0,
			errorMsg:     "",
			mciStatusMsg: "",
		},
		{
			timestamp:    "2018-02-21 12:27:22 +0200",
			socketID:     0,
			errorMsg:     "",
			mciStatusMsg: "",
		},
		{
			timestamp:    ts,
			socketID:     0,
			errorMsg:     "",
			mciStatusMsg: "",
		},
	}...)
	for _, mce := range testData {
		err := ras.updateLatestTimestamp(mce.timestamp)
		require.NoError(t, err)
	}
	require.Equal(t, ts, ras.latestTimestamp.Format(dateLayout))
}

func TestMultipleSockets(t *testing.T) {
	ras := newRas()
	cacheL2 := "Instruction CACHE Level-2 Generic Error"
	overflow := "Error_overflow Corrected_error"
	testData = []machineCheckError{
		{
			timestamp:    "2019-05-20 08:25:55 +0200",
			socketID:     0,
			errorMsg:     cacheL2,
			mciStatusMsg: overflow,
		},
		{
			timestamp:    "2018-02-21 12:27:22 +0200",
			socketID:     1,
			errorMsg:     cacheL2,
			mciStatusMsg: overflow,
		},
		{
			timestamp:    "2020-03-21 14:17:28 +0200",
			socketID:     2,
			errorMsg:     cacheL2,
			mciStatusMsg: overflow,
		},
		{
			timestamp:    "2020-03-21 17:24:18 +0200",
			socketID:     3,
			errorMsg:     cacheL2,
			mciStatusMsg: overflow,
		},
	}
	for i := range testData {
		ras.updateCounters(&testData[i])
	}
	require.Len(t, ras.cpuSocketCounters, 4, "Should contain counters for four sockets")

	for _, metricData := range ras.cpuSocketCounters {
		for metric, value := range metricData {
			if metric == levelTwoCache {
				require.Equal(t, int64(1), value, levelTwoCache+" should have value of 1")
			} else {
				require.Equal(t, int64(0), value, metric+" should have value of 0")
			}
		}
	}
}

func TestMissingDatabase(t *testing.T) {
	var acc testutil.Accumulator
	ras := newRas()
	ras.DBPath = "/nonexistent/ras.db"
	err := ras.Start(&acc)
	require.Error(t, err)
}

func TestEmptyDatabase(t *testing.T) {
	ras := newRas()

	require.Len(t, ras.cpuSocketCounters, 1, "Should contain default counters for one socket")
	require.Len(t, ras.serverCounters, 2, "Should contain default counters for server")

	for metric, value := range ras.cpuSocketCounters[0] {
		require.Equal(t, int64(0), value, metric+" should have value of 0")
	}

	for metric, value := range ras.serverCounters {
		require.Equal(t, int64(0), value, metric+" should have value of 0")
	}
}

func newRas() *Ras {
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
}

var testData = []machineCheckError{
	{
		timestamp:    "2020-05-20 07:34:53 +0200",
		socketID:     0,
		errorMsg:     "MEMORY CONTROLLER RD_CHANNEL0_ERR Transaction: Memory read error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 07:35:11 +0200",
		socketID:     0,
		errorMsg:     "MEMORY CONTROLLER RD_CHANNEL0_ERR Transaction: Memory read error",
		mciStatusMsg: "Uncorrected_error",
	},
	{
		timestamp:    "2020-05-20 07:37:50 +0200",
		socketID:     0,
		errorMsg:     "MEMORY CONTROLLER RD_CHANNEL2_ERR Transaction: Memory write error",
		mciStatusMsg: "Uncorrected_error",
	},
	{
		timestamp:    "2020-05-20 08:14:51 +0200",
		socketID:     0,
		errorMsg:     "MEMORY CONTROLLER WR_CHANNEL2_ERR Transaction: Memory write error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:15:31 +0200",
		socketID:     0,
		errorMsg:     "corrected filtering (some unreported errors in same region) Instruction CACHE Level-0 Read Error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:16:32 +0200",
		socketID:     0,
		errorMsg:     "Instruction TLB Level-0 Error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:16:56 +0200",
		socketID:     0,
		errorMsg:     "No Error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:17:24 +0200",
		socketID:     0,
		errorMsg:     "Unclassified",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:17:41 +0200",
		socketID:     0,
		errorMsg:     "Microcode ROM parity error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:17:48 +0200",
		socketID:     0,
		errorMsg:     "FRC error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:18:18 +0200",
		socketID:     0,
		errorMsg:     "Internal parity error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:18:34 +0200",
		socketID:     0,
		errorMsg:     "SMM Handler Code Access Violation",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:18:54 +0200",
		socketID:     0,
		errorMsg:     "Internal Timer error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:21:23 +0200",
		socketID:     0,
		errorMsg:     "BUS Level-3 Generic Generic IO Request-did-not-timeout Error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:23:23 +0200",
		socketID:     0,
		errorMsg:     "External error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:25:31 +0200",
		socketID:     0,
		errorMsg:     "UPI: COR LL Rx detected CRC error - successful LLR without Phy Reinit",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
	{
		timestamp:    "2020-05-20 08:25:55 +0200",
		socketID:     0,
		errorMsg:     "Instruction CACHE Level-2 Generic Error",
		mciStatusMsg: "Error_overflow Corrected_error",
	},
}
