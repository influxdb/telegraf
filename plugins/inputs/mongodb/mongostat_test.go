package mongodb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLatencyStats(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Bits:              0,
					Resident:          0,
					Virtual:           0,
					Supported:         false,
					Mapped:            0,
					MappedWithJournal: 0,
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Bits:              0,
					Resident:          0,
					Virtual:           0,
					Supported:         false,
					Mapped:            0,
					MappedWithJournal: 0,
				},
				OpLatencies: &opLatenciesStats{
					Reads: &latencyStats{
						Ops:     0,
						Latency: 0,
					},
					Writes: &latencyStats{
						Ops:     0,
						Latency: 0,
					},
					Commands: &latencyStats{
						Ops:     0,
						Latency: 0,
					},
				},
			},
		},
		"foo",
		60,
	)

	require.Equal(t, int64(0), sl.CommandLatency)
	require.Equal(t, int64(0), sl.ReadLatency)
	require.Equal(t, int64(0), sl.WriteLatency)
	require.Equal(t, int64(0), sl.CommandOpsCnt)
	require.Equal(t, int64(0), sl.ReadOpsCnt)
	require.Equal(t, int64(0), sl.WriteOpsCnt)
}

func TestLatencyStatsDiffZero(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Bits:              0,
					Resident:          0,
					Virtual:           0,
					Supported:         false,
					Mapped:            0,
					MappedWithJournal: 0,
				},
				OpLatencies: &opLatenciesStats{
					Reads: &latencyStats{
						Ops:     0,
						Latency: 0,
					},
					Writes: &latencyStats{
						Ops:     0,
						Latency: 0,
					},
					Commands: &latencyStats{
						Ops:     0,
						Latency: 0,
					},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Bits:              0,
					Resident:          0,
					Virtual:           0,
					Supported:         false,
					Mapped:            0,
					MappedWithJournal: 0,
				},
				OpLatencies: &opLatenciesStats{
					Reads: &latencyStats{
						Ops:     0,
						Latency: 0,
					},
					Writes: &latencyStats{
						Ops:     0,
						Latency: 0,
					},
					Commands: &latencyStats{
						Ops:     0,
						Latency: 0,
					},
				},
			},
		},
		"foo",
		60,
	)

	require.Equal(t, int64(0), sl.CommandLatency)
	require.Equal(t, int64(0), sl.ReadLatency)
	require.Equal(t, int64(0), sl.WriteLatency)
	require.Equal(t, int64(0), sl.CommandOpsCnt)
	require.Equal(t, int64(0), sl.ReadOpsCnt)
	require.Equal(t, int64(0), sl.WriteOpsCnt)
}

func TestLatencyStatsDiff(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Bits:              0,
					Resident:          0,
					Virtual:           0,
					Supported:         false,
					Mapped:            0,
					MappedWithJournal: 0,
				},
				OpLatencies: &opLatenciesStats{
					Reads: &latencyStats{
						Ops:     4189041956,
						Latency: 2255922322753,
					},
					Writes: &latencyStats{
						Ops:     1691019457,
						Latency: 494478256915,
					},
					Commands: &latencyStats{
						Ops:     1019150402,
						Latency: 59177710371,
					},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Bits:              0,
					Resident:          0,
					Virtual:           0,
					Supported:         false,
					Mapped:            0,
					MappedWithJournal: 0,
				},
				OpLatencies: &opLatenciesStats{
					Reads: &latencyStats{
						Ops:     4189049884,
						Latency: 2255946760057,
					},
					Writes: &latencyStats{
						Ops:     1691021287,
						Latency: 494479456987,
					},
					Commands: &latencyStats{
						Ops:     1019152861,
						Latency: 59177981552,
					},
				},
			},
		},
		"foo",
		60,
	)

	require.Equal(t, int64(59177981552), sl.CommandLatency)
	require.Equal(t, int64(2255946760057), sl.ReadLatency)
	require.Equal(t, int64(494479456987), sl.WriteLatency)
	require.Equal(t, int64(1019152861), sl.CommandOpsCnt)
	require.Equal(t, int64(4189049884), sl.ReadOpsCnt)
	require.Equal(t, int64(1691021287), sl.WriteOpsCnt)
}

func TestLocksStatsNilWhenLocksMissingInOldStat(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		"foo",
		60,
	)

	require.Nil(t, sl.CollectionLocks)
}

func TestLocksStatsNilWhenGlobalLockStatsMissingInOldStat(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		"foo",
		60,
	)

	require.Nil(t, sl.CollectionLocks)
}

func TestLocksStatsNilWhenGlobalLockStatsEmptyInOldStat(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		"foo",
		60,
	)

	require.Nil(t, sl.CollectionLocks)
}

func TestLocksStatsNilWhenCollectionLockStatsMissingInOldStat(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		"foo",
		60,
	)

	require.Nil(t, sl.CollectionLocks)
}

func TestLocksStatsNilWhenCollectionLockStatsEmptyInOldStat(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
					"Collection": {},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		"foo",
		60,
	)

	require.Nil(t, sl.CollectionLocks)
}

func TestLocksStatsNilWhenLocksMissingInNewStat(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
			},
		},
		"foo",
		60,
	)

	require.Nil(t, sl.CollectionLocks)
}

func TestLocksStatsNilWhenGlobalLockStatsMissingInNewStat(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{},
			},
		},
		"foo",
		60,
	)

	require.Nil(t, sl.CollectionLocks)
}

func TestLocksStatsNilWhenGlobalLockStatsEmptyInNewStat(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {},
				},
			},
		},
		"foo",
		60,
	)

	require.Nil(t, sl.CollectionLocks)
}

func TestLocksStatsNilWhenCollectionLockStatsMissingInNewStat(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		"foo",
		60,
	)

	require.Nil(t, sl.CollectionLocks)
}

func TestLocksStatsNilWhenCollectionLockStatsEmptyInNewStat(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
					"Collection": {},
				},
			},
		},
		"foo",
		60,
	)

	require.Nil(t, sl.CollectionLocks)
}

func TestLocksStatsPopulated(t *testing.T) {
	sl := newStatLine(
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
					"Collection": {
						AcquireWaitCount: &readWriteLockTimes{
							Read:  1,
							Write: 2,
						},
						AcquireCount: &readWriteLockTimes{
							Read:  5,
							Write: 10,
						},
						TimeAcquiringMicros: readWriteLockTimes{
							Read:  100,
							Write: 200,
						},
					},
				},
			},
		},
		mongoStatus{
			ServerStatus: &serverStatus{
				Connections: &connectionStats{},
				Mem: &memStats{
					Supported: false,
				},
				Locks: map[string]lockStats{
					"Global": {
						AcquireCount: &readWriteLockTimes{},
					},
					"Collection": {
						AcquireWaitCount: &readWriteLockTimes{
							Read:  2,
							Write: 4,
						},
						AcquireCount: &readWriteLockTimes{
							Read:  10,
							Write: 30,
						},
						TimeAcquiringMicros: readWriteLockTimes{
							Read:  250,
							Write: 310,
						},
					},
				},
			},
		},
		"foo",
		60,
	)

	expected := &collectionLockStatus{
		ReadAcquireWaitsPercentage:  20,
		WriteAcquireWaitsPercentage: 10,
		ReadAcquireTimeMicros:       150,
		WriteAcquireTimeMicros:      55,
	}

	require.Equal(t, expected, sl.CollectionLocks)
}
