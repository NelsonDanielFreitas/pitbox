package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"golang.org/x/oauth2"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/ratelimit"
)

// mobileRedirect is the redirect URI of the native application in the tests.
const mobileRedirect = "app.pitbox.mobile:/auth/callback"

// --- helpers: a phone talking to the handlers ---

func (f *flowHarness) post(path string, body any, hdr map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	var raw string
	switch b := body.(type) {
	case string:
		raw = b
	default:
		enc, err := json.Marshal(b)
		if err != nil {
			f.t.Fatalf("marshal: %v", err)
		}
		raw = string(enc)
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, publicURL+path, strings.NewReader(raw))
	r.RemoteAddr = "203.0.113.7:40000"
	r.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("not an error body: %q", w.Body.String())
	}
	return e.Error
}

// attempt of the mobile login: what the app holds between start and token.
type mobileAttempt struct {
	key               deviceKey
	state, nonce      string
	verifier, authURL string
	challenge         string
}

func (f *flowHarness) mobileStart(key deviceKey) mobileAttempt { return f.mobileStartWith(key, "") }

// mobileStartWith starts a login; with a Bearer it is a reauthentication of that session.
func (f *flowHarness) mobileStartWith(key deviceKey, bearer string) mobileAttempt {
	f.t.Helper()
	verifier := oauth2.GenerateVerifier()
	a := mobileAttempt{key: key, verifier: verifier, challenge: oauth2.S256ChallengeFromVerifier(verifier)}
	body, hdr := map[string]any{"session_pub": key.spkiB6, "code_challenge": a.challenge}, map[string]string(nil)
	if bearer != "" {
		body["step_up"], hdr = true, bearerHdr(bearer)
	}
	w := f.post(MobileStartPath, body, hdr)
	if w.Code != http.StatusOK {
		f.t.Fatalf("start: status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		AuthorizationURL string `json:"authorization_url"`
		State            string `json:"state"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		f.t.Fatalf("start body: %v", err)
	}
	u, err := url.Parse(resp.AuthorizationURL)
	if err != nil {
		f.t.Fatalf("authorization_url: %v", err)
	}
	q := u.Query()
	a.state, a.nonce, a.authURL = resp.State, q.Get("nonce"), resp.AuthorizationURL
	if q.Get("state") != a.state {
		f.t.Fatal("the URL carries another state than the response")
	}
	return a
}

// tokenBody is a valid request body for the attempt and a code the identity provider issued
// to the mobile application.
func (f *flowHarness) tokenBody(a mobileAttempt, sub string) map[string]string {
	f.t.Helper()
	code := f.idp.issueCodeFor(f.idp.mobileClientID, sub, a.nonce, a.challenge)
	return map[string]string{
		"state": a.state, "code": code, "code_verifier": a.verifier,
		"proof": a.key.proof(f.t, proofTokenDomain, a.state, code),
	}
}

// mobileLogin runs the whole login and returns the response.
func (f *flowHarness) mobileLogin(key deviceKey, sub string) bearerResponse {
	f.t.Helper()
	a := f.mobileStart(key)
	w := f.post(MobileTokenPath, f.tokenBody(a, sub), nil)
	if w.Code != http.StatusOK {
		f.t.Fatalf("token: status %d: %s", w.Code, w.Body.String())
	}
	var resp bearerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		f.t.Fatalf("token body: %v", err)
	}
	return resp
}

func (f *flowHarness) refreshBody(key deviceKey, session string, counter uint64) map[string]any {
	return map[string]any{
		"session": session, "counter": counter,
		"proof": key.proof(f.t, proofRefreshDomain, session, strconv.FormatUint(counter, 10)),
	}
}

func bearerHdr(bearer string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + bearer}
}

func (f *flowHarness) sessionStatus(hdr map[string]string) (int, string) {
	f.t.Helper()
	w := f.do(http.MethodGet, SessionPath, nil, hdr)
	return w.Code, w.Body.String()
}

// --- the login ---

func TestMobileLoginHappyPath(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	key := newDeviceKey(t)

	a := f.mobileStart(key)
	u, _ := url.Parse(a.authURL)
	q := u.Query()
	for k, want := range map[string]string{
		"client_id": f.idp.mobileClientID, "redirect_uri": mobileRedirect, "response_type": "code",
		"code_challenge": a.challenge, "code_challenge_method": "S256", "prompt": "login",
	} {
		if q.Get(k) != want {
			t.Errorf("authorization URL: %s = %q, want %q", k, q.Get(k), want)
		}
	}
	if !strings.Contains(q.Get("scope"), "offline_access") || !strings.Contains(q.Get("scope"), "openid") {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if q.Get("client_secret") != "" {
		t.Error("a public client must not send a secret")
	}

	w := f.post(MobileTokenPath, f.tokenBody(a, "ana"), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp bearerResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.HasPrefix(resp.AccessToken, bearerPrefix) || bearerSecret(resp.AccessToken) == "" {
		t.Fatalf("access_token has the wrong shape: %q", resp.AccessToken)
	}
	if resp.ExpiresIn != 600 || !validRef(resp.Session) {
		t.Errorf("response %+v", resp)
	}
	// Nothing but those three fields: the refresh token and the ID token stay on the server.
	var fields map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &fields)
	if len(fields) != 3 {
		t.Errorf("the response has %d fields: %v", len(fields), fields)
	}
	body := w.Body.String()
	if strings.Contains(body, "rt-") || strings.Contains(body, "at-") || strings.Count(body, ".") > 0 {
		t.Errorf("the response leaks a token: %s", body)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Error("a mobile login must not set a cookie")
	}

	// The session is a mobile one, bound to the key, and was audited.
	sess, err := f.store.GetByRef(context.Background(), resp.Session)
	if err != nil {
		t.Fatalf("GetByRef: %v", err)
	}
	if sess.Kind != KindMobile || string(sess.DevicePub) != string(key.der) || sess.UserID != "user-ana" || sess.RefreshToken == "" {
		t.Errorf("session %+v", sess)
	}
	if !slices.Contains(f.audit.actions(), "auth.login:mobile") {
		t.Errorf("audit trail: %v", f.audit.actions())
	}

	// And the Bearer works on /auth/session.
	code, body := f.sessionStatus(bearerHdr(resp.AccessToken))
	if code != http.StatusOK || !strings.Contains(body, `"user_id":"user-ana"`) || !strings.Contains(body, `"amr":["pwd","mfa"]`) {
		t.Errorf("/auth/session with the Bearer: %d %s", code, body)
	}
}

func TestMobileTokenRefusals(t *testing.T) {
	t.Parallel()
	other := newDeviceKey(t)
	otherVerifier := oauth2.GenerateVerifier()
	hs256 := func(f *flowHarness) func(h, p map[string]any) string {
		return func(h, p map[string]any) string {
			h["alg"] = "HS256"
			hb, _ := json.Marshal(h)
			pb, _ := json.Marshal(p)
			in := b64(hb) + "." + b64(pb)
			m := hmac.New(sha256.New, []byte("a shared secret nobody should be able to use"))
			m.Write([]byte(in))
			return in + "." + b64(m.Sum(nil))
		}
	}
	none := func(h, p map[string]any) string {
		h["alg"] = "none"
		hb, _ := json.Marshal(h)
		pb, _ := json.Marshal(p)
		return b64(hb) + "." + b64(pb) + "."
	}

	tests := []struct {
		name string
		// edit changes the request body or the identity provider before the request.
		edit       func(f *flowHarness, a mobileAttempt, body map[string]string)
		status     int
		code       string
		wasIssued  bool // the code exchange happened, so the refresh token must be revoked
		wantNoCall bool // nothing may reach the identity provider's token endpoint
	}{
		{"unknown state", func(_ *flowHarness, _ mobileAttempt, b map[string]string) { b["state"] = strings.Repeat("A", 43) }, 400, mobileErrInvalid, false, true},
		{"state of the wrong shape", func(_ *flowHarness, _ mobileAttempt, b map[string]string) { b["state"] = "short" }, 400, mobileErrInvalid, false, true},
		{"another verifier (PKCE)", func(_ *flowHarness, _ mobileAttempt, b map[string]string) { b["code_verifier"] = otherVerifier }, 400, mobileErrInvalid, false, true},
		{"proof of another key", func(f *flowHarness, a mobileAttempt, b map[string]string) {
			b["proof"] = other.proof(f.t, proofTokenDomain, a.state, b["code"])
		}, 400, mobileErrInvalid, false, true},
		{"proof over another code", func(f *flowHarness, a mobileAttempt, b map[string]string) {
			b["proof"] = a.key.proof(f.t, proofTokenDomain, a.state, "another-code")
		}, 400, mobileErrInvalid, false, true},
		{"proof over another state", func(f *flowHarness, a mobileAttempt, b map[string]string) {
			b["proof"] = a.key.proof(f.t, proofTokenDomain, strings.Repeat("B", 43), b["code"])
		}, 400, mobileErrInvalid, false, true},
		{"a refresh proof used as a token proof", func(f *flowHarness, a mobileAttempt, b map[string]string) {
			b["proof"] = a.key.proof(f.t, proofRefreshDomain, a.state, b["code"])
		}, 400, mobileErrInvalid, false, true},
		{"no proof", func(_ *flowHarness, _ mobileAttempt, b map[string]string) { b["proof"] = "" }, 400, mobileErrInvalid, false, true},
		{"code of the wrong shape", func(f *flowHarness, a mobileAttempt, b map[string]string) {
			b["code"] = "has spaces"
		}, 400, mobileErrInvalid, false, true},
		{"a code issued to the web application", func(f *flowHarness, a mobileAttempt, b map[string]string) {
			b["code"] = f.idp.issueCodeFor(f.idp.clientID, "ana", a.nonce, a.challenge)
			b["proof"] = a.key.proof(f.t, proofTokenDomain, a.state, b["code"])
		}, 400, mobileErrInvalid, false, false},
		{"an unknown code", func(f *flowHarness, a mobileAttempt, b map[string]string) {
			b["code"] = "code-unknown"
			b["proof"] = a.key.proof(f.t, proofTokenDomain, a.state, b["code"])
		}, 400, mobileErrInvalid, false, false},

		// The ID token (same table as the web login's TestIDTokenValidation, for the mobile audience).
		{"ID token for the web audience", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.idp.mutateClaims = func(c map[string]any) { c["aud"] = []string{f.idp.clientID} }
		}, 403, mobileErrNotAllowed, true, false},
		{"ID token for another audience", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.idp.mutateClaims = func(c map[string]any) { c["aud"] = []string{"another-client"} }
		}, 403, mobileErrNotAllowed, true, false},
		{"another issuer", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.idp.mutateClaims = func(c map[string]any) { c["iss"] = "https://evil.example" }
		}, 403, mobileErrNotAllowed, true, false},
		{"expired", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.idp.mutateClaims = func(c map[string]any) { c["exp"] = f.idp.now().Add(-time.Second).Unix() }
		}, 403, mobileErrNotAllowed, true, false},
		{"not valid yet", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.idp.mutateClaims = func(c map[string]any) { c["nbf"] = f.idp.now().Add(time.Hour).Unix() }
		}, 403, mobileErrNotAllowed, true, false},
		{"wrong nonce", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.idp.mutateClaims = func(c map[string]any) { c["nonce"] = "someone-elses-nonce" }
		}, 403, mobileErrNotAllowed, true, false},
		{"no nonce", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.idp.mutateClaims = func(c map[string]any) { delete(c, "nonce") }
		}, 403, mobileErrNotAllowed, true, false},
		{"alg none", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.idp.signWith = func(h, p map[string]any) string { return none(h, p) }
		}, 403, mobileErrNotAllowed, true, false},
		{"alg HS256", func(f *flowHarness, _ mobileAttempt, _ map[string]string) { f.idp.signWith = hs256(f) }, 403, mobileErrNotAllowed, true, false},
		{"a kid the provider never published", func(f *flowHarness, _ mobileAttempt, _ map[string]string) { f.idp.signKid = "forged" }, 403, mobileErrNotAllowed, true, false},

		// What the userinfo and the directory say.
		{"email not verified", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.idp.mutateUserinfo = func(c map[string]any) { c["email_verified"] = false }
		}, 403, mobileErrEmailNotVerif, true, false},
		{"userinfo of another subject", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.idp.mutateUserinfo = func(c map[string]any) { c["sub"] = "someone-else" }
		}, 403, mobileErrNotAllowed, true, false},
		{"userinfo down", func(f *flowHarness, _ mobileAttempt, _ map[string]string) { f.idp.userinfoStatus = 500 }, 503, mobileErrUnavailable, true, false},
		{"an owner without a strong factor", func(f *flowHarness, _ mobileAttempt, _ map[string]string) {
			f.dir.owners = map[string]bool{"user-ana": true}
		}, 403, mobileErrNotAllowed, true, false},
		{"the directory is down", func(f *flowHarness, _ mobileAttempt, _ map[string]string) { f.dir.err = ErrAccountDeleted }, 403, mobileErrNotAllowed, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFlowHarness(t)
			a := f.mobileStart(newDeviceKey(t))
			body := f.tokenBody(a, "ana")
			tc.edit(f, a, body)
			before := f.idp.tokenN.Load()

			w := f.post(MobileTokenPath, body, nil)
			if w.Code != tc.status || errorCode(t, w) != tc.code {
				t.Fatalf("status %d, error %q (%s), want %d %q", w.Code, errorCode(t, w), w.Body.String(), tc.status, tc.code)
			}
			if f.sessions() != 0 {
				t.Error("a session was created")
			}
			if tc.wantNoCall && f.idp.tokenN.Load() != before {
				t.Error("the identity provider was called before the app proved itself")
			}
			if tc.wasIssued && len(f.idp.revokedTokens()) == 0 {
				t.Error("the refresh token that will not be used was not revoked")
			}
			// The state was used up, whatever the outcome (when the request reached the store).
			if body["state"] != a.state || tc.name == "code of the wrong shape" {
				return
			}
			f.idp.mutateClaims, f.idp.signWith, f.idp.signKid, f.idp.mutateUserinfo, f.idp.userinfoStatus = nil, nil, "", nil, 0
			f.dir.err, f.dir.owners = nil, nil
			again := f.tokenBody(a, "ana")
			if w2 := f.post(MobileTokenPath, again, nil); w2.Code != http.StatusBadRequest {
				t.Errorf("the state worked a second time: status %d", w2.Code)
			}
			if strings.Contains(w.Body.String(), "ana") || strings.Contains(w.Body.String(), "example") {
				t.Errorf("the error body says too much: %s", w.Body.String())
			}
		})
	}
}

func TestMobileLoginAuditFailureLeavesNoSession(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	a := f.mobileStart(newDeviceKey(t))
	f.audit.err = context.DeadlineExceeded
	w := f.post(MobileTokenPath, f.tokenBody(a, "ana"), nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
	if f.sessions() != 0 {
		t.Error("a login that could not be audited left a session")
	}
	if len(f.idp.revokedTokens()) == 0 {
		t.Error("the refresh token was not revoked")
	}
}

func TestMobileStartRefusals(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	key := newDeviceKey(t)
	good := map[string]any{"session_pub": key.spkiB6, "code_challenge": oauth2.S256ChallengeFromVerifier(oauth2.GenerateVerifier())}
	with := func(k string, v any) map[string]any {
		m := map[string]any{}
		for kk, vv := range good {
			m[kk] = vv
		}
		m[k] = v
		return m
	}
	tests := []struct {
		name string
		body any
		hdr  map[string]string
		want int
	}{
		{"no key", with("session_pub", ""), nil, 400},
		{"not a key", with("session_pub", "AAAA"), nil, 400},
		{"challenge of the wrong length", with("code_challenge", "short"), nil, 400},
		{"challenge with a forbidden character", with("code_challenge", strings.Repeat("+", 43)), nil, 400},
		{"unknown field", with("extra", 1), nil, 400},
		{"not JSON", "{{{", nil, 400},
		{"a second JSON value after the first", `{"session_pub":"` + key.spkiB6 + `"} {}`, nil, 400},
		{"wrong content type", good, map[string]string{"Content-Type": "text/plain"}, 415},
		{"no content type", good, map[string]string{"Content-Type": ""}, 415},
		{"too big", with("session_pub", strings.Repeat("A", mobileMaxBody)), nil, 400},
		{"a number where a string goes", with("code_challenge", 1), nil, 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := f.post(MobileStartPath, tc.body, tc.hdr)
			if w.Code != tc.want {
				t.Fatalf("status %d (%s), want %d", w.Code, w.Body.String(), tc.want)
			}
		})
	}
	// Only POST.
	if w := f.do(http.MethodGet, MobileStartPath, nil, nil); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status %d", w.Code)
	}
}

func TestMobileEndpointsAreOffWhenMobileIsNotConfigured(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	// A new Handler (not a copy of f.h: it holds locks) whose provider has no mobile client.
	h := &Handler{Provider: NewProvider(ProviderConfig{
		Issuer: f.idp.srv.URL, ClientID: f.idp.clientID, ClientSecret: f.idp.clientSecret,
		RedirectURL: publicURL + CallbackPath, AllowedAlgs: []string{"ES256"}, Now: f.h.Now,
	})}
	mux := http.NewServeMux()
	h.Mount(mux)
	for _, p := range []string{MobileStartPath, MobileTokenPath, MobileRefreshPath, MobileLogoutPath} {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, publicURL+p, strings.NewReader("{}"))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, w.Code)
		}
	}
}

type fixedLimiter struct{ d ratelimit.Decision }

func (l fixedLimiter) Allow(context.Context, ratelimit.Scope, string, bool) ratelimit.Decision {
	return l.d
}

func TestMobileEndpointsAreRateLimitedAndFailClosed(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		d    ratelimit.Decision
		want int
	}{
		"limited":     {ratelimit.RateLimited, http.StatusTooManyRequests},
		"unavailable": {ratelimit.Unavailable, http.StatusServiceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFlowHarness(t)
			f.h.Limiter = fixedLimiter{tc.d}
			key := newDeviceKey(t)
			for _, p := range []string{MobileStartPath, MobileTokenPath, MobileRefreshPath, MobileLogoutPath} {
				w := f.post(p, map[string]string{"session_pub": key.spkiB6, "code_challenge": strings.Repeat("A", 43)}, nil)
				if w.Code != tc.want {
					t.Errorf("%s: status %d, want %d", p, w.Code, tc.want)
				}
			}
			for _, k := range f.adminKeys() {
				if strings.HasPrefix(k, pendingPrefix) {
					t.Error("a refused request left a pending login")
				}
			}
		})
	}
}

func TestMobileEndpointsWithoutALimiterAreNotServed(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	f.h.Limiter = nil
	if w := f.post(MobileStartPath, "{}", nil); w.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503", w.Code)
	}
}

// --- the Bearer ---

func TestBearerIsOpaqueAndOtherCredentialsAreRefused(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	resp := h.mobileLogin(newDeviceKey(t), "ana")
	cookie := h.signIn("bruno")
	idToken := h.idp.idTokenFor(h.idp.mobileClientID, "ana", "")

	call := func(hdr map[string][]string) (*Principal, bool, connect.Code) {
		p, called, err := h.callRPC(privateProc, hdr)
		if err == nil {
			return p, called, 0
		}
		code, msg := codeOf(t, err)
		if msg != msgUnauthenticated && msg != msgAuthUnavailable {
			t.Errorf("message %q leaks something", msg)
		}
		return p, called, code
	}

	// The real thing.
	if p, called, code := call(map[string][]string{"Authorization": {"Bearer " + resp.AccessToken}}); !called || code != 0 || p == nil || p.UserID != "user-ana" || p.SessionRef != resp.Session {
		t.Fatalf("a valid Bearer: principal %+v, called %v, code %v", p, called, code)
	}
	// The scheme is case-insensitive (RFC 7235).
	if _, called, _ := call(map[string][]string{"Authorization": {"bearer " + resp.AccessToken}}); !called {
		t.Error("lower-case scheme refused")
	}
	// The same cross-site headers a WebView sends must not matter for a Bearer.
	if _, called, _ := call(map[string][]string{"Authorization": {"Bearer " + resp.AccessToken}, "Origin": {"https://localhost"}, "Sec-Fetch-Site": {"cross-site"}}); !called {
		t.Error("a Bearer was refused for being cross-site")
	}

	refused := map[string]map[string][]string{
		"an ID token as the Bearer":             {"Authorization": {"Bearer " + idToken}},
		"a JWT-shaped string":                   {"Authorization": {"Bearer aaa.bbb.ccc"}},
		"a web cookie value as the Bearer":      {"Authorization": {"Bearer " + cookie.Value}},
		"the prefix without a value":            {"Authorization": {"Bearer " + bearerPrefix}},
		"a Bearer of the right shape, unknown":  {"Authorization": {"Bearer " + bearerPrefix + strings.Repeat("A", 43)}},
		"the Bearer with a character appended":  {"Authorization": {"Bearer " + resp.AccessToken + "A"}},
		"the Bearer with a character cut":       {"Authorization": {"Bearer " + resp.AccessToken[:len(resp.AccessToken)-1]}},
		"Basic instead of Bearer":               {"Authorization": {"Basic " + resp.AccessToken}},
		"no scheme":                             {"Authorization": {resp.AccessToken}},
		"two Authorization headers":             {"Authorization": {"Bearer " + resp.AccessToken, "Bearer " + resp.AccessToken}},
		"an empty Authorization header":         {"Authorization": {""}},
		"Bearer and the session cookie":         {"Authorization": {"Bearer " + resp.AccessToken}, "Cookie": {SessionCookieName + "=" + cookie.Value}},
		"a bad Bearer with a valid cookie":      {"Authorization": {"Bearer nope"}, "Cookie": {SessionCookieName + "=" + cookie.Value}},
		"the cookie of a mobile session's hash": {"Cookie": {SessionCookieName + "=" + resp.Session[:43]}},
	}
	for name, hdr := range refused {
		p, called, code := call(hdr)
		if called || p != nil || code != connect.CodeUnauthenticated {
			t.Errorf("%s: called %v, principal %v, code %v", name, called, p, code)
		}
	}
	// And the other way: the Bearer is not a cookie.
	if _, called, _ := call(map[string][]string{"Cookie": {SessionCookieName + "=" + strings.TrimPrefix(resp.AccessToken, bearerPrefix)}}); called {
		t.Error("the secret of a Bearer opened a web session")
	}
}

func TestBearerOnTheSessionEndpointFollowsTheSameRules(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	resp := f.mobileLogin(newDeviceKey(t), "ana")
	cookie := f.signIn("bruno")
	idToken := f.idp.idTokenFor(f.idp.mobileClientID, "ana", "")

	if code, _ := f.sessionStatus(bearerHdr(resp.AccessToken)); code != http.StatusOK {
		t.Errorf("valid Bearer: %d", code)
	}
	if code, _ := f.sessionStatus(map[string]string{"Authorization": "Bearer " + resp.AccessToken, "Origin": "https://localhost", "Sec-Fetch-Site": "cross-site"}); code != http.StatusOK {
		t.Errorf("a Bearer from the WebView origin: %d", code)
	}
	for name, hdr := range map[string]map[string]string{
		"an ID token":          bearerHdr(idToken),
		"unknown":              bearerHdr(bearerPrefix + strings.Repeat("A", 43)),
		"Basic":                {"Authorization": "Basic abc"},
		"Bearer with a cookie": {"Authorization": "Bearer " + resp.AccessToken, "Cookie": SessionCookieName + "=" + cookie.Value},
	} {
		if code, body := f.sessionStatus(hdr); code != http.StatusUnauthorized || strings.Contains(body, "user-") {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	// A cross-site request with a cookie is still refused.
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, publicURL+SessionPath, nil)
	r.AddCookie(cookie)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("a cross-site cookie request: %d", w.Code)
	}
}

// --- the renewal ---

func TestMobileRefreshRotatesTheBearer(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	key := newDeviceKey(t)
	first := h.mobileLogin(key, "ana")

	w := h.post(MobileRefreshPath, h.refreshBody(key, first.Session, 1), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var second bearerResponse
	_ = json.Unmarshal(w.Body.Bytes(), &second)
	if second.AccessToken == first.AccessToken || !strings.HasPrefix(second.AccessToken, bearerPrefix) || second.Session != "" || second.ExpiresIn != 600 {
		t.Fatalf("response %+v", second)
	}
	// The old Bearer stops working at once; the new one works.
	if code, _ := h.sessionStatus(bearerHdr(first.AccessToken)); code != http.StatusUnauthorized {
		t.Errorf("the previous Bearer still works: %d", code)
	}
	if code, _ := h.sessionStatus(bearerHdr(second.AccessToken)); code != http.StatusOK {
		t.Errorf("the new Bearer does not work: %d", code)
	}
	// Only one Bearer key is left in the store for the session.
	n := 0
	for _, k := range h.adminKeys() {
		if strings.HasPrefix(k, bearerKeyPrefix) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d Bearer keys, want 1", n)
	}
}

func TestMobileRefreshWorksAfterTheBearerExpired(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	key := newDeviceKey(t)
	first := h.mobileLogin(key, "ana")
	// The Bearer's key is gone (its 10 minutes passed).
	if err := h.admin.Do(context.Background(), h.admin.B().Del().Key(bearerKeyPrefix+hashHex(bearerSecret(first.AccessToken))).Build()).Error(); err != nil {
		t.Fatal(err)
	}
	if code, _ := h.sessionStatus(bearerHdr(first.AccessToken)); code != http.StatusUnauthorized {
		t.Fatalf("an expired Bearer: %d", code)
	}
	w := h.post(MobileRefreshPath, h.refreshBody(key, first.Session, 1), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("renewal after expiry: %d %s", w.Code, w.Body.String())
	}
}

func TestMobileRefreshReuseEndsTheSession(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	key := newDeviceKey(t)
	first := h.mobileLogin(key, "ana")

	// A copy of the first renewal, as an attacker who recorded it would send it.
	copyOf := h.refreshBody(key, first.Session, 1)
	w := h.post(MobileRefreshPath, copyOf, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("first use: %d", w.Code)
	}
	var live bearerResponse
	_ = json.Unmarshal(w.Body.Bytes(), &live)
	rtBefore := h.mustSession(first.Session).RefreshToken

	w = h.post(MobileRefreshPath, copyOf, nil)
	if w.Code != http.StatusUnauthorized || errorCode(t, w) != mobileErrUnauthenticated {
		t.Fatalf("the copy: %d %s", w.Code, w.Body.String())
	}
	// The whole session is gone: even the legitimate holder's Bearer and key are useless now.
	if code, _ := h.sessionStatus(bearerHdr(live.AccessToken)); code != http.StatusUnauthorized {
		t.Errorf("the Bearer survived the reuse alarm: %d", code)
	}
	if w := h.post(MobileRefreshPath, h.refreshBody(key, first.Session, 2), nil); w.Code != http.StatusUnauthorized {
		t.Errorf("a fresh counter on a dead session: %d", w.Code)
	}
	if h.sessions() != 0 {
		t.Error("the session is still stored")
	}
	if !slices.Contains(h.idp.revokedTokens(), rtBefore) {
		t.Error("the refresh token was not revoked at the identity provider")
	}
	if !slices.Contains(h.audit.actions(), "auth.mobile_reuse:refresh_reuse") {
		t.Errorf("audit trail: %v", h.audit.actions())
	}
}

func (f *flowHarness) mustSession(ref string) Session {
	f.t.Helper()
	s, err := f.store.GetByRef(context.Background(), ref)
	if err != nil {
		f.t.Fatalf("GetByRef: %v", err)
	}
	return s
}

func TestMobileRefreshCounterRules(t *testing.T) {
	t.Parallel()
	key := newDeviceKey(t)
	tests := []struct {
		name     string
		counters []uint64 // spent in order; the last one is the one checked
		want     int
		revoked  bool
	}{
		{"the next one", []uint64{1}, 200, false},
		{"a gap left by lost responses", []uint64{1, 5}, 200, false},
		{"the largest gap", []uint64{1, 1 + counterGap}, 200, false},
		{"one past the gap", []uint64{1, 2 + counterGap}, 401, false},
		{"the same counter twice", []uint64{1, 1}, 401, true},
		{"an older counter", []uint64{3, 2}, 401, true},
		{"counter zero", []uint64{0}, 400, false},
		{"over the limit", []uint64{maxCounter + 1}, 400, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFlowHarness(t)
			resp := f.mobileLogin(key, "ana")
			var w *httptest.ResponseRecorder
			for _, c := range tc.counters {
				w = f.post(MobileRefreshPath, f.refreshBody(key, resp.Session, c), nil)
			}
			if w.Code != tc.want {
				t.Fatalf("status %d (%s), want %d", w.Code, w.Body.String(), tc.want)
			}
			if gone := f.sessions() == 0; gone != tc.revoked {
				t.Errorf("session gone = %v, want %v", gone, tc.revoked)
			}
		})
	}
}

func TestMobileRefreshRefusals(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	key, other := newDeviceKey(t), newDeviceKey(t)
	resp := f.mobileLogin(key, "ana")
	web := f.signIn("bruno")
	webRef := hashHex(web.Value)

	bodyFor := func(k deviceKey, session string, counter uint64) map[string]any {
		return f.refreshBody(k, session, counter)
	}
	tests := []struct {
		name string
		body any
		want int
	}{
		{"another key signs", bodyFor(other, resp.Session, 1), 401},
		{"a session that does not exist", bodyFor(key, strings.Repeat("a", 64), 1), 401},
		{"a web session's reference", bodyFor(key, webRef, 1), 401},
		{"a reference of the wrong shape", map[string]any{"session": "nope", "counter": 1, "proof": "x"}, 400},
		{"a proof for another counter", map[string]any{"session": resp.Session, "counter": 1, "proof": key.proof(t, proofRefreshDomain, resp.Session, "2")}, 401},
		{"a proof for another session", map[string]any{"session": resp.Session, "counter": 1, "proof": key.proof(t, proofRefreshDomain, strings.Repeat("a", 64), "1")}, 401},
		{"a token proof used for a renewal", map[string]any{"session": resp.Session, "counter": 1, "proof": key.proof(t, proofTokenDomain, resp.Session, "1")}, 401},
		{"no proof", map[string]any{"session": resp.Session, "counter": 1, "proof": ""}, 401},
		{"a negative counter", `{"session":"` + resp.Session + `","counter":-1,"proof":"x"}`, 400},
		{"a fractional counter", `{"session":"` + resp.Session + `","counter":1.5,"proof":"x"}`, 400},
		{"unknown field", map[string]any{"session": resp.Session, "counter": 1, "proof": "x", "admin": true}, 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if w := f.post(MobileRefreshPath, tc.body, nil); w.Code != tc.want {
				t.Fatalf("status %d (%s), want %d", w.Code, w.Body.String(), tc.want)
			}
		})
	}
	// Nobody who does not hold the key can spend a counter or set off the alarm: the real
	// session is intact and the counter is still 1.
	if f.sessions() != 2 {
		t.Fatalf("%d sessions, want 2 (the phone's and the browser's)", f.sessions())
	}
	if w := f.post(MobileRefreshPath, f.refreshBody(key, resp.Session, 1), nil); w.Code != http.StatusOK {
		t.Errorf("the legitimate renewal after all the refusals: %d", w.Code)
	}
}

func TestMobileRefreshRefreshesTheProviderTokensAndEndsRevokedSessions(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	key := newDeviceKey(t)
	resp := h.mobileLogin(key, "ana")
	rtBefore := h.mustSession(resp.Session).RefreshToken

	// The provider's access token expires: the next renewal refreshes it, server side.
	*h.now = h.now.Add(11 * time.Minute)
	if w := h.post(MobileRefreshPath, h.refreshBody(key, resp.Session, 1), nil); w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	if h.idp.refreshN.Load() != 1 {
		t.Errorf("%d refresh grants, want 1", h.idp.refreshN.Load())
	}
	if h.mustSession(resp.Session).RefreshToken == rtBefore {
		t.Error("the refresh token was not rotated")
	}

	// The provider no longer honours the session (user disabled): the renewal ends it.
	*h.now = h.now.Add(11 * time.Minute)
	h.idp.refreshErr = "invalid_grant"
	w := h.post(MobileRefreshPath, h.refreshBody(key, resp.Session, 2), nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked session: %d", w.Code)
	}
	if h.sessions() != 0 {
		t.Error("the session survived")
	}
}

func TestMobileRefreshOutageIsUnavailableNotUnauthenticated(t *testing.T) {
	t.Parallel()
	h := newAuthHarness(t)
	key := newDeviceKey(t)
	resp := h.mobileLogin(key, "ana")
	*h.now = h.now.Add(RefreshGrace + 20*time.Minute) // past the grace period
	h.idp.refreshStatus = http.StatusServiceUnavailable
	w := h.post(MobileRefreshPath, h.refreshBody(key, resp.Session, 1), nil)
	if w.Code != http.StatusServiceUnavailable || errorCode(t, w) != mobileErrUnavailable {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if h.sessions() != 1 {
		t.Error("an outage ended the session")
	}
}

// --- the logout ---

func TestMobileLogout(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	key := newDeviceKey(t)
	resp := f.mobileLogin(key, "ana")
	rt := f.mustSession(resp.Session).RefreshToken

	if w := f.post(MobileLogoutPath, "{}", nil); w.Code != http.StatusUnauthorized {
		t.Errorf("without a Bearer: %d", w.Code)
	}
	if w := f.post(MobileLogoutPath, "{}", map[string]string{"Authorization": "Bearer nope"}); w.Code != http.StatusUnauthorized {
		t.Errorf("a malformed Bearer: %d", w.Code)
	}
	w := f.post(MobileLogoutPath, "{}", bearerHdr(resp.AccessToken))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d", w.Code)
	}
	if f.sessions() != 0 {
		t.Error("the session is still stored")
	}
	if !slices.Contains(f.idp.revokedTokens(), rt) {
		t.Error("the refresh token was not revoked at the provider, as the mobile client")
	}
	if code, _ := f.sessionStatus(bearerHdr(resp.AccessToken)); code != http.StatusUnauthorized {
		t.Errorf("the Bearer works after the logout: %d", code)
	}
	if !slices.Contains(f.audit.actions(), "auth.logout:") {
		t.Errorf("audit trail: %v", f.audit.actions())
	}
	// Again: the app can always finish wiping itself.
	if w := f.post(MobileLogoutPath, "{}", bearerHdr(resp.AccessToken)); w.Code != http.StatusNoContent {
		t.Errorf("second logout: %d", w.Code)
	}
	// A renewal can no longer bring it back.
	if w := f.post(MobileRefreshPath, f.refreshBody(key, resp.Session, 1), nil); w.Code != http.StatusUnauthorized {
		t.Errorf("renewal after logout: %d", w.Code)
	}
}

func TestMobileLogoutStillEndsOurSessionWhenTheProviderIsDown(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	resp := f.mobileLogin(newDeviceKey(t), "ana")
	f.idp.revokeStatus = http.StatusServiceUnavailable
	if w := f.post(MobileLogoutPath, "{}", bearerHdr(resp.AccessToken)); w.Code != http.StatusNoContent {
		t.Fatalf("status %d", w.Code)
	}
	if f.sessions() != 0 {
		t.Error("a failed revocation kept the session")
	}
	if !slices.Contains(f.audit.actions(), "auth.logout:revoke_failed") {
		t.Errorf("audit trail: %v", f.audit.actions())
	}
}

// --- who can end a phone's session ---

func TestBackchannelLogoutAndTheWebSessionListReachThePhone(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	resp := f.mobileLogin(newDeviceKey(t), "ana") // the mobile token has sid "sid-ana"
	form := url.Values{"logout_token": {f.idp.logoutToken("ana", "sid-ana", nil)}}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, publicURL+BackChannelLogoutPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("back-channel logout: %d", w.Code)
	}
	if code, _ := f.sessionStatus(bearerHdr(resp.AccessToken)); code != http.StatusUnauthorized {
		t.Errorf("the phone's Bearer survived the provider's logout: %d", code)
	}
	if f.sessions() != 0 {
		t.Error("the session is still stored")
	}
}

func TestDeletingAMobileSessionDeletesItsBearer(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	resp := f.mobileLogin(newDeviceKey(t), "ana")
	if err := f.store.DeleteByRef(context.Background(), resp.Session); err != nil {
		t.Fatal(err)
	}
	for _, k := range f.adminKeys() {
		if strings.HasPrefix(k, bearerKeyPrefix) {
			t.Errorf("a Bearer key outlived its session: %s", k)
		}
	}
}

// --- the store ---

func TestPendingLoginIsUsedOnceAndBounded(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	ctx := context.Background()
	state, _ := randomToken()
	p := MobilePending{Challenge: strings.Repeat("c", 43), Nonce: "n", DevicePub: []byte{1}}
	if err := f.store.PutPending(ctx, state, p); err != nil {
		t.Fatal(err)
	}
	if err := f.store.PutPending(ctx, state, p); err == nil {
		t.Error("the same state was stored twice")
	}
	got, err := f.store.TakePending(ctx, state)
	if err != nil || got.Nonce != "n" {
		t.Fatalf("TakePending: %+v, %v", got, err)
	}
	if _, err := f.store.TakePending(ctx, state); !errors.Is(err, ErrNoPending) {
		t.Errorf("second take: %v", err)
	}
	if _, err := f.store.TakePending(ctx, "not a token"); !errors.Is(err, ErrNoPending) {
		t.Errorf("take with a bad state: %v", err)
	}

	// The cap: nobody fills the store through the public /start.
	var full error
	for range maxPending + 5 {
		s, _ := randomToken()
		if err := f.store.PutPending(ctx, s, p); err != nil {
			full = err
			break
		}
	}
	if !errors.Is(full, ErrPendingFull) {
		t.Fatalf("after %d pending logins: %v, want ErrPendingFull", maxPending, full)
	}
	// And a finished login frees a place.
	s, _ := randomToken()
	if err := f.store.PutPending(ctx, s, p); err == nil {
		t.Fatal("still full, expected a refusal")
	}
}

func TestPendingExpires(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	ctx := context.Background()
	state, _ := randomToken()
	if err := f.store.PutPending(ctx, state, MobilePending{Challenge: "c", Nonce: "n", DevicePub: []byte{1}}); err != nil {
		t.Fatal(err)
	}
	ttl, err := f.admin.Do(ctx, f.admin.B().Pttl().Key(pendingPrefix+hashHex(state)).Build()).AsInt64()
	if err != nil || ttl <= 0 || time.Duration(ttl)*time.Millisecond > PendingTTL {
		t.Fatalf("pending TTL = %d ms, %v", ttl, err)
	}
}

func TestBearerHasAShortLife(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	resp := f.mobileLogin(newDeviceKey(t), "ana")
	ttl, err := f.admin.Do(context.Background(), f.admin.B().Pttl().Key(bearerKeyPrefix+hashHex(bearerSecret(resp.AccessToken))).Build()).AsInt64()
	if err != nil || ttl <= 0 || time.Duration(ttl)*time.Millisecond > BearerTTL {
		t.Fatalf("Bearer TTL = %d ms, %v", ttl, err)
	}
}

func TestOnlyTheHashOfABearerIsStored(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	resp := f.mobileLogin(newDeviceKey(t), "ana")
	secret := bearerSecret(resp.AccessToken)
	for _, k := range f.adminKeys() {
		if strings.Contains(k, secret) {
			t.Errorf("the Bearer is in a key name: %s", k)
		}
		v, err := f.admin.Do(context.Background(), f.admin.B().Type().Key(k).Build()).ToString()
		if err != nil {
			continue
		}
		var dump string
		switch v {
		case "string":
			dump, _ = f.admin.Do(context.Background(), f.admin.B().Get().Key(k).Build()).ToString()
		case "hash":
			m, _ := f.admin.Do(context.Background(), f.admin.B().Hgetall().Key(k).Build()).AsStrMap()
			for kk, vv := range m {
				dump += kk + "=" + vv + ";"
			}
		}
		if strings.Contains(dump, secret) {
			t.Errorf("the Bearer is stored in clear in %s", k)
		}
	}
}

func FuzzBearerFromHeader(f *testing.F) {
	f.Add("Bearer " + bearerPrefix + strings.Repeat("A", 43))
	f.Add("bearer x")
	f.Add("")
	f.Add("Basic abc")
	f.Fuzz(func(t *testing.T, v string) {
		tok, present := BearerFromHeader([]string{v})
		if !present {
			t.Fatal("a header that exists is reported as absent")
		}
		if tok != "" && (!strings.HasPrefix(tok, bearerPrefix) || bearerSecret(tok) == "" || len(tok) != len(bearerPrefix)+43) {
			t.Fatalf("accepted %q", tok)
		}
	})
}
