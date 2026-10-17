package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"
	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

const publicURL = "https://api.example.eu"

type fakeDirectory struct {
	mu     sync.Mutex
	err    error
	calls  int
	owners map[string]bool
	last   struct{ sub, email, name string }
}

func (d *fakeDirectory) ResolveLogin(_ context.Context, sub, email, name string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.last.sub, d.last.email, d.last.name = sub, email, name
	if d.err != nil {
		return "", d.err
	}
	return "user-" + sub, nil
}

func (d *fakeDirectory) IsOwner(_ context.Context, userID string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return false, d.err
	}
	return d.owners[userID], nil
}

type fakeAudit struct {
	mu     sync.Mutex
	err    error
	events []AuditEvent
}

func (a *fakeAudit) Record(_ context.Context, e AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	a.events = append(a.events, e)
	return nil
}

func (a *fakeAudit) actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, e := range a.events {
		out = append(out, e.Action+":"+e.Reason)
	}
	return out
}

type fakeRefusals struct {
	mu     sync.Mutex
	counts map[string]int
}

func (f *fakeRefusals) Count(_ context.Context, reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counts == nil {
		f.counts = map[string]int{}
	}
	f.counts[reason]++
}

func (f *fakeRefusals) get(reason string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[reason]
}

type flowHarness struct {
	t        *testing.T
	idp      *fakeIdP
	h        *Handler
	mux      *http.ServeMux
	store    *Store
	dir      *fakeDirectory
	audit    *fakeAudit
	admin    valkey.Client
	refusals *fakeRefusals
	now      *time.Time
	logs     *syncLog
}

type syncLog struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *syncLog) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncLog) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func newFlowHarness(t *testing.T) *flowHarness {
	t.Helper()
	v := testutil.StartValkey(t)
	now := t0
	clock := func() time.Time { return now }
	idp := newFakeIdP(t, clock)
	app := newClient(t, v.Addr, v.Username, v.Password)
	store := NewStore(app, sealer(t, PurposeSessionToken, key("k1", 1)), 168*time.Hour, 720*time.Hour, clock)
	logs := &syncLog{}
	f := &flowHarness{
		t: t, idp: idp, store: store, dir: &fakeDirectory{}, audit: &fakeAudit{}, now: &now, logs: logs, mux: http.NewServeMux(),
		admin: newClient(t, v.Addr, "default", v.AdminPassword), refusals: &fakeRefusals{},
	}
	f.h = &Handler{
		Provider: NewProvider(ProviderConfig{
			Issuer: idp.srv.URL, ClientID: idp.clientID, ClientSecret: idp.clientSecret,
			RedirectURL: publicURL + CallbackPath, AllowedAlgs: []string{"ES256"}, Now: clock,
		}),
		Store:        store,
		LoginCookies: NewLoginCookies(sealer(t, PurposeLoginCookie, key("k1", 1)), clock),
		Users:        f.dir, RecoveryCodes: &fakeRecoveryStore{}, Audit: f.audit,
		Log: slog.New(slog.NewJSONHandler(logs, nil)), Refusals: f.refusals,
		PublicURL: publicURL, SessionMaxAge: 720 * time.Hour, Now: clock,
	}
	f.h.Authn = &Authenticator{Provider: f.h.Provider, Store: store, Audit: f.audit, Log: f.h.Log, Now: clock}
	f.h.Mount(f.mux)
	return f
}

func (f *flowHarness) login(subject, sid string, amr []string) string {
	f.t.Helper()
	a := f.startLogin("")
	code := f.idp.issueCode(subject, a.nonce, a.challenge)
	f.idp.mutateClaims = func(m map[string]any) {
		m["sid"] = sid
		m["amr"] = amr
	}
	defer func() { f.idp.mutateClaims = nil }()
	w := f.callback(a, code)
	if w.Code != http.StatusOK {
		f.t.Fatalf("login failed: status %d: %s", w.Code, w.Body.String())
	}
	ck := cookieNamed(w, SessionCookieName)
	if ck == nil {
		f.t.Fatal("login did not set session cookie")
	}
	return ck.Value
}

func (f *flowHarness) do(method, target string, cookies []*http.Cookie, hdr map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), method, publicURL+target, nil)
	r.RemoteAddr = "203.0.113.7:40000"
	r.Header.Set("User-Agent", "test-agent")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// attempt is a login that has been started: what the browser holds and what the provider
// would have recorded at /authorize.
type attempt struct {
	state, nonce, challenge string
	cookie                  *http.Cookie
}

