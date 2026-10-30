package domain

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The TypeScript client (app/src/sync/signing.test.ts) asserts the same hex strings. If the
// message format changes, both tests fail until the tag is bumped on both sides.
const (
	vectorOpMessage  = "00000011706974626f782d73796e632d6f702d7631000000100190a1b2000070008000000000000001000000100190a1b2000070008000000000000002000000100190a1b20000700080000000000000030000000a6761726167652e636172000000100190a1b200007000800000000000000400000008000000000000000200000008000000000000000300000008000001a17720ca7b000000209750d7600148f1d08b70618c9d064965f49c443f76628e83d959306ad908d30b"
	vectorRegMessage = "00000015706974626f782d73796e632d6465766963652d7631000000100190a1b2000070008000000000000002000000100190a1b2000070008000000000000005000000053059010203"
)

func vectorOp() (uuid.UUID, uuid.UUID, Operation) {
	base := int32(3)
	return uuid.MustParse("0190a1b2-0000-7000-8000-000000000002"),
		uuid.MustParse("0190a1b2-0000-7000-8000-000000000003"),
		Operation{
			OpID:        uuid.MustParse("0190a1b2-0000-7000-8000-000000000001"),
			Entity:      "garage.car",
			EntityID:    uuid.MustParse("0190a1b2-0000-7000-8000-000000000004"),
			Kind:        KindPatch,
			FieldsJSON:  []byte(`{"nickname":"Ação <&>"}`),
			BaseVersion: &base,
			ClientTS:    time.UnixMilli(1793000000123),
		}
}

func TestSigningMessageVector(t *testing.T) {
	team, dev, op := vectorOp()
	got := hex.EncodeToString(SigningMessage(team, dev, op))
	if got != vectorOpMessage {
		t.Fatalf("signing message changed:\n%s", got)
	}
}

func TestRegistrationMessageVector(t *testing.T) {
	team, _, _ := vectorOp()
	user := uuid.MustParse("0190a1b2-0000-7000-8000-000000000005")
	key := []byte{0x30, 0x59, 0x01, 0x02, 0x03}
	got := hex.EncodeToString(RegistrationMessage(team, user, key))
	if got != vectorRegMessage {
		t.Fatalf("registration message changed:\n%s", got)
	}
}
