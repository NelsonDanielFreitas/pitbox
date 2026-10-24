package auth

import (
	"crypto/hmac"
	"strings"
	"testing"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
)

func recoveryHasher(t *testing.T, keys ...config.AuthKey) *RecoveryCodeHasher {
	t.Helper()
	kr, err := NewKeyring(keys)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	h, err := kr.RecoveryCodeHasher()
	if err != nil {
		t.Fatalf("RecoveryCodeHasher: %v", err)
	}
	return h
}

func TestGenerateRecoveryCodesShape(t *testing.T) {
	t.Parallel()
	h := recoveryHasher(t, key("k1", 1))

	codes, hashes, err := h.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(codes) != recoveryCodeCount || len(hashes) != recoveryCodeCount {
		t.Fatalf("codes = %d, hashes = %d, want %d", len(codes), len(hashes), recoveryCodeCount)
	}
	if h.KeyID() != "k1" {
		t.Fatalf("KeyID = %q, want k1", h.KeyID())
	}

	seen := map[string]struct{}{}
	for i, code := range codes {
		normalized, ok := NormalizeRecoveryCode(code)
		if !ok {
			t.Fatalf("code %d is not valid: %q", i, code)
		}
		if len(normalized) != recoveryCodeLength {
			t.Fatalf("normalized code %d has %d characters, want %d", i, len(normalized), recoveryCodeLength)
		}
		raw, err := recoveryCodeEncoding.DecodeString(normalized)
		if err != nil || len(raw) != recoveryCodeEntropy {
			t.Fatalf("decode code %d: %d bytes, %v", i, len(raw), err)
		}
		groups := strings.Split(code, "-")
		if len(groups) != 7 {
			t.Fatalf("code %d has %d groups: %q", i, len(groups), code)
		}
		for j, group := range groups {
			want := recoveryCodeGroup
			if j == len(groups)-1 {
				want = recoveryCodeLength % recoveryCodeGroup
			}
			if len(group) != want {
				t.Fatalf("code %d group %d has %d characters, want %d", i, j, len(group), want)
			}
		}
		if _, ok := seen[normalized]; ok {
			t.Fatalf("duplicate code %d", i)
		}
		seen[normalized] = struct{}{}
		if len(hashes[i]) != 32 || !hmac.Equal(hashes[i], h.Hash(code)) {
			t.Fatalf("hash %d does not match the active-key HMAC", i)
		}
	}
}

func TestRecoveryCodeHashesAreKeyed(t *testing.T) {
	t.Parallel()
	code := strings.Repeat("A", recoveryCodeLength)
	first := recoveryHasher(t, key("k1", 1))
	second := recoveryHasher(t, key("k2", 2))

	if hmac.Equal(first.Hash(code), second.Hash(code)) {
		t.Fatal("the same code must have a different hash under another key")
	}
	if !hmac.Equal(first.Hash(code), first.Hash(code)) {
		t.Fatal("the same code and key must produce the same hash")
	}
}

func TestRecoveryCodeRotation(t *testing.T) {
	t.Parallel()
	old := recoveryHasher(t, key("k1", 1))
	code := "AAAA-AAAA-AAAA-AAAA-AAAA-AAAA-AA"
	oldHash := old.Hash(code)

	rotated := recoveryHasher(t, key("k2", 2), key("k1", 1))
	if rotated.KeyID() != "k2" {
		t.Fatalf("KeyID = %q, want k2", rotated.KeyID())
	}
	if hmac.Equal(rotated.Hash(code), oldHash) {
		t.Fatal("new codes must use the active key")
	}
	found := false
	for _, candidate := range rotated.Hashes(code) {
		if hmac.Equal(candidate, oldHash) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the old key must still verify codes generated before the rotation")
	}
}

func TestNormalizeRecoveryCode(t *testing.T) {
	t.Parallel()
	canonical := strings.Repeat("A", recoveryCodeLength)

	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{name: "canonical", in: canonical, want: canonical, ok: true},
		{name: "lowercase and separators", in: "aaaa-aaaa aaaa\taaaa\naaaa-aaaa-aa", want: canonical, ok: true},
		{name: "wrong length short", in: strings.Repeat("A", recoveryCodeLength-1), ok: false},
		{name: "wrong length long", in: strings.Repeat("A", recoveryCodeLength+1), ok: false},
		{name: "invalid character", in: canonical[:25] + "!", ok: false},
		{name: "zero is not base32", in: canonical[:25] + "0", ok: false},
		{name: "non-canonical padding bits", in: canonical[:25] + "B", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := NormalizeRecoveryCode(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("normalized = %q, want %q", got, tc.want)
			}
		})
	}
}

func FuzzNormalizeRecoveryCode(f *testing.F) {
	f.Add(strings.Repeat("A", recoveryCodeLength))
	f.Add("aaaa-bbbb-cccc-dddd-eeee-ffff-gg")
	f.Add("not a recovery code")
	f.Fuzz(func(t *testing.T, s string) {
		normalized, ok := NormalizeRecoveryCode(s)
		if !ok {
			return
		}
		if len(normalized) != recoveryCodeLength {
			t.Fatalf("normalized length = %d", len(normalized))
		}
		raw, err := recoveryCodeEncoding.DecodeString(normalized)
		if err != nil || len(raw) != recoveryCodeEntropy {
			t.Fatalf("decode: %d bytes, %v", len(raw), err)
		}
		if recoveryCodeEncoding.EncodeToString(raw) != normalized {
			t.Fatalf("normalized code is not canonical")
		}
	})
}
