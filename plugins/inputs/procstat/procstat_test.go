package procstat

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func init() {
	execCommand = mockExecCommand
}
func mockExecCommand(arg0 string, args ...string) *exec.Cmd {
	args = append([]string{"-test.run=TestMockExecCommand", "--", arg0}, args...)
	cmd := exec.Command(os.Args[0], args...)
	cmd.Stderr = os.Stderr
	return cmd
}
func TestMockExecCommand(_ *testing.T) {
	var cmd []string //nolint:prealloc // Pre-allocated this slice would break the algorithm
	for _, arg := range os.Args {
		if arg == "--" {
			cmd = []string{}
			continue
		}
		if cmd == nil {
			continue
		}
		cmd = append(cmd, arg)
	}
	if cmd == nil {
		return
	}
	cmdline := strings.Join(cmd, " ")

	if cmdline == "systemctl show TestGather_systemdUnitPIDs" {
		fmt.Printf(`PIDFile=
GuessMainPID=yes
MainPID=11408
ControlPID=0
ExecMainPID=11408
`)
		//nolint:revive // error code is important for this "test"
		os.Exit(0)
	}

	if cmdline == "supervisorctl status TestGather_supervisorUnitPIDs" {
		fmt.Printf(`TestGather_supervisorUnitPIDs                             RUNNING   pid 7311, uptime 0:00:19
`)
		//nolint:revive // error code is important for this "test"
		os.Exit(0)
	}

	if cmdline == "supervisorctl status TestGather_STARTINGsupervisorUnitPIDs TestGather_FATALsupervisorUnitPIDs" {
		fmt.Printf(`TestGather_FATALsupervisorUnitPIDs                       FATAL     Exited too quickly (process log may have details)
TestGather_STARTINGsupervisorUnitPIDs                          STARTING`)
		//nolint:revive // error code is important for this "test"
		os.Exit(0)
	}

	fmt.Printf("command not found\n")
	//nolint:revive // error code is important for this "test"
	os.Exit(1)
}

type testPgrep struct {
	pids []PID
	err  error
}

func newTestFinder(pids []PID) PIDFinder {
	return &testPgrep{
		pids: pids,
		err:  nil,
	}
}

func (pg *testPgrep) PidFile(_ string) ([]PID, error) {
	return pg.pids, pg.err
}

func (p *testProc) Cmdline() (string, error) {
	return "test_proc", nil
}

func (pg *testPgrep) Pattern(_ string) ([]PID, error) {
	return pg.pids, pg.err
}

func (pg *testPgrep) UID(_ string) ([]PID, error) {
	return pg.pids, pg.err
}

func (pg *testPgrep) FullPattern(_ string) ([]PID, error) {
	return pg.pids, pg.err
}

func (pg *testPgrep) Children(_ PID) ([]PID, error) {
	pids := []PID{7311, 8111, 8112}
	return pids, pg.err
}

type testProc struct {
	pid  PID
	tags map[string]string
}

func newTestProc(_ PID) (Process, error) {
	proc := &testProc{
		tags: make(map[string]string),
	}
	return proc, nil
}

func (p *testProc) PID() PID {
	return p.pid
}

func (p *testProc) Username() (string, error) {
	return "testuser", nil
}

func (p *testProc) Tags() map[string]string {
	return p.tags
}

func (p *testProc) PageFaults() (*process.PageFaultsStat, error) {
	return &process.PageFaultsStat{}, nil
}

func (p *testProc) IOCounters() (*process.IOCountersStat, error) {
	return &process.IOCountersStat{}, nil
}

func (p *testProc) MemoryInfo() (*process.MemoryInfoStat, error) {
	return &process.MemoryInfoStat{}, nil
}

func (p *testProc) MemoryMaps(bool) (*[]process.MemoryMapsStat, error) {
	return &[]process.MemoryMapsStat{}, nil
}

func (p *testProc) Name() (string, error) {
	return "test_proc", nil
}

func (p *testProc) NumCtxSwitches() (*process.NumCtxSwitchesStat, error) {
	return &process.NumCtxSwitchesStat{}, nil
}

func (p *testProc) NumFDs() (int32, error) {
	return 0, nil
}

func (p *testProc) NumThreads() (int32, error) {
	return 0, nil
}

func (p *testProc) Percent(_ time.Duration) (float64, error) {
	return 0, nil
}

func (p *testProc) MemoryPercent() (float32, error) {
	return 0, nil
}

func (p *testProc) CreateTime() (int64, error) {
	return 0, nil
}

