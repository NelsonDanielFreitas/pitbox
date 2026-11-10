package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// stepClock is a clock the tests move by hand.
type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

func newStepClock() *stepClock {
	return &stepClock{t: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)}
}
func (c *stepClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type testKey struct {
	kid string
	key *ecdsa.PrivateKey
}

func newTestKey(t testing.TB, kid string) testKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	return testKey{kid: kid, key: k}
}

// sign makes a compact JWS with the key, whatever kid the header should carry.
func (k testKey) sign(t testing.TB, kid string, payload string) string {
	t.Helper()
	opts := (&jose.SignerOptions{}).WithType("JWT")
	if kid != "" {
		opts = opts.WithHeader("kid", kid)
	}
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: k.key}, opts)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	obj, err := s.Sign([]byte(payload))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	out, err := obj.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return out
}

// jwksServer serves a JWKS and counts the requests.
type jwksServer struct {
	srv  *httptest.Server
	hits atomic.Int32

	mu     sync.Mutex
	keys   []testKey
	status int    // when set, the response status
	body   string // when set, the response body
	gate   chan struct{}
}

func newJWKSServer(t testing.TB, keys ...testKey) *jwksServer {
	t.Helper()
	s := &jwksServer{keys: keys}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		s.mu.Lock()
		keys, status, body, gate := s.keys, s.status, s.body, s.gate
		s.mu.Unlock()
		if gate != nil {
			<-gate
		}
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if body != "" {
			_, _ = w.Write([]byte(body))
			return
		}
		set := jose.JSONWebKeySet{}
		for _, k := range keys {
			set.Keys = append(set.Keys, jose.JSONWebKey{Key: &k.key.PublicKey, KeyID: k.kid, Algorithm: "ES256", Use: "sig"})
		}
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *jwksServer) set(f func(*jwksServer)) { s.mu.Lock(); defer s.mu.Unlock(); f(s) }

func newTestKeySet(t testing.TB, s *jwksServer, clock *stepClock) *boundedKeySet {
	t.Helper()
	return newBoundedKeySet(keySetConfig{
		URI: s.srv.URL, Client: s.srv.Client(), Algs: []jose.SignatureAlgorithm{jose.ES256}, Now: clock.Now,
	})
}

func TestBoundedKeySetVerifiesAndCaches(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, "k1")
	srv := newJWKSServer(t, key)
	clock := newStepClock()
	ks := newTestKeySet(t, srv, clock)

	for range 5 {
		payload, err := ks.VerifySignature(context.Background(), key.sign(t, "k1", `{"a":1}`))
		if err != nil || string(payload) != `{"a":1}` {
			t.Fatalf("verify: %q, %v", payload, err)
		}
	}
	if got := srv.hits.Load(); got != 1 {
		t.Fatalf("%d JWKS requests for 5 verifications, want 1", got)
	}

	// After the TTL the keys are fetched again, once.
	clock.Advance(keySetTTL + time.Second)
	for range 3 {
		if _, err := ks.VerifySignature(context.Background(), key.sign(t, "k1", `{}`)); err != nil {
			t.Fatalf("verify after TTL: %v", err)
		}
	}
	if got := srv.hits.Load(); got != 2 {
		t.Fatalf("%d JWKS requests after the TTL, want 2", got)
	}
}

// The burst the ADR 0032 criterion asks for: a stream of unknown kids, minute after minute,
// never makes more than two requests per minute.
func TestBoundedKeySetUnknownKidBurstIsBounded(t *testing.T) {
	t.Parallel()
	known := newTestKey(t, "k1")
	attacker := newTestKey(t, "attacker")
	srv := newJWKSServer(t, known)
	clock := newStepClock()
	ks := newTestKeySet(t, srv, clock)

	const minutes = 5
	for sec := range minutes * 60 {
		_ = sec
		for i := range 20 { // 20 forged tokens per second, each with a fresh kid
			_, err := ks.VerifySignature(context.Background(), attacker.sign(t, fmt.Sprintf("kid-%d-%d", sec, i), `{}`))
			if err == nil {
				t.Fatal("a token signed by an unknown key was accepted")
			}
		}
		clock.Advance(time.Second)
	}
	// 2 per minute, plus the first.
	if got, limit := int(srv.hits.Load()), 2*minutes+1; got > limit {
		t.Fatalf("%d JWKS requests in %d minutes, want at most %d", got, minutes, limit)
	}
}

