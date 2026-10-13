package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
)

// Paths of the BFF endpoints (plain HTTP, outside the Connect interceptor chain).
const (
	LoginPath     = "/auth/login"
	CallbackPath  = "/auth/callback"
	LogoutPath    = "/auth/logout"
	SignedOutPath = "/auth/signed-out"
	SessionPath   = "/auth/session"
)

// Fixed, public messages. What went wrong goes to the log (a code) and the audit trail,
// never to the browser.
const (
	msgBadRequest  = "invalid login request"
	msgNotAllowed  = "login not allowed"
	msgUnavailable = "login temporarily unavailable"
)

// Handler serves /auth/login, /auth/callback, /auth/logout and /auth/signed-out.
type Handler struct {
	Provider       *Provider
	Authn          *Authenticator // for /auth/session
	Store          *Store
	LoginCookies   *LoginCookies
	Users          UserDirectory
	Audit          Auditor
	Log            *slog.Logger
	PublicURL      string // origin the browser sees, without a trailing slash
	ClientIPHeader string // "" = TCP peer
	SessionMaxAge  time.Duration
	Now            func() time.Time
}

// Mount registers the routes. Go 1.22+ patterns: the method is part of the pattern, so a
// wrong method gets 405 from the mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET "+LoginPath, h.login)
	mux.HandleFunc("GET "+CallbackPath, h.callback)
	mux.HandleFunc("POST "+LogoutPath, h.logout)
	mux.HandleFunc("GET "+SignedOutPath, h.signedOut)
	mux.HandleFunc("GET "+SessionPath, h.sessionInfo)
}

