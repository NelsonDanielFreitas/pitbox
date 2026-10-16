package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/valkey-io/valkey-go"
)

// Limiter is the three-layer rate limiter.
type Limiter struct {
	cfg         Config
	localLimits map[Kind]Limit
	local       *localLimiter
	valkey      *valkeyLimiter
	breaker     *breaker
	metrics     *metrics
	log         *slog.Logger
	clock       clock.Clock
}

// localMultiplier makes the in-memory bucket wider than the distributed Valkey limit.
// It is a safety net: when Valkey is healthy, Valkey is the limiting layer; when Valkey
// is down, the local layer still protects the instance, but with a higher ceiling.
const localMultiplier = 2

// NewLimiter builds a Limiter. The Valkey client is shared with the session store; the
// ACL confines it to pitbox:rl:*.
func NewLimiter(client valkey.Client, cfg Config, meter metric.Meter, clk clock.Clock, log *slog.Logger) (*Limiter, error) {
	if client == nil {
		return nil, fmt.Errorf("ratelimit: valkey client is required")
	}
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}

	brk := newBreaker(clk, 5, 5*time.Second)
	m, err := newMetrics(meter, brk)
	if err != nil {
		return nil, fmt.Errorf("ratelimit metrics: %w", err)
	}

	localLimits := make(map[Kind]Limit, len(cfg.Limits))
	for k, lim := range cfg.Limits {
		localLimits[k] = Limit{RPS: lim.RPS * localMultiplier, Burst: lim.Burst * localMultiplier}
	}

	return &Limiter{
		cfg:         cfg,
		localLimits: localLimits,
		local:       newLocalLimiter(clk),
		valkey:      newValkeyLimiter(client),
		breaker:     brk,
		metrics:     m,
		log:         log,
		clock:       clk,
	}, nil
}

// maxRPS caps the configured rate. Above 1000 RPS the millisecond-based GCRA script rounds
// the increment down to zero, breaking the algorithm. If higher rates are needed later, the
// script should switch to microsecond precision.
const maxRPS = 1000

func validateConfig(cfg Config) error {
	for k := KindUserRead; k <= KindPublicWrite; k++ {
		if _, ok := cfg.Limits[k]; !ok {
			return fmt.Errorf("ratelimit: missing limit for %s", k)
		}
		lim := cfg.Limits[k]
		if lim.RPS <= 0 || lim.RPS > maxRPS {
			return fmt.Errorf("ratelimit: RPS for %s must be between 1 and %d", k, maxRPS)
		}
		if lim.Burst <= 0 || lim.Burst < lim.RPS {
			return fmt.Errorf("ratelimit: burst for %s must be at least the RPS", k)
		}
	}
	if cfg.ValkeyTimeout <= 0 {
		return fmt.Errorf("ratelimit: ValkeyTimeout must be positive")
	}
	return nil
}

// Allow checks the three layers and returns a Decision.
//
//   - ScopeUser/ScopeTeam: local bucket runs first; if it rejects, RateLimited. Valkey GCRA
//     runs if the circuit is closed; on Valkey error, the local decision stands.
//   - ScopePublic: no local bucket; Valkey must answer. If Valkey is unavailable,
//     Unavailable is returned (fail closed).
func (l *Limiter) Allow(ctx context.Context, scope Scope, id string, write bool) Decision {
	kind := kindFor(scope, write)
	limit := l.cfg.Limits[kind]

	// Layer 1: local token bucket (not for public endpoints). Wider than Valkey.
	if scope != ScopePublic {
		if !l.local.allow(kind, id, l.localLimits[kind]) {
			l.metrics.recordRejection(scope, write, "local")
			return RateLimited
		}
	}

	// Layer 2 & 3: Valkey GCRA with circuit breaker.
	if l.breaker.allow() {
		vctx, cancel := context.WithTimeout(ctx, l.cfg.ValkeyTimeout)
		defer cancel()

		allowed, err := l.valkey.allow(vctx, kind, id, limit, l.clock.Now())
		if err != nil {
			l.breaker.recordFailure()
			l.metrics.recordValkeyError()
			l.log.WarnContext(ctx, "valkey rate limit unavailable",
				slog.String("scope", scope.String()),
				slog.String("kind", kind.String()))
			if scope == ScopePublic {
				return Unavailable
			}
			return Allowed
		}
		l.breaker.recordSuccess()
		if !allowed {
			l.metrics.recordRejection(scope, write, "valkey")
			return RateLimited
		}
	} else {
		// Circuit is open.
		if scope == ScopePublic {
			return Unavailable
		}
		// Authenticated requests rely on the local decision already made.
	}

	return Allowed
}