func (f *flowHarness) startLogin(returnTo string) attempt {
	f.t.Helper()
	target := LoginPath
	if returnTo != "" {
		target += "?return_to=" + url.QueryEscape(returnTo)
	}
	w := f.do(http.MethodGet, target, nil, nil)
	if w.Code != http.StatusFound {
		f.t.Fatalf("login: status %d: %s", w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		f.t.Fatalf("login location: %v", err)
	}
	q := loc.Query()
	ck := cookieNamed(w, LoginCookieName)
	if ck == nil {
		f.t.Fatal("login did not set the pre-login cookie")
	}
	return attempt{state: q.Get("state"), nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), cookie: ck}
}

func (f *flowHarness) callback(a attempt, code string, extra ...*http.Cookie) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.doCallback(a, a.state, code, extra...)
}

func (f *flowHarness) doCallback(a attempt, state, code string, extra ...*http.Cookie) *httptest.ResponseRecorder {
	f.t.Helper()
	target := CallbackPath + "?state=" + url.QueryEscape(state) + "&code=" + url.QueryEscape(code)
	return f.do(http.MethodGet, target, append([]*http.Cookie{a.cookie}, extra...), nil)
}

// signIn runs a full successful login and returns the session cookie.
func (f *flowHarness) signIn(sub string) *http.Cookie {
	f.t.Helper()
	a := f.startLogin("/")
	w := f.callback(a, f.idp.issueCode(sub, a.nonce, a.challenge))
	if w.Code != http.StatusOK {
		f.t.Fatalf("callback: status %d: %s", w.Code, w.Body.String())
	}
	ck := cookieNamed(w, SessionCookieName)
	if ck == nil {
		f.t.Fatal("no session cookie after login")
	}
	return &http.Cookie{Name: ck.Name, Value: ck.Value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}

func (f *flowHarness) sessions() int {
	f.t.Helper()
	n := 0
	for _, k := range f.adminKeys() {
		if len(k) == len(sessPrefix)+64 && strings.HasPrefix(k, sessPrefix) {
			n++
		}
	}
	return n
}

func (f *flowHarness) adminKeys() []string {
	f.t.Helper()
	keys, err := f.admin.Do(context.Background(), f.admin.B().Keys().Pattern("*").Build()).AsStrSlice()
	if err != nil {
		f.t.Fatalf("KEYS: %v", err)
	}
	return keys
}

func TestLoginRedirectsWithPKCEAndNonce(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	w := f.do(http.MethodGet, LoginPath+"?return_to=/equipas", nil, nil)

	if w.Code != http.StatusFound {
		t.Fatalf("status %d", w.Code)
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	q := loc.Query()
	if !strings.HasPrefix(w.Header().Get("Location"), f.idp.srv.URL+"/authorize?") {
		t.Errorf("location = %s", w.Header().Get("Location"))
	}
	for k, want := range map[string]string{
		"response_type": "code", "client_id": "pitbox-client", "redirect_uri": publicURL + CallbackPath,
		"code_challenge_method": "S256",
	} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	for _, scope := range []string{"openid", "profile", "email", "offline_access"} {
		if !strings.Contains(" "+q.Get("scope")+" ", " "+scope+" ") {
			t.Errorf("scope %q missing from %q", scope, q.Get("scope"))
		}
	}
	if q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge") == "" {
		t.Errorf("missing state, nonce or challenge: %v", q)
	}
	if q.Get("client_secret") != "" {
		t.Error("the client secret must never travel in the browser URL")
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}
	ck := cookieNamed(w, LoginCookieName)
	if ck == nil || ck.SameSite != http.SameSiteLaxMode || !ck.Secure || !ck.HttpOnly {
		t.Errorf("pre-login cookie = %+v", ck)
	}
}

func TestLoginIsUnavailableUntilTheProviderIsReachableAndBacksOff(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	f.idp.down.Store(true)

	if w := f.do(http.MethodGet, LoginPath, nil, nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("provider down: status %d, want 503", w.Code)
	}
	// The provider is back, but the backoff has not passed: no new attempt yet.
	f.idp.down.Store(false)
	if w := f.do(http.MethodGet, LoginPath, nil, nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("inside the backoff: status %d, want 503", w.Code)
	}
	*f.now = f.now.Add(6 * time.Second)
	if w := f.do(http.MethodGet, LoginPath, nil, nil); w.Code != http.StatusFound {
		t.Fatalf("after the backoff: status %d, want 302", w.Code)
	}
}

func TestSuccessfulLogin(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	a := f.startLogin("/equipas?x=1")
	code := f.idp.issueCode("ana", a.nonce, a.challenge)
	w := f.callback(a, code)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `http-equiv="refresh" content="0;url=/equipas?x=1"`) {
		t.Errorf("no meta refresh to the return_to: %s", body)
	}
	for _, leak := range []string{code, a.state, a.nonce, "<script"} {
		if strings.Contains(body, leak) {
			t.Errorf("the landing page contains %q", leak)
		}
	}
	for k, want := range map[string]string{
		"Cache-Control": "no-store", "Referrer-Policy": "no-referrer",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	} {
		if got := w.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	sc := cookieNamed(w, SessionCookieName)
	if sc == nil || sc.Name != "__Host-pitbox_session" || !sc.Secure || !sc.HttpOnly || sc.SameSite != http.SameSiteStrictMode ||
		sc.Path != "/" || sc.Domain != "" || sc.MaxAge != 30*24*3600 {
		t.Fatalf("session cookie = %+v", sc)
	}
	if lc := cookieNamed(w, LoginCookieName); lc == nil || lc.MaxAge >= 0 {
		t.Errorf("the pre-login cookie must be cleared: %+v", lc)
	}

	sess, err := f.store.Get(context.Background(), sc.Value)
	if err != nil {
		t.Fatalf("session not stored: %v", err)
	}
	if sess.UserID != "user-ana" || sess.Subject != "ana" || sess.SessionID != "sid-ana" || len(sess.AMR) != 2 ||
		!sess.AuthTime.Equal(t0.Add(-time.Minute)) || !sess.AccessExpires.Equal(t0.Add(600*time.Second)) ||
		!strings.HasPrefix(sess.RefreshToken, "rt-") {
		t.Errorf("unexpected session: %+v", sess)
	}
	if f.dir.last.sub != "ana" || f.dir.last.email != "ana@example.eu" || f.dir.last.name != "Name ana" {
		t.Errorf("directory got %+v", f.dir.last)
	}

	if len(f.audit.events) != 1 {
		t.Fatalf("audit events = %v", f.audit.actions())
	}
	e := f.audit.events[0]
	if e.Action != "auth.login" || e.ActorID != "user-ana" || e.SessionRef != sess.Ref() || e.Meta.IP.String() != "203.0.113.7" || e.Meta.UserAgent != "test-agent" {
		t.Errorf("audit event = %+v", e)
	}
	// Nothing sensitive in the logs.
	for _, secret := range []string{code, a.state, a.nonce, sess.RefreshToken, "ana@example.eu", sc.Value} {
		if strings.Contains(f.logs.String(), secret) {
			t.Errorf("a log line contains %q", secret)
		}
	}
}

func TestCallbackWithoutAValidLoginStateNeverCallsTheProvider(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	a := f.startLogin("")
	code := f.idp.issueCode("ana", a.nonce, a.challenge)
	before := f.idp.tokenN.Load()

	tamper := cookieWith(a.cookie.Value[:len(a.cookie.Value)-3] + "AAA")
	cases := map[string]*httptest.ResponseRecorder{
		"no cookie":      f.do(http.MethodGet, CallbackPath+"?state="+a.state+"&code="+code, nil, nil),
		"tampered":       f.callback(attempt{state: a.state, cookie: tamper}, code),
		"another state":  f.doCallback(a, a.state+"x", code),
		"empty state":    f.doCallback(a, "", code),
		"idp error":      f.do(http.MethodGet, CallbackPath+"?state="+a.state+"&error=access_denied", []*http.Cookie{a.cookie}, nil),
		"missing code":   f.do(http.MethodGet, CallbackPath+"?state="+a.state, []*http.Cookie{a.cookie}, nil),
		"login state ko": f.do(http.MethodGet, CallbackPath+"?code="+code, []*http.Cookie{a.cookie}, nil),
	}
	for name, w := range cases {
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, w.Code)
		}
		if w.Body.String() != msgBadRequest+"\n" {
			t.Errorf("%s: body %q", name, w.Body.String())
		}
		if cookieNamed(w, SessionCookieName) != nil {
			t.Errorf("%s: a session cookie was set", name)
		}
	}
	if got := f.idp.tokenN.Load(); got != before {
		t.Errorf("the token endpoint was called %d times for refused callbacks", got-before)
	}
	if f.sessions() != 0 {
		t.Error("a refused callback created a session")
	}
}

