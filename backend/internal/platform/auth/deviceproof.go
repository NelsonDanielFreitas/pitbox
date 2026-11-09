package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math/big"
)

// The phone holds a P-256 session key in its Keystore (ADR 0032). It never leaves the
// device, and every sensitive step of the mobile login is signed with it: the code exchange
// (so a stolen authorization code is useless) and every renewal of the Bearer (so a stolen
// Bearer cannot be renewed).
//
// Each signature covers a domain tag, so a signature made for one purpose is never valid for
// another, and length-prefixed fields, so "ab"+"c" and "a"+"bc" are different messages. The
// tag also keeps these signatures apart from the sync operation signatures (ADR 0025), even
// if one day the same key were used for both.
const (
	proofTokenDomain   = "pitbox-mobile-v1/token" //nolint:gosec // a domain tag, not a credential
	proofRefreshDomain = "pitbox-mobile-v1/refresh"

	// p256SPKIMaxLen bounds the key we accept: an ECDSA P-256 SPKI is 91 bytes.
	p256SPKIMaxLen = 128
	// p1363Len is the size of a P-256 signature as r||s.
	p1363Len = 64
)

// errDeviceKey and errDeviceProof carry no detail on purpose: the input is the client's.
var (
	errDeviceKey   = errors.New("auth: invalid device key")
	errDeviceProof = errors.New("auth: invalid device proof")
)

// parseDeviceKey accepts only an ECDSA P-256 key in SPKI DER, as standard base64. Other
// curves, other algorithms and trailing bytes are refused: the algorithm is fixed by the
// protocol, never chosen by the client.
func parseDeviceKey(b64 string) (spki []byte, pub *ecdsa.PublicKey, err error) {
	if len(b64) == 0 || len(b64) > base64.StdEncoding.EncodedLen(p256SPKIMaxLen) {
		return nil, nil, errDeviceKey
	}
	der, err := base64.StdEncoding.Strict().DecodeString(b64)
	if err != nil {
		return nil, nil, errDeviceKey
	}
	return parseDeviceKeyDER(der)
}

func parseDeviceKeyDER(der []byte) ([]byte, *ecdsa.PublicKey, error) {
	if len(der) == 0 || len(der) > p256SPKIMaxLen {
		return nil, nil, errDeviceKey
	}
	k, err := x509.ParsePKIXPublicKey(der) // errors on trailing data
	if err != nil {
		return nil, nil, errDeviceKey
	}
	pub, ok := k.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, nil, errDeviceKey
	}
	return der, pub, nil
}

// proofMessage is the signed message: the domain tag, then each field with a 4-byte length.
func proofMessage(domain string, fields ...string) []byte {
	var out []byte
	put := func(s string) {
		out = binary.BigEndian.AppendUint32(out, uint32(len(s))) //nolint:gosec // every field is length-checked by the caller before it gets here
		out = append(out, s...)
	}
	put(domain)
	for _, f := range fields {
		put(f)
	}
	return out
}

// verifyDeviceProof checks an ECDSA P-256 / SHA-256 signature in IEEE P1363 form (r||s, 64
// bytes, which is what the Keystore signature becomes after the app converts it, ADR 0025),
// given as unpadded base64url. Any malformed input is simply a bad proof.
func verifyDeviceProof(pub *ecdsa.PublicKey, proofB64, domain string, fields ...string) error {
	if pub == nil || len(proofB64) != base64.RawURLEncoding.EncodedLen(p1363Len) {
		return errDeviceProof
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(proofB64)
	if err != nil || len(sig) != p1363Len {
		return errDeviceProof
	}
	der, err := asn1.Marshal(struct{ R, S *big.Int }{new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])})
	if err != nil {
		return errDeviceProof
	}
	digest := sha256.Sum256(proofMessage(domain, fields...))
	if !ecdsa.VerifyASN1(pub, digest[:], der) {
		return errDeviceProof
	}
	return nil
}
