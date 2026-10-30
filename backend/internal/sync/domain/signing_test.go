package domain

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"testing"
	"time"

	"github.com/google/uuid"
)

func sign(t *testing.T, key *ecdsa.PrivateKey, team, dev uuid.UUID, op Operation) []byte {
	t.Helper()
	d := sha256.Sum256(SigningMessage(team, dev, op))
	r, s, err := ecdsa.Sign(rand.Reader, key, d[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return sig
}

func newOp() Operation {
	v := int32(3)
	return Operation{
		OpID: uuid.Must(uuid.NewV7()), Entity: "setup.setup", EntityID: uuid.Must(uuid.NewV7()),
		Kind: KindPatch, BaseVersion: &v, FieldsJSON: []byte(`{"name":"A"}`),
		ClientTS: time.UnixMilli(1_700_000_000_123),
	}
}

func TestVerifyOperation(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	team, dev := uuid.New(), uuid.New()
	op := newOp()
	op.Signature = sign(t, key, team, dev, op)

	if err := VerifyOperation(&key.PublicKey, team, dev, op); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	tamper := map[string]func(o *Operation, tm, dv *uuid.UUID){
		"fields":  func(o *Operation, _, _ *uuid.UUID) { o.FieldsJSON = []byte(`{"name":"B"}`) },
		"entity":  func(o *Operation, _, _ *uuid.UUID) { o.Entity = "setup.other" },
		"kind":    func(o *Operation, _, _ *uuid.UUID) { o.Kind = KindDelete },
		"version": func(o *Operation, _, _ *uuid.UUID) { v := int32(4); o.BaseVersion = &v },
		"no base": func(o *Operation, _, _ *uuid.UUID) { o.BaseVersion = nil },
		"ts":      func(o *Operation, _, _ *uuid.UUID) { o.ClientTS = o.ClientTS.Add(time.Millisecond) },
		"team":    func(_ *Operation, tm, _ *uuid.UUID) { *tm = uuid.New() },
		"device":  func(_ *Operation, _, dv *uuid.UUID) { *dv = uuid.New() },
		"short":   func(o *Operation, _, _ *uuid.UUID) { o.Signature = o.Signature[:63] },
	}
	for name, f := range tamper {
		o, tm, dv := op, team, dev
		o.Signature = append([]byte(nil), op.Signature...)
		f(&o, &tm, &dv)
		if err := VerifyOperation(&key.PublicKey, tm, dv, o); err == nil {
			t.Errorf("%s: tampered operation accepted", name)
		}
	}

	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if VerifyOperation(&other.PublicKey, team, dev, op) == nil {
		t.Error("signature accepted under another key")
	}
	if VerifyOperation(nil, team, dev, op) == nil {
		t.Error("nil key accepted")
	}
}

func TestParsePublicKey(t *testing.T) {
	p256, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&p256.PublicKey)
	if _, err := ParsePublicKey(der); err != nil {
		t.Fatalf("P-256 refused: %v", err)
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	der384, _ := x509.MarshalPKIXPublicKey(&p384.PublicKey)
	if _, err := ParsePublicKey(der384); err == nil {
		t.Error("P-384 accepted")
	}
	if _, err := ParsePublicKey(append(der, 0)); err == nil {
		t.Error("trailing bytes accepted")
	}
	if _, err := ParsePublicKey([]byte("junk")); err == nil {
		t.Error("junk accepted")
	}
}

func FuzzVerifyOperation(f *testing.F) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	f.Add([]byte{1}, []byte(`{}`), "x.y")
	f.Fuzz(func(t *testing.T, sig, fields []byte, entity string) {
		op := newOp()
		op.Signature, op.FieldsJSON, op.Entity = sig, fields, entity
		_ = VerifyOperation(&key.PublicKey, uuid.New(), uuid.New(), op) // must not panic
	})
}