func (p *testProc) Times() (*cpu.TimesStat, error) {
	return &cpu.TimesStat{}, nil
}

func (p *testProc) RlimitUsage(_ bool) ([]process.RlimitStat, error) {
	return []process.RlimitStat{}, nil
}

func (p *testProc) Ppid() (int32, error) {
	return 0, nil
}

func (p *testProc) Status() ([]string, error) {
	return []string{"running"}, nil
}

var pid = PID(42)
var exe = "foo"

func TestInitInvalidFinder(t *testing.T) {
	plugin := Procstat{
		PidFinder:     "foo",
		createProcess: newTestProc,
	}
	require.Error(t, plugin.Init())
}

func TestInitRequiresChildDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping test on non-darwin platform")
	}

	p := Procstat{
		Pattern:        "somepattern",
		SupervisorUnit: []string{"a_unit"},
		PidFinder:      "native",
	}
	require.ErrorContains(t, p.Init(), "requires the 'pgrep' finder")
}

func TestGather_CreateProcessErrorOk(t *testing.T) {
	var acc testutil.Accumulator

	p := Procstat{
		Exe:    exe,
		finder: newTestFinder([]PID{pid}),
		createProcess: func(PID) (Process, error) {
			return nil, fmt.Errorf("createProcess error")
		},
	}
	require.NoError(t, acc.GatherError(p.Gather))
}

func TestGather_ProcessName(t *testing.T) {
	var acc testutil.Accumulator

	p := Procstat{
		Exe:           exe,
		ProcessName:   "custom_name",
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))

	require.Equal(t, "custom_name", acc.TagValue("procstat", "process_name"))
}

func TestGather_NoProcessNameUsesReal(t *testing.T) {
	var acc testutil.Accumulator
	pid := PID(os.Getpid())

	p := Procstat{
		Exe:           exe,
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))

	require.True(t, acc.HasTag("procstat", "process_name"))
}

func TestGather_NoPidTag(t *testing.T) {
	var acc testutil.Accumulator

	p := Procstat{
		Exe:           exe,
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))
	require.True(t, acc.HasInt32Field("procstat", "pid"))
	require.False(t, acc.HasTag("procstat", "pid"))
}

func TestGather_PidTag(t *testing.T) {
	var acc testutil.Accumulator

	p := Procstat{
		Exe:           exe,
		PidTag:        true,
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))
	require.Equal(t, "42", acc.TagValue("procstat", "pid"))
	require.False(t, acc.HasInt32Field("procstat", "pid"))
}

func TestGather_Prefix(t *testing.T) {
	var acc testutil.Accumulator

	p := Procstat{
		Exe:           exe,
		Prefix:        "custom_prefix",
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))
	require.True(t, acc.HasInt32Field("procstat", "custom_prefix_num_fds"))
}

func TestGather_Exe(t *testing.T) {
	var acc testutil.Accumulator

	p := Procstat{
		Exe:           exe,
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))

	require.Equal(t, exe, acc.TagValue("procstat", "exe"))
}

func TestGather_User(t *testing.T) {
	var acc testutil.Accumulator
	user := "ada"

	p := Procstat{
		User:          user,
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))

	require.Equal(t, user, acc.TagValue("procstat", "user"))
}

func TestGather_Pattern(t *testing.T) {
	var acc testutil.Accumulator
	pattern := "foo"

	p := Procstat{
		Pattern:       pattern,
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))

	require.Equal(t, pattern, acc.TagValue("procstat", "pattern"))
}

func TestGather_MissingPidMethod(t *testing.T) {
	var acc testutil.Accumulator

	p := Procstat{
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.Error(t, acc.GatherError(p.Gather))
}

func TestGather_PidFile(t *testing.T) {
	var acc testutil.Accumulator
	pidfile := "/path/to/pidfile"

	p := Procstat{
		PidFile:       pidfile,
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))

	require.Equal(t, pidfile, acc.TagValue("procstat", "pidfile"))
}

func TestGather_PercentFirstPass(t *testing.T) {
	var acc testutil.Accumulator
	pid := PID(os.Getpid())

	p := Procstat{
		Pattern:       "foo",
		PidTag:        true,
		finder:        newTestFinder([]PID{pid}),
		createProcess: NewProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))

	require.True(t, acc.HasFloatField("procstat", "cpu_time_user"))
	require.False(t, acc.HasFloatField("procstat", "cpu_usage"))
}

