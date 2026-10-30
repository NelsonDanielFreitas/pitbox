package domain

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PassTTL is the longest an offline pass lives (ADR 0003).
const PassTTL = 72 * time.Hour

var ErrBadPass = errors.New("sync: invalid offline pass")

// PassClaims is what the server vouches for in an offline pass. The database row is the
// source of truth (it can be revoked); the token lets the device show its own expiry and
// lets the server reject a forged id before touching the database.
type PassClaims struct {
	PassID   uuid.UUID `json:"pid"`
	TeamID   uuid.UUID `json:"tid"`
	DeviceID uuid.UUID `json:"did"`
	Role     string    `json:"role"`
	Expires  int64     `json:"exp"` // unix seconds
}

// PassSigner signs passes with Ed25519. KeyID names the key so it can be rotated: the
// verifier accepts every key of the ring, the signer uses only the current one.
type PassSigner struct {
	KeyID string
	Key   ed25519.PrivateKey
}

// PassVerifier holds the public keys by KeyID.
type PassVerifier map[string]ed25519.PublicKey

type passHeader struct {
	KID string `json:"kid"`
	Alg string `json:"alg"`
}

const passAlg = "EdDSA"

// Sign returns header.payload.signature, each base64url without padding.
func (s PassSigner) Sign(c PassClaims) (string, error) {
	if s.KeyID == "" || len(s.Key) != ed25519.PrivateKeySize {
		return "", ErrBadPass
	}
	h, err := json.Marshal(passHeader{KID: s.KeyID, Alg: passAlg})
	if err != nil {
		return "", err
	}
	p, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	signed := enc.EncodeToString(h) + "." + enc.EncodeToString(p)
	return signed + "." + enc.EncodeToString(ed25519.Sign(s.Key, []byte(signed))), nil
}

// Verify checks the signature and the expiry and returns the claims.
func (v PassVerifier) Verify(token string, now time.Time) (PassClaims, error) {
	c, err := v.Open(token)
	if err != nil || now.Unix() >= c.Expires {
		return PassClaims{}, ErrBadPass
	}
	return c, nil
}

// Open checks the signature only. The Push uses it because an expired pass still vouches
// for operations the device made before it expired. The algorithm is fixed: a header that
// names another one (including "none") is refused.
func (v PassVerifier) Open(token string) (PassClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return PassClaims{}, ErrBadPass
	}
	enc := base64.RawURLEncoding
	hb, err1 := enc.DecodeString(parts[0])
	pb, err2 := enc.DecodeString(parts[1])
	sig, err3 := enc.DecodeString(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return PassClaims{}, ErrBadPass
	}
	var h passHeader
	if json.Unmarshal(hb, &h) != nil || h.Alg != passAlg {
		return PassClaims{}, ErrBadPass
	}
	pub, ok := v[h.KID]
	if !ok || len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), sig) {
		return PassClaims{}, ErrBadPass
	}
	var c PassClaims
	if json.Unmarshal(pb, &c) != nil {
		return PassClaims{}, ErrBadPass
	}
	return c, nil
}
