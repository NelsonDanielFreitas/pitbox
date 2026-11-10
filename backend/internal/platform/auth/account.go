package auth

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/auth/principal"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
)

// The account endpoints (the user's sessions and recovery codes) answer to the web session
// cookie and to the phone's Bearer (ADR 0032, amended for F2-06b). Both go through the same
// Authenticator as the RPCs, so a disabled user or a revoked session is refused here too.

const (
	accountErrUnauthenticated = `{"error":"unauthenticated"}`
	accountErrUnavailable     = `{"error":"authentication temporarily unavailable"}`
	accountErrStepUp          = `{"error":"step-up required"}`
	accountErrCrossOrigin     = `{"error":"cross-origin request refused"}`
)

func accountFail(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\n"))
}

// callerPrincipal authenticates the caller of an account endpoint. It has answered the request
// when it returns false.
//
//   - A Bearer never comes with a cookie, and a malformed Authorization header is refused (no
//     quiet fallback to the cookie). The Bearer is not an ambient credential, so the
//     cross-site checks do not apply to it.
//   - A cookie request from another origin or site is refused before the session is looked at.
func (h *Handler) callerPrincipal(w http.ResponseWriter, r *http.Request) (principal.Principal, bool) {
	noStore(w)
	var (
		p   principal.Principal
		err error
	)
	bearer, hasBearer := BearerFromHeader(r.Header.Values("Authorization"))
	switch {
	case hasBearer:
		if bearer == "" || SessionTokenFromHeader(r.Header) != "" {
			err = ErrUnauthenticated
		} else {
			p, err = h.Authn.AuthenticateBearer(r.Context(), bearer)
		}
	case foreignRequest(h.PublicURL, r.Header.Get("Origin"), r.Header.Get("Sec-Fetch-Site")):
		accountFail(w, http.StatusForbidden, accountErrCrossOrigin)
		return principal.Principal{}, false
	default:
		p, err = h.Authn.Authenticate(r.Context(), SessionTokenFromHeader(r.Header))
	}
	switch {
	case errors.Is(err, ErrUnauthenticated):
		accountFail(w, http.StatusUnauthorized, accountErrUnauthenticated)
		return principal.Principal{}, false
	case err != nil:
		accountFail(w, http.StatusServiceUnavailable, accountErrUnavailable)
		return principal.Principal{}, false
	}
	return p, true
}

// sessionView is what the user sees of a session of theirs: no token, no user id, and a
// reference that is worth nothing as a credential.
type sessionView struct {
	Ref       string    `json:"ref"`
	Kind      string    `json:"kind"` // "web" or "mobile"
	CreatedAt time.Time `json:"created_at"`
	AuthTime  time.Time `json:"auth_time"`
	UserAgent string    `json:"user_agent"`
	IP        string    `json:"ip"`
	Current   bool      `json:"current"`
}

func kindName(kind string) string {
	if kind == KindMobile {
		return "mobile"
	}
	return "web"
}

// listSessions returns the active sessions of the authenticated user.
func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := h.callerPrincipal(w, r)
	if !ok {
		return
	}
	infos, err := h.Store.ListBySubject(ctx, p.Subject, p.SessionRef)
	if err != nil {
		h.Log.ErrorContext(ctx, "list sessions", slog.String("reason", "store"))
		accountFail(w, http.StatusServiceUnavailable, accountErrUnavailable)
		return
	}
	views := make([]sessionView, 0, len(infos))
	for _, i := range infos {
		views = append(views, sessionView{
			Ref: i.Ref, Kind: kindName(i.Kind), CreatedAt: i.CreatedAt, AuthTime: i.AuthTime,
			UserAgent: i.UserAgent, IP: i.IP, Current: i.Current,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Sessions []sessionView `json:"sessions"`
	}{views})
}

// endSession revokes the identity provider's tokens of the session (best effort) and deletes it.
func (h *Handler) endSession(r *http.Request, target Session) error {
	if target.RefreshToken != "" {
		if err := h.revokeFor(r.Context(), target.RefreshToken, target.Kind == KindMobile); err != nil {
			h.Log.WarnContext(r.Context(), "session revocation at the identity provider failed")
		}
	}
	return h.Store.Delete(r.Context(), target)
}

