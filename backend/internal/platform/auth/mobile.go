package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/oauth2"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/interceptors"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/ratelimit"
)

// Paths of the mobile login (ADR 0032), plain HTTP outside the Connect chain like the rest of
// /auth/*. They speak JSON and are called by the app's code, never by a browser navigation.
const (
	MobileStartPath   = "/auth/mobile/start"
	MobileTokenPath   = "/auth/mobile/token" //nolint:gosec // a URL path, not a credential
	MobileRefreshPath = "/auth/mobile/refresh"
	MobileLogoutPath  = "/auth/mobile/logout"
)

// mobileMaxBody bounds every request body: the largest is the token request, about 700 bytes.
const mobileMaxBody = 4 << 10

// Fixed error codes of the mobile endpoints (the JSON body is {"error": code}). What went wrong
// goes to the log and the audit trail, never to the client.
const (
	mobileErrInvalid         = "invalid_request"
	mobileErrUnauthenticated = "unauthenticated"
	mobileErrNotAllowed      = "not_allowed"
	mobileErrEmailNotVerif   = "email_not_verified"
	mobileErrRateLimited     = "rate_limited"
	mobileErrUnavailable     = "unavailable"
)

var (
	// A code from the identity provider: opaque, but only characters of the base64 and URL
	// families. A bound on shape and size before it is sent anywhere.
	authCodeShape = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]{1,512}$`)
	// A PKCE verifier (RFC 7636, section 4.1).
	verifierShape = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

// PublicLimiter is the part of the rate limiter the mobile endpoints use. They are public
// (the caller has no session yet), so the limit is per client address and fails closed.
type PublicLimiter interface {
	Allow(ctx context.Context, scope ratelimit.Scope, id string, write bool) ratelimit.Decision
}

func (h *Handler) mountMobile(mux *http.ServeMux) {
	mux.HandleFunc("POST "+MobileStartPath, h.mobileStart)
	mux.HandleFunc("POST "+MobileTokenPath, h.mobileToken)
	mux.HandleFunc("POST "+MobileRefreshPath, h.mobileRefresh)
	mux.HandleFunc("POST "+MobileLogoutPath, h.mobileLogout)
}

func mobileFail(w http.ResponseWriter, status int, code string) {
	noStore(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + code + `"}`))
}

func mobileJSON(w http.ResponseWriter, v any) {
	noStore(w)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// decodeMobileBody reads one JSON object: right content type, bounded size, no unknown fields,
// nothing after the object. On failure it has answered and returns false.
func decodeMobileBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		mobileFail(w, http.StatusUnsupportedMediaType, mobileErrInvalid)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, mobileMaxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return false
	}
	return true
}

// mobileAllow applies the per-address limit. It answers and returns false when the request
// must stop.
func (h *Handler) mobileAllow(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	if h.Limiter == nil {
		// Fail closed: a public endpoint without a limit is not served.
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return false
	}
	id := "auth-mobile:" + endpoint + ":unknown"
	if ip := interceptors.RequestMeta(r, h.ClientIPHeader).IP; ip.IsValid() {
		id = "auth-mobile:" + endpoint + ":" + ip.String()
	}
	switch h.Limiter.Allow(r.Context(), ratelimit.ScopePublic, id, true) {
	case ratelimit.Allowed:
		return true
	case ratelimit.RateLimited:
		mobileFail(w, http.StatusTooManyRequests, mobileErrRateLimited)
	default:
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
	}
	return false
}

