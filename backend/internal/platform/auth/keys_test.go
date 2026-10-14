package auth

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
)

func key(id string, fill byte) config.AuthKey {
	return config.AuthKey{ID: id, Secret: bytes.Repeat([]byte{fill}, 32)}
}

func sealer(t *testing.T, purpose string, keys ...config.AuthKey) *Sealer {
	t.Helper()
	s, err := newSealer(purpose, keys...)
	if err != nil {
		t.Fatalf("sealer: %v", err)
	}
	return s
}

func newSealer(purpose string, keys ...config.AuthKey) (*Sealer, error) {
	kr, err := NewKeyring(keys)
	if err != nil {
		return nil, err
	}
	return kr.Sealer(purpose)
}

func TestSealOpenRoundTrip(t *testing.T) {
	t.Parallel()

	s := sealer(t, PurposeLoginCookie, key("k1", 1))
	sealed, err := s.Seal([]byte("segredo"), []byte("aad"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed, "segredo") {
		t.Fatalf("sealed value contains the plaintext: %s", sealed)
	}
	if !strings.HasPrefix(sealed, "k1.") {
		t.Errorf("sealed value does not name its key: %s", sealed)
	}
	got, err := s.Open(sealed, []byte("aad"))
	if err != nil || string(got) != "segredo" {
		t.Fatalf("Open = %q, %v", got, err)
	}
}

func TestSealIsRandomised(t *testing.T) {
	t.Parallel()

	s := sealer(t, PurposeLoginCookie, key("k1", 1))
	a, _ := s.Seal([]byte("x"), nil)
	b, _ := s.Seal([]byte("x"), nil)
	if a == b {
		t.Error("two seals of the same value are identical: the nonce is not random")
	}
}

func TestOpenRejects(t *testing.T) {
	t.Parallel()

	s := sealer(t, PurposeLoginCookie, key("k1", 1))
	good, _ := s.Seal([]byte("segredo"), []byte("aad"))
	kid, body, _ := strings.Cut(good, ".")

	// A bit flipped anywhere in the body must be caught by the tag.
	flipped := []byte(body)
	flipped[len(flipped)/2] ^= 0x01
	other := sealer(t, PurposeSessionToken, key("k1", 1))
	otherKey := sealer(t, PurposeLoginCookie, key("k1", 2))

	tests := map[string]struct {
		sealer *Sealer
		value  string
		aad    string
	}{
		"empty":                    {s, "", "aad"},
		"no separator":             {s, body, "aad"},
		"unknown kid":              {s, "k9." + body, "aad"},
		"not base64":               {s, kid + ".%%%", "aad"},
		"too short":                {s, kid + ".AAAA", "aad"},
		"tampered":                 {s, kid + "." + string(flipped), "aad"},
		"other aad":                {s, good, "aad2"},
		"other purpose":            {other, good, "aad"},
		"same kid, different key":  {otherKey, good, "aad"},
		"oversized":                {s, kid + "." + strings.Repeat("A", maxSealedLen), "aad"},
		"truncated":                {s, good[:len(good)-4], "aad"},
		"kid only":                 {s, kid + ".", "aad"},
		"extra separator in value": {s, good + ".x", "aad"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := tc.sealer.Open(tc.value, []byte(tc.aad))
			if !errors.Is(err, ErrInvalidSealed) {
				t.Fatalf("Open = %v, want ErrInvalidSealed", err)
			}
		})
	}
}

// Rotation: values sealed with the old key still open once a new first key is added, and
// new values use the new key.
func TestKeyRotation(t *testing.T) {
	t.Parallel()

	old := sealer(t, PurposeSessionToken, key("k1", 1))
	sealedOld, _ := old.Seal([]byte("refresh"), nil)

	rotated := sealer(t, PurposeSessionToken, key("k2", 2), key("k1", 1))
	if got, err := rotated.Open(sealedOld, nil); err != nil || string(got) != "refresh" {
		t.Fatalf("old value must still open: %q, %v", got, err)
	}
	sealedNew, _ := rotated.Seal([]byte("refresh"), nil)
	if !strings.HasPrefix(sealedNew, "k2.") {
		t.Errorf("new values must use the first key: %s", sealedNew)
	}
	if _, err := old.Open(sealedNew, nil); !errors.Is(err, ErrInvalidSealed) {
		t.Errorf("a retired deployment must not open new values: %v", err)
	}
}

func TestNewKeyringRefusesWeakKeys(t *testing.T) {
	t.Parallel()

	for name, keys := range map[string][]config.AuthKey{
		"none":         nil,
		"short secret": {{ID: "k1", Secret: []byte("short")}},
		"empty kid":    {{ID: "", Secret: bytes.Repeat([]byte{1}, 32)}},
		"kid with dot": {{ID: "k.1", Secret: bytes.Repeat([]byte{1}, 32)}},
	} {
		if _, err := NewKeyring(keys); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func FuzzOpen(f *testing.F) {
	s, err := newSealer(PurposeLoginCookie, key("k1", 1))
	if err != nil {
		f.Fatalf("newSealer: %v", err)
	}
	good, _ := s.Seal([]byte("x"), nil)
	for _, seed := range []string{"", good, "k1.", ".", "k1.AAAA", strings.Repeat("k1.", 100)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		// Property: Open never panics, and the only plaintext that can ever come out is the
		// one we sealed (the fuzzing workers run this setup again, so `good` differs per
		// process; any value that opens must still decrypt to "x", never to anything forged).
		if got, err := s.Open(value, nil); err == nil && string(got) != "x" {
			t.Fatalf("a forged value opened to %q: %q", got, value)
		}
	})
}

// Each sealed value has one valid spelling: changing the unused trailing bits of the base64
// (which a lenient decoder would ignore) must not produce a second value that opens.
func TestOpenRejectsNonCanonicalBase64(t *testing.T) {
	t.Parallel()

	s := sealer(t, PurposeLoginCookie, key("k1", 1))
	good, _ := s.Seal([]byte("segredo"), nil)
	alphabet := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	tried := 0
	for _, c := range alphabet {
		variant := good[:len(good)-1] + string(c)
		if variant == good {
			continue
		}
		tried++
		if _, err := s.Open(variant, nil); err == nil {
			t.Fatalf("a second spelling of the same value opened: %s", variant)
		}
	}
	if tried == 0 {
		t.Fatal("no variants tried")
	}
}
