package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeIdP is a minimal OpenID provider: discovery, JWKS (ES256), the token endpoint (code
// and refresh grants, with the PKCE check and client authentication), revocation and
// end_session. The tests play the browser and the provider's login page themselves.
type fakeIdP struct {
	t   *testing.T
	srv *httptest.Server
	key *ecdsa.PrivateKey
	kid string

	mu       sync.Mutex
	codes    map[string]codeGrant // code -> what it was issued for
	refresh  map[string]string    // refresh token -> subject
	access   map[string]string    // access token -> subject
	revoked  []string
	refreshN atomic.Int32 // refresh grants served
	tokenN   atomic.Int32 // token endpoint calls (any grant)
	now      func() time.Time

	// Knobs.
	clientID, clientSecret string
	refreshErr             string        // e.g. "invalid_grant": the refresh grant fails with it
	refreshErrDesc         string        // its error_description
	refreshStatus          int           // e.g. 503
	refreshDelay           time.Duration // slows the refresh grant down
	down                   atomic.Bool   // discovery answers 503
	revokeStatus           int           // when set, /revoke answers with it
	mutateClaims           func(map[string]any)
	mutateUserinfo         func(map[string]any) // changes the userinfo response
	userinfoStatus         int                  // when set, /userinfo answers with it
	userinfoCalls          atomic.Int32
	signWith               func(header map[string]any, payload map[string]any) string // override the signer
	idTokenAlg             string
}

type codeGrant struct {
	subject, nonce, challenge string
}

func newFakeIdP(t *testing.T, now func() time.Time) *fakeIdP {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	f := &fakeIdP{
		t: t, key: key, kid: "kid-1", now: now, codes: map[string]codeGrant{}, refresh: map[string]string{}, access: map[string]string{},
		clientID: "pitbox-client", clientSecret: "pitbox-secret", idTokenAlg: "ES256",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", f.discovery)
	mux.HandleFunc("/keys", f.keys)
	mux.HandleFunc("/token", f.token)
	mux.HandleFunc("/revoke", f.revoke)
	mux.HandleFunc("/userinfo", f.userinfo)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIdP) discovery(w http.ResponseWriter, _ *http.Request) {
	if f.down.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
		"jwks_uri": f.srv.URL + "/keys", "userinfo_endpoint": f.srv.URL + "/userinfo", "revocation_endpoint": f.srv.URL + "/revoke", "end_session_endpoint": f.srv.URL + "/end_session",
		"id_token_signing_alg_values_supported": []string{"ES256"},
	})
}

func (f *fakeIdP) keys(w http.ResponseWriter, _ *http.Request) {
	pub, err := f.key.PublicKey.Bytes() // uncompressed point: 0x04 || X || Y
	if err != nil {
		f.t.Errorf("public key: %v", err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
		"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": f.kid,
		"x": b64(pub[1:33]), "y": b64(pub[33:65]),
	}}})
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (f *fakeIdP) authenticated(r *http.Request) bool {
	id, secret, ok := r.BasicAuth()
	return ok && id == f.clientID && secret == f.clientSecret
}

func (f *fakeIdP) token(w http.ResponseWriter, r *http.Request) {
	f.tokenN.Add(1)
	if !f.authenticated(r) {
		writeErr(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	_ = r.ParseForm()
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		f.mu.Lock()
		g, ok := f.codes[r.PostForm.Get("code")]
		delete(f.codes, r.PostForm.Get("code")) // single use
		f.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if b64(sum[:]) != g.challenge {
			writeErr(w, http.StatusBadRequest, "invalid_grant") // PKCE failed
			return
		}
		f.respond(w, g.subject, g.nonce)
	case "refresh_token":
		if f.refreshDelay > 0 {
			time.Sleep(f.refreshDelay)
		}
		f.refreshN.Add(1)
		if f.refreshStatus != 0 {
			w.WriteHeader(f.refreshStatus)
			return
		}
		if f.refreshErr != "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": f.refreshErr, "error_description": f.refreshErrDesc})
			return
		}
		f.mu.Lock()
		sub, ok := f.refresh[r.PostForm.Get("refresh_token")]
		delete(f.refresh, r.PostForm.Get("refresh_token")) // rotation: the old one stops working
		f.mu.Unlock()
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		f.respond(w, sub, "")
	default:
		writeErr(w, http.StatusBadRequest, "unsupported_grant_type")
	}
}