func TestAnAuthorizationCodeWorksOnce(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	a := f.startLogin("")
	code := f.idp.issueCode("ana", a.nonce, a.challenge)
	if w := f.callback(a, code); w.Code != http.StatusOK {
		t.Fatalf("first use: %d", w.Code)
	}
	// An attacker (or a browser retry) replays the same callback, cookie included.
	if w := f.callback(a, code); w.Code != http.StatusBadRequest {
		t.Fatalf("replay: status %d, want 400", w.Code)
	}
	if f.sessions() != 1 {
		t.Errorf("sessions = %d, want 1", f.sessions())
	}
}

func TestPKCEVerifierMustMatch(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	a := f.startLogin("")
	// The code was issued for another challenge (an attacker who stole the code but not the cookie).
	other := sha256.Sum256([]byte("another verifier"))
	code := f.idp.issueCode("ana", a.nonce, b64(other[:]))
	if w := f.callback(a, code); w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if f.sessions() != 0 {
		t.Error("a session was created despite the PKCE failure")
	}
}

func TestIDTokenValidation(t *testing.T) {
	t.Parallel()

	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tests := []struct {
		name   string
		setup  func(f *flowHarness, nonce string)
		reason string
	}{
		{"wrong nonce", func(f *flowHarness, _ string) {
			f.idp.mutateClaims = func(c map[string]any) { c["nonce"] = "someone-elses-nonce" }
		}, "nonce_mismatch"},
		{"no nonce", func(f *flowHarness, _ string) {
			f.idp.mutateClaims = func(c map[string]any) { delete(c, "nonce") }
		}, "nonce_mismatch"},
		{"another audience", func(f *flowHarness, _ string) {
			f.idp.mutateClaims = func(c map[string]any) { c["aud"] = []string{"another-client"} }
		}, "id_token_invalid"},
		{"another issuer", func(f *flowHarness, _ string) {
			f.idp.mutateClaims = func(c map[string]any) { c["iss"] = "https://evil.example" }
		}, "id_token_invalid"},
		{"expired", func(f *flowHarness, _ string) {
			f.idp.mutateClaims = func(c map[string]any) { c["exp"] = f.idp.now().Add(-time.Second).Unix() }
		}, "id_token_invalid"},
		{"not valid yet (nbf in the future)", func(f *flowHarness, _ string) {
			f.idp.mutateClaims = func(c map[string]any) { c["nbf"] = f.idp.now().Add(time.Hour).Unix() }
		}, "id_token_invalid"},
		{"alg none", func(f *flowHarness, _ string) {
			f.idp.signWith = func(h, p map[string]any) string {
				h["alg"] = "none"
				hb, _ := json.Marshal(h)
				pb, _ := json.Marshal(p)
				return b64(hb) + "." + b64(pb) + "."
			}
		}, "id_token_invalid"},
		{"alg HS256 signed with the client secret", func(f *flowHarness, _ string) {
			f.idp.signWith = func(h, p map[string]any) string {
				h["alg"] = "HS256"
				hb, _ := json.Marshal(h)
				pb, _ := json.Marshal(p)
				in := b64(hb) + "." + b64(pb)
				m := hmac.New(sha256.New, []byte(f.idp.clientSecret))
				m.Write([]byte(in))
				return in + "." + b64(m.Sum(nil))
			}
		}, "id_token_invalid"},
		{"signed by another key", func(f *flowHarness, _ string) {
			real := f.idp.key
			f.idp.key = otherKey
			f.idp.signWith = func(h, p map[string]any) string { defer func() { f.idp.key = real }(); return f.idp.sign(h, p) }
		}, "id_token_invalid"},
		{"payload altered after signing", func(f *flowHarness, _ string) {
			f.idp.signWith = func(h, p map[string]any) string {
				tok := f.idp.sign(h, p)
				parts := strings.Split(tok, ".")
				p["email"] = "admin@example.eu"
				pb, _ := json.Marshal(p)
				return parts[0] + "." + b64(pb) + "." + parts[2]
			}
		}, "id_token_invalid"},
		{"unknown kid", func(f *flowHarness, _ string) {
			f.idp.signWith = func(h, p map[string]any) string { h["kid"] = "other"; return f.idp.sign(h, p) }
		}, "id_token_invalid"},
		{"no subject", func(f *flowHarness, _ string) {
			f.idp.mutateClaims = func(c map[string]any) { c["sub"] = "" }
		}, "claims_invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFlowHarness(t)
			a := f.startLogin("")
			tc.setup(f, a.nonce)
			w := f.callback(a, f.idp.issueCode("ana", a.nonce, a.challenge))

			if w.Code != http.StatusForbidden || w.Body.String() != msgNotAllowed+"\n" {
				t.Fatalf("status %d body %q, want 403 with the fixed message", w.Code, w.Body.String())
			}
			if cookieNamed(w, SessionCookieName) != nil || f.sessions() != 0 || f.dir.calls != 0 {
				t.Error("a refused ID token must not reach the directory or create a session")
			}
			if got := f.audit.actions(); len(got) != 1 || got[0] != "auth.login_failed:"+tc.reason {
				t.Errorf("audit = %v, want auth.login_failed:%s", got, tc.reason)
			}
		})
	}
}