func TestBoundedKeySetBurstWithinOneInstantIsOneFetch(t *testing.T) {
	t.Parallel()
	attacker := newTestKey(t, "attacker")
	srv := newJWKSServer(t, newTestKey(t, "k1"))
	ks := newTestKeySet(t, srv, newStepClock())
	for i := range 1000 {
		if _, err := ks.VerifySignature(context.Background(), attacker.sign(t, fmt.Sprintf("kid-%d", i), `{}`)); err == nil {
			t.Fatal("accepted")
		}
	}
	if got := srv.hits.Load(); got != 1 {
		t.Fatalf("%d JWKS requests for 1000 unknown kids at one instant, want 1", got)
	}
}

func TestBoundedKeySetRemembersAnUnknownKid(t *testing.T) {
	t.Parallel()
	attacker := newTestKey(t, "attacker")
	srv := newJWKSServer(t, newTestKey(t, "k1"))
	clock := newStepClock()
	ks := newTestKeySet(t, srv, clock)
	token := attacker.sign(t, "ghost", `{}`)

	_, _ = ks.VerifySignature(context.Background(), token)
	clock.Advance(keySetCooldown + time.Second) // the cooldown allows a fetch, the memory must not
	_, _ = ks.VerifySignature(context.Background(), token)
	if got := srv.hits.Load(); got != 1 {
		t.Fatalf("%d requests: a remembered unknown kid caused a fetch", got)
	}
	clock.Advance(keySetNegativeTTL) // the memory expires
	_, _ = ks.VerifySignature(context.Background(), token)
	if got := srv.hits.Load(); got != 2 {
		t.Fatalf("%d requests after the memory expired, want 2", got)
	}
}

func TestBoundedKeySetNegativeMemoryIsBounded(t *testing.T) {
	t.Parallel()
	attacker := newTestKey(t, "attacker")
	srv := newJWKSServer(t, newTestKey(t, "k1"))
	ks := newTestKeySet(t, srv, newStepClock())
	for i := range 5 * keySetMaxNegative {
		_, _ = ks.VerifySignature(context.Background(), attacker.sign(t, fmt.Sprintf("kid-%d", i), `{}`))
	}
	ks.mu.Lock()
	n := len(ks.unknown)
	ks.mu.Unlock()
	if n > keySetMaxNegative {
		t.Fatalf("%d remembered kids, limit %d", n, keySetMaxNegative)
	}
}

func TestBoundedKeySetKeyRotation(t *testing.T) {
	t.Parallel()
	oldKey, newKey := newTestKey(t, "old"), newTestKey(t, "new")
	srv := newJWKSServer(t, oldKey)
	clock := newStepClock()
	ks := newTestKeySet(t, srv, clock)
	if _, err := ks.VerifySignature(context.Background(), oldKey.sign(t, "old", `{}`)); err != nil {
		t.Fatalf("old key: %v", err)
	}

	srv.set(func(s *jwksServer) { s.keys = []testKey{oldKey, newKey} })
	token := newKey.sign(t, "new", `{}`)
	// Inside the cooldown the new key is not known yet: refused, no request.
	if _, err := ks.VerifySignature(context.Background(), token); !errors.Is(err, errKeySetUnknownKey) {
		t.Fatalf("inside the cooldown: %v, want unknown key", err)
	}
	if got := srv.hits.Load(); got != 1 {
		t.Fatalf("%d requests inside the cooldown, want 1", got)
	}
	// Once the cooldown and the memory of the refusal have passed, the rotation is picked up.
	clock.Advance(keySetNegativeTTL + time.Second)
	if _, err := ks.VerifySignature(context.Background(), token); err != nil {
		t.Fatalf("after the cooldown: %v", err)
	}
	if got := srv.hits.Load(); got != 2 {
		t.Fatalf("%d requests, want 2", got)
	}
}

func TestBoundedKeySetFailedFetchCountsAgainstTheCooldown(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, "k1")
	srv := newJWKSServer(t, key)
	srv.set(func(s *jwksServer) { s.status = http.StatusInternalServerError })
	clock := newStepClock()
	ks := newTestKeySet(t, srv, clock)
	token := key.sign(t, "k1", `{}`)

	for range 50 {
		if _, err := ks.VerifySignature(context.Background(), token); !errors.Is(err, errKeySetUnavailable) {
			t.Fatalf("with the IdP down: %v, want unavailable (fail closed)", err)
		}
	}
	if got := srv.hits.Load(); got != 1 {
		t.Fatalf("%d requests while the IdP is down, want 1", got)
	}
	// It recovers by itself after the cooldown.
	srv.set(func(s *jwksServer) { s.status = 0 })
	clock.Advance(keySetCooldown + time.Second)
	if _, err := ks.VerifySignature(context.Background(), token); err != nil {
		t.Fatalf("after recovery: %v", err)
	}
}