func writeErr(w http.ResponseWriter, code int, e string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": e})
}

func (f *fakeIdP) respond(w http.ResponseWriter, subject, nonce string) {
	rt, at := "rt-"+randomHexT(f.t), "at-"+randomHexT(f.t)
	f.mu.Lock()
	f.refresh[rt] = subject
	f.access[at] = subject
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": at, "token_type": "Bearer", "expires_in": 600,
		"refresh_token": rt, "id_token": f.idToken(subject, nonce),
	})
}

// userinfo answers for the subject the access token was issued to, and only with a token it issued.
func (f *fakeIdP) userinfo(w http.ResponseWriter, r *http.Request) {
	f.userinfoCalls.Add(1)
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	f.mu.Lock()
	sub, ok := f.access[tok]
	f.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if f.userinfoStatus != 0 {
		w.WriteHeader(f.userinfoStatus)
		_, _ = w.Write([]byte("internal detail that must never be logged"))
		return
	}
	c := map[string]any{"sub": sub, "email": sub + "@example.eu", "email_verified": true, "name": "Name " + sub}
	if f.mutateUserinfo != nil {
		f.mutateUserinfo(c)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(c)
}

func (f *fakeIdP) revoke(w http.ResponseWriter, r *http.Request) {
	if !f.authenticated(r) {
		writeErr(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	if f.revokeStatus != 0 {
		w.WriteHeader(f.revokeStatus)
		return
	}
	_ = r.ParseForm()
	f.mu.Lock()
	tok := r.PostForm.Get("token")
	f.revoked = append(f.revoked, tok)
	delete(f.refresh, tok)
	f.mu.Unlock()
}

// claims returns the standard claims of a valid ID token for subject.
func (f *fakeIdP) claims(subject, nonce string) map[string]any {
	now := f.now()
	c := map[string]any{
		"iss": f.srv.URL, "sub": subject, "aud": []string{f.clientID}, "iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(), "auth_time": now.Add(-time.Minute).Unix(),
		"amr": []string{"pwd", "mfa"}, "sid": "sid-" + subject,
		// No email or name: they come from the userinfo endpoint (ADR 0013).
	}
	if nonce != "" {
		c["nonce"] = nonce
	}
	if f.mutateClaims != nil {
		f.mutateClaims(c)
	}
	return c
}

func (f *fakeIdP) idToken(subject, nonce string) string {
	header := map[string]any{"alg": f.idTokenAlg, "typ": "JWT", "kid": f.kid}
	payload := f.claims(subject, nonce)
	if f.signWith != nil {
		return f.signWith(header, payload)
	}
	return f.sign(header, payload)
}

func (f *fakeIdP) logoutToken(subject, sid string, mutate func(map[string]any)) string {
	header := map[string]any{"alg": f.idTokenAlg, "typ": "JWT", "kid": f.kid}
	payload := map[string]any{
		"iss":    f.srv.URL,
		"sub":    subject,
		"aud":    f.clientID,
		"iat":    f.now().Unix(),
		"exp":    f.now().Add(time.Hour).Unix(),
		"jti":    randomHexT(f.t),
		"sid":    sid,
		"events": map[string]any{"http://schemas.openid.net/event/backchannel-logout": map[string]any{}},
	}
	if mutate != nil {
		mutate(payload)
	}
	return f.sign(header, payload)
}

// sign builds a compact JWS with ES256 (r||s, 32 bytes each).
func (f *fakeIdP) sign(header, payload map[string]any) string {
	h, _ := json.Marshal(header)
	p, _ := json.Marshal(payload)
	input := b64(h) + "." + b64(p)
	sum := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, f.key, sum[:])
	if err != nil {
		f.t.Fatalf("sign: %v", err)
	}
	sig := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return input + "." + b64(sig)
}

// issueCode registers an authorization code for what /authorize would have recorded.
func (f *fakeIdP) issueCode(subject, nonce, challenge string) string {
	code := "code-" + randomHexT(f.t)
	f.mu.Lock()
	f.codes[code] = codeGrant{subject: subject, nonce: nonce, challenge: challenge}
	f.mu.Unlock()
	return code
}

func (f *fakeIdP) revokedTokens() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.revoked...)
}

func randomHexT(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return b64(b)
}
