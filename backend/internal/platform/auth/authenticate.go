package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/oauth2"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth/principal"
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

	// refreshWait is how long a request that lost the refresh lock, and is past the grace
	// period, waits for the winner: the winner's own deadline plus a second for the store.
	refreshWait = refreshTimeout + time.Second
	// refreshPoll is how often the loser rereads the session while it waits.
	refreshPoll = 100 * time.Millisecond
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
	// WaitTimeout bounds how long a request waits for another one's refresh (zero: refreshWait).
	WaitTimeout time.Duration
}

// Authenticate resolves the cookie value to the principal of the request.
func (a *Authenticator) Authenticate(ctx context.Context, token string) (principal.Principal, error) {
	if token == "" {
		return principal.Principal{}, ErrUnauthenticated
	}
	sess, err := a.Store.Get(ctx, token)
	switch {
	case errors.Is(err, ErrNoSession):
		return principal.Principal{}, ErrUnauthenticated
	case err != nil:
		a.Log.ErrorContext(ctx, "read session", slog.String("reason", "store"))
		return principal.Principal{}, ErrUnavailable
	}
	if sess.Kind != KindWeb {
		// A cookie never opens a mobile session (nor the other way round): the credential
		// of each kind is separate. Cannot happen by hash; refused anyway.
		return principal.Principal{}, ErrUnauthenticated
	}
	return a.principalFor(ctx, sess)
}

// AuthenticateBearer resolves a mobile Bearer (ADR 0032) to the principal of the request. The
// Bearer is opaque and looked up like a cookie value: anything that is not one of ours (a
// JWT, an ID token, garbage) fails the shape check without touching the store. The session
// it names is the same kind of record as the web's, so the Zitadel tokens are refreshed the
// same way, when the access token has expired.
func (a *Authenticator) AuthenticateBearer(ctx context.Context, bearer string) (principal.Principal, error) {
	sess, err := a.Store.GetByBearer(ctx, bearer)
	switch {
	case errors.Is(err, ErrNoSession):
		return principal.Principal{}, ErrUnauthenticated
	case err != nil:
		a.Log.ErrorContext(ctx, "read session", slog.String("reason", "store"))
		return principal.Principal{}, ErrUnavailable
	}
	return a.principalFor(ctx, sess)
}

// principalFor refreshes the Zitadel tokens of the session if they expired and returns its
// principal.
func (a *Authenticator) principalFor(ctx context.Context, sess Session) (principal.Principal, error) {
	if a.Now().Before(sess.AccessExpires) {
		return principalOf(sess), nil
	}
	sess, err := a.refresh(ctx, sess.Ref(), sess)
	if err != nil {
		return principal.Principal{}, err
	}
	return principalOf(sess), nil
}

func principalOf(s Session) principal.Principal {
	return principal.Principal{UserID: s.UserID, Subject: s.Subject, SessionRef: s.Ref(), AMR: s.AMR, AuthTime: s.AuthTime}
}