func TestBoundedKeySetNeverUsesStaleKeys(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, "k1")
	srv := newJWKSServer(t, key)
	clock := newStepClock()
	ks := newTestKeySet(t, srv, clock)
	token := key.sign(t, "k1", `{}`)
	if _, err := ks.VerifySignature(context.Background(), token); err != nil {
		t.Fatalf("first: %v", err)
	}
	// The TTL passes and the IdP is down: the refresh fails, and then the keys are not used.
	srv.set(func(s *jwksServer) { s.status = http.StatusServiceUnavailable })
	clock.Advance(keySetTTL + time.Second)
	if _, err := ks.VerifySignature(context.Background(), token); err == nil {
		t.Fatal("a stale key set verified a token")
	}
	if _, err := ks.VerifySignature(context.Background(), token); err == nil {
		t.Fatal("a stale key set verified a token inside the cooldown")
	}
}

func TestBoundedKeySetSingleFlight(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, "k1")
	srv := newJWKSServer(t, key)
	gate := make(chan struct{})
	srv.set(func(s *jwksServer) { s.gate = gate })
	ks := newTestKeySet(t, srv, newStepClock())
	token := key.sign(t, "k1", `{}`)

	const callers = 50
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			_, err := ks.VerifySignature(context.Background(), token)
			errs <- err
		})
	}
	// Wait until the first request is at the server, then let it answer.
	deadline := time.Now().Add(5 * time.Second)
	for srv.hits.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond) // let the others queue behind it
	close(gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a caller failed: %v", err)
		}
	}
	if got := srv.hits.Load(); got != 1 {
		t.Fatalf("%d JWKS requests for %d concurrent callers, want 1", got, callers)
	}
}

func TestBoundedKeySetRefusals(t *testing.T) {
	t.Parallel()
	good := newTestKey(t, "k1")
	other := newTestKey(t, "k1") // same kid, a different key: a forged signature
	srv := newJWKSServer(t, good)
	ks := newTestKeySet(t, srv, newStepClock())

	hs256 := func() string {
		s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: []byte("0123456789abcdef0123456789abcdef")},
			(&jose.SignerOptions{}).WithHeader("kid", "k1"))
		if err != nil {
			t.Fatal(err)
		}
		o, _ := s.Sign([]byte(`{}`))
		c, _ := o.CompactSerialize()
		return c
	}()
	twoSignatures := func() string {
		mk := func(k testKey) jose.SigningKey { return jose.SigningKey{Algorithm: jose.ES256, Key: k.key} }
		s, err := jose.NewMultiSigner([]jose.SigningKey{mk(good), mk(good)}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
		if err != nil {
			t.Fatal(err)
		}
		o, _ := s.Sign([]byte(`{}`))
		return o.FullSerialize()
	}()

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"garbage", "not.a.jwt"},
		{"alg none", "eyJhbGciOiJub25lIiwia2lkIjoiazEifQ.e30."},
		{"HS256", hs256},
		{"no kid", good.sign(t, "", `{}`)},
		{"kid too long", good.sign(t, strings.Repeat("k", keySetMaxKidLen+1), `{}`)},
		{"two signatures", twoSignatures},
		{"forged signature, right kid", other.sign(t, "k1", `{}`)},
		{"unknown kid", good.sign(t, "nope", `{}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ks.VerifySignature(context.Background(), tc.token); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestBoundedKeySetKeyAlgorithmIsBinding(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, "k1")
	srv := newJWKSServer(t, key)
	// The JWKS says this key is for ES384; a token that claims ES256 with it must fail.
	srv.set(func(s *jwksServer) {
		set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.key.PublicKey, KeyID: "k1", Algorithm: "ES384", Use: "sig"}}}
		b, _ := json.Marshal(set)
		s.body = string(b)
	})
	ks := newTestKeySet(t, srv, newStepClock())
	if _, err := ks.VerifySignature(context.Background(), key.sign(t, "k1", `{}`)); err == nil {
		t.Fatal("a key restricted to another algorithm verified the token")
	}
}

func TestBoundedKeySetBadResponses(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, "k1")
	pub := jose.JSONWebKey{Key: &key.key.PublicKey, KeyID: "k1", Use: "sig"}
	priv := jose.JSONWebKey{Key: key.key, KeyID: "k1", Use: "sig"}
	enc := jose.JSONWebKey{Key: &key.key.PublicKey, KeyID: "k1", Use: "enc"}
	doc := func(keys ...jose.JSONWebKey) string {
		b, _ := json.Marshal(jose.JSONWebKeySet{Keys: keys})
		return string(b)
	}
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"status 500", http.StatusInternalServerError, ""},
		{"not JSON", 0, "<html>"},
		{"no keys", 0, `{"keys":[]}`},
		{"a private key is not accepted as a verification key", 0, doc(priv)},
		{"an encryption key is not a signing key", 0, doc(enc)},
		{"a key without a kid", 0, doc(jose.JSONWebKey{Key: &key.key.PublicKey})},
		{"body over the limit", 0, `{"keys":[],"pad":"` + strings.Repeat("a", keySetMaxBody) + `"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newJWKSServer(t, key)
			srv.set(func(s *jwksServer) { s.status, s.body = tc.status, tc.body })
			ks := newTestKeySet(t, srv, newStepClock())
			if _, err := ks.VerifySignature(context.Background(), key.sign(t, "k1", `{}`)); !errors.Is(err, errKeySetUnavailable) {
				t.Fatalf("%v, want unavailable", err)
			}
		})
	}
	// Control: a good public key in the same shape does verify.
	srv := newJWKSServer(t, key)
	srv.set(func(s *jwksServer) { s.body = doc(pub) })
	if _, err := newTestKeySet(t, srv, newStepClock()).VerifySignature(context.Background(), key.sign(t, "k1", `{}`)); err != nil {
		t.Fatalf("control: %v", err)
	}
}

