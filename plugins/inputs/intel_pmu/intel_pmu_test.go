//go:build linux && amd64

package intel_pmu

import (
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	ia "github.com/intel/iaevents"
	"github.com/stretchr/testify/require"

	"github.com/influxdata/telegraf/testutil"
)

func TestInitialization(t *testing.T) {
	mError := errors.New("mock error")
	mParser := &mockEntitiesParser{}
	mResolver := &mockEntitiesResolver{}
	mActivator := &mockEntitiesActivator{}
	mFileInfo := &mockFileInfoProvider{}

	file := "path/to/file"
	paths := []string{file}

	t.Run("missing parser, resolver or activator", func(t *testing.T) {
		err := (&IntelPMU{}).initialization(mParser, nil, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "entities parser and/or resolver and/or activator is nil")
		err = (&IntelPMU{}).initialization(nil, mResolver, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "entities parser and/or resolver and/or activator is nil")
		err = (&IntelPMU{}).initialization(nil, nil, mActivator)
		require.Error(t, err)
		require.Contains(t, err.Error(), "entities parser and/or resolver and/or activator is nil")
	})

	t.Run("parse entities error", func(t *testing.T) {
		mIntelPMU := &IntelPMU{EventListPaths: paths, fileInfo: mFileInfo}

		mParser.On("parseEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(mError).Once()

		err := mIntelPMU.initialization(mParser, mResolver, mActivator)
		require.Error(t, err)
		require.Contains(t, err.Error(), "error during parsing configuration sections")
		mParser.AssertExpectations(t)
	})

	t.Run("resolver error", func(t *testing.T) {
		mIntelPMU := &IntelPMU{EventListPaths: paths, fileInfo: mFileInfo}

		mParser.On("parseEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(nil).Once()
		mResolver.On("resolveEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(mError).Once()

		err := mIntelPMU.initialization(mParser, mResolver, mActivator)
		require.Error(t, err)
		require.Contains(t, err.Error(), "error during events resolving")
		mParser.AssertExpectations(t)
	})

	t.Run("exceeded file descriptors", func(t *testing.T) {
		limit := []byte("10")
		uncoreEntities := []*uncoreEventEntity{{parsedEvents: makeEvents(10, 21), parsedSockets: makeIDs(5)}}
		estimation := 1050

		mIntelPMU := IntelPMU{EventListPaths: paths, Log: testutil.Logger{}, fileInfo: mFileInfo, UncoreEntities: uncoreEntities}

		mParser.On("parseEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(nil).Once()
		mResolver.On("resolveEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(nil).Once()
		mFileInfo.On("readFile", fileMaxPath).Return(limit, nil).Once()

		err := mIntelPMU.initialization(mParser, mResolver, mActivator)
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("required file descriptors number `%d` exceeds maximum number of available file descriptors `%d`"+
			": consider increasing the maximum number", estimation, 10))
		mFileInfo.AssertExpectations(t)
		mParser.AssertExpectations(t)
		mResolver.AssertExpectations(t)
	})

	t.Run("failed to activate entities", func(t *testing.T) {
		mIntelPMU := IntelPMU{EventListPaths: paths, Log: testutil.Logger{}, fileInfo: mFileInfo}

		mParser.On("parseEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(nil).Once()
		mResolver.On("resolveEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(nil).Once()
		mActivator.On("activateEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(mError).Once()
		mFileInfo.On("readFile", fileMaxPath).Return(nil, mError).
			On("fileLimit").Return(uint64(0), mError).Once()

		err := mIntelPMU.initialization(mParser, mResolver, mActivator)
		require.Error(t, err)
		require.Contains(t, err.Error(), "error during events activation")
		mFileInfo.AssertExpectations(t)
		mParser.AssertExpectations(t)
		mResolver.AssertExpectations(t)
		mActivator.AssertExpectations(t)
	})

	t.Run("everything all right", func(t *testing.T) {
		mIntelPMU := IntelPMU{EventListPaths: paths, Log: testutil.Logger{}, fileInfo: mFileInfo}

		mParser.On("parseEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(nil).Once()
		mResolver.On("resolveEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(nil).Once()
		mFileInfo.On("readFile", fileMaxPath).Return(nil, mError).
			On("fileLimit").Return(uint64(0), mError).Once()
		mActivator.On("activateEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).Return(nil).Once()

		err := mIntelPMU.initialization(mParser, mResolver, mActivator)
		require.NoError(t, err)
		mFileInfo.AssertExpectations(t)
		mParser.AssertExpectations(t)
		mResolver.AssertExpectations(t)
		mActivator.AssertExpectations(t)
	})
}

func TestGather(t *testing.T) {
	mEntitiesValuesReader := &mockEntitiesValuesReader{}
	mAcc := &testutil.Accumulator{}

	mIntelPMU := &IntelPMU{entitiesReader: mEntitiesValuesReader}

	type fieldWithTags struct {
		fields map[string]interface{}
		tags   map[string]string
	}

	t.Run("entities reader is nil", func(t *testing.T) {
		err := (&IntelPMU{entitiesReader: nil}).Gather(mAcc)

		require.Error(t, err)
		require.Contains(t, err.Error(), "entities reader is nil")
	})

	t.Run("error while reading entities", func(t *testing.T) {
		errMock := errors.New("houston we have a problem")
		mEntitiesValuesReader.On("readEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).
			Return(nil, nil, errMock).Once()

		err := mIntelPMU.Gather(mAcc)

		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("failed to read entities events values: %v", errMock))
		mEntitiesValuesReader.AssertExpectations(t)
	})

	tests := []struct {
		name          string
		coreMetrics   []coreMetric
		uncoreMetrics []uncoreMetric
		results       []fieldWithTags
		errMSg        string
	}{
		{
			name: "successful readings",
			coreMetrics: []coreMetric{
				{
					values: ia.CounterValue{Raw: 100, Enabled: 200, Running: 200},
					name:   "CORE_EVENT_1",
					tag:    "DOGES",
					cpu:    1,
				},
				{
					values: ia.CounterValue{Raw: 2100, Enabled: 400, Running: 200},
					name:   "CORE_EVENT_2",
					cpu:    0,
				},
			},
			uncoreMetrics: []uncoreMetric{
				{
					values:   ia.CounterValue{Raw: 2134562, Enabled: 1000000, Running: 1000000},
					name:     "UNCORE_EVENT_1",
					tag:      "SHIBA",
					unitType: "cbox",
					unit:     "cbox_1",
					socket:   3,
					agg:      false,
				},
				{
					values:   ia.CounterValue{Raw: 2134562, Enabled: 3222222, Running: 2100000},
					name:     "UNCORE_EVENT_2",
					unitType: "cbox",
					socket:   0,
					agg:      true,
				},
			},
			results: []fieldWithTags{
				{
					fields: map[string]interface{}{
						"raw":     uint64(100),
						"enabled": uint64(200),
						"running": uint64(200),
						"scaled":  uint64(100),
					},
					tags: map[string]string{
						"event":      "CORE_EVENT_1",
						"cpu":        "1",
						"events_tag": "DOGES",
					},
				},
				{
					fields: map[string]interface{}{
						"raw":     uint64(2100),
						"enabled": uint64(400),
						"running": uint64(200),
						"scaled":  uint64(4200),
					},
					tags: map[string]string{
						"event": "CORE_EVENT_2",
						"cpu":   "0",
					},
				},
				{
					fields: map[string]interface{}{
						"raw":     uint64(2134562),
						"enabled": uint64(1000000),
						"running": uint64(1000000),
						"scaled":  uint64(2134562),
					},
					tags: map[string]string{
						"event":      "UNCORE_EVENT_1",
						"events_tag": "SHIBA",
						"socket":     "3",
						"unit_type":  "cbox",
						"unit":       "cbox_1",
					},
				},
				{
					fields: map[string]interface{}{
						"raw":     uint64(2134562),
						"enabled": uint64(3222222),
						"running": uint64(2100000),
						"scaled":  uint64(3275253),
					},
					tags: map[string]string{
						"event":     "UNCORE_EVENT_2",
						"socket":    "0",
						"unit_type": "cbox",
					},
				},
			},
		},
		{
			name: "core scaled value greater then max uint64",
			coreMetrics: []coreMetric{
				{
					values: ia.CounterValue{Raw: math.MaxUint64, Enabled: 400000, Running: 200000},
					name:   "I_AM_TOO_BIG",
					tag:    "BIG_FISH",
				},
			},
			errMSg: `cannot process "I_AM_TOO_BIG" scaled value "36893488147419103230": exceeds uint64`,
		},
		{
			name: "uncore scaled value greater then max uint64",
			uncoreMetrics: []uncoreMetric{
				{
					values: ia.CounterValue{Raw: math.MaxUint64, Enabled: 400000, Running: 200000},
					name:   "I_AM_TOO_BIG_UNCORE",
					tag:    "BIG_FISH",
				},
			},
			errMSg: `cannot process "I_AM_TOO_BIG_UNCORE" scaled value "36893488147419103230": exceeds uint64`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mEntitiesValuesReader.On("readEntities", mIntelPMU.CoreEntities, mIntelPMU.UncoreEntities).
				Return(test.coreMetrics, test.uncoreMetrics, nil).Once()

			err := mIntelPMU.Gather(mAcc)

			mEntitiesValuesReader.AssertExpectations(t)
			if len(test.errMSg) > 0 {
				require.Error(t, err)
				require.Contains(t, err.Error(), test.errMSg)
				return
			}
			require.NoError(t, err)
			for _, result := range test.results {
				mAcc.AssertContainsTaggedFields(t, "pmu_metric", result.fields, result.tags)
			}
		})
	}
}

func TestCheckFileDescriptors(t *testing.T) {
	tests := []struct {
		name       string
		uncores    []*uncoreEventEntity
		cores      []*coreEventEntity
		estimation uint64
		maxFD      []byte
		fileLimit  uint64
		errMsg     string
	}{
		{"exceed maximum file descriptors number", []*uncoreEventEntity{
			{parsedEvents: makeEvents(100, 21), parsedSockets: makeIDs(5)},
			{parsedEvents: makeEvents(25, 3), parsedSockets: makeIDs(7)},
			{parsedEvents: makeEvents(2, 7), parsedSockets: makeIDs(20)}},
			[]*coreEventEntity{
				{parsedEvents: makeEvents(100, 1), parsedCores: makeIDs(5)},
				{parsedEvents: makeEvents(25, 1), parsedCores: makeIDs(7)},
				{parsedEvents: makeEvents(2, 1), parsedCores: makeIDs(20)}},
			12020, []byte("11000"), 8000, fmt.Sprintf("required file descriptors number `%d` exceeds maximum number of available file descriptors `%d`"+
				": consider increasing the maximum number", 12020, 11000),
		},
		{"exceed soft file limit", []*uncoreEventEntity{{parsedEvents: makeEvents(100, 21), parsedSockets: makeIDs(5)}}, []*coreEventEntity{
			{parsedEvents: makeEvents(100, 1), parsedCores: makeIDs(5)}},
			11000, []byte("2515357"), 800, fmt.Sprintf("required file descriptors number `%d` exceeds soft limit of open files `%d`"+
				": consider increasing the limit", 11000, 800),
		},
		{"no exceeds", []*uncoreEventEntity{{parsedEvents: makeEvents(100, 21), parsedSockets: makeIDs(5)}},
			[]*coreEventEntity{{parsedEvents: makeEvents(100, 1), parsedCores: makeIDs(5)}},
			11000, []byte("2515357"), 13000, "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mFileInfo := &mockFileInfoProvider{}
			mIntelPMU := IntelPMU{
				CoreEntities:   test.cores,
				UncoreEntities: test.uncores,
				fileInfo:       mFileInfo,
				Log:            testutil.Logger{},
			}
			mFileInfo.On("readFile", fileMaxPath).Return(test.maxFD, nil).
				On("fileLimit").Return(test.fileLimit, nil).Once()

			err := mIntelPMU.checkFileDescriptors()
			if len(test.errMsg) > 0 {
				require.Error(t, err)
				require.Contains(t, err.Error(), test.errMsg)
				return
			}
			require.NoError(t, err)
			mFileInfo.AssertExpectations(t)
		})
	}
}

func TestEstimateUncoreFd(t *testing.T) {
	tests := []struct {
		name     string
		entities []*uncoreEventEntity
		result   uint64
	}{
		{"nil entities", nil, 0},
		{"nil perf event", []*uncoreEventEntity{{parsedEvents: []*eventWithQuals{{"", nil, ia.CustomizableEvent{}}}, parsedSockets: makeIDs(0)}}, 0},
		{"one uncore entity", []*uncoreEventEntity{{parsedEvents: makeEvents(10, 10), parsedSockets: makeIDs(20)}}, 2000},
		{"nil entity", []*uncoreEventEntity{nil, {parsedEvents: makeEvents(1, 8), parsedSockets: makeIDs(1)}}, 8},
		{"many core entities", []*uncoreEventEntity{
			{parsedEvents: makeEvents(100, 21), parsedSockets: makeIDs(5)},
			{parsedEvents: makeEvents(25, 3), parsedSockets: makeIDs(7)},
			{parsedEvents: makeEvents(2, 7), parsedSockets: makeIDs(20)},
		}, 11305},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mIntelPMU := IntelPMU{UncoreEntities: test.entities}
			result, err := estimateUncoreFd(mIntelPMU.UncoreEntities)
			require.Equal(t, test.result, result)
			require.NoError(t, err)
		})
	}
}

func TestEstimateCoresFd(t *testing.T) {
	tests := []struct {
		name     string
		entities []*coreEventEntity
		result   uint64
	}{
		{"nil entities", nil, 0},
		{"one core entity", []*coreEventEntity{{parsedEvents: makeEvents(10, 1), parsedCores: makeIDs(20)}}, 200},
		{"nil entity", []*coreEventEntity{nil, {parsedEvents: makeEvents(10, 1), parsedCores: makeIDs(20)}}, 200},
		{"many core entities", []*coreEventEntity{
			{parsedEvents: makeEvents(100, 1), parsedCores: makeIDs(5)},
			{parsedEvents: makeEvents(25, 1), parsedCores: makeIDs(7)},
			{parsedEvents: makeEvents(2, 1), parsedCores: makeIDs(20)},
		}, 715},
		{"1024 events", []*coreEventEntity{{parsedEvents: makeEvents(1024, 1), parsedCores: makeIDs(12)}}, 12288},
		{"big number", []*coreEventEntity{{parsedEvents: makeEvents(1024, 1), parsedCores: makeIDs(1048576)}}, 1073741824},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mIntelPMU := IntelPMU{CoreEntities: test.entities}
			result, err := estimateCoresFd(mIntelPMU.CoreEntities)
			require.NoError(t, err)
			require.Equal(t, test.result, result)
		})
	}
}

func makeEvents(number, pmusNumber int) []*eventWithQuals {
	a := make([]*eventWithQuals, number)
	for i := range a {
		b := make([]ia.NamedPMUType, pmusNumber)
		for j := range b {
			b[j] = ia.NamedPMUType{}
		}
		a[i] = &eventWithQuals{fmt.Sprintf("EVENT.%d", i), nil,
			ia.CustomizableEvent{Event: &ia.PerfEvent{PMUTypes: b}},
		}
	}
	return a
}

func makeIDs(number int) []int {
	a := make([]int, number)
	for i := range a {
		a[i] = i
	}
	return a
}

func TestReadMaxFD(t *testing.T) {
	mFileReader := &mockFileInfoProvider{}

	t.Run("reader is nil", func(t *testing.T) {
		result, err := readMaxFD(nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "file reader is nil")
		require.Zero(t, result)
	})

	openErrorMsg := fmt.Sprintf("cannot open file %q", fileMaxPath)
	parseErrorMsg := fmt.Sprintf("cannot parse file content of %q", fileMaxPath)

	tests := []struct {
		name    string
		err     error
		content []byte
		maxFD   uint64
		failMsg string
	}{
		{"read file error", errors.New("mock error"), nil, 0, openErrorMsg},
		{"file content parse error", nil, []byte("wrong format"), 0, parseErrorMsg},
		{"negative value reading", nil, []byte("-10000"), 0, parseErrorMsg},
		{"max uint exceeded", nil, []byte("18446744073709551616"), 0, parseErrorMsg},
		{"reading succeeded", nil, []byte("12343122"), 12343122, ""},
		{"min value reading", nil, []byte("0"), 0, ""},
		{"max uint 64 reading", nil, []byte("18446744073709551615"), math.MaxUint64, ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mFileReader.On("readFile", fileMaxPath).Return(test.content, test.err).Once()
			result, err := readMaxFD(mFileReader)

			if len(test.failMsg) > 0 {
				require.Error(t, err)
				require.Contains(t, err.Error(), test.failMsg)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.maxFD, result)
			mFileReader.AssertExpectations(t)
		})
	}
}

func TestAddFiles(t *testing.T) {
	mFileInfo := &mockFileInfoProvider{}
	mError := errors.New("mock error")

	t.Run("no paths", func(t *testing.T) {
		err := checkFiles(nil, mFileInfo)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no paths were given")
	})

	t.Run("no file info provider", func(t *testing.T) {
		err := checkFiles([]string{"path/1, path/2"}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "file info provider is nil")
	})

	t.Run("stat error", func(t *testing.T) {
		file := "path/to/file"
		paths := []string{file}
		mFileInfo.On("lstat", file).Return(nil, mError).Once()

		err := checkFiles(paths, mFileInfo)
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("cannot obtain file info of %q", file))
		mFileInfo.AssertExpectations(t)
	})

	t.Run("file does not exist", func(t *testing.T) {
		file := "path/to/file"
		paths := []string{file}
		mFileInfo.On("lstat", file).Return(nil, os.ErrNotExist).Once()

		err := checkFiles(paths, mFileInfo)
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("file %q doesn't exist", file))
		mFileInfo.AssertExpectations(t)
	})

	t.Run("file is symlink", func(t *testing.T) {
		file := "path/to/symlink"
		paths := []string{file}
		fileInfo := fakeFileInfo{fileMode: os.ModeSymlink}
		mFileInfo.On("lstat", file).Return(fileInfo, nil).Once()

		err := checkFiles(paths, mFileInfo)
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("file %q is a symlink", file))
		mFileInfo.AssertExpectations(t)
	})

	t.Run("file doesn't point to a regular file", func(t *testing.T) {
		file := "path/to/file"
		paths := []string{file}
		fileInfo := fakeFileInfo{fileMode: os.ModeDir}
		mFileInfo.On("lstat", file).Return(fileInfo, nil).Once()

		err := checkFiles(paths, mFileInfo)
		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("file %q doesn't point to a reagular file", file))
		mFileInfo.AssertExpectations(t)
	})

	t.Run("checking succeeded", func(t *testing.T) {
		paths := []string{"path/to/file1", "path/to/file2", "path/to/file3"}
		fileInfo := fakeFileInfo{}

		for _, file := range paths {
			mFileInfo.On("lstat", file).Return(fileInfo, nil).Once()
		}

		err := checkFiles(paths, mFileInfo)
		require.NoError(t, err)
		mFileInfo.AssertExpectations(t)
	})
}

type fakeFileInfo struct {
	fileMode os.FileMode
}

func (fakeFileInfo) Name() string        { return "" }
func (fakeFileInfo) Size() int64         { return 0 }
func (f fakeFileInfo) Mode() os.FileMode { return f.fileMode }
func (fakeFileInfo) ModTime() time.Time  { return time.Time{} }
func (fakeFileInfo) IsDir() bool         { return false }
func (fakeFileInfo) Sys() interface{}    { return nil }
