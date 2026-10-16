package ratelimit

import (
	"sync"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
)

// breaker protects the API from a slow or unreachable Valkey. It starts closed,
// opens after a threshold of consecutive failures, and half-opens after a timeout.
type breaker struct {
	mu          sync.Mutex
	state       breakerState
	failures    int
	lastFailure time.Time
	threshold   int
	timeout     time.Duration
	clock       clock.Clock
}

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

func newBreaker(clk clock.Clock, threshold int, timeout time.Duration) *breaker {
	return &breaker{
		threshold: threshold,
		timeout:   timeout,
		clock:     clk,
	}
}

// allow reports whether a Valkey call may be attempted.
func (b *breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case breakerClosed:
		return true
	case breakerOpen:
		if b.clock.Now().Sub(b.lastFailure) >= b.timeout {
			b.state = breakerHalfOpen
			return true
		}
		return false
	case breakerHalfOpen:
		return true
	}
	return true
}

// recordSuccess resets the failure count.
func (b *breaker) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	if b.state == breakerHalfOpen {
		b.state = breakerClosed
	}
}

// recordFailure increments the failure count and opens the circuit when the threshold is
// reached. In half-open, a single failure reopens the circuit.
func (b *breaker) recordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.failures++
	b.lastFailure = b.clock.Now()

	switch b.state {
	case breakerClosed:
		if b.failures >= b.threshold {
			b.state = breakerOpen
		}
	case breakerHalfOpen:
		b.state = breakerOpen
	}
}

// currentState returns the current breaker state. It is used by the metrics callback.
func (b *breaker) currentState() breakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}
