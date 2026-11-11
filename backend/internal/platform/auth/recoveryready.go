package auth

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth/principal"
)

// Limits of the recovery readiness check (#158, ADR 0034).
const (
	readinessTTL     = time.Minute
	readinessMax     = 1024
	readinessTimeout = 2 * time.Second
)

// readinessCache remembers the answer per user for a short time, so /auth/session (asked at
// every app start) does not go to the database and the identity provider each time. Bounded:
// a full table is emptied, which only costs a recomputation.
type readinessCache struct {
	mu sync.Mutex
	m  map[string]readinessEntry
}

type readinessEntry struct {
	ready   bool
	expires time.Time
}

func (c *readinessCache) get(userID string, now time.Time) (ready, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, found := c.m[userID]
	if !found || !now.Before(e.expires) {
		return false, false
	}
	return e.ready, true
}

func (c *readinessCache) put(userID string, ready bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) >= readinessMax {
		c.m = make(map[string]readinessEntry, 16)
	}
	c.m[userID] = readinessEntry{ready: ready, expires: now.Add(readinessTTL)}
}

func (c *readinessCache) forget(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, userID)
}

// recoveryReady reports whether the account can get back in if it loses its only passkey:
// it has unused recovery codes, a password, or no passkey at all. It is false only for an
// account whose sole credential is a passkey and that has no recovery code. nil means "not
// known" (the feature is off or something failed): the app shows no warning then, and the
// sign-in is never held up by this check.
func (h *Handler) recoveryReady(ctx context.Context, p principal.Principal) *bool {
	if h.Accounts == nil || h.RecoveryCodes == nil || p.UserID == "" || p.Subject == "" {
		return nil
	}
	now := h.Now()
	if ready, ok := h.readiness.get(p.UserID, now); ok {
		return &ready
	}
	ctx, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()
	ready, err := h.computeRecoveryReady(ctx, p)
	if err != nil {
		h.Log.WarnContext(ctx, "recovery readiness unknown", slog.String("reason", "lookup"))
		return nil
	}
	h.readiness.put(p.UserID, ready, now)
	return &ready
}

func (h *Handler) computeRecoveryReady(ctx context.Context, p principal.Principal) (bool, error) {
	n, err := h.RecoveryCodes.CountUnused(ctx, p.UserID)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	methods, err := h.Accounts.AuthMethods(ctx, p.Subject)
	if err != nil {
		return false, err
	}
	return !methods.Passkey || methods.Password, nil
}
