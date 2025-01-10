package ratelimiter

import (
	"math"
	"testing"
	"time"

	"github.com/influxdata/telegraf/config"
	"github.com/stretchr/testify/require"
)

func TestInvalidPeriod(t *testing.T) {
	cfg := &RateLimitConfig{Limit: config.Size(1024)}
	_, err := cfg.CreateRateLimiter()
	require.ErrorContains(t, err, "invalid period for rate-limit")
}

func TestUnlimited(t *testing.T) {
	cfg := &RateLimitConfig{}
	limiter, err := cfg.CreateRateLimiter()
	require.NoError(t, err)

	start := time.Now()
	end := start.Add(30 * time.Minute)
	for ts := start; ts.Before(end); ts = ts.Add(1 * time.Minute) {
		require.EqualValues(t, int64(math.MaxInt64), limiter.Remaining(ts))
	}
}

func TestUnlimitedWithPeriod(t *testing.T) {
	cfg := &RateLimitConfig{
		Period: config.Duration(5 * time.Minute),
	}
	limiter, err := cfg.CreateRateLimiter()
	require.NoError(t, err)

	start := time.Now()
	end := start.Add(30 * time.Minute)
	for ts := start; ts.Before(end); ts = ts.Add(1 * time.Minute) {
		require.EqualValues(t, int64(math.MaxInt64), limiter.Remaining(ts))
	}
}

func TestLimited(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *RateLimitConfig
		step     time.Duration
		request  []int64
		expected []int64
	}{
		{
			name: "constant usage",
			cfg: &RateLimitConfig{
				Limit:  config.Size(1024),
				Period: config.Duration(5 * time.Minute),
			},
			step:     time.Minute,
			request:  []int64{300},
			expected: []int64{1024, 724, 424, 124, 0, 1024, 724, 424, 124, 0},
		},
		{
			name: "variable usage",
			cfg: &RateLimitConfig{
				Limit:  config.Size(1024),
				Period: config.Duration(5 * time.Minute),
			},
			step:     time.Minute,
			request:  []int64{256, 128, 512, 64, 64, 1024, 0, 0, 0, 0, 128, 4096, 4096, 4096, 4096, 4096},
			expected: []int64{1024, 768, 640, 128, 64, 1024, 0, 0, 0, 0, 1024, 896, 0, 0, 0, 1024},
		},
	}

	// Run the test with an offset of period multiples
	for _, tt := range tests {
		t.Run(tt.name+" at period", func(t *testing.T) {
			// Setup the limiter
			limiter, err := tt.cfg.CreateRateLimiter()
			require.NoError(t, err)

			// Compute the actual values
			start := time.Now().Truncate(tt.step)
			for i, expected := range tt.expected {
				ts := start.Add(time.Duration(i) * tt.step)
				remaining := limiter.Remaining(ts)
				use := min(remaining, tt.request[i%len(tt.request)])
				require.Equalf(t, expected, remaining, "mismatch at index %d", i)
				limiter.Accept(ts, use)
			}
		})
	}

	// Run the test at a time of period multiples
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup the limiter
			limiter, err := tt.cfg.CreateRateLimiter()
			require.NoError(t, err)

			// Compute the actual values
			start := time.Now().Truncate(tt.step).Add(1 * time.Second)
			for i, expected := range tt.expected {
				ts := start.Add(time.Duration(i) * tt.step)
				remaining := limiter.Remaining(ts)
				use := min(remaining, tt.request[i%len(tt.request)])
				require.Equalf(t, expected, remaining, "mismatch at index %d", i)
				limiter.Accept(ts, use)
			}
		})
	}
}

func TestUndo(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *RateLimitConfig
		step     time.Duration
		request  []int64
		expected []int64
	}{
		{
			name: "constant usage",
			cfg: &RateLimitConfig{
				Limit:  config.Size(1024),
				Period: config.Duration(5 * time.Minute),
			},
			step:     time.Minute,
			request:  []int64{300},
			expected: []int64{1024, 724, 424, 124, 124, 1024, 724, 424, 124, 124},
		},
		{
			name: "variable usage",
			cfg: &RateLimitConfig{
				Limit:  config.Size(1024),
				Period: config.Duration(5 * time.Minute),
			},
			step:     time.Minute,
			request:  []int64{256, 128, 512, 64, 64, 1024, 0, 0, 0, 0, 128, 4096, 4096, 4096, 4096, 4096},
			expected: []int64{1024, 768, 640, 128, 64, 1024, 0, 0, 0, 0, 1024, 896, 896, 896, 896, 1024},
		},
	}

	// Run the test with an offset of period multiples
	for _, tt := range tests {
		t.Run(tt.name+" at period", func(t *testing.T) {
			// Setup the limiter
			limiter, err := tt.cfg.CreateRateLimiter()
			require.NoError(t, err)

			// Compute the actual values
			start := time.Now().Truncate(tt.step)
			for i, expected := range tt.expected {
				ts := start.Add(time.Duration(i) * tt.step)
				remaining := limiter.Remaining(ts)
				use := min(remaining, tt.request[i%len(tt.request)])
				require.Equalf(t, expected, remaining, "mismatch at index %d", i)
				limiter.Accept(ts, use)
				// Undo too large operations
				if tt.request[i%len(tt.request)] > remaining {
					limiter.Undo(ts, use)
				}
			}
		})
	}

	// Run the test at a time of period multiples
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup the limiter
			limiter, err := tt.cfg.CreateRateLimiter()
			require.NoError(t, err)

			// Compute the actual values
			start := time.Now().Truncate(tt.step).Add(1 * time.Second)
			for i, expected := range tt.expected {
				ts := start.Add(time.Duration(i) * tt.step)
				remaining := limiter.Remaining(ts)
				use := min(remaining, tt.request[i%len(tt.request)])
				require.Equalf(t, expected, remaining, "mismatch at index %d", i)
				limiter.Accept(ts, use)
				// Undo too large operations
				if tt.request[i%len(tt.request)] > remaining {
					limiter.Undo(ts, use)
				}
			}
		})
	}
}
