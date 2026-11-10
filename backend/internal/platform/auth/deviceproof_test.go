package auth

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"
)

// deviceKey is a phone's session key in the tests.
type deviceKey struct {
	priv   *ecdsa.PrivateKey
	der    []byte
	spkiB6 string // standard base64, as the app sends it
}

func newDeviceKey(t testing.TB) deviceKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return deviceKey{priv: priv, der: der, spkiB6: base64.StdEncoding.EncodeToString(der)}
}

// proof signs like the app does (ECDSA P-256, SHA-256, r||s, base64url).
func (k deviceKey) proof(t testing.TB, domain string, fields ...string) string {
	t.Helper()
	digest := sha256.Sum256(proofMessage(domain, fields...))
	r, s, err := ecdsa.Sign(rand.Reader, k.priv, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...))
}

func TestDeviceProofVerifies(t *testing.T) {
	t.Parallel()
	k := newDeviceKey(t)
	for range 50 { // the signatures differ every time; r and s have leading zeros now and then
		p := k.proof(t, proofTokenDomain, "state", "code")
		if err := verifyDeviceProof(&k.priv.PublicKey, p, proofTokenDomain, "state", "code"); err != nil {
			t.Fatalf("a valid proof was refused: %v", err)
		}
	}
}

func TestDeviceProofRefusals(t *testing.T) {
	t.Parallel()
	k, other := newDeviceKey(t), newDeviceKey(t)
	good := k.proof(t, proofRefreshDomain, "ref", "7")
	pub := &k.priv.PublicKey

	tests := []struct {
		name   string
		pub    *ecdsa.PublicKey
		proof  string
		domain string
		fields []string
	}{
		{"another key", &other.priv.PublicKey, good, proofRefreshDomain, []string{"ref", "7"}},
		{"another domain: a token proof is not a refresh proof", pub, k.proof(t, proofTokenDomain, "ref", "7"), proofRefreshDomain, []string{"ref", "7"}},
		{"another domain, the other way round", pub, good, proofTokenDomain, []string{"ref", "7"}},
		{"another counter", pub, good, proofRefreshDomain, []string{"ref", "8"}},
		{"another session", pub, good, proofRefreshDomain, []string{"ref2", "7"}},
		{"fewer fields", pub, good, proofRefreshDomain, []string{"ref"}},
		{"more fields", pub, good, proofRefreshDomain, []string{"ref", "7", ""}},
		// Length prefixes: the same bytes split in another way are another message.
		{"fields joined", pub, k.proof(t, proofRefreshDomain, "ab", "c"), proofRefreshDomain, []string{"abc"}},
		{"fields split elsewhere", pub, k.proof(t, proofRefreshDomain, "ab", "c"), proofRefreshDomain, []string{"a", "bc"}},
		{"no key", nil, good, proofRefreshDomain, []string{"ref", "7"}},
		{"empty proof", pub, "", proofRefreshDomain, []string{"ref", "7"}},
		{"proof too short", pub, good[:len(good)-1], proofRefreshDomain, []string{"ref", "7"}},
		{"proof too long", pub, good + "A", proofRefreshDomain, []string{"ref", "7"}},
		{"padded base64", pub, good + "==", proofRefreshDomain, []string{"ref", "7"}},
		{"standard base64 alphabet", pub, strings.NewReplacer("-", "+", "_", "/").Replace(base64.RawURLEncoding.EncodeToString(make([]byte, 64))), proofRefreshDomain, []string{"ref", "7"}},
		{"r and s are zero", pub, base64.RawURLEncoding.EncodeToString(make([]byte, 64)), proofRefreshDomain, []string{"ref", "7"}},
		{"r and s are the group order", pub, base64.RawURLEncoding.EncodeToString(append(elliptic.P256().Params().N.FillBytes(make([]byte, 32)), elliptic.P256().Params().N.FillBytes(make([]byte, 32))...)), proofRefreshDomain, []string{"ref", "7"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifyDeviceProof(tc.pub, tc.proof, tc.domain, tc.fields...); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	// Control: the unmodified proof passes.
	if err := verifyDeviceProof(pub, good, proofRefreshDomain, "ref", "7"); err != nil {
		t.Fatalf("control: %v", err)
	}
}

func TestParseDeviceKey(t *testing.T) {
	t.Parallel()
	k := newDeviceKey(t)

	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	p384der, _ := x509.MarshalPKIXPublicKey(&p384.PublicKey)
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 1024) //nolint:gosec // a weak key on purpose: it is only here to be refused
	rsader, _ := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	edPub, _, _ := ed25519.GenerateKey(rand.Reader)
	eder, _ := x509.MarshalPKIXPublicKey(edPub)
	enc := base64.StdEncoding.EncodeToString

	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"P-256", k.spkiB6, true},
		{"P-384", enc(p384der), false},
		{"RSA", enc(rsader), false},
		{"Ed25519", enc(eder), false},
		{"trailing byte", enc(append(append([]byte{}, k.der...), 0)), false},
		{"truncated", enc(k.der[:len(k.der)-1]), false},
		{"empty", "", false},
		{"not base64", "%%%", false},
		{"URL-safe alphabet", strings.NewReplacer("+", "-", "/", "_").Replace(k.spkiB6), k.spkiB6 == strings.NewReplacer("+", "-", "/", "_").Replace(k.spkiB6)},
		{"too big", strings.Repeat("A", 4000), false},
		{"a raw point, not SPKI", enc(elliptic.Marshal(elliptic.P256(), k.priv.X, k.priv.Y)), false}, //nolint:staticcheck // the encoding is what is being refused
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spki, pub, err := parseDeviceKey(tc.in)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok = %v", err, tc.ok)
			}
			if err == nil && (pub == nil || len(spki) == 0 || !pub.Equal(&k.priv.PublicKey)) {
				t.Fatal("accepted, but not the same key")
			}
		})
	}
}

