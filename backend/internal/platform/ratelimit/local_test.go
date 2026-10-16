package ratelimit

import (
	"testing"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/stretchr/testify/require"
)

func TestLocalLimiterAllowsUpToBurst(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(time.Now())
	l := newLocalLimiter(clk)
	limit := Limit{RPS: 5, Burst: 10}

	for i := 0; i < 10; i++ {
		require.True(t, l.allow(KindUserRead, "u1", limit), "request %d", i+1)
	}
	require.False(t, l.allow(KindUserRead, "u1", limit), "request over burst")
}

func TestLocalLimiterRefusesDifferentUsersIndependently(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(time.Now())
	l := newLocalLimiter(clk)
	limit := Limit{RPS: 5, Burst: 10}

	for i := 0; i < 10; i++ {
		require.True(t, l.allow(KindUserRead, "u1", limit))
	}
	require.False(t, l.allow(KindUserRead, "u1", limit))
	require.True(t, l.allow(KindUserRead, "u2", limit), "u2 must not be affected by u1")
}

func TestLocalLimiterRefillsOverTime(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(time.Now())
	l := newLocalLimiter(clk)
	limit := Limit{RPS: 10, Burst: 10}

	for i := 0; i < 10; i++ {
		require.True(t, l.allow(KindUserRead, "u1", limit))
	}
	require.False(t, l.allow(KindUserRead, "u1", limit))

	clk.Add(100 * time.Millisecond) // 1 token at 10 RPS
	require.True(t, l.allow(KindUserRead, "u1", limit))
	require.False(t, l.allow(KindUserRead, "u1", limit))
}

func TestLocalLimiterDifferentKindsAreIndependent(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(time.Now())
	l := newLocalLimiter(clk)
	limit := Limit{RPS: 5, Burst: 5}

	for i := 0; i < 5; i++ {
		require.True(t, l.allow(KindUserRead, "u1", limit))
	}
	require.False(t, l.allow(KindUserRead, "u1", limit))
	require.True(t, l.allow(KindUserWrite, "u1", limit), "write must not consume read tokens")
}
