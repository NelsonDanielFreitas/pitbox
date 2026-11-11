package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// strongMobile signs a phone in with a passkey-grade factor.
func (f *flowHarness) strongMobile(sub string) (deviceKey, bearerResponse) {
	f.t.Helper()
	f.idp.mutateClaims = func(m map[string]any) { m["amr"] = []string{"user"} }
	defer func() { f.idp.mutateClaims = nil }()
	key := newDeviceKey(f.t)
	return key, f.mobileLogin(key, sub)
}

func (f *flowHarness) doWith(method, target string, cookie *http.Cookie, hdr map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	var cs []*http.Cookie
	if cookie != nil {
		cs = append(cs, cookie)
	}
	return f.do(method, target, cs, hdr)
}

func (f *flowHarness) webCookie(sub string, amr []string) *http.Cookie {
	f.t.Helper()
	v := f.login(sub, "sid-"+sub, amr)
	return &http.Cookie{Name: SessionCookieName, Value: v, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode}
}

func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return m
}

func TestAccountEndpointsRefuseWithoutCredentials(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, SessionsPath},
		{http.MethodDelete, SessionsPath},
		{http.MethodDelete, "/auth/sessions/" + strings.Repeat("a", 64)},
		{http.MethodGet, RecoveryCodesPath},
		{http.MethodPost, RecoveryCodesPath},
	} {
		if w := f.do(c.method, c.path, nil, nil); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without credentials: %d, want 401", c.method, c.path, w.Code)
		}
	}
}

func TestAccountEndpointsRefuseAmbiguousOrMalformedCredentials(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	ck := f.webCookie("sub-amb", []string{"user"})
	_, mob := f.strongMobile("sub-amb")
	for name, c := range map[string]struct {
		cookie *http.Cookie
		hdr    map[string]string
	}{
		"bearer and cookie together": {ck, bearerHdr(mob.AccessToken)},
		"malformed bearer":           {nil, map[string]string{"Authorization": "Bearer nope"}},
		"a non-bearer scheme":        {ck, map[string]string{"Authorization": "Basic Zm9vOmJhcg=="}},
		"a made-up bearer":           {nil, bearerHdr("pbm1_" + strings.Repeat("A", 43))},
	} {
		w := f.doWith(http.MethodGet, SessionsPath, c.cookie, c.hdr)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401 (%s)", name, w.Code, w.Body.String())
		}
	}
}

// A cookie request from another origin or site is refused before the session is read; the
// Bearer, which a browser never adds by itself, is not subject to that check.
func TestAccountEndpointsRefuseCrossSiteCookieRequests(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	ck := f.webCookie("sub-xs", []string{"user"})
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, SessionsPath},
		{http.MethodDelete, SessionsPath},
		{http.MethodDelete, "/auth/sessions/" + strings.Repeat("a", 64)},
		{http.MethodGet, RecoveryCodesPath},
		{http.MethodPost, RecoveryCodesPath},
	} {
		for _, hdr := range []map[string]string{
			{"Origin": "https://evil.example"},
			{"Sec-Fetch-Site": "cross-site"},
			{"Sec-Fetch-Site": "same-site"},
		} {
			if w := f.doWith(c.method, c.path, ck, hdr); w.Code != http.StatusForbidden {
				t.Errorf("%s %s %v: %d, want 403", c.method, c.path, hdr, w.Code)
			}
		}
	}
	if len(f.audit.actions()) != 1 { // only the login
		t.Errorf("a refused request left a trail: %v", f.audit.actions())
	}
}

