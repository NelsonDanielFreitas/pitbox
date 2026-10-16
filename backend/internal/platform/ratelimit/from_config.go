package ratelimit

import (
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
)

// FromConfig converts the application configuration into a ratelimit Config.
func FromConfig(cfg config.Config) Config {
	return Config{
		Limits: map[Kind]Limit{
			KindUserRead:  {RPS: cfg.RateLimitUserReadRPS, Burst: cfg.RateLimitUserReadBurst},
			KindUserWrite: {RPS: cfg.RateLimitUserWriteRPS, Burst: cfg.RateLimitUserWriteBurst},
			KindTeamRead:  {RPS: cfg.RateLimitTeamReadRPS, Burst: cfg.RateLimitTeamReadBurst},
			KindTeamWrite: {RPS: cfg.RateLimitTeamWriteRPS, Burst: cfg.RateLimitTeamWriteBurst},
			KindPublicRead: {
				RPS:   cfg.RateLimitPublicRPS,
				Burst: cfg.RateLimitPublicBurst,
			},
			KindPublicWrite: {
				RPS:   cfg.RateLimitPublicRPS,
				Burst: cfg.RateLimitPublicBurst,
			},
		},
		ValkeyTimeout: cfg.RateLimitValkeyTimeout,
	}
}

// DefaultConfig returns the default configuration. It is useful for tests.
func DefaultConfig() Config {
	return Config{
		Limits: map[Kind]Limit{
			KindUserRead:    {RPS: 20, Burst: 30},
			KindUserWrite:   {RPS: 5, Burst: 10},
			KindTeamRead:    {RPS: 100, Burst: 150},
			KindTeamWrite:   {RPS: 20, Burst: 40},
			KindPublicRead:  {RPS: 10, Burst: 20},
			KindPublicWrite: {RPS: 10, Burst: 20},
		},
		ValkeyTimeout: 50 * time.Millisecond,
	}
}
