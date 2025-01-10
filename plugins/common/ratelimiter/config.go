package ratelimiter

import (
	"errors"
	"time"

	"github.com/influxdata/telegraf/config"
)

type RateLimitConfig struct {
	Limit  config.Size     `toml:"rate_limit"`
	Period config.Duration `toml:"rate_limit_period"`
}

func (cfg *RateLimitConfig) CreateRateLimiter() (*RateLimiter, error) {
	if cfg.Limit > 0 && cfg.Period <= 0 {
		return nil, errors.New("invalid period for rate-limit")
	}
	return &RateLimiter{
		limit:  int64(cfg.Limit),
		period: time.Duration(cfg.Period),
	}, nil
}