func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func fail(w http.ResponseWriter, code int, msg string) {
	noStore(w)
	http.Error(w, msg, code)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	prov, err := h.Provider.get()
	if err != nil {
		h.Log.WarnContext(r.Context(), "login unavailable", slog.String("reason", "provider_unavailable"))
		fail(w, http.StatusServiceUnavailable, msgUnavailable)
		return
	}
	returnTo, _ := SafeReturnTo(r.URL.Query().Get("return_to"))
	st, err := NewLoginState(h.Now(), returnTo)
	if err != nil {
		h.Log.ErrorContext(r.Context(), "login state", slog.String("reason", "random"))
		fail(w, http.StatusInternalServerError, msgUnavailable)
		return
	}
	if err := h.LoginCookies.Set(w, st); err != nil {
		h.Log.ErrorContext(r.Context(), "login cookie", slog.String("reason", "seal"))
		fail(w, http.StatusInternalServerError, msgUnavailable)
		return
	}
	authURL := prov.oauth.AuthCodeURL(st.State,
		oauth2.S256ChallengeOption(st.Verifier), oidc.Nonce(st.Nonce))
	noStore(w)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Without a valid pre-login cookie nothing else happens: no call to the identity
	// provider (an attacker cannot make us spend requests on it).
	st, err := h.LoginCookies.Read(r)
	h.LoginCookies.Clear(w) // always: a login attempt is single-use
	if err != nil {
		h.failLogin(ctx, r, "no_login_state", "")
		fail(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	q := r.URL.Query()
	if !st.MatchesState(q.Get("state")) {
		h.failLogin(ctx, r, "state_mismatch", "")
		fail(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		// The user denied, or the provider reported a problem. Only the fixed code is kept.
		h.failLogin(ctx, r, "idp_error", "")
		fail(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	prov, err := h.Provider.get()
	if err != nil {
		h.Log.WarnContext(ctx, "login unavailable", slog.String("reason", "provider_unavailable"))
		fail(w, http.StatusServiceUnavailable, msgUnavailable)
		return
	}

	// 2. Code -> tokens (PKCE verifier from the cookie), then the ID token.
	exCtx, cancel := context.WithTimeout(h.Provider.withHTTP(ctx), 10*time.Second)
	defer cancel()
	tok, err := prov.oauth.Exchange(exCtx, q.Get("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		h.failLogin(ctx, r, "code_exchange", "")
		fail(w, http.StatusBadRequest, msgBadRequest)
		return
	}
	claims, err := h.verifyIDToken(exCtx, prov, tok, st.Nonce)
	if err != nil {
		h.failLogin(ctx, r, err.Error(), "")
		fail(w, http.StatusForbidden, msgNotAllowed)
		return
	}
	if tok.RefreshToken == "" {
		h.failLogin(ctx, r, "no_refresh_token", "")
		fail(w, http.StatusBadGateway, msgUnavailable)
		return
	}

	// 3. The PitBox user. Refused logins look the same to the person; the reason is audited.
	userID, err := h.Users.ResolveLogin(ctx, claims.Subject, claims.Email, claims.Name)
	switch {
	case errors.Is(err, ErrEmailInUse):
		h.failLogin(ctx, r, "email_in_use", claims.Subject)
		fail(w, http.StatusForbidden, msgNotAllowed)
		return
	case errors.Is(err, ErrAccountDeleted):
		h.failLogin(ctx, r, "account_deleted", claims.Subject)
		fail(w, http.StatusForbidden, msgNotAllowed)
		return
	case err != nil:
		h.Log.ErrorContext(ctx, "resolve login", slog.String("reason", "directory"))
		fail(w, http.StatusServiceUnavailable, msgUnavailable)
		return
	}

	// 4. A new session, always with a new cookie value (no session fixation). The one the
	// browser may already hold is removed.
	if old := SessionTokenFromHeader(r.Header); old != "" {
		if s, err := h.Store.Get(ctx, old); err == nil {
			_ = h.Store.Delete(ctx, s)
		}
	}
	token, err := h.Store.Create(ctx, Session{
		UserID: userID, Subject: claims.Subject, SessionID: claims.SessionID, AMR: claims.AMR,
		AuthTime: claims.authTime(), AccessExpires: accessExpiry(h.Now(), tok),
		RefreshToken: tok.RefreshToken, IDToken: claims.Raw,
	})
	if err != nil {
		h.Log.ErrorContext(ctx, "create session", slog.String("reason", "store"))
		fail(w, http.StatusServiceUnavailable, msgUnavailable)
		return
	}
	// The trail first: a login that cannot be audited does not happen (fail closed).
	if err := h.Audit.Record(ctx, AuditEvent{
		Action: "auth.login", ActorKind: ActorUser, ActorID: userID, SessionRef: hashHex(token),
		Meta: interceptors.RequestMeta(r, h.ClientIPHeader),
	}); err != nil {
		h.Log.ErrorContext(ctx, "audit login", slog.String("reason", "audit"))
		s, gerr := h.Store.Get(ctx, token)
		if gerr == nil {
			_ = h.Store.Delete(ctx, s)
		}
		fail(w, http.StatusServiceUnavailable, msgUnavailable)
		return
	}

	SetSessionCookie(w, token, h.SessionMaxAge)
	h.landing(w, st.ReturnTo)
}

// landing answers 200 with a meta refresh to returnTo. A 302 here would be a redirect that
// started on another site (the identity provider), which browsers treat as cross-site: the
// SameSite=Strict session cookie would not be sent and the app would load signed out.
// A 200 page whose refresh is a fresh same-site navigation carries the cookie. No script,
// so the CSP can forbid all of them.
func (h *Handler) landing(w http.ResponseWriter, returnTo string) {
	target, ok := SafeReturnTo(returnTo)
	if !ok {
		target = "/"
	}
	noStore(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="pt"><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=%s"><title>PitBox</title></head><body><a href="%s">Continuar</a></body></html>`,
		html.EscapeString(target), html.EscapeString(target))
}

type idClaims struct {
	Subject       string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Name          string   `json:"name"`
	SessionID     string   `json:"sid"`
	AuthTime      int64    `json:"auth_time"`
	AMR           []string `json:"amr"`
	Raw           string   `json:"-"`
}

// verifyIDToken checks the ID token (signature, issuer, audience, expiry and the algorithm
// list are go-oidc's job), then the nonce, the at_hash if present, and what a login needs.
// The returned error text is a fixed reason code.
func (h *Handler) verifyIDToken(ctx context.Context, prov *ready, tok *oauth2.Token, nonce string) (idClaims, error) {
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return idClaims{}, errors.New("no_id_token")
	}
	idt, err := prov.verifier.Verify(ctx, raw)
	if err != nil {
		return idClaims{}, errors.New("id_token_invalid")
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(nonce)) != 1 {
		return idClaims{}, errors.New("nonce_mismatch")
	}
	if idt.AccessTokenHash != "" {
		if err := idt.VerifyAccessToken(tok.AccessToken); err != nil {
			return idClaims{}, errors.New("at_hash_mismatch")
		}
	}
	var c idClaims
	if err := idt.Claims(&c); err != nil || idt.Subject == "" {
		return idClaims{}, errors.New("claims_invalid")
	}
	c.Subject, c.Raw = idt.Subject, raw
	if c.Email == "" {
		return idClaims{}, errors.New("no_email")
	}
	if !c.EmailVerified {
		return idClaims{}, errors.New("email_not_verified")
	}
	if c.AuthTime == 0 {
		// Zitadel always sends it; without it the best honest value is "now", never older.
		c.AuthTime = h.Now().Unix()
	}
	return c, nil
}

// accessExpiry is when the access token stops being valid, measured on our clock: the
// oauth2 package computes Token.Expiry with the real clock, which would make a test with an
// injected clock (and any clock skew between the token endpoint and us) inconsistent.
func accessExpiry(now time.Time, tok *oauth2.Token) time.Time {
	switch {
	case tok.ExpiresIn > 0:
		return now.Add(time.Duration(tok.ExpiresIn) * time.Second)
	case !tok.Expiry.IsZero():
		return now.Add(time.Until(tok.Expiry))
	default:
		return now // unknown: refresh at the next request
	}
}

func (c idClaims) authTime() time.Time { return time.Unix(c.AuthTime, 0).UTC() }

func (h *Handler) failLogin(ctx context.Context, r *http.Request, reason, subject string) {
	h.Log.WarnContext(ctx, "login refused", slog.String("reason", reason))
	actor := "anonymous"
	if subject != "" {
		actor = "sub:" + subject
	}
	if err := h.Audit.Record(ctx, AuditEvent{
		Action: "auth.login_failed", ActorKind: ActorSystem, ActorID: actor, Reason: reason,
		Meta: interceptors.RequestMeta(r, h.ClientIPHeader),
	}); err != nil {
		h.Log.ErrorContext(ctx, "audit login failure", slog.String("reason", "audit"))
	}
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// CSRF: a cross-site form could sign someone out. Browsers always send Origin on a POST.
	if r.Header.Get("Origin") != h.PublicURL {
		fail(w, http.StatusForbidden, msgNotAllowed)
		return
	}
	noStore(w)

	redirect := h.PublicURL + SignedOutPath
	if token := SessionTokenFromHeader(r.Header); token != "" {
		if sess, err := h.Store.Get(ctx, token); err == nil {
			redirect = h.endSessionURL(sess, r)
			// First revoke at the provider (the refresh token stops working everywhere), then
			// delete ours. A failed revocation does not keep the user signed in here.
			reason := ""
			if err := h.revoke(ctx, sess.RefreshToken); err != nil {
				reason = "revoke_failed"
				h.Log.WarnContext(ctx, "logout: revocation failed")
			}
			if err := h.Store.Delete(ctx, sess); err != nil {
				h.Log.ErrorContext(ctx, "logout: delete session", slog.String("reason", "store"))
			}
			if err := h.Audit.Record(ctx, AuditEvent{
				Action: "auth.logout", ActorKind: ActorUser, ActorID: sess.UserID, SessionRef: sess.Ref(), Reason: reason,
				Meta: interceptors.RequestMeta(r, h.ClientIPHeader),
			}); err != nil {
				h.Log.ErrorContext(ctx, "audit logout", slog.String("reason", "audit"))
			}
		}
	}
	ClearSessionCookie(w)
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

// endSessionURL is where the identity provider ends its own session. id_token_hint is
// accepted expired; post_logout_redirect_uri must be registered for the application.
func (h *Handler) endSessionURL(sess Session, r *http.Request) string {
	prov, err := h.Provider.get()
	if err != nil || prov.endSession == "" {
		return h.PublicURL + SignedOutPath
	}
	v := url.Values{}
	v.Set("id_token_hint", sess.IDToken)
	v.Set("client_id", prov.oauth.ClientID)
	v.Set("post_logout_redirect_uri", h.PublicURL+SignedOutPath)
	return prov.endSession + "?" + v.Encode()
}

// revoke revokes a refresh token at the provider (RFC 7009), as the confidential client.
func (h *Handler) revoke(ctx context.Context, refreshToken string) error {
	prov, err := h.Provider.get()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	form := url.Values{"token": {refreshToken}, "token_type_hint": {"refresh_token"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, prov.revocation, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("new revoke request: %w", err)
	}
	req.SetBasicAuth(url.QueryEscape(prov.oauth.ClientID), url.QueryEscape(prov.oauth.ClientSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.Provider.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("revoke: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("revoke: status %d", resp.StatusCode)
	}
	return nil
}

func (h *Handler) signedOut(w http.ResponseWriter, _ *http.Request) {
	noStore(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	_, _ = w.Write([]byte(`<!doctype html><html lang="pt"><head><meta charset="utf-8"><title>PitBox</title></head><body><p>Sessão terminada.</p><p><a href="/">Voltar</a></p></body></html>`))
}

// sessionInfo tells the app whether it is signed in and who it is, so it can decide between
// the login and its screens. It answers with identifiers only: no tokens, no email, no name.
// It goes through the Authenticator, so a session with an expired access token is refreshed.
func (h *Handler) sessionInfo(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	w.Header().Set("Content-Type", "application/json")
	if foreignRequest(h.PublicURL, r.Header.Get("Origin"), r.Header.Get("Sec-Fetch-Site")) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"cross-origin request refused"}`))
		return
	}
	p, err := h.Authn.Authenticate(r.Context(), SessionTokenFromHeader(r.Header))
	switch {
	case errors.Is(err, ErrUnauthenticated):
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"authenticated":false}`))
		return
	case err != nil:
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"authentication temporarily unavailable"}`))
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		Authenticated bool      `json:"authenticated"`
		UserID        string    `json:"user_id"`
		AMR           []string  `json:"amr"`
		AuthTime      time.Time `json:"auth_time"`
	}{true, p.UserID, p.AMR, p.AuthTime})
}
