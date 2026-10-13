package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
)

var (
	// ErrUnauthenticated: no usable session. The caller answers UNAUTHENTICATED.
	ErrUnauthenticated = errors.New("auth: unauthenticated")
	// ErrUnavailable: the session store or the identity provider could not be reached and
	// the grace period is over. The caller answers UNAVAILABLE, not UNAUTHENTICATED: the
	// client must not read a server outage as "you are signed out".
	ErrUnavailable = errors.New("auth: authentication unavailable")
)

const (
	// RefreshGrace is how long a request may keep using a session whose access token has
	// expired while the refresh cannot be completed (another request is doing it, or the
	// identity provider is down). After it, the request is refused.
	RefreshGrace = 2 * time.Minute

	refreshTimeout = 5 * time.Second
)

// Authenticator turns a session cookie into a Principal, refreshing the tokens when the
// access token has expired (ADR 0013).
//
// The session is the credential: the BFF does not forward or re-validate the access token.
// What the 10-minute access token buys is the revocation window: a user disabled at the
// identity provider loses the session at the next refresh, so within 10 minutes.
type Authenticator struct {
	Provider *Provider
	Store    *Store
	Audit    Auditor
	Log      *slog.Logger
	Now      func() time.Time
}

// Authenticate resolves the cookie value to the principal of the request.
func (a *Authenticator) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}
	sess, err := a.Store.Get(ctx, token)
	switch {
	case errors.Is(err, ErrNoSession):
		return Principal{}, ErrUnauthenticated
	case err != nil:
		a.Log.ErrorContext(ctx, "read session", slog.String("reason", "store"))
		return Principal{}, ErrUnavailable
	}
	if a.Now().Before(sess.AccessExpires) {
		return principalOf(sess), nil
	}
	sess, err = a.refresh(ctx, token, sess)
	if err != nil {
		return Principal{}, err
	}
	return principalOf(sess), nil
}

func principalOf(s Session) Principal {
	return Principal{UserID: s.UserID, Subject: s.Subject, SessionRef: s.Ref(), AMR: s.AMR, AuthTime: s.AuthTime}
}

// refresh renews the tokens of a session whose access token has expired. Exactly one of
// the concurrent requests does it (a lock in the store); the others carry on with the
// session they have during the grace period, so a slow identity provider never blocks them
// and, above all, they never present the same (rotated) refresh token twice, which would
// make the provider reject it and sign the user out for nothing.
func (a *Authenticator) refresh(ctx context.Context, token string, sess Session) (Session, error) {
	lock, err := a.Store.TryLockRefresh(ctx, sess)
	if err != nil {
		a.Log.ErrorContext(ctx, "refresh lock", slog.String("reason", "store"))
		return a.graceOr(ctx, sess, ErrUnavailable)
	}
	if lock == nil {
		return a.graceOr(ctx, sess, ErrUnauthenticated) // another request is refreshing
	}
	defer func() {
		// Not the request's context: the lock must be freed even if the caller gave up.
		rel, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
		defer cancel()
		if err := lock.Release(rel); err != nil {
			a.Log.WarnContext(ctx, "release refresh lock", slog.String("reason", "store"))
		}
	}()

	// Someone may have refreshed between our read and our lock.
	fresh, err := a.Store.Get(ctx, token)
	switch {
	case errors.Is(err, ErrNoSession):
		return Session{}, ErrUnauthenticated
	case err != nil:
		return a.graceOr(ctx, sess, ErrUnavailable)
	}
	if a.Now().Before(fresh.AccessExpires) {
		return fresh, nil
	}

	prov, err := a.Provider.get()
	if err != nil {
		a.Log.WarnContext(ctx, "refresh unavailable", slog.String("reason", "provider_unavailable"))
		return a.graceOr(ctx, fresh, ErrUnavailable)
	}
	rctx, cancel := context.WithTimeout(a.Provider.withHTTP(ctx), refreshTimeout)
	defer cancel()
	tok, err := prov.oauth.TokenSource(rctx, &oauth2.Token{RefreshToken: fresh.RefreshToken}).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.ErrorCode == "invalid_grant" {
			// The provider no longer honours this session: the user was disabled, signed out
			// elsewhere, or the token expired. End ours.
			a.revoked(ctx, fresh, "invalid_grant")
			return Session{}, ErrUnauthenticated
		}
		a.Log.WarnContext(ctx, "refresh failed", slog.String("reason", "provider_error"))
		return a.graceOr(ctx, fresh, ErrUnavailable)
	}

	if tok.RefreshToken != "" {
		fresh.RefreshToken = tok.RefreshToken
	}
	if raw, _ := tok.Extra("id_token").(string); raw != "" {
		// Only used as id_token_hint at logout; it came from the token endpoint over the
		// connection we opened, not from the client.
		fresh.IDToken = raw
	}
	fresh.AccessExpires = accessExpiry(a.Now(), tok)
	if err := a.Store.Update(ctx, fresh); err != nil {
		if errors.Is(err, ErrConflict) {
			// The session changed under us (logout, or an update from elsewhere): reread it.
			again, gerr := a.Store.Get(ctx, token)
			if gerr != nil {
				return Session{}, ErrUnauthenticated
			}
			return again, nil
		}
		a.Log.ErrorContext(ctx, "store refreshed session", slog.String("reason", "store"))
		return a.graceOr(ctx, fresh, ErrUnavailable)
	}
	return fresh, nil
}

// graceOr lets the request through on the current session while the access token expired
// less than RefreshGrace ago, and otherwise returns refusal.
func (a *Authenticator) graceOr(_ context.Context, sess Session, refusal error) (Session, error) {
	if a.Now().Before(sess.AccessExpires.Add(RefreshGrace)) {
		return sess, nil
	}
	return Session{}, refusal
}

func (a *Authenticator) revoked(ctx context.Context, sess Session, reason string) {
	if err := a.Store.Delete(ctx, sess); err != nil {
		a.Log.ErrorContext(ctx, "delete revoked session", slog.String("reason", "store"))
	}
	meta := interceptors.AuditMeta{}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		meta.TraceID = sc.TraceID().String()
	}
	if err := a.Audit.Record(ctx, AuditEvent{
		Action: "auth.session_revoked", ActorID: sess.UserID, SessionRef: sess.Ref(), Reason: reason, Meta: meta,
	}); err != nil {
		a.Log.ErrorContext(ctx, "audit session revoked", slog.String("reason", "audit"))
	}
}