func TestListSessionsShowsOnlyWhatTheUserNeeds(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	ck := f.webCookie("sub-list", []string{"user"})
	_, mob := f.strongMobile("sub-list")
	f.webCookie("sub-other", []string{"user"}) // somebody else's session never shows

	for name, w := range map[string]*httptest.ResponseRecorder{
		"cookie": f.doWith(http.MethodGet, SessionsPath, ck, nil),
		"bearer": f.doWith(http.MethodGet, SessionsPath, nil, bearerHdr(mob.AccessToken)),
	} {
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control %q", name, cc)
		}
		var body struct {
			Sessions []map[string]any `json:"sessions"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Sessions) != 2 {
			t.Fatalf("%s: %d sessions, want 2 (web and mobile of the same user)", name, len(body.Sessions))
		}
		current, kinds := 0, map[any]bool{}
		for _, s := range body.Sessions {
			if len(s) != 7 {
				t.Errorf("%s: unexpected keys %v", name, s)
			}
			for _, k := range []string{"ref", "kind", "created_at", "auth_time", "user_agent", "ip", "current"} {
				if _, ok := s[k]; !ok {
					t.Errorf("%s: missing %q", name, k)
				}
			}
			if s["current"] == true {
				current++
			}
			kinds[s["kind"]] = true
		}
		if current != 1 || !kinds["web"] || !kinds["mobile"] {
			t.Errorf("%s: current=%d kinds=%v", name, current, kinds)
		}
		if strings.Contains(w.Body.String(), "sub-list") || strings.Contains(w.Body.String(), "user_id") {
			t.Errorf("%s: the list leaks an identifier: %s", name, w.Body.String())
		}
	}
}

func TestRevokeSession(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	ck := f.webCookie("sub-rev", []string{"user"})
	_, mob := f.strongMobile("sub-rev")
	victim := f.webCookie("sub-victim", []string{"user"})

	var list struct {
		Sessions []struct {
			Ref  string `json:"ref"`
			Kind string `json:"kind"`
		} `json:"sessions"`
	}
	_ = json.Unmarshal(f.doWith(http.MethodGet, SessionsPath, ck, nil).Body.Bytes(), &list)
	var mobileRef string
	for _, s := range list.Sessions {
		if s.Kind == "mobile" {
			mobileRef = s.Ref
		}
	}
	victimRef := hashHex(victim.Value)

	if w := f.doWith(http.MethodDelete, "/auth/sessions/not-a-ref", ck, nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad ref: %d", w.Code)
	}
	// Another user's session and an unknown one look the same, and the other user keeps theirs.
	for _, ref := range []string{victimRef, strings.Repeat("b", 64)} {
		if w := f.doWith(http.MethodDelete, "/auth/sessions/"+ref, ck, nil); w.Code != http.StatusNotFound {
			t.Errorf("ref %.8s: %d, want 404", ref, w.Code)
		}
	}
	if w := f.doWith(http.MethodGet, SessionPath, victim, nil); w.Code != http.StatusOK {
		t.Fatalf("the other user lost the session: %d", w.Code)
	}
	// The user's own session on the phone ends, from the browser.
	if w := f.doWith(http.MethodDelete, "/auth/sessions/"+mobileRef, ck, nil); w.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", w.Code)
	}
	if code, _ := f.sessionStatus(bearerHdr(mob.AccessToken)); code != http.StatusUnauthorized {
		t.Errorf("the revoked phone session still works: %d", code)
	}
	if !contains(f.audit.actions(), "auth.session_revoked") {
		t.Errorf("no audit entry: %v", f.audit.actions())
	}
}

func TestRevokeOtherSessionsKeepsTheCaller(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	ck := f.webCookie("sub-many", []string{"user"})
	_, mob := f.strongMobile("sub-many")
	other := f.webCookie("sub-bystander", []string{"user"})

	w := f.doWith(http.MethodDelete, SessionsPath, nil, bearerHdr(mob.AccessToken))
	if w.Code != http.StatusOK || decodeMap(t, w)["revoked"] != float64(1) {
		t.Fatalf("revoke others: %d %s", w.Code, w.Body.String())
	}
	if code, _ := f.sessionStatus(bearerHdr(mob.AccessToken)); code != http.StatusOK {
		t.Errorf("the caller's own session ended: %d", code)
	}
	if w := f.doWith(http.MethodGet, SessionPath, ck, nil); w.Code != http.StatusUnauthorized {
		t.Errorf("the other session of the user still works: %d", w.Code)
	}
	if w := f.doWith(http.MethodGet, SessionPath, other, nil); w.Code != http.StatusOK {
		t.Errorf("somebody else's session ended: %d", w.Code)
	}
	if !contains(f.audit.actions(), "auth.sessions_revoked_others") {
		t.Errorf("no audit entry: %v", f.audit.actions())
	}
}

func TestRecoveryCodesWithBearer(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	fs := &fakeRecoveryStore{}
	f.h.RecoveryCodes = fs
	_, mob := f.strongMobile("sub-phone")
	hdr := bearerHdr(mob.AccessToken)

	w := f.doWith(http.MethodGet, RecoveryCodesPath, nil, hdr)
	if w.Code != http.StatusOK || decodeMap(t, w)["unused"] != float64(0) {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	w = f.doWith(http.MethodPost, RecoveryCodesPath, nil, hdr)
	if w.Code != http.StatusOK {
		t.Fatalf("generate: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Codes []string `json:"codes"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Codes) != 8 {
		t.Fatalf("%d codes", len(resp.Codes))
	}
	for _, code := range resp.Codes {
		if strings.Contains(f.logs.String(), code) {
			t.Fatal("a code reached the logs")
		}
	}
	w = f.doWith(http.MethodGet, RecoveryCodesPath, nil, hdr)
	if decodeMap(t, w)["unused"] != float64(8) || strings.Contains(w.Body.String(), resp.Codes[0]) {
		t.Errorf("status after generate: %s", w.Body.String())
	}
}