func TestGather_PercentSecondPass(t *testing.T) {
	var acc testutil.Accumulator
	pid := PID(os.Getpid())

	p := Procstat{
		Pattern:       "foo",
		PidTag:        true,
		finder:        newTestFinder([]PID{pid}),
		createProcess: NewProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))
	require.NoError(t, acc.GatherError(p.Gather))

	require.True(t, acc.HasFloatField("procstat", "cpu_time_user"))
	require.True(t, acc.HasFloatField("procstat", "cpu_usage"))
}

func TestGather_systemdUnitPIDs(t *testing.T) {
	p := Procstat{
		finder:       newTestFinder([]PID{pid}),
		SystemdUnits: "TestGather_systemdUnitPIDs",
	}
	pidsTags := p.findPids()
	for _, pidsTag := range pidsTags {
		pids := pidsTag.PIDS
		tags := pidsTag.Tags
		err := pidsTag.Err
		require.NoError(t, err)
		require.Equal(t, []PID{11408}, pids)
		require.Equal(t, "TestGather_systemdUnitPIDs", tags["systemd_unit"])
	}
}

func TestGather_cgroupPIDs(t *testing.T) {
	//no cgroups in windows
	if runtime.GOOS == "windows" {
		t.Skip("no cgroups in windows")
	}
	td := t.TempDir()
	err := os.WriteFile(filepath.Join(td, "cgroup.procs"), []byte("1234\n5678\n"), 0640)
	require.NoError(t, err)

	p := Procstat{
		finder: newTestFinder([]PID{pid}),
		CGroup: td,
	}
	pidsTags := p.findPids()
	for _, pidsTag := range pidsTags {
		pids := pidsTag.PIDS
		tags := pidsTag.Tags
		err := pidsTag.Err
		require.NoError(t, err)
		require.Equal(t, []PID{1234, 5678}, pids)
		require.Equal(t, td, tags["cgroup"])
	}
}

func TestProcstatLookupMetric(t *testing.T) {
	p := Procstat{
		finder:        newTestFinder([]PID{543}),
		createProcess: NewProc,
		Exe:           "-Gsys",
	}
	var acc testutil.Accumulator
	require.NoError(t, acc.GatherError(p.Gather))
	require.Len(t, acc.Metrics, len(p.procs)+1)
}

func TestGather_SameTimestamps(t *testing.T) {
	var acc testutil.Accumulator
	pidfile := "/path/to/pidfile"

	p := Procstat{
		PidFile:       pidfile,
		finder:        newTestFinder([]PID{pid}),
		createProcess: newTestProc,
	}
	require.NoError(t, acc.GatherError(p.Gather))

	procstat, _ := acc.Get("procstat")
	procstatLookup, _ := acc.Get("procstat_lookup")

	require.Equal(t, procstat.Time, procstatLookup.Time)
}

func TestGather_supervisorUnitPIDs(t *testing.T) {
	p := Procstat{
		finder:         newTestFinder([]PID{pid}),
		SupervisorUnit: []string{"TestGather_supervisorUnitPIDs"},
	}
	pidsTags := p.findPids()
	for _, pidsTag := range pidsTags {
		pids := pidsTag.PIDS
		tags := pidsTag.Tags
		err := pidsTag.Err
		require.NoError(t, err)
		require.Equal(t, []PID{7311, 8111, 8112}, pids)
		require.Equal(t, "TestGather_supervisorUnitPIDs", tags["supervisor_unit"])
	}
}

func TestGather_MoresupervisorUnitPIDs(t *testing.T) {
	p := Procstat{
		finder:         newTestFinder([]PID{pid}),
		SupervisorUnit: []string{"TestGather_STARTINGsupervisorUnitPIDs", "TestGather_FATALsupervisorUnitPIDs"},
	}
	pidsTags := p.findPids()
	for _, pidsTag := range pidsTags {
		pids := pidsTag.PIDS
		tags := pidsTag.Tags
		require.Empty(t, pids)
		require.NoError(t, pidsTag.Err)
		switch tags["supervisor_unit"] {
		case "TestGather_STARTINGsupervisorUnitPIDs":
			require.Equal(t, "STARTING", tags["status"])
		case "TestGather_FATALsupervisorUnitPIDs":
			require.Equal(t, "FATAL", tags["status"])
			require.Equal(t, "Exited too quickly (process log may have details)", tags["error"])
		default:
			t.Fatalf("unexpected value for tag 'supervisor_unit': %q", tags["supervisor_unit"])
		}
	}
}
