package statsd

import (
	"math"
	"math/rand"
	"sort"
)

const defaultPercentileLimit = 1000
const defaultMedianLimit = 1000

// runningStats calculates a running mean, variance, standard deviation,
// lower bound, upper bound, count, and can calculate estimated percentiles.
// It is based on the incremental algorithm described here:
//
//	https://en.wikipedia.org/wiki/Algorithms_for_calculating_variance
type runningStats struct {
	k   float64
	n   int64
	ex  float64
	ex2 float64

	// Array used to calculate estimated percentiles
	// We will store a maximum of percLimit values, at which point we will start
	// randomly replacing old values, hence it is an estimated percentile.
	perc      []float64
	percLimit int

	totalSum float64

	lowerBound float64
	upperBound float64

	// cache if we have sorted the list so that we never re-sort a sorted list,
	// which can have very bad performance.
	sortedPerc bool

	// Array used to calculate estimated median values
	// We will store a maximum of medLimit values, at which point we will start
	// slicing old values
	med            []float64
	medLimit       int
	medInsertIndex int
}

func (rs *runningStats) addValue(v float64) {
	// Whenever a value is added, the list is no longer sorted.
	rs.sortedPerc = false

	if rs.n == 0 {
		rs.k = v
		rs.upperBound = v
		rs.lowerBound = v
		if rs.percLimit == 0 {
			rs.percLimit = defaultPercentileLimit
		}
		if rs.medLimit == 0 {
			rs.medLimit = defaultMedianLimit
			rs.medInsertIndex = 0
		}
		rs.perc = make([]float64, 0, rs.percLimit)
		rs.med = make([]float64, 0, rs.medLimit)
	}

	// These are used for the running mean and variance
	rs.n++
	rs.ex += v - rs.k
	rs.ex2 += (v - rs.k) * (v - rs.k)

	// add to running sum
	rs.totalSum += v

	// track upper and lower bounds
	if v > rs.upperBound {
		rs.upperBound = v
	} else if v < rs.lowerBound {
		rs.lowerBound = v
	}

	if len(rs.perc) < rs.percLimit {
		rs.perc = append(rs.perc, v)
	} else {
		// Reached limit, choose random index to overwrite in the percentile array
		rs.perc[rand.Intn(len(rs.perc))] = v //nolint:gosec // G404: not security critical
	}

	if len(rs.med) < rs.medLimit {
		rs.med = append(rs.med, v)
	} else {
		// Reached limit, start over
		rs.med[rs.medInsertIndex] = v
	}
	rs.medInsertIndex = (rs.medInsertIndex + 1) % rs.medLimit
}

func (rs *runningStats) mean() float64 {
	return rs.k + rs.ex/float64(rs.n)
}

func (rs *runningStats) median() float64 {
	// Need to sort for median, but keep temporal order
	var values []float64
	values = append(values, rs.med...)
	sort.Float64s(values)
	count := len(values)
	if count == 0 {
		return 0
	} else if count%2 == 0 {
		return (values[count/2-1] + values[count/2]) / 2
	}
	return values[count/2]
}

func (rs *runningStats) variance() float64 {
	return (rs.ex2 - (rs.ex*rs.ex)/float64(rs.n)) / float64(rs.n)
}

func (rs *runningStats) stddev() float64 {
	return math.Sqrt(rs.variance())
}

func (rs *runningStats) sum() float64 {
	return rs.totalSum
}

func (rs *runningStats) upper() float64 {
	return rs.upperBound
}

func (rs *runningStats) lower() float64 {
	return rs.lowerBound
}

func (rs *runningStats) count() int64 {
	return rs.n
}

func (rs *runningStats) percentile(n float64) float64 {
	if n > 100 {
		n = 100
	}

	if !rs.sortedPerc {
		sort.Float64s(rs.perc)
		rs.sortedPerc = true
	}

	i := float64(len(rs.perc)) * n / float64(100)
	return rs.perc[max(0, min(int(i), len(rs.perc)-1))]
}