// revokeSession ends one session of the authenticated user. A reference that is unknown, or
// that belongs to somebody else, is the same 404.
func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := h.callerPrincipal(w, r)
	if !ok {
		return
	}
	ref := r.PathValue("ref")
	if ref == "" || !validRef(ref) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	target, err := h.Store.GetByRef(ctx, ref)
	switch {
	case errors.Is(err, ErrNoSession):
		w.WriteHeader(http.StatusNotFound)
		return
	case err != nil:
		h.Log.ErrorContext(ctx, "revoke session", slog.String("reason", "store"))
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if subtle.ConstantTimeCompare([]byte(target.Subject), []byte(p.Subject)) != 1 {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if err := h.endSession(r, target); err != nil {
		h.Log.ErrorContext(ctx, "revoke session", slog.String("reason", "store"))
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	h.auditAccount(r, "auth.session_revoked", p, ref, "")
	w.WriteHeader(http.StatusNoContent)
}

// revokeOtherSessions ends every session of the user except the one that asks.
func (h *Handler) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := h.callerPrincipal(w, r)
	if !ok {
		return
	}
	infos, err := h.Store.ListBySubject(ctx, p.Subject, p.SessionRef)
	if err != nil {
		h.Log.ErrorContext(ctx, "revoke other sessions", slog.String("reason", "store"))
		accountFail(w, http.StatusServiceUnavailable, accountErrUnavailable)
		return
	}
	revoked := 0
	for _, i := range infos {
		if i.Current {
			continue
		}
		target, err := h.Store.GetByRef(ctx, i.Ref)
		if errors.Is(err, ErrNoSession) {
			continue
		}
		if err != nil || subtle.ConstantTimeCompare([]byte(target.Subject), []byte(p.Subject)) != 1 {
			h.Log.ErrorContext(ctx, "revoke other sessions", slog.String("reason", "store"))
			accountFail(w, http.StatusServiceUnavailable, accountErrUnavailable)
			return
		}
		if err := h.endSession(r, target); err != nil {
			h.Log.ErrorContext(ctx, "revoke other sessions", slog.String("reason", "store"))
			accountFail(w, http.StatusServiceUnavailable, accountErrUnavailable)
			return
		}
		revoked++
	}
	if revoked > 0 {
		h.auditAccount(r, "auth.sessions_revoked_others", p, p.SessionRef, "others")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Revoked int `json:"revoked"`
	}{revoked})
}

// recoveryCodesStatus says how many recovery codes the user has left, never the codes.
func (h *Handler) recoveryCodesStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, ok := h.callerPrincipal(w, r)
	if !ok {
		return
	}
	n, err := h.RecoveryCodes.CountUnused(ctx, p.UserID)
	if err != nil {
		h.Log.ErrorContext(ctx, "recovery codes status", slog.String("reason", "store"))
		accountFail(w, http.StatusServiceUnavailable, accountErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Unused int `json:"unused"`
	}{n})
}

// generateRecoveryCodes creates a fresh set of single-use recovery codes. It needs a strong
// factor used in the last StepUpWindow; the plaintext codes are returned exactly once and are
// never stored. The new set replaces the old one, so a copy of an old set stops working.
func (h *Handler) generateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.mobileAllow(w, r, "codes") {
		return
	}
	p, ok := h.callerPrincipal(w, r)
	if !ok {
		return
	}
	if principal.RequiresReauthentication(p, h.Now()) {
		accountFail(w, http.StatusForbidden, accountErrStepUp)
		return
	}
	if h.RecoveryCodeHasher == nil {
		h.Log.ErrorContext(ctx, "generate recovery codes", slog.String("reason", "keyring"))
		accountFail(w, http.StatusServiceUnavailable, accountErrUnavailable)
		return
	}
	codes, hashes, err := h.RecoveryCodeHasher.Generate()
	if err != nil {
		h.Log.ErrorContext(ctx, "generate recovery codes", slog.String("reason", "random"))
		accountFail(w, http.StatusServiceUnavailable, accountErrUnavailable)
		return
	}
	if err := h.RecoveryCodes.Replace(ctx, p.UserID, h.RecoveryCodeHasher.KeyID(), hashes); err != nil {
		h.Log.ErrorContext(ctx, "generate recovery codes", slog.String("reason", "store"))
		accountFail(w, http.StatusServiceUnavailable, accountErrUnavailable)
		return
	}
	h.readiness.forget(p.UserID)
	h.auditAccount(r, "auth.recovery_codes_generated", p, p.SessionRef, "")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Codes []string `json:"codes"`
	}{codes})
}

// auditAccount records an account action. A failure to record is logged, not fatal: the
// action already happened and the user must not be left half-way (same as before).
func (h *Handler) auditAccount(r *http.Request, action string, p principal.Principal, sessionRef, reason string) {
	if err := h.Audit.Record(r.Context(), AuditEvent{
		Action: action, ActorKind: ActorUser, ActorID: p.UserID, SessionRef: sessionRef, Reason: reason,
		Meta: interceptors.RequestMeta(r, h.ClientIPHeader),
	}); err != nil {
		h.Log.ErrorContext(r.Context(), "audit account action", slog.String("reason", "audit"))
	}
}
