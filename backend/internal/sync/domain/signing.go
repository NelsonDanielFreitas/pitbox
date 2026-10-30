package domain

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"

	"github.com/google/uuid"
)

// signingTag separates this signature from any other use of the device key and carries
// the format version, so a future change of the message cannot be confused with this one.
const signingTag = "pitbox-sync-op-v1"

// SigningMessage builds the bytes a device signs: the tag followed by length-prefixed
// fields (uint32 big-endian length, then the bytes). Length prefixes make the encoding
// injective, so no two different operations produce the same message. Signing the team
// and the device stops an operation being replayed in another team or by another key.
// The fields are covered through SHA-256 of the exact bytes sent (FieldsJSON), so there
// is no JSON canonicalisation to get wrong on either side.
func SigningMessage(teamID, deviceID uuid.UUID, op Operation) []byte {
	var out []byte
	put := func(b []byte) {
		out = binary.BigEndian.AppendUint32(out, uint32(len(b))) //nolint:gosec // every field is a fixed-size value or the 64-byte entity name, validated upstream
		out = append(out, b...)
	}
	putInt := func(v int64) { put(binary.BigEndian.AppendUint64(nil, uint64(v))) } //nolint:gosec // two's complement on purpose: -1 marks an absent base_version

	put([]byte(signingTag))
	put(op.OpID[:])
	put(teamID[:])
	put(deviceID[:])
	put([]byte(op.Entity))
	put(op.EntityID[:])
	putInt(int64(op.Kind))
	if op.BaseVersion == nil {
		putInt(-1)
	} else {
		putInt(int64(*op.BaseVersion))
	}
	putInt(op.ClientTS.UnixMilli())
	sum := sha256.Sum256(op.FieldsJSON)
	put(sum[:])
	return out
}

// ParsePublicKey accepts only an ECDSA P-256 key in SPKI DER. Anything else (other
// curves, RSA, trailing bytes) is refused: the algorithm is fixed by the protocol.
func ParsePublicKey(der []byte) (*ecdsa.PublicKey, error) {
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, ErrBadKey
	}
	pub, ok := k.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, ErrBadKey
	}
	return pub, nil
}

// VerifyOperation checks the device signature (ECDSA P-256 over SHA-256, IEEE P1363
// r||s as produced by WebCrypto). It fails closed: any malformed input is a bad signature.
func VerifyOperation(pub *ecdsa.PublicKey, teamID, deviceID uuid.UUID, op Operation) error {
	if pub == nil || len(op.Signature) != 64 {
		return ErrBadSignature
	}
	digest := sha256.Sum256(SigningMessage(teamID, deviceID, op))
	if !ecdsa.VerifyASN1(pub, digest[:], p1363ToASN1(op.Signature)) {
		return ErrBadSignature
	}
	return nil
}

// regTag separates the proof of possession from operation signatures.
const regTag = "pitbox-sync-device-v1"

// RegistrationMessage is what a new device key signs to prove it holds the private key.
// It binds the team and the user, so a captured proof cannot register the key elsewhere.
func RegistrationMessage(teamID, userID uuid.UUID, publicKey []byte) []byte {
	var out []byte
	put := func(b []byte) {
		out = binary.BigEndian.AppendUint32(out, uint32(len(b))) //nolint:gosec // fixed-size fields; the key is length-checked by the contract
		out = append(out, b...)
	}
	put([]byte(regTag))
	put(teamID[:])
	put(userID[:])
	put(publicKey)
	return out
}

// VerifyRegistration checks the proof of possession for a new device key.
func VerifyRegistration(pub *ecdsa.PublicKey, teamID, userID uuid.UUID, publicKey, proof []byte) error {
	if pub == nil || len(proof) != 64 {
		return ErrBadSignature
	}
	digest := sha256.Sum256(RegistrationMessage(teamID, userID, publicKey))
	if !ecdsa.VerifyASN1(pub, digest[:], p1363ToASN1(proof)) {
		return ErrBadSignature
	}
	return nil
}
