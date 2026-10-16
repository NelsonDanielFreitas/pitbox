package ratelimit

import (
	"sync"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
)

// localLimiter is an in-memory token bucket. It protects a single process; a fleet of
// pitbox-api instances needs the Valkey layer for a shared, stricter limit.
type localLimiter struct {
	mu      sync.Mutex
	buckets map[bucketKey]*bucket
	clock   clock.Clock

	// cleanupEvery controls how often stale buckets are removed. It is not a constant
	// so tests can lower it.
	cleanupEvery int
	calls        int
}

type bucketKey struct {
	kind Kind
	id   string
}

type bucket struct {
	tokens   float64
	lastTime time.Time
}

// staleAfter is how long a bucket must be idle before it is eligible for cleanup.
const staleAfter = 5 * time.Minute

func newLocalLimiter(clk clock.Clock) *localLimiter {
	return &localLimiter{
		buckets:      make(map[bucketKey]*bucket),
		clock:        clk,
		cleanupEvery: 1000,
	}
}

// allow consumes one token if available. It is safe for concurrent use.
func (l *localLimiter) allow(kind Kind, id string, limit Limit) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := bucketKey{kind: kind, id: id}
	now := l.clock.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(limit.Burst), lastTime: now}
		l.buckets[key] = b
	}

	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens = min(float64(limit.Burst), b.tokens+elapsed*float64(limit.RPS))
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	b.lastTime = now

	l.calls++
	if l.calls >= l.cleanupEvery {
		l.cleanup(now)
		l.calls = 0
	}
	return true
}

func (l *localLimiter) cleanup(now time.Time) {
	threshold := now.Add(-staleAfter)
	for k, b := range l.buckets {
		if b.lastTime.Before(threshold) {
			delete(l.buckets, k)
		}
	}
}
