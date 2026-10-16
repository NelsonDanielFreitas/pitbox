// Package ratelimit implements three-layer rate limiting for the API (F1-04b, ADR 0014):
//
//  1. An in-memory token bucket per user/team, always active, wider than the distributed
//     limit so it does not reject traffic Valkey would accept.
//  2. An atomic GCRA check in Valkey, tighter than the local limit; writes are more
//     restrictive than reads (NO_SIDE_EFFECTS).
//  3. A circuit breaker around Valkey: when open, authenticated requests fall back to the
//     local limit; public endpoints fail closed (Unavailable).
//
// The package exposes a single Decision from Limiter.Allow. Callers must not reveal quotas,
// keys or breaker state to clients.
package ratelimit

import "time"

// Scope identifies whose quota is being consumed.
type Scope int

const (
	// ScopeUser limits a single authenticated user.
	ScopeUser Scope = iota
	// ScopeTeam limits the whole team.
	ScopeTeam
	// ScopePublic limits an unauthenticated public endpoint by procedure.
	ScopePublic
)

// Kind is the concrete bucket kind. It maps directly to a configured limit.
type Kind int

const (
	KindUserRead Kind = iota
	KindUserWrite
	KindTeamRead
	KindTeamWrite
	KindPublicRead
	KindPublicWrite
)

// Limit is a rate and a burst.
type Limit struct {
	RPS   int
	Burst int
}

// Config for the three-layer limiter.
type Config struct {
	// Limits maps each Kind to its allowed rate and burst.
	Limits map[Kind]Limit
	// ValkeyTimeout bounds every GCRA call. It is short so a slow Valkey does not
	// starve the API.
	ValkeyTimeout time.Duration
}

// Decision is the outcome of an Allow call.
type Decision int

const (
	// Allowed means the request may proceed.
	Allowed Decision = iota
	// RateLimited means the quota is exhausted. The caller answers ResourceExhausted
	// with a fixed message.
	RateLimited
	// Unavailable means the global limit could not be checked and the scope fails closed.
	// Only ScopePublic returns this; authenticated scopes fall back to the local limit.
	Unavailable
)

func kindFor(scope Scope, write bool) Kind {
	switch scope {
	case ScopeUser:
		if write {
			return KindUserWrite
		}
		return KindUserRead
	case ScopeTeam:
		if write {
			return KindTeamWrite
		}
		return KindTeamRead
	case ScopePublic:
		if write {
			return KindPublicWrite
		}
		return KindPublicRead
	default:
		return KindUserRead
	}
}

func (k Kind) String() string {
	switch k {
	case KindUserRead:
		return "user_read"
	case KindUserWrite:
		return "user_write"
	case KindTeamRead:
		return "team_read"
	case KindTeamWrite:
		return "team_write"
	case KindPublicRead:
		return "public_read"
	case KindPublicWrite:
		return "public_write"
	default:
		return "unknown"
	}
}

func (s Scope) String() string {
	switch s {
	case ScopeUser:
		return "user"
	case ScopeTeam:
		return "team"
	case ScopePublic:
		return "public"
	default:
		return "unknown"
	}
}
