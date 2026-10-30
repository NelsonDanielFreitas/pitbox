package domain

import (
	"encoding/asn1"
	"math/big"
)

// p1363ToASN1 converts r||s (what WebCrypto produces) to the DER SEQUENCE of two
// INTEGERs that ecdsa.VerifyASN1 expects. Go's ecdsa has no P1363 verifier, and
// verifying with big integers by hand is not worth the risk.
func p1363ToASN1(sig []byte) []byte {
	der, err := asn1.Marshal(struct{ R, S *big.Int }{
		new(big.Int).SetBytes(sig[:32]),
		new(big.Int).SetBytes(sig[32:]),
	})
	if err != nil {
		return nil // VerifyASN1 rejects an empty signature
	}
	return der
}