// Email and name come from the userinfo endpoint, with the access token, and the "sub" there must
// be the ID token's.
func TestUserInfoValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		setup  func(f *flowHarness)
		status int
		reason string
	}{
		{"email not verified", func(f *flowHarness) { f.idp.mutateUserinfo = func(c map[string]any) { c["email_verified"] = false } }, 403, "email_not_verified"},
		{"email_verified missing", func(f *flowHarness) { f.idp.mutateUserinfo = func(c map[string]any) { delete(c, "email_verified") } }, 403, "email_not_verified"},
		{"no email", func(f *flowHarness) { f.idp.mutateUserinfo = func(c map[string]any) { delete(c, "email") } }, 403, "no_email"},
		{"a different subject (OIDC Core 5.3.2)", func(f *flowHarness) { f.idp.mutateUserinfo = func(c map[string]any) { c["sub"] = "someone-else" } }, 403, "userinfo_sub_mismatch"},
		{"no subject", func(f *flowHarness) { f.idp.mutateUserinfo = func(c map[string]any) { delete(c, "sub") } }, 403, "userinfo_sub_mismatch"},
		{"the endpoint fails", func(f *flowHarness) { f.idp.userinfoStatus = http.StatusInternalServerError }, 503, "userinfo_failed"},
		{"the endpoint refuses the token", func(f *flowHarness) { f.idp.userinfoStatus = http.StatusUnauthorized }, 503, "userinfo_failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFlowHarness(t)
			a := f.startLogin("")
			tc.setup(f)
			w := f.callback(a, f.idp.issueCode("ana", a.nonce, a.challenge))

			want := msgNotAllowed
			if tc.status == 503 {
				want = msgUnavailable
			}
			if w.Code != tc.status || w.Body.String() != want+"\n" {
				t.Fatalf("status %d body %q", w.Code, w.Body.String())
			}
			if cookieNamed(w, SessionCookieName) != nil || f.sessions() != 0 || f.dir.calls != 0 {
				t.Error("a refused profile must not reach the directory or create a session")
			}
			if got := f.audit.actions(); len(got) != 1 || got[0] != "auth.login_failed:"+tc.reason {
				t.Errorf("audit = %v, want auth.login_failed:%s", got, tc.reason)
			}
			if strings.Contains(f.logs.String(), "internal detail") {
				t.Error("the userinfo response body reached the log")
			}
		})
	}
}

