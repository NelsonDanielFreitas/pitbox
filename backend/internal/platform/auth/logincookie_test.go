package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func newCookies(t *testing.T, now *time.Time) *LoginCookies {
	t.Helper()
	return NewLoginCookies(sealer(t, PurposeLoginCookie, key("k1", 1)), func() time.Time { return *now })
}

// cookieWith builds a request cookie. The attributes only matter to the browser, but they are
// set the way the server sets them so the test fixtures cannot be mistaken for insecure cookies.
func cookieWith(value string) *http.Cookie {
	return &http.Cookie{Name: LoginCookieName, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

func request(t *testing.T, cookies ...*http.Cookie) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.eu/auth/callback", nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}

func TestNewLoginStateIsRandom(t *testing.T) {
	t.Parallel()

	a, err := NewLoginState(t0, "/")
	if err != nil {
		t.Fatalf("NewLoginState: %v", err)
	}
	b, _ := NewLoginState(t0, "/")
	if a.State == b.State || a.Nonce == b.Nonce || a.Verifier == b.Verifier {
		t.Error("two attempts share a random value")
	}
	if a.State == a.Nonce {
		t.Error("state and nonce must differ")
	}
	for name, v := range map[string]string{"state": a.State, "nonce": a.Nonce} {
		if len(v) != 43 { // 32 bytes, base64url without padding
			t.Errorf("%s has %d chars, want 43 (256 bits)", name, len(v))
		}
	}
	if len(a.Verifier) < 43 || len(a.Verifier) > 128 { // RFC 7636 §4.1
		t.Errorf("verifier length %d outside 43..128", len(a.Verifier))
	}
	if !a.Expires.Equal(t0.Add(10 * time.Minute)) {
		t.Errorf("expires = %s", a.Expires)
	}
}

func TestLoginCookieAttributes(t *testing.T) {
	t.Parallel()

	now := t0
	c := newCookies(t, &now)
	st, _ := NewLoginState(now, "/equipas")
	rec := httptest.NewRecorder()
	if err := c.Set(rec, st); err != nil {
		t.Fatalf("Set: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies", len(cookies))
	}
	ck := cookies[0]
	if ck.Name != "__Host-pitbox_login" || !ck.Secure || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode ||
		ck.Path != "/" || ck.Domain != "" || ck.MaxAge != 600 {
		t.Errorf("unexpected cookie attributes: %+v", ck)
	}
	for _, secret := range []string{st.State, st.Nonce, st.Verifier, "equipas"} {
		if strings.Contains(ck.Value, secret) {
			t.Errorf("the cookie value leaks %q", secret)
		}
	}
}

func TestLoginCookieRoundTrip(t *testing.T) {
	t.Parallel()

	now := t0
	c := newCookies(t, &now)
	st, _ := NewLoginState(now, "/equipas?x=1")
	rec := httptest.NewRecorder()
	if err := c.Set(rec, st); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Read(request(t, rec.Result().Cookies()[0]))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.State != st.State || got.Nonce != st.Nonce || got.Verifier != st.Verifier || got.ReturnTo != "/equipas?x=1" {
		t.Errorf("round trip changed the state: %+v", got)
	}
	if !got.MatchesState(st.State) || got.MatchesState(st.State+"x") || got.MatchesState("") {
		t.Error("MatchesState is wrong")
	}
}

func TestLoginCookieReadFailsClosed(t *testing.T) {
	t.Parallel()

	now := t0
	c := newCookies(t, &now)
	st, _ := NewLoginState(now, "/")
	rec := httptest.NewRecorder()
	_ = c.Set(rec, st)
	good := rec.Result().Cookies()[0]

	// Sealed for another purpose with the same master key (a session token is not a login cookie).
	other := sealer(t, PurposeSessionToken, key("k1", 1))
	sealedOther, _ := other.Seal([]byte(`{"s":"a","n":"b","v":"c","e":"2099-01-01T00:00:00Z"}`), []byte(LoginCookieName))
	// Right key and purpose, but not bound to this cookie's name (sealed with no aad: a
	// value copied from somewhere else that uses the same key must not be accepted here).
	sameSealer := sealer(t, PurposeLoginCookie, key("k1", 1))
	sealedOtherName, _ := sameSealer.Seal([]byte(`{"s":"a","n":"b","v":"c","e":"2099-01-01T00:00:00Z"}`), nil)
	// Valid seal, but empty fields / not JSON.
	sealedEmpty, _ := sameSealer.Seal([]byte(`{"e":"2099-01-01T00:00:00Z"}`), []byte(LoginCookieName))
	sealedGarbage, _ := sameSealer.Seal([]byte(`not json`), []byte(LoginCookieName))

	tests := map[string]*http.Request{
		"no cookie":         request(t),
		"garbage":           request(t, cookieWith("garbage")),
		"other purpose":     request(t, cookieWith(sealedOther)),
		"other cookie name": request(t, cookieWith(sealedOtherName)),
		"empty fields":      request(t, cookieWith(sealedEmpty)),
		"not json":          request(t, cookieWith(sealedGarbage)),
		"tampered":          request(t, cookieWith(good.Value[:len(good.Value)-3]+"AAA")),
	}
	for name, r := range tests {
		if _, err := c.Read(r); !errors.Is(err, ErrNoLoginState) {
			t.Errorf("%s: Read = %v, want ErrNoLoginState", name, err)
		}
	}

	// Expiry: still valid just before, refused at and after the deadline.
	now = st.Expires.Add(-time.Second)
	if _, err := c.Read(request(t, good)); err != nil {
		t.Errorf("one second before expiry: %v", err)
	}
	now = st.Expires
	if _, err := c.Read(request(t, good)); !errors.Is(err, ErrNoLoginState) {
		t.Errorf("at expiry: %v", err)
	}
}

func TestLoginCookieClear(t *testing.T) {
	t.Parallel()

	now := t0
	c := newCookies(t, &now)
	rec := httptest.NewRecorder()
	c.Clear(rec)
	ck := rec.Result().Cookies()[0]
	if ck.Name != LoginCookieName || ck.MaxAge >= 0 || ck.Value != "" || !ck.Secure || ck.Path != "/" {
		t.Errorf("Clear did not delete the cookie with the same attributes: %+v", ck)
	}
}
