package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/require"
)

func TestAlignedTicker(t *testing.T) {
	interval := 10 * time.Second
	jitter := 0 * time.Second
	offset := 0 * time.Second

	clk := clock.NewMock()
	since := clk.Now()
	until := since.Add(60 * time.Second)

	ticker := &AlignedTicker{
		interval:    interval,
		jitter:      jitter,
		offset:      offset,
		minInterval: interval / 100,
	}
	ticker.start(since, clk)
	defer ticker.Stop()

	expected := []time.Time{
		time.Unix(10, 0).UTC(),
		time.Unix(20, 0).UTC(),
		time.Unix(30, 0).UTC(),
		time.Unix(40, 0).UTC(),
		time.Unix(50, 0).UTC(),
		time.Unix(60, 0).UTC(),
	}

	actual := make([]time.Time, 0)
	clk.Add(10 * time.Second)
	for !clk.Now().After(until) {
		tm := <-ticker.Elapsed()
		actual = append(actual, tm.UTC())

		clk.Add(10 * time.Second)
	}

	require.Equal(t, expected, actual)
}

func TestAlignedTickerJitter(t *testing.T) {
	interval := 10 * time.Second
	jitter := 5 * time.Second
	offset := 0 * time.Second

	clk := clock.NewMock()
	since := clk.Now()
	until := since.Add(61 * time.Second)

	ticker := &AlignedTicker{
		interval:    interval,
		jitter:      jitter,
		offset:      offset,
		minInterval: interval / 100,
	}
	ticker.start(since, clk)
	defer ticker.Stop()

	last := since
	for !clk.Now().After(until) {
		select {
		case tm := <-ticker.Elapsed():
			dur := tm.Sub(last)
			// 10s interval + 5s jitter + up to 1s late firing.
			require.LessOrEqual(t, dur, 16*time.Second, "expected elapsed time to be less than 16 seconds, but was %s", dur)
			require.GreaterOrEqual(t, dur, 5*time.Second, "expected elapsed time to be more than 5 seconds, but was %s", dur)
			last = last.Add(interval)
		default:
		}
		clk.Add(1 * time.Second)
	}
}

func TestAlignedTickerOffset(t *testing.T) {
	interval := 10 * time.Second
	jitter := 0 * time.Second
	offset := 3 * time.Second

	clk := clock.NewMock()
	since := clk.Now()
	until := since.Add(61 * time.Second)

	ticker := &AlignedTicker{
		interval:    interval,
		jitter:      jitter,
		offset:      offset,
		minInterval: interval / 100,
	}
	ticker.start(since, clk)
	defer ticker.Stop()

	expected := []time.Time{
		time.Unix(13, 0).UTC(),
		time.Unix(23, 0).UTC(),
		time.Unix(33, 0).UTC(),
		time.Unix(43, 0).UTC(),
		time.Unix(53, 0).UTC(),
	}

	actual := make([]time.Time, 0)
	clk.Add(10*time.Second + offset)
	for !clk.Now().After(until) {
		tm := <-ticker.Elapsed()
		actual = append(actual, tm.UTC())
		clk.Add(10 * time.Second)
	}

	require.Equal(t, expected, actual)
}

func TestAlignedTickerMissedTick(t *testing.T) {
	interval := 10 * time.Second
	jitter := 0 * time.Second
	offset := 0 * time.Second

	clk := clock.NewMock()
	since := clk.Now()

	ticker := &AlignedTicker{
		interval:    interval,
		jitter:      jitter,
		offset:      offset,
		minInterval: interval / 100,
	}
	ticker.start(since, clk)
	defer ticker.Stop()

	clk.Add(25 * time.Second)
	tm := <-ticker.Elapsed()
	require.Equal(t, time.Unix(10, 0).UTC(), tm.UTC())
	clk.Add(5 * time.Second)
	tm = <-ticker.Elapsed()
	require.Equal(t, time.Unix(30, 0).UTC(), tm.UTC())
}

func TestUnalignedTicker(t *testing.T) {
	interval := 10 * time.Second
	jitter := 0 * time.Second
	offset := 0 * time.Second

	clk := clock.NewMock()
	clk.Add(1 * time.Second)
	since := clk.Now()
	until := since.Add(60 * time.Second)

	ticker := &UnalignedTicker{
		interval: interval,
		jitter:   jitter,
		offset:   offset,
	}
	ticker.start(clk)
	defer ticker.Stop()

	expected := []time.Time{
		time.Unix(1, 0).UTC(),
		time.Unix(11, 0).UTC(),
		time.Unix(21, 0).UTC(),
		time.Unix(31, 0).UTC(),
		time.Unix(41, 0).UTC(),
		time.Unix(51, 0).UTC(),
		time.Unix(61, 0).UTC(),
	}

	actual := make([]time.Time, 0)
	for !clk.Now().After(until) {
		select {
		case tm := <-ticker.Elapsed():
			actual = append(actual, tm.UTC())
		default:
		}
		clk.Add(10 * time.Second)
	}

	require.Equal(t, expected, actual)
}