func TestTheProfileComesFromUserInfoAndTheIDTokenCarriesNone(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	a := f.startLogin("")
	w := f.callback(a, f.idp.issueCode("ana", a.nonce, a.challenge))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if f.idp.userinfoCalls.Load() != 1 {
		t.Errorf("userinfo calls = %d, want 1", f.idp.userinfoCalls.Load())
	}
	if f.dir.last.email != "ana@example.eu" || f.dir.last.name != "Name ana" {
		t.Errorf("directory got %+v", f.dir.last)
	}
	sess, _ := f.store.Get(context.Background(), cookieNamed(w, SessionCookieName).Value)
	payload, _ := base64.RawURLEncoding.DecodeString(strings.Split(sess.IDToken, ".")[1])
	for _, personal := range []string{"email", "ana@example.eu", `"name"`, "Name ana"} {
		if strings.Contains(string(payload), personal) {
			t.Errorf("the stored ID token (the id_token_hint) contains %q", personal)
		}
	}
}

func TestRefusedAccountsLookTheSameAndAreAudited(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err    error
		reason string
	}{
		"email in use":    {ErrEmailInUse, "email_in_use"},
		"account deleted": {ErrAccountDeleted, "account_deleted"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFlowHarness(t)
			f.dir.err = tc.err
			a := f.startLogin("")
			w := f.callback(a, f.idp.issueCode("ana", a.nonce, a.challenge))
			if w.Code != http.StatusForbidden || w.Body.String() != msgNotAllowed+"\n" {
				t.Fatalf("status %d body %q", w.Code, w.Body.String())
			}
			if f.sessions() != 0 || cookieNamed(w, SessionCookieName) != nil {
				t.Error("a session was created")
			}
			if got := f.audit.actions(); len(got) != 1 || got[0] != "auth.login_failed:"+tc.reason {
				t.Errorf("audit = %v", got)
			}
			if e := f.audit.events[0]; e.ActorID != "sub:ana" {
				t.Errorf("actor = %q", e.ActorID)
			}
		})
	}
}