func TestProofMessageIsInjective(t *testing.T) {
	t.Parallel()
	seen := map[string][]string{}
	for _, fields := range [][]string{{}, {""}, {"", ""}, {"a"}, {"a", ""}, {"", "a"}, {"ab"}, {"a", "b"}, {"ab", "c"}, {"a", "bc"}, {"abc"}} {
		m := string(proofMessage("d", fields...))
		if prev, dup := seen[m]; dup {
			t.Fatalf("%q and %q give the same message", prev, fields)
		}
		seen[m] = fields
	}
}

func FuzzParseDeviceKey(f *testing.F) {
	k := newDeviceKey(f)
	f.Add(k.spkiB6)
	f.Add("")
	f.Add("MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE")
	f.Fuzz(func(t *testing.T, in string) {
		spki, pub, err := parseDeviceKey(in)
		if err != nil {
			return
		}
		if pub == nil || pub.Curve != elliptic.P256() || len(spki) == 0 || len(spki) > p256SPKIMaxLen {
			t.Fatalf("accepted a key that is not P-256 in a bounded SPKI: %q", in)
		}
	})
}

func FuzzVerifyDeviceProof(f *testing.F) {
	k := newDeviceKey(f)
	f.Add(k.proof(f, proofTokenDomain, "s", "c"), "s", "c")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, proof, a, b string) {
		err := verifyDeviceProof(&k.priv.PublicKey, proof, proofTokenDomain, a, b)
		if err != nil {
			return
		}
		// Whatever verifies must be a genuine signature over exactly these fields: the only
		// way to get one is to have the key, so re-checking with a different split must fail.
		if a != "" && b != "" {
			if verifyDeviceProof(&k.priv.PublicKey, proof, proofTokenDomain, a+b) == nil {
				t.Fatal("a proof verified for two different splits of the same bytes")
			}
		}
	})
}
