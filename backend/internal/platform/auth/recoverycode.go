package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

const (
	// recoveryCodeCount is how many single-use codes a user gets per generation.
	recoveryCodeCount = 8
	// recoveryCodeEntropy is the number of random bytes before base32 encoding.
	recoveryCodeEntropy = 8
)

// GenerateRecoveryCodes creates a fresh set of single-use codes, returning both the plaintext
// values (shown exactly once to the user) and their SHA-256 hashes for storage. Codes are
// uppercase base32 strings with no padding or separators.
func GenerateRecoveryCodes() (codes []string, hashes [][]byte, err error) {
	codes = make([]string, recoveryCodeCount)
	hashes = make([][]byte, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		b := make([]byte, recoveryCodeEntropy)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		code := encodeRecoveryCode(b)
		codes[i] = code
		hashes[i] = HashRecoveryCode(code)
	}
	return codes, hashes, nil
}

// encodeRecoveryCode returns a readable, uppercase base32 string without padding.
func encodeRecoveryCode(b []byte) string {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return enc.EncodeToString(b)
}

// NormalizeRecoveryCode canonicalizes a code entered by the user: uppercase and drop any
// separators or whitespace. It keeps only characters that are valid in base32.
func NormalizeRecoveryCode(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		// Drop everything outside the base32 alphabet (letters A-Z and digits 2-7).
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '2' && c <= '7') {
			if c >= 'a' && c <= 'z' {
				c = c - 32
			}
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

// HashRecoveryCode returns the SHA-256 hash of a normalized code.
func HashRecoveryCode(code string) []byte {
	sum := sha256.Sum256([]byte(NormalizeRecoveryCode(code)))
	return sum[:]
}