func TestADirectoryFailureIsUnavailableNotForbidden(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	f.dir.err = errors.New("database down")
	a := f.startLogin("")
	w := f.callback(a, f.idp.issueCode("ana", a.nonce, a.challenge))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", w.Code)
	}
	if strings.Contains(w.Body.String(), "database") || strings.Contains(f.logs.String(), "database down") {
		t.Error("the cause of an internal failure must reach neither the response nor the log")
	}
	if f.sessions() != 0 {
		t.Error("a session was created")
	}
}

func TestALoginThatCannotBeAuditedDoesNotHappen(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	f.audit.err = errors.New("audit down")
	a := f.startLogin("")
	w := f.callback(a, f.idp.issueCode("ana", a.nonce, a.challenge))
	if w.Code != http.StatusServiceUnavailable || cookieNamed(w, SessionCookieName) != nil {
		t.Fatalf("status %d, session cookie %v", w.Code, cookieNamed(w, SessionCookieName))
	}
	if f.sessions() != 0 {
		t.Error("the session survived a failed audit")
	}
}

func TestLoginReplacesTheSessionTheBrowserHolds(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	old := f.signIn("ana")

	a := f.startLogin("")
	w := f.callback(a, f.idp.issueCode("ana", a.nonce, a.challenge), old)
	fresh := cookieNamed(w, SessionCookieName)
	if fresh == nil || fresh.Value == old.Value {
		t.Fatal("the new login must get a new cookie value (no session fixation)")
	}
	if _, err := f.store.Get(context.Background(), old.Value); !errors.Is(err, ErrNoSession) {
		t.Errorf("the old session must be gone: %v", err)
	}
	if f.sessions() != 1 {
		t.Errorf("sessions = %d", f.sessions())
	}
}

func TestReturnToIsRevalidatedAtTheLanding(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	for _, bad := range []string{"//evil.example", "https://evil.example", "/\\evil.example", "/auth/login"} {
		a := f.startLogin(bad)
		w := f.callback(a, f.idp.issueCode("ana", a.nonce, a.challenge))
		if !strings.Contains(w.Body.String(), `content="0;url=/"`) {
			t.Errorf("return_to %q: %s", bad, w.Body.String())
		}
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	sc := f.signIn("ana")
	sess, _ := f.store.Get(context.Background(), sc.Value)

	// CSRF: no Origin, another Origin, GET.
	for name, hdr := range map[string]map[string]string{
		"no origin":      nil,
		"another origin": {"Origin": "https://evil.example"},
		"null origin":    {"Origin": "null"},
		"same site":      {"Origin": "https://other.example.eu"},
	} {
		if w := f.do(http.MethodPost, LogoutPath, []*http.Cookie{sc}, hdr); w.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, w.Code)
		}
	}
	if w := f.do(http.MethodGet, LogoutPath, []*http.Cookie{sc}, map[string]string{"Origin": publicURL}); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status %d, want 405", w.Code)
	}
	if _, err := f.store.Get(context.Background(), sc.Value); err != nil {
		t.Fatalf("a refused logout must not end the session: %v", err)
	}

	w := f.do(http.MethodPost, LogoutPath, []*http.Cookie{sc}, map[string]string{"Origin": publicURL})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	loc, _ := url.Parse(w.Header().Get("Location"))
	if loc.Scheme+"://"+loc.Host+loc.Path != f.idp.srv.URL+"/end_session" {
		t.Errorf("location = %s", loc)
	}
	q := loc.Query()
	if q.Get("id_token_hint") == "" || q.Get("client_id") != "pitbox-client" || q.Get("post_logout_redirect_uri") != publicURL+SignedOutPath {
		t.Errorf("end_session query = %v", q)
	}
	if got := f.idp.revokedTokens(); len(got) != 1 || got[0] != sess.RefreshToken {
		t.Errorf("revoked tokens = %v, want the session's refresh token", got)
	}
	if _, err := f.store.Get(context.Background(), sc.Value); !errors.Is(err, ErrNoSession) {
		t.Errorf("the session must be gone: %v", err)
	}
	if c := cookieNamed(w, SessionCookieName); c == nil || c.MaxAge >= 0 {
		t.Errorf("session cookie not cleared: %+v", c)
	}
	if got := f.audit.actions(); got[len(got)-1] != "auth.logout:" {
		t.Errorf("audit = %v", got)
	}
}