func TestBoundedKeySetFetchTimeout(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, "k1")
	srv := newJWKSServer(t, key)
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	srv.set(func(s *jwksServer) { s.gate = gate })
	ks := newBoundedKeySet(keySetConfig{
		URI: srv.srv.URL, Client: srv.srv.Client(), Algs: []jose.SignatureAlgorithm{jose.ES256},
		Timeout: 100 * time.Millisecond,
	})
	start := time.Now()
	if _, err := ks.VerifySignature(context.Background(), key.sign(t, "k1", `{}`)); !errors.Is(err, errKeySetUnavailable) {
		t.Fatalf("%v, want unavailable", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("a hanging IdP held the caller for %v", d)
	}
}

func TestBoundedKeySetErrorsCarryNothingFromTheInput(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, "k1")
	ks := newTestKeySet(t, newJWKSServer(t, key), newStepClock())
	_, err := ks.VerifySignature(context.Background(), key.sign(t, "secret-kid-value", `{"sub":"someone@example.com"}`))
	if err == nil {
		t.Fatal("accepted")
	}
	for _, leaked := range []string{"secret-kid-value", "someone@example.com"} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("the error leaks %q: %v", leaked, err)
		}
	}
}

func FuzzBoundedKeySetVerify(f *testing.F) {
	key := newTestKey(f, "k1")
	srv := newJWKSServer(f, key)
	ks := newTestKeySet(f, srv, newStepClock())
	f.Add(key.sign(f, "k1", `{"a":1}`))
	f.Add("")
	f.Add("a.b.c")
	f.Add("eyJhbGciOiJub25lIn0.e30.")
	f.Fuzz(func(t *testing.T, token string) {
		// The fuzzer cannot sign: whatever verifies must carry the seed's payload.
		payload, err := ks.VerifySignature(context.Background(), token)
		if err == nil && string(payload) != `{"a":1}` {
			t.Fatalf("accepted %q with payload %q", token, payload)
		}
	})
}

func FuzzParseKeySet(f *testing.F) {
	f.Add([]byte(`{"keys":[]}`))
	f.Add([]byte(`{"keys":[{"kty":"EC","crv":"P-256","kid":"a","x":"AA","y":"AA"}]}`))
	f.Add([]byte(`not json`))
	f.Fuzz(func(t *testing.T, body []byte) {
		set, err := parseKeySet(body)
		if err != nil {
			return
		}
		if len(set) == 0 {
			t.Fatal("nil error with no keys")
		}
		for kid, keys := range set {
			if kid == "" || len(kid) > keySetMaxKidLen {
				t.Fatalf("bad kid %q kept", kid)
			}
			for _, k := range keys {
				if !k.IsPublic() || !k.Valid() {
					t.Fatal("a private or invalid key was kept")
				}
			}
		}
	})
}

// A token with no kid, or an absurd one, is refused before anything else: it must not cost
// a request to the identity provider.
func TestBoundedKeySetBadHeaderCostsNoRequest(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, "k1")
	srv := newJWKSServer(t, key)
	ks := newTestKeySet(t, srv, newStepClock())
	for _, token := range []string{
		key.sign(t, "", `{}`),
		key.sign(t, strings.Repeat("k", keySetMaxKidLen+1), `{}`),
		"not.a.jwt",
	} {
		if _, err := ks.VerifySignature(context.Background(), token); !errors.Is(err, errKeySetToken) {
			t.Fatalf("%v, want a malformed-header refusal", err)
		}
	}
	if got := srv.hits.Load(); got != 0 {
		t.Fatalf("%d JWKS requests for malformed headers, want 0", got)
	}
}
