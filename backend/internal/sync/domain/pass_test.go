package domain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPassRoundTripAndTamper(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer := PassSigner{KeyID: "k1", Key: priv}
	ver := PassVerifier{"k1": pub}
	now := time.Unix(1_700_000_000, 0)
	c := PassClaims{PassID: uuid.New(), TeamID: uuid.New(), DeviceID: uuid.New(), Role: "mechanic", Expires: now.Add(time.Hour).Unix()}

	tok, err := signer.Sign(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ver.Verify(tok, now)
	if err != nil || got != c {
		t.Fatalf("round trip: %v %+v", err, got)
	}
	if _, err := ver.Verify(tok, now.Add(2*time.Hour)); err == nil {
		t.Error("expired pass accepted")
	}

	parts := strings.Split(tok, ".")
	enc := base64.RawURLEncoding
	forged := enc.EncodeToString([]byte(`{"pid":"` + c.PassID.String() + `","tid":"` + uuid.NewString() + `","did":"` + c.DeviceID.String() + `","role":"owner","exp":9999999999}`))
	for name, bad := range map[string]string{
		"payload swapped": parts[0] + "." + forged + "." + parts[2],
		"no signature":    parts[0] + "." + parts[1] + ".",
		"alg none":        enc.EncodeToString([]byte(`{"kid":"k1","alg":"none"}`)) + "." + parts[1] + ".",
		"unknown kid":     enc.EncodeToString([]byte(`{"kid":"zz","alg":"EdDSA"}`)) + "." + parts[1] + "." + parts[2],
		"garbage":         "a.b",
		"empty":           "",
	} {
		if _, err := ver.Verify(bad, now); err == nil {
			t.Errorf("%s accepted", name)
		}
	}

	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := (PassVerifier{"k1": other}).Verify(tok, now); err == nil {
		t.Error("accepted under another key")
	}
}
