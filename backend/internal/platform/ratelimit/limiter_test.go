package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
)

func newTestLimiter(t *testing.T, cfg Config) *Limiter {
	t.Helper()
	vk := testutil.StartValkey(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{vk.Addr},
		Username:     vk.Username,
		Password:     vk.Password,
		DisableCache: true,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)

	l, err := NewLimiter(client, cfg, metricnoop.NewMeterProvider().Meter("test"), clock.Real(), nil)
	require.NoError(t, err)
	return l
}

func TestNewLimiterRejectsInvalidRPS(t *testing.T) {
	t.Parallel()

	vk := testutil.StartValkey(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{vk.Addr},
		Username:     vk.Username,
		Password:     vk.Password,
		DisableCache: true,
	})
	require.NoError(t, err)
	defer client.Close()

	cfg := DefaultConfig()
	cfg.Limits[KindUserRead] = Limit{RPS: 1001, Burst: 1001}
	_, err = NewLimiter(client, cfg, metricnoop.NewMeterProvider().Meter("test"), clock.Real(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "RPS")
}

func TestLimiterTeamIsolation(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Limits[KindTeamRead] = Limit{RPS: 5, Burst: 5}
	l := newTestLimiter(t, cfg)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.Equal(t, Allowed, l.Allow(ctx, ScopeTeam, "team-a", false))
	}
	require.Equal(t, RateLimited, l.Allow(ctx, ScopeTeam, "team-a", false))

	for i := 0; i < 5; i++ {
		require.Equal(t, Allowed, l.Allow(ctx, ScopeTeam, "team-b", false), "team-b request %d", i+1)
	}
}

func TestLimiterUserIsolation(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Limits[KindUserRead] = Limit{RPS: 5, Burst: 5}
	l := newTestLimiter(t, cfg)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.Equal(t, Allowed, l.Allow(ctx, ScopeUser, "user-x", false))
	}
	require.Equal(t, RateLimited, l.Allow(ctx, ScopeUser, "user-x", false))
	require.Equal(t, Allowed, l.Allow(ctx, ScopeUser, "user-y", false), "user-y must not be limited")
}

func TestLimiterReadWriteDifferentQuotas(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Limits[KindUserRead] = Limit{RPS: 10, Burst: 10}
	cfg.Limits[KindUserWrite] = Limit{RPS: 2, Burst: 2}
	l := newTestLimiter(t, cfg)
	ctx := context.Background()

	require.Equal(t, Allowed, l.Allow(ctx, ScopeUser, "u", true))
	require.Equal(t, Allowed, l.Allow(ctx, ScopeUser, "u", true))
	require.Equal(t, RateLimited, l.Allow(ctx, ScopeUser, "u", true), "write burst exhausted")
	require.Equal(t, Allowed, l.Allow(ctx, ScopeUser, "u", false), "read still allowed")
}

func TestLimiterValkeyDownFallsBackToLocal(t *testing.T) {
	t.Parallel()

	vk := testutil.StartValkey(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{vk.Addr},
		Username:     vk.Username,
		Password:     vk.Password,
		DisableCache: true,
	})
	require.NoError(t, err)

	cfg := DefaultConfig()
	// Keep limits small so the test stays fast; local limit is 2x (burst 4).
	cfg.Limits[KindUserRead] = Limit{RPS: 1, Burst: 2}
	cfg.ValkeyTimeout = 50 * time.Millisecond
	clk := clock.NewFake(time.Now())
	l, err := NewLimiter(client, cfg, metricnoop.NewMeterProvider().Meter("test"), clk, nil)
	require.NoError(t, err)
	ctx := context.Background()

	client.Close()

	// The local layer (burst 4) still protects even when Valkey is unreachable.
	require.Equal(t, Allowed, l.Allow(ctx, ScopeUser, "u", false), "first local request allowed")
	require.Equal(t, Allowed, l.Allow(ctx, ScopeUser, "u", false), "second local request allowed")
	require.Equal(t, Allowed, l.Allow(ctx, ScopeUser, "u", false), "third local request allowed")
	require.Equal(t, Allowed, l.Allow(ctx, ScopeUser, "u", false), "fourth local request allowed")
	require.Equal(t, RateLimited, l.Allow(ctx, ScopeUser, "u", false), "fifth local request rejected")

	// Other users are isolated.
	require.Equal(t, Allowed, l.Allow(ctx, ScopeUser, "other", false), "other user not affected")
}

func TestLimiterPublicFailsClosedWhenValkeyDown(t *testing.T) {
	t.Parallel()

	vk := testutil.StartValkey(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:  []string{vk.Addr},
		Username:     vk.Username,
		Password:     vk.Password,
		DisableCache: true,
	})
	require.NoError(t, err)

	cfg := DefaultConfig()
	cfg.ValkeyTimeout = 50 * time.Millisecond
	l, err := NewLimiter(client, cfg, metricnoop.NewMeterProvider().Meter("test"), clock.Real(), nil)
	require.NoError(t, err)
	ctx := context.Background()

	client.Close()
	for i := 0; i < 5; i++ {
		l.Allow(ctx, ScopePublic, "proc", false)
	}

	require.Equal(t, Unavailable, l.Allow(ctx, ScopePublic, "proc", false))
}

func TestLimiterConcurrentRequestsDoNotExceedQuota(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	cfg.Limits[KindTeamWrite] = Limit{RPS: 10, Burst: 10}
	l := newTestLimiter(t, cfg)
	ctx := context.Background()

	const workers = 20
	allowed := make(chan Decision, workers)
	for i := 0; i < workers; i++ {
		go func() {
			allowed <- l.Allow(ctx, ScopeTeam, "team-concurrent", true)
		}()
	}

	allowCount := 0
	for i := 0; i < workers; i++ {
		if <-allowed == Allowed {
			allowCount++
		}
	}
	require.LessOrEqual(t, allowCount, 10, "concurrent requests must not exceed burst")
}
