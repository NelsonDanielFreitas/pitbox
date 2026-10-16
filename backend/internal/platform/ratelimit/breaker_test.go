package ratelimit

import (
	"testing"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/stretchr/testify/require"
)

func TestBreakerStartsClosed(t *testing.T) {
	t.Parallel()

	b := newBreaker(clock.Real(), 3, time.Second)
	require.True(t, b.allow())
}

func TestBreakerOpensAfterFailures(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(time.Now())
	b := newBreaker(clk, 3, time.Second)

	b.recordFailure()
	b.recordFailure()
	require.True(t, b.allow(), "still closed after 2 failures")
	b.recordFailure()
	require.False(t, b.allow(), "open after 3 failures")
}

func TestBreakerHalfOpensAfterTimeout(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(time.Now())
	b := newBreaker(clk, 3, 5*time.Second)

	b.recordFailure()
	b.recordFailure()
	b.recordFailure()
	require.False(t, b.allow())

	clk.Add(5 * time.Second)
	require.True(t, b.allow(), "half-open after timeout")
	require.Equal(t, breakerHalfOpen, b.currentState())
}

func TestBreakerClosesOnSuccessInHalfOpen(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(time.Now())
	b := newBreaker(clk, 3, 5*time.Second)

	b.recordFailure()
	b.recordFailure()
	b.recordFailure()
	clk.Add(5 * time.Second)
	require.True(t, b.allow())
	b.recordSuccess()
	require.Equal(t, breakerClosed, b.currentState())
	require.True(t, b.allow())
}

func TestBreakerReopensOnFailureInHalfOpen(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(time.Now())
	b := newBreaker(clk, 3, 5*time.Second)

	b.recordFailure()
	b.recordFailure()
	b.recordFailure()
	clk.Add(5 * time.Second)
	require.True(t, b.allow())
	b.recordFailure()
	require.Equal(t, breakerOpen, b.currentState())
	require.False(t, b.allow())
}

func TestBreakerSuccessResetsFailureCount(t *testing.T) {
	t.Parallel()

	clk := clock.NewFake(time.Now())
	b := newBreaker(clk, 3, time.Second)

	b.recordFailure()
	b.recordFailure()
	b.recordSuccess()
	b.recordFailure()
	b.recordFailure()
	require.True(t, b.allow(), "still closed because count was reset")
}