// A password-only sign-in, or a passkey sign-in older than the window, cannot replace the
// recovery codes: whoever sits at an unlocked phone or holds a stolen session does not get a
// fresh set of codes.
func TestRecoveryCodesNeedARecentStrongFactor(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	fs := &fakeRecoveryStore{}
	f.h.RecoveryCodes = fs
	_, mob := f.strongMobile("sub-stale")
	weak := f.webCookie("sub-weak", []string{"pwd", "mfa"})

	if w := f.doWith(http.MethodPost, RecoveryCodesPath, weak, nil); w.Code != http.StatusForbidden {
		t.Errorf("weak factor: %d", w.Code)
	}
	*f.now = f.now.Add(6 * time.Minute)
	w := f.doWith(http.MethodPost, RecoveryCodesPath, nil, bearerHdr(mob.AccessToken))
	if w.Code != http.StatusForbidden || decodeMap(t, w)["error"] != "step-up required" {
		t.Errorf("stale authentication: %d %s", w.Code, w.Body.String())
	}
	if len(fs.hashes) != 0 {
		t.Error("codes were stored for a refused request")
	}
}

// --- reauthentication of a phone session ---

func (f *flowHarness) stepUpStart(key deviceKey, bearer string) *httptest.ResponseRecorder {
	f.t.Helper()
	hdr := map[string]string{}
	if bearer != "" {
		hdr = bearerHdr(bearer)
	}
	return f.post(MobileStartPath, map[string]any{
		"session_pub": key.spkiB6, "code_challenge": "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNO12", "step_up": true,
	}, hdr)
}

func (f *flowHarness) stepUpLogin(bearer, sub string, amr []string) (*httptest.ResponseRecorder, bearerResponse) {
	f.t.Helper()
	f.idp.mutateClaims = func(m map[string]any) { m["amr"] = amr }
	defer func() { f.idp.mutateClaims = nil }()
	key := newDeviceKey(f.t)
	a := f.mobileStartWith(key, bearer)
	w := f.post(MobileTokenPath, f.tokenBody(a, sub), nil)
	var resp bearerResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w, resp
}

func TestMobileStepUpReplacesTheSession(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	_, old := f.strongMobile("sub-su")
	f.strongMobile("sub-su-other-device")

	w, fresh := f.stepUpLogin(old.AccessToken, "sub-su", []string{"user"})
	if w.Code != http.StatusOK || fresh.Session == "" || fresh.Session == old.Session {
		t.Fatalf("step-up: %d %s", w.Code, w.Body.String())
	}
	if code, _ := f.sessionStatus(bearerHdr(old.AccessToken)); code != http.StatusUnauthorized {
		t.Errorf("the replaced session still works: %d", code)
	}
	if code, _ := f.sessionStatus(bearerHdr(fresh.AccessToken)); code != http.StatusOK {
		t.Errorf("the new session does not work: %d", code)
	}
}

func TestMobileStepUpRefusals(t *testing.T) {
	t.Parallel()
	f := newFlowHarness(t)
	key, old := f.strongMobile("sub-su2")

	// Without a valid Bearer there is nothing to reauthenticate.
	for name, bearer := range map[string]string{"no bearer": "", "unknown bearer": "pbm1_" + strings.Repeat("A", 43)} {
		if w := f.stepUpStart(key, bearer); w.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", name, w.Code)
		}
	}
	// Another account coming back is refused and the old session stays.
	if w, _ := f.stepUpLogin(old.AccessToken, "sub-intruder", []string{"user"}); w.Code != http.StatusForbidden {
		t.Errorf("another account: %d, want 403", w.Code)
	}
	// A weak factor is refused with its own code, and the old session stays.
	w, _ := f.stepUpLogin(old.AccessToken, "sub-su2", []string{"pwd", "mfa"})
	if w.Code != http.StatusForbidden || errorCode(t, w) != mobileErrWeakFactor {
		t.Errorf("weak factor: %d %s", w.Code, w.Body.String())
	}
	if code, _ := f.sessionStatus(bearerHdr(old.AccessToken)); code != http.StatusOK {
		t.Errorf("a refused step-up ended the session: %d", code)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if strings.HasPrefix(v, want+":") {
			return true
		}
	}
	return false
}