func TestRollingTicker(t *testing.T) {
	interval := 10 * time.Second
	jitter := 0 * time.Second
	offset := 0 * time.Second

	clk := clock.NewMock()
	clk.Add(1 * time.Second)
	since := clk.Now()
	until := since.Add(60 * time.Second)

	ticker := &UnalignedTicker{
		interval: interval,
		jitter:   jitter,
		offset:   offset,
	}
	ticker.start(clk)
	defer ticker.Stop()

	expected := []time.Time{
		time.Unix(1, 0).UTC(),
		time.Unix(11, 0).UTC(),
		time.Unix(21, 0).UTC(),
		time.Unix(31, 0).UTC(),
		time.Unix(41, 0).UTC(),
		time.Unix(51, 0).UTC(),
		time.Unix(61, 0).UTC(),
	}

	actual := make([]time.Time, 0)
	for !clk.Now().After(until) {
		select {
		case tm := <-ticker.Elapsed():
			actual = append(actual, tm.UTC())
		default:
		}
		clk.Add(10 * time.Second)
	}

	require.Equal(t, expected, actual)
}

// Simulates running the Ticker for an hour and displays stats about the
// operation.
func TestAlignedTickerDistribution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	interval := 10 * time.Second
	jitter := 5 * time.Second
	offset := 0 * time.Second

	clk := clock.NewMock()
	since := clk.Now()

	ticker := &AlignedTicker{
		interval:    interval,
		jitter:      jitter,
		offset:      offset,
		minInterval: interval / 100,
	}
	ticker.start(since, clk)
	defer ticker.Stop()
	dist := simulatedDist(ticker, clk)
	printDist(dist)
	require.Less(t, 350, dist.Count)
	require.True(t, 9 < dist.Mean() && dist.Mean() < 11)
}

func TestAlignedTickerDistributionWithOffset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	interval := 10 * time.Second
	jitter := 5 * time.Second
	offset := 3 * time.Second

	clk := clock.NewMock()
	since := clk.Now()

	ticker := &AlignedTicker{
		interval:    interval,
		jitter:      jitter,
		offset:      offset,
		minInterval: interval / 100,
	}
	ticker.start(since, clk)
	defer ticker.Stop()
	dist := simulatedDist(ticker, clk)
	printDist(dist)
	require.Less(t, 350, dist.Count)
	require.True(t, 9 < dist.Mean() && dist.Mean() < 11)
}

// Simulates running the Ticker for an hour and displays stats about the
// operation.
func TestUnalignedTickerDistribution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	interval := 10 * time.Second
	jitter := 5 * time.Second
	offset := 0 * time.Second

	clk := clock.NewMock()

	ticker := &UnalignedTicker{
		interval: interval,
		jitter:   jitter,
		offset:   offset,
	}
	ticker.start(clk)
	defer ticker.Stop()
	dist := simulatedDist(ticker, clk)
	printDist(dist)
	require.Less(t, 350, dist.Count)
	require.True(t, 9 < dist.Mean() && dist.Mean() < 11)
}

func TestUnalignedTickerDistributionWithOffset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	interval := 10 * time.Second
	jitter := 5 * time.Second
	offset := 3 * time.Second

	clk := clock.NewMock()

	ticker := &UnalignedTicker{
		interval: interval,
		jitter:   jitter,
		offset:   offset,
	}
	ticker.start(clk)
	defer ticker.Stop()
	dist := simulatedDist(ticker, clk)
	printDist(dist)
	require.Less(t, 350, dist.Count)
	require.True(t, 9 < dist.Mean() && dist.Mean() < 11)
}

// Simulates running the Ticker for an hour and displays stats about the
// operation.
func TestRollingTickerDistribution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode.")
	}

	interval := 10 * time.Second
	jitter := 5 * time.Second

	clk := clock.NewMock()

	ticker := &RollingTicker{
		interval: interval,
		jitter:   jitter,
	}
	ticker.start(clk)
	defer ticker.Stop()
	dist := simulatedDist(ticker, clk)
	printDist(dist)
	require.Less(t, 275, dist.Count)
	require.True(t, 12 < dist.Mean() && 13 > dist.Mean())
}

type Distribution struct {
	Buckets  [60]int
	Count    int
	Waittime float64
}

func (d *Distribution) Mean() float64 {
	return d.Waittime / float64(d.Count)
}

func printDist(dist Distribution) {
	for i, count := range dist.Buckets {
		fmt.Printf("%2d %s\n", i, strings.Repeat("x", count))
	}
	fmt.Printf("Average interval: %f\n", dist.Mean())
	fmt.Printf("Count: %d\n", dist.Count)
}

func simulatedDist(ticker Ticker, clk *clock.Mock) Distribution {
	since := clk.Now()
	until := since.Add(1 * time.Hour)

	var dist Distribution

	last := clk.Now()
	for !clk.Now().After(until) {
		select {
		case tm := <-ticker.Elapsed():
			dist.Buckets[tm.Second()]++
			dist.Count++
			dist.Waittime += tm.Sub(last).Seconds()
			last = tm
		default:
			clk.Add(1 * time.Second)
		}
	}

	return dist
}