// refresh renews the tokens of a session whose access token has expired. Exactly one of
// the concurrent requests does it (a lock in the store), so nobody presents the same
// (rotated) refresh token twice, which would make the provider reject it and sign the user
// out for nothing. The others:
//
//   - inside the grace period carry on with the session they have (the grace covers a slow
//     identity provider, not the race);
//   - past it wait for the winner, because a user who was away for longer than the grace
//     period comes back with several requests at once, and refusing the losers would sign
//     that user out.
func (a *Authenticator) refresh(ctx context.Context, ref string, sess Session) (Session, error) {
	lock, err := a.Store.TryLockRefresh(ctx, sess)
	if err != nil {
		a.Log.ErrorContext(ctx, "refresh lock", slog.String("reason", "store"))
		return a.graceOr(ctx, sess, ErrUnavailable)
	}
	if lock == nil {
		// Another request is refreshing.
		if a.Now().Before(sess.AccessExpires.Add(RefreshGrace)) {
			return sess, nil
		}
		return a.awaitRefresh(ctx, ref, sess)
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
	fresh, err := a.Store.GetByRef(ctx, ref)
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
	// The refresh token is bound to the client it was issued to (the web application's or
	// the native one's); presenting it as the other client is refused by the provider.
	oauthCfg := prov.oauth
	if fresh.Kind == KindMobile {
		oauthCfg = prov.mobileOAuth
	}
	tok, err := oauthCfg.TokenSource(rctx, &oauth2.Token{RefreshToken: fresh.RefreshToken}).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			if reason := endsTheSession(re); reason != "" {
				// The provider no longer honours this session: the user was disabled or locked,
				// signed out elsewhere, or the token expired or was revoked. End ours.
				a.revoked(ctx, fresh, reason)
				return Session{}, ErrUnauthenticated
			}
			// The OAuth error code (a short fixed token) and the HTTP status are useful to
			// operate and carry no secret. The description is not logged: it is the provider's text.
			level := slog.LevelWarn
			if re.ErrorCode == "invalid_client" {
				level = slog.LevelError // our client credentials were refused: a misconfiguration, not the user
			}
			a.Log.LogAttrs(ctx, level, "refresh failed", slog.String("reason", "provider_error"),
				slog.String("oauth_error", re.ErrorCode), slog.Int("status", re.Response.StatusCode))
		} else {
			a.Log.WarnContext(ctx, "refresh failed", slog.String("reason", "provider_unreachable"))
		}
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
			again, gerr := a.Store.GetByRef(ctx, ref)
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

// awaitRefresh waits for the request that holds the refresh lock, rereading the session until
// its access token moves forward. It gives up after the wait timeout with ErrUnavailable,
// never ErrUnauthenticated: not getting an answer in time says nothing about the user. A
// session the winner ended (invalid_grant) is gone, and that one is ErrUnauthenticated.
func (a *Authenticator) awaitRefresh(ctx context.Context, ref string, stale Session) (Session, error) {
	wait := a.WaitTimeout
	if wait <= 0 {
		wait = refreshWait
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	timer := time.NewTimer(refreshPoll)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			a.Log.WarnContext(ctx, "refresh wait ended", slog.String("reason", "wait_timeout"))
			return Session{}, ErrUnavailable
		case <-timer.C:
		}
		fresh, err := a.Store.GetByRef(ctx, ref)
		switch {
		case errors.Is(err, ErrNoSession):
			return Session{}, ErrUnauthenticated
		case err != nil:
			if ctx.Err() != nil {
				continue // the next turn of the loop reports the timeout
			}
			a.Log.ErrorContext(ctx, "read session", slog.String("reason", "store"))
			return Session{}, ErrUnavailable
		case fresh.AccessExpires.After(stale.AccessExpires) && a.Now().Before(fresh.AccessExpires):
			return fresh, nil
		}
		timer.Reset(refreshPoll)
	}
}

// endsTheSession says whether a refusal from the token endpoint means the session is over, and
// why (a fixed reason for the audit trail). Only definitive answers end it: an outage, a
// wrong client secret or a malformed request of ours must never sign everybody out.
//
//   - invalid_grant (RFC 6749 §5.2): the refresh token is invalid, expired or revoked.
//   - Zitadel answers invalid_request with an "Errors.User.*" description (NotActive, Locked,
//     NotFound...) when the user was deactivated, locked or deleted. Verified against v4.19.4
//     by scripts/auth-smoke.py; a Zitadel upgrade that changes it makes that smoke test fail.
func endsTheSession(re *oauth2.RetrieveError) string {
	switch {
	case re.ErrorCode == "invalid_grant":
		return "invalid_grant"
	case re.ErrorCode == "invalid_request" && strings.HasPrefix(re.ErrorDescription, "Errors.User."):
		return "user_not_active"
	}
	return ""
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
		Action: "auth.session_revoked", ActorKind: ActorUser, ActorID: sess.UserID, SessionRef: sess.Ref(), Reason: reason, Meta: meta,
	}); err != nil {
		a.Log.ErrorContext(ctx, "audit session revoked", slog.String("reason", "audit"))
	}
}