func TestLogoutStillEndsTheSessionWhenRevocationFails(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	sc := f.signIn("ana")
	f.idp.revokeStatus = http.StatusInternalServerError
	w := f.do(http.MethodPost, LogoutPath, []*http.Cookie{sc}, map[string]string{"Origin": publicURL})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status %d", w.Code)
	}
	if _, err := f.store.Get(context.Background(), sc.Value); !errors.Is(err, ErrNoSession) {
		t.Errorf("the session must be gone anyway: %v", err)
	}
	if got := f.audit.actions(); got[len(got)-1] != "auth.logout:revoke_failed" {
		t.Errorf("audit = %v", got)
	}
}

func TestLogoutWithoutASessionStillClearsTheCookie(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	w := f.do(http.MethodPost, LogoutPath, nil, map[string]string{"Origin": publicURL})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != publicURL+SignedOutPath {
		t.Fatalf("status %d location %q", w.Code, w.Header().Get("Location"))
	}
	if c := cookieNamed(w, SessionCookieName); c == nil || c.MaxAge >= 0 {
		t.Errorf("cookie not cleared: %+v", c)
	}
}

func TestSignedOutPage(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	w := f.do(http.MethodGet, SignedOutPath, nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Sessão terminada") {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestSessionInfo(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)

	if w := f.do(http.MethodGet, SessionPath, nil, nil); w.Code != http.StatusUnauthorized || strings.TrimSpace(w.Body.String()) != `{"authenticated":false}` {
		t.Errorf("without a session: %d %s", w.Code, w.Body.String())
	}
	sc := f.signIn("ana")
	w := f.do(http.MethodGet, SessionPath, []*http.Cookie{sc}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body: %v", err)
	}
	if got["authenticated"] != true || got["user_id"] != "user-ana" || len(got) != 4 {
		t.Errorf("body = %v: identifiers only, nothing else", got)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("the answer must not be cached")
	}
	for _, leak := range []string{sc.Value, "refresh", "ana@example.eu", "Name ana", "rt-"} {
		if strings.Contains(w.Body.String(), leak) {
			t.Errorf("the body leaks %q", leak)
		}
	}
	for name, hdr := range map[string]map[string]string{
		"foreign origin": {"Origin": "https://evil.example"}, "cross-site": {"Sec-Fetch-Site": "cross-site"},
	} {
		if w := f.do(http.MethodGet, SessionPath, []*http.Cookie{sc}, hdr); w.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, w.Code)
		}
	}
	f.store.c.Close()
	if w := f.do(http.MethodGet, SessionPath, []*http.Cookie{sc}, nil); w.Code != http.StatusServiceUnavailable {
		t.Errorf("store outage: status %d, want 503 (not 401: the user is not signed out)", w.Code)
	}
}

// The audit trail starts once the state has matched. What anybody can send without a valid
// login state costs a log line and a counter, not a database write.
func TestCallbacksWithoutAValidStateAreCountedNotAudited(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	a := f.startLogin("")

	for range 100 {
		if w := f.do(http.MethodGet, CallbackPath+"?state=x&code=y", nil, nil); w.Code != http.StatusBadRequest {
			t.Fatalf("status %d", w.Code)
		}
	}
	for range 20 {
		if w := f.doCallback(a, a.state+"x", "code"); w.Code != http.StatusBadRequest {
			t.Fatalf("status %d", w.Code)
		}
	}
	if got := f.audit.actions(); len(got) != 0 {
		t.Fatalf("%d audit entries for unauthenticated callbacks, want 0: %v", len(got), got)
	}
	if f.refusals.get("no_login_state") != 100 || f.refusals.get("state_mismatch") != 20 {
		t.Errorf("counts = %v", f.refusals.counts)
	}

	// With the state matched, a failure is audited (the browser did start this login).
	b := f.startLogin("")
	if w := f.callback(b, "a-code-the-provider-never-issued"); w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
	if got := f.audit.actions(); len(got) != 1 || got[0] != "auth.login_failed:code_exchange" {
		t.Errorf("audit = %v", got)
	}
}