// mobileStart begins a login: it remembers what the app sent (its session key and the PKCE
// challenge) under a fresh state and returns the identity provider's authorization URL.
func (h *Handler) mobileStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.mobileAllow(w, r, "start") {
		return
	}
	var req struct {
		SessionPub    string `json:"session_pub"`
		CodeChallenge string `json:"code_challenge"`
	}
	if !decodeMobileBody(w, r, &req) {
		return
	}
	spki, _, err := parseDeviceKey(req.SessionPub)
	if err != nil || !validToken(req.CodeChallenge) { // an S256 challenge is 43 base64url characters
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return
	}
	prov, err := h.Provider.get()
	if err != nil {
		h.Log.WarnContext(ctx, "mobile login unavailable", slog.String("reason", "provider_unavailable"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	state, err := randomToken()
	if err != nil {
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	if err := h.Store.PutPending(ctx, state, MobilePending{Challenge: req.CodeChallenge, Nonce: nonce, DevicePub: spki}); err != nil {
		reason := "store"
		if errors.Is(err, ErrPendingFull) {
			reason = "pending_full"
		}
		h.Log.WarnContext(ctx, "mobile login not started", slog.String("reason", reason))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	// prompt=login: a shared phone must never sign in silently with an earlier session of the
	// identity provider's browser.
	authURL := prov.mobileOAuth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", req.CodeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("prompt", "login"),
	)
	mobileJSON(w, struct {
		AuthorizationURL string `json:"authorization_url"`
		State            string `json:"state"`
	}{authURL, state})
}

// mobileToken finishes a login: it checks everything the app must prove before the code goes
// to the identity provider, then runs the same admission as the web login and creates the
// session.
func (h *Handler) mobileToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.mobileAllow(w, r, "token") {
		return
	}
	var req struct {
		State        string `json:"state"`
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
		Proof        string `json:"proof"`
	}
	if !decodeMobileBody(w, r, &req) {
		return
	}
	if !validToken(req.State) || !authCodeShape.MatchString(req.Code) || !verifierShape.MatchString(req.CodeVerifier) {
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return
	}

	// 1. The state is used once, whatever happens next. Nothing is sent to the identity
	// provider until the app has proved it started this login and holds its key.
	pending, err := h.Store.TakePending(ctx, req.State)
	switch {
	case errors.Is(err, ErrNoPending):
		h.refuseUnauthenticated(ctx, "mobile_no_pending")
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return
	case err != nil:
		h.Log.ErrorContext(ctx, "mobile login", slog.String("reason", "store"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	if subtle.ConstantTimeCompare([]byte(oauth2.S256ChallengeFromVerifier(req.CodeVerifier)), []byte(pending.Challenge)) != 1 {
		h.refuseUnauthenticated(ctx, "mobile_pkce_mismatch")
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return
	}
	_, pub, err := parseDeviceKeyDER(pending.DevicePub)
	if err != nil {
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return
	}
	if err := verifyDeviceProof(pub, req.Proof, proofTokenDomain, req.State, req.Code); err != nil {
		h.refuseUnauthenticated(ctx, "mobile_bad_proof")
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return
	}
	prov, err := h.Provider.get()
	if err != nil {
		h.Log.WarnContext(ctx, "mobile login unavailable", slog.String("reason", "provider_unavailable"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}

	// 2. Code -> tokens, then the ID token, checked for the mobile audience only.
	exCtx, cancel := context.WithTimeout(h.Provider.withHTTP(ctx), 10*time.Second)
	defer cancel()
	tok, err := prov.mobileOAuth.Exchange(exCtx, req.Code, oauth2.VerifierOption(req.CodeVerifier))
	if err != nil {
		h.failLogin(ctx, r, "code_exchange", "")
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return
	}
	claims, err := h.verifyIDToken(exCtx, prov.mobileVerifier, tok, pending.Nonce)
	if err != nil {
		h.discardTokensFor(ctx, tok, true)
		h.failLogin(ctx, r, err.Error(), "")
		mobileFail(w, http.StatusForbidden, mobileErrNotAllowed)
		return
	}
	profile, err := h.userInfo(exCtx, prov, tok, claims.Subject)
	if err != nil {
		reason := err.Error()
		h.discardTokensFor(ctx, tok, true)
		h.failLogin(ctx, r, reason, claims.Subject)
		switch reason {
		case "email_not_verified":
			mobileFail(w, http.StatusForbidden, mobileErrEmailNotVerif)
		case "userinfo_failed":
			mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		default:
			mobileFail(w, http.StatusForbidden, mobileErrNotAllowed)
		}
		return
	}
	claims.Email, claims.Name = profile.Email, profile.Name
	if tok.RefreshToken == "" {
		h.failLogin(ctx, r, "no_refresh_token", "")
		mobileFail(w, http.StatusBadGateway, mobileErrUnavailable)
		return
	}

	// 3. The same admission as the web login.
	userID, admit := h.admitLoginFor(ctx, r, tok, claims, true)
	switch admit {
	case admitRefused:
		mobileFail(w, http.StatusForbidden, mobileErrNotAllowed)
		return
	case admitUnavailable:
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}

	// 4. The session, bound to the phone's key, and its first Bearer.
	meta := interceptors.RequestMeta(r, h.ClientIPHeader)
	sessIP := ""
	if meta.IP.IsValid() {
		sessIP = meta.IP.String()
	}
	ref, err := h.Store.CreateMobile(ctx, Session{
		UserID: userID, Subject: claims.Subject, SessionID: claims.SessionID, AMR: claims.AMR,
		AuthTime: claims.authTime(), AccessExpires: accessExpiry(h.Now(), tok),
		RefreshToken: tok.RefreshToken, IDToken: claims.Raw,
		UserAgent: meta.UserAgent, IP: sessIP,
	}, pending.DevicePub)
	if err != nil {
		h.discardTokensFor(ctx, tok, true)
		h.Log.ErrorContext(ctx, "create mobile session", slog.String("reason", "store"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	undo := func() {
		_ = h.Store.DeleteByRef(ctx, ref)
		h.discardTokensFor(ctx, tok, true)
	}
	bearer, err := h.Store.RotateBearer(ctx, ref)
	if err != nil {
		undo()
		h.Log.ErrorContext(ctx, "issue bearer", slog.String("reason", "store"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	// The trail first: a login that cannot be audited does not happen (fail closed).
	if err := h.Audit.Record(ctx, AuditEvent{
		Action: "auth.login", ActorKind: ActorUser, ActorID: userID, SessionRef: ref, Reason: "mobile", Meta: meta,
	}); err != nil {
		undo()
		h.Log.ErrorContext(ctx, "audit login", slog.String("reason", "audit"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	mobileJSON(w, bearerResponse{Session: ref, AccessToken: bearer, ExpiresIn: int(BearerTTL / time.Second)})
}

// bearerResponse is what the app gets after a login or a renewal. Session is the session's
// reference: an identifier the app needs to ask for a renewal, worth nothing without the key.
type bearerResponse struct {
	Session     string `json:"session,omitempty"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// mobileRefresh renews the Bearer. It is authorised by the phone's key alone, so it works
// when the Bearer has already expired, and a stolen Bearer cannot use it.
//
// Every request carries a counter the app has spent in advance. A counter that is not above
// the last one means a signed request is being sent a second time: someone else has a copy
// of what only the app can make. The whole session is ended (RFC 9700, section 4.14: refresh
// token reuse), at the identity provider as well.
func (h *Handler) mobileRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.mobileAllow(w, r, "refresh") {
		return
	}
	var req struct {
		Session string `json:"session"`
		Counter uint64 `json:"counter"`
		Proof   string `json:"proof"`
	}
	if !decodeMobileBody(w, r, &req) {
		return
	}
	if !validRef(req.Session) || req.Counter == 0 || req.Counter > maxCounter {
		mobileFail(w, http.StatusBadRequest, mobileErrInvalid)
		return
	}
	sess, err := h.Store.GetByRef(ctx, req.Session)
	switch {
	case errors.Is(err, ErrNoSession):
		mobileFail(w, http.StatusUnauthorized, mobileErrUnauthenticated)
		return
	case err != nil:
		h.Log.ErrorContext(ctx, "mobile refresh", slog.String("reason", "store"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	if sess.Kind != KindMobile {
		mobileFail(w, http.StatusUnauthorized, mobileErrUnauthenticated)
		return
	}
	// The signature comes first: only the holder of the key can spend a counter or trigger
	// the reuse alarm, so nobody else can burn counters or end a session by guessing.
	_, pub, err := parseDeviceKeyDER(sess.DevicePub)
	if err != nil || verifyDeviceProof(pub, req.Proof, proofRefreshDomain, req.Session, strconv.FormatUint(req.Counter, 10)) != nil {
		h.refuseUnauthenticated(ctx, "mobile_bad_proof")
		mobileFail(w, http.StatusUnauthorized, mobileErrUnauthenticated)
		return
	}
	res, err := h.Store.ConsumeCounter(ctx, sess.Ref(), req.Counter)
	if err != nil {
		h.Log.ErrorContext(ctx, "mobile refresh", slog.String("reason", "store"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	switch res {
	case CounterReused:
		h.mobileReuse(ctx, r, sess)
		mobileFail(w, http.StatusUnauthorized, mobileErrUnauthenticated)
		return
	case CounterTooFar, CounterNoSession:
		mobileFail(w, http.StatusUnauthorized, mobileErrUnauthenticated)
		return
	}

	// The Zitadel tokens of the session are refreshed here too, so a user disabled at the
	// identity provider loses the session at the renewal at the latest.
	if _, err := h.Authn.principalFor(ctx, sess); err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			mobileFail(w, http.StatusUnauthorized, mobileErrUnauthenticated)
		} else {
			mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		}
		return
	}
	bearer, err := h.Store.RotateBearer(ctx, sess.Ref())
	switch {
	case errors.Is(err, ErrNoSession):
		mobileFail(w, http.StatusUnauthorized, mobileErrUnauthenticated)
		return
	case err != nil:
		h.Log.ErrorContext(ctx, "issue bearer", slog.String("reason", "store"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	mobileJSON(w, bearerResponse{AccessToken: bearer, ExpiresIn: int(BearerTTL / time.Second)})
}

// mobileReuse ends a session whose renewal was replayed: revoke at the identity provider
// (best effort), delete ours, and leave a trail.
func (h *Handler) mobileReuse(ctx context.Context, r *http.Request, sess Session) {
	h.Log.WarnContext(ctx, "mobile refresh reuse: session ended", slog.String("reason", "refresh_reuse"))
	if err := h.revokeFor(ctx, sess.RefreshToken, true); err != nil {
		h.Log.WarnContext(ctx, "mobile refresh reuse: revocation failed", slog.String("reason", "revoke"))
	}
	if err := h.Store.Delete(ctx, sess); err != nil {
		h.Log.ErrorContext(ctx, "mobile refresh reuse: delete session", slog.String("reason", "store"))
	}
	if err := h.Audit.Record(ctx, AuditEvent{
		Action: "auth.mobile_reuse", ActorKind: ActorUser, ActorID: sess.UserID, SessionRef: sess.Ref(), Reason: "refresh_reuse",
		Meta: interceptors.RequestMeta(r, h.ClientIPHeader),
	}); err != nil {
		h.Log.ErrorContext(ctx, "audit mobile reuse", slog.String("reason", "audit"))
	}
}

// mobileLogout ends the session of the Bearer: revoke at the identity provider, delete ours.
// It answers 204 even when the session is already gone, so the app can always wipe its data.
func (h *Handler) mobileLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.mobileAllow(w, r, "logout") {
		return
	}
	bearer, present := BearerFromHeader(r.Header.Values("Authorization"))
	if !present || bearer == "" {
		mobileFail(w, http.StatusUnauthorized, mobileErrUnauthenticated)
		return
	}
	sess, err := h.Store.GetByBearer(ctx, bearer)
	switch {
	case errors.Is(err, ErrNoSession):
		noStore(w)
		w.WriteHeader(http.StatusNoContent)
		return
	case err != nil:
		h.Log.ErrorContext(ctx, "mobile logout", slog.String("reason", "store"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	reason := ""
	if err := h.revokeFor(ctx, sess.RefreshToken, true); err != nil {
		reason = "revoke_failed"
		h.Log.WarnContext(ctx, "mobile logout: revocation failed")
	}
	if err := h.Store.Delete(ctx, sess); err != nil {
		h.Log.ErrorContext(ctx, "mobile logout: delete session", slog.String("reason", "store"))
		mobileFail(w, http.StatusServiceUnavailable, mobileErrUnavailable)
		return
	}
	if err := h.Audit.Record(ctx, AuditEvent{
		Action: "auth.logout", ActorKind: ActorUser, ActorID: sess.UserID, SessionRef: sess.Ref(), Reason: reason,
		Meta: interceptors.RequestMeta(r, h.ClientIPHeader),
	}); err != nil {
		h.Log.ErrorContext(ctx, "audit logout", slog.String("reason", "audit"))
	}
	noStore(w)
	w.WriteHeader(http.StatusNoContent)
}
