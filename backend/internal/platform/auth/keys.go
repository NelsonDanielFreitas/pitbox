// Package auth holds the BFF login for pitbox-api (ADR 0013): sealed cookies, the session
// store, the OIDC flow and the authentication interceptor.
//
// Nothing in this package validates a JWT by hand: the ID token is verified by go-oidc.
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/config"
)

// Purposes: each one gets its own key derived from the master key, so a value sealed or
// authenticated for one purpose can never be opened or verified as another (and a bug in one
// place cannot decrypt or authenticate the other).
const (
	PurposeLoginCookie  = "login-cookie"
	PurposeSessionToken = "session-token"
	PurposeRecoveryCode = "recovery-code"
)

// ErrInvalidSealed is the only error Open returns. A malformed value, an unknown kid and
// a failed authentication tag all look the same, so a caller (or an attacker watching
// the response) learns nothing from the difference.
var ErrInvalidSealed = errors.New("auth: invalid sealed value")

// maxSealedLen bounds what Open will even look at.
const maxSealedLen = 8192

// Keyring holds the master keys. The first one seals; every one can open (rotation: add
// a new first key, keep the old ones until the sessions sealed with them have expired and
// no unused recovery code carries their kid).
type Keyring struct {
	keys []config.AuthKey
}

// NewKeyring checks the keys again (they come from config, but this package must not
// trust its caller with the one thing that protects sessions and recovery codes).
func NewKeyring(keys []config.AuthKey) (*Keyring, error) {
	if len(keys) == 0 {
		return nil, errors.New("auth: no master key configured")
	}
	for _, k := range keys {
		if k.ID == "" || strings.ContainsAny(k.ID, ".:,") || len(k.Secret) < 32 {
			return nil, errors.New("auth: invalid master key")
		}
	}
	return &Keyring{keys: keys}, nil
}

// deriveKey derives one 32-byte key per purpose and kid. HKDF-SHA256 (RFC 5869):
// info binds the derived key to the purpose and the kid.
func deriveKey(secret []byte, purpose, kid string) ([]byte, error) {
	return hkdf.Key(sha256.New, secret, nil, "pitbox/v1/"+purpose+"/"+kid, 32)
}

// Sealer returns the AEAD for one purpose.
func (k *Keyring) Sealer(purpose string) (*Sealer, error) {
	s := &Sealer{aeads: make(map[string]cipher.AEAD, len(k.keys)), active: k.keys[0].ID}
	for _, key := range k.keys {
		dk, err := deriveKey(key.Secret, purpose, key.ID)
		if err != nil {
			return nil, fmt.Errorf("derive key: %w", err)
		}
		block, err := aes.NewCipher(dk)
		if err != nil {
			return nil, fmt.Errorf("new cipher: %w", err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("new gcm: %w", err)
		}
		s.aeads[key.ID] = aead
	}
	return s, nil
}

// RecoveryCodeHasher derives one HMAC key per purpose and kid. It is deliberately a
// different purpose from the AEAD sealers: a key never seals a cookie and authenticates a
// recovery code.
func (k *Keyring) RecoveryCodeHasher() (*RecoveryCodeHasher, error) {
	if len(k.keys) > maxRecoveryCodeKeys {
		return nil, errors.New("auth: too many recovery code keys")
	}
	h := &RecoveryCodeHasher{keys: make([]recoveryCodeKey, 0, len(k.keys))}
	for _, key := range k.keys {
		macKey, err := deriveKey(key.Secret, PurposeRecoveryCode, key.ID)
		if err != nil {
			return nil, fmt.Errorf("derive recovery code key: %w", err)
		}
		h.keys = append(h.keys, recoveryCodeKey{id: key.ID, macKey: macKey})
	}
	return h, nil
}

// Sealer seals and opens values with AES-256-GCM. The format is "<kid>.<base64url(nonce||ciphertext)>".
//
// Go note: GCM with a random 96-bit nonce is safe for up to about 2^32 messages per key
// (NIST SP 800-38D). A login cookie and a refresh token per session are far below that.
type Sealer struct {
	aeads  map[string]cipher.AEAD
	active string
}

// Seal encrypts and authenticates plaintext. aad is not secret but is authenticated: a
// value sealed with one aad does not open with another (use it to bind a value to the
// cookie name or the session it belongs to).
func (s *Sealer) Seal(plaintext, aad []byte) (string, error) {
	aead := s.aeads[s.active]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, plaintext, aad) // nonce || ciphertext || tag
	return s.active + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open verifies and decrypts a value from Seal.
func (s *Sealer) Open(value string, aad []byte) ([]byte, error) {
	if len(value) == 0 || len(value) > maxSealedLen {
		return nil, ErrInvalidSealed
	}
	kid, b64, ok := strings.Cut(value, ".")
	if !ok {
		return nil, ErrInvalidSealed
	}
	aead, ok := s.aeads[kid]
	if !ok {
		return nil, ErrInvalidSealed
	}
	// Strict: a non-canonical encoding of the same bytes is rejected, so each sealed value
	// has exactly one valid spelling.
	raw, err := base64.RawURLEncoding.Strict().DecodeString(b64)
	if err != nil || len(raw) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrInvalidSealed
	}
	nonce, ct := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, ErrInvalidSealed
	}
	return plaintext, nil
}