func TestRefusalCounterBuildsOnAnyMeter(t *testing.T) {
	t.Parallel()
	c, err := NewRefusalCounter(metricnoop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewRefusalCounter: %v", err)
	}
	c.Count(context.Background(), "no_login_state") // must not panic
}

func TestBackchannelLogoutBySID(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	_ = f.login("sub-alice", "sid-alice", []string{"user"})

	tok := f.idp.logoutToken("sub-alice", "sid-alice", nil)
	form := url.Values{"logout_token": {tok}}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, publicURL+BackChannelLogoutPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	// The SID index is gone.
	keys := f.adminKeys()
	for _, k := range keys {
		if strings.Contains(k, ":s:") {
			t.Errorf("sid index still present: %s", k)
		}
	}
}

func TestBackchannelLogoutRejectsInvalidToken(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)

	// Token with a nonce (must be rejected).
	tok := f.idp.logoutToken("sub-alice", "sid-alice", func(p map[string]any) {
		p["nonce"] = "nope"
	})
	form := url.Values{"logout_token": {tok}}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, publicURL+BackChannelLogoutPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
}

type fakeRecoveryStore struct {
	mu     sync.Mutex
	hashes map[string]map[string]struct{}
	used   map[string]map[string]struct{}
}

func (r *fakeRecoveryStore) Replace(_ context.Context, userID string, codeHashes [][]byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hashes == nil {
		r.hashes = map[string]map[string]struct{}{}
		r.used = map[string]map[string]struct{}{}
	}
	r.hashes[userID] = map[string]struct{}{}
	r.used[userID] = map[string]struct{}{}
	for _, h := range codeHashes {
		r.hashes[userID][string(h)] = struct{}{}
	}
	return nil
}

func (r *fakeRecoveryStore) Consume(_ context.Context, userID string, codeHash []byte) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := string(codeHash)
	if _, ok := r.hashes[userID][key]; !ok {
		return false, nil
	}
	if _, ok := r.used[userID][key]; ok {
		return false, nil
	}
	if r.used[userID] == nil {
		r.used[userID] = map[string]struct{}{}
	}
	r.used[userID][key] = struct{}{}
	return true, nil
}

func TestGenerateRecoveryCodesRequiresStrongFactor(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	f.h.RecoveryCodes = &fakeRecoveryStore{}
	v := f.login("sub-bob", "sid-bob", []string{"pwd"})
	ck := &http.Cookie{Name: SessionCookieName, Value: v, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}

	w := f.do(http.MethodPost, RecoveryCodesPath, []*http.Cookie{ck}, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", w.Code)
	}
}

func TestGenerateRecoveryCodes(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	fs := &fakeRecoveryStore{}
	f.h.RecoveryCodes = fs
	v := f.login("sub-bob", "sid-bob", []string{"user"})
	ck := &http.Cookie{Name: SessionCookieName, Value: v, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}

	w := f.do(http.MethodPost, RecoveryCodesPath, []*http.Cookie{ck}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Codes []string `json:"codes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Codes) != 8 {
		t.Fatalf("codes = %d, want 8", len(resp.Codes))
	}
	if len(fs.hashes["user-sub-bob"]) != 8 {
		t.Fatalf("stored hashes = %d, want 8", len(fs.hashes["user-sub-bob"]))
	}
}

func TestConsumeRecoveryCode(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	fs := &fakeRecoveryStore{}
	f.h.RecoveryCodes = fs
	v := f.login("sub-bob", "sid-bob", []string{"pwd"})

	codes, hashes, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := fs.Replace(context.Background(), "user-sub-bob", hashes); err != nil {
		t.Fatalf("replace: %v", err)
	}

	// First consume succeeds and step-ups the session.
	body := strings.NewReader(`{"code":"` + codes[0] + `"}`)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, publicURL+RecoveryCodeConsumePath, body)
	r.Header.Set("Cookie", SessionCookieName+"="+v)
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "203.0.113.7:40000"
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", w.Code)
	}

	sess, err := f.store.Get(context.Background(), v)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if len(sess.AMR) != 1 || sess.AMR[0] != "otp" {
		t.Fatalf("amr = %v, want [otp]", sess.AMR)
	}

	// Second consume of the same code is refused.
	body = strings.NewReader(`{"code":"` + codes[0] + `"}`)
	r = httptest.NewRequestWithContext(context.Background(), http.MethodPost, publicURL+RecoveryCodeConsumePath, body)
	r.Header.Set("Cookie", SessionCookieName+"="+v)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", w.Code)
	}
}
