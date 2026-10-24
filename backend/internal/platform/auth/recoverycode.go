package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"strings"
	"unicode"
)

const (
	// recoveryCodeCount is how many single-use codes a user gets per generation.
	recoveryCodeCount = 8
	// recoveryCodeEntropy is the number of random bytes before base32 encoding.
	recoveryCodeEntropy = 16
	// recoveryCodeLength is the base32 length of recoveryCodeEntropy bytes.
	recoveryCodeLength = 26
	// recoveryCodeGroup is the size of the readable groups shown to the user.
	recoveryCodeGroup = 4
)

// recoveryCodeEncoding is unpadded standard base32: 16 bytes -> 26 characters.
var recoveryCodeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

type recoveryCodeKey struct {
	id     string
	macKey []byte
}

// RecoveryCodeHasher generates and hashes recovery codes with a keyed MAC. The first key
// in the ring is active for new codes; every key is used to verify an existing code.
type RecoveryCodeHasher struct {
	keys []recoveryCodeKey
}

// KeyID identifies the active key that hashes new codes.
func (h *RecoveryCodeHasher) KeyID() string {
	if h == nil || len(h.keys) == 0 {
		return ""
	}
	return h.keys[0].id
}

// Generate creates a fresh set of single-use codes, returning both the plaintext values
// (shown exactly once to the user) and their HMACs for storage.
func (h *RecoveryCodeHasher) Generate() (codes []string, hashes [][]byte, err error) {
	if h == nil || len(h.keys) == 0 {
		return nil, nil, errors.New("auth: no recovery code key")
	}
	codes = make([]string, recoveryCodeCount)
	hashes = make([][]byte, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		b := make([]byte, recoveryCodeEntropy)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		code := formatRecoveryCode(recoveryCodeEncoding.EncodeToString(b))
		codes[i] = code
		hashes[i] = h.Hash(code)
	}
	return codes, hashes, nil
}

// Hash returns the HMAC of a normalized code with the active key. It returns nil when the
// code is not well formed.
func (h *RecoveryCodeHasher) Hash(code string) []byte {
	normalized, ok := NormalizeRecoveryCode(code)
	if !ok || h == nil || len(h.keys) == 0 {
		return nil
	}
	return h.mac(h.keys[0].macKey, normalized)
}

// Hashes returns the HMACs of a normalized code with every key in the ring, in ring order.
// The active key comes first. It returns nil when the code is not well formed.
func (h *RecoveryCodeHasher) Hashes(code string) [][]byte {
	normalized, ok := NormalizeRecoveryCode(code)
	if !ok || h == nil || len(h.keys) == 0 {
		return nil
	}
	hashes := make([][]byte, 0, len(h.keys))
	for _, key := range h.keys {
		hashes = append(hashes, h.mac(key.macKey, normalized))
	}
	return hashes
}

func (h *RecoveryCodeHasher) mac(key []byte, normalized string) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(normalized))
	return m.Sum(nil)
}

// formatRecoveryCode returns a readable, uppercase base32 string in groups.
func formatRecoveryCode(encoded string) string {
	var sb strings.Builder
	sb.Grow(len(encoded) + len(encoded)/recoveryCodeGroup)
	for i := 0; i < len(encoded); i++ {
		if i > 0 && i%recoveryCodeGroup == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(encoded[i])
	}
	return sb.String()
}

// NormalizeRecoveryCode canonicalizes a code entered by the user: uppercase, no separators
// or whitespace, exact base32 length, and a canonical (unpadded) encoding. It rejects any
// other character and any non-canonical spelling.
func NormalizeRecoveryCode(s string) (string, bool) {
	var sb strings.Builder
	sb.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			sb.WriteRune(r - 'a' + 'A')
		case (r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7'):
			sb.WriteRune(r)
		case r == '-' || unicode.IsSpace(r):
			// separators and whitespace are ignored
		default:
			return "", false
		}
	}
	code := sb.String()
	if len(code) != recoveryCodeLength {
		return "", false
	}
	raw, err := recoveryCodeEncoding.DecodeString(code)
	if err != nil || len(raw) != recoveryCodeEntropy {
		return "", false
	}
	// encoding/base32 has no Strict mode: a non-zero padding bit still decodes without
	// error, so compare with the canonical spelling.
	if recoveryCodeEncoding.EncodeToString(raw) != code {
		return "", false
	}
	return code, true
}
