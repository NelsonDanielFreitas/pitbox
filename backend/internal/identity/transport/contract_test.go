package transport

import (
	"errors"
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"

	identityv1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/identity/v1"
)

// validRequestID is a UUIDv7. protovalidate only checks that it is a UUID;
// the version is checked in the domain.
const validRequestID = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60"

func validCreateTeam() *identityv1.CreateTeamRequest {
	return &identityv1.CreateTeamRequest{
		RequestId:   validRequestID,
		Name:        "Equipa Nitro",
		Category:    "1/8 TT nitro",
		CountryCode: "PT",
	}
}

// TestCreateTeamContract checks the protovalidate rules of CreateTeamRequest
// against the limits in identity.team (name 1..80 characters, char(2) country).
//
// Go note: a table-driven test keeps one slice of cases and runs each one as a
// subtest with t.Run, so a failure names the exact case and the others still run.
func TestCreateTeamContract(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatalf("protovalidate.New: %v", err)
	}

	tests := []struct {
		name      string
		mutate    func(r *identityv1.CreateTeamRequest)
		wantField string // empty = must be valid
	}{
		{name: "valid"},
		{name: "club absent is valid", mutate: func(r *identityv1.CreateTeamRequest) { r.Club = nil }},
		{name: "club present with value", mutate: func(r *identityv1.CreateTeamRequest) { r.Club = proto.String("Clube de Braga") }},
		{name: "empty category means server default", mutate: func(r *identityv1.CreateTeamRequest) { r.Category = "" }},

		{name: "request_id empty", mutate: func(r *identityv1.CreateTeamRequest) { r.RequestId = "" }, wantField: "request_id"},
		{name: "request_id not a uuid", mutate: func(r *identityv1.CreateTeamRequest) { r.RequestId = "not-a-uuid" }, wantField: "request_id"},

		{name: "name empty", mutate: func(r *identityv1.CreateTeamRequest) { r.Name = "" }, wantField: "name"},
		// Whitespace-only passes the contract on purpose: trimming is the domain's job.
		{name: "name only spaces passes the contract", mutate: func(r *identityv1.CreateTeamRequest) { r.Name = "   " }},
		// Limits count characters, not bytes: 80 accented letters are 160 bytes.
		{name: "name 80 accented characters", mutate: func(r *identityv1.CreateTeamRequest) { r.Name = strings.Repeat("á", 80) }},
		{name: "name 81 characters", mutate: func(r *identityv1.CreateTeamRequest) { r.Name = strings.Repeat("a", 81) }, wantField: "name"},

		{name: "category 40 characters", mutate: func(r *identityv1.CreateTeamRequest) { r.Category = strings.Repeat("c", 40) }},
		{name: "category 41 characters", mutate: func(r *identityv1.CreateTeamRequest) { r.Category = strings.Repeat("c", 41) }, wantField: "category"},

		{name: "club present but empty", mutate: func(r *identityv1.CreateTeamRequest) { r.Club = proto.String("") }, wantField: "club"},
		{name: "club 81 characters", mutate: func(r *identityv1.CreateTeamRequest) { r.Club = proto.String(strings.Repeat("c", 81)) }, wantField: "club"},

		{name: "country lower case", mutate: func(r *identityv1.CreateTeamRequest) { r.CountryCode = "pt" }, wantField: "country_code"},
		{name: "country three letters", mutate: func(r *identityv1.CreateTeamRequest) { r.CountryCode = "PRT" }, wantField: "country_code"},
		{name: "country empty", mutate: func(r *identityv1.CreateTeamRequest) { r.CountryCode = "" }, wantField: "country_code"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := validCreateTeam()
			if tc.mutate != nil {
				tc.mutate(req)
			}
			assertViolation(t, v.Validate(req), tc.wantField)
		})
	}
}

func TestListMyTeamsContract(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatalf("protovalidate.New: %v", err)
	}

	tests := []struct {
		name      string
		req       *identityv1.ListMyTeamsRequest
		wantField string
	}{
		{name: "defaults", req: &identityv1.ListMyTeamsRequest{}},
		{name: "page_size 200", req: &identityv1.ListMyTeamsRequest{PageSize: 200}},
		{name: "page_size 201", req: &identityv1.ListMyTeamsRequest{PageSize: 201}, wantField: "page_size"},
		{name: "page_size negative", req: &identityv1.ListMyTeamsRequest{PageSize: -1}, wantField: "page_size"},
		{name: "page_token 512", req: &identityv1.ListMyTeamsRequest{PageToken: strings.Repeat("t", 512)}},
		{name: "page_token 513", req: &identityv1.ListMyTeamsRequest{PageToken: strings.Repeat("t", 513)}, wantField: "page_token"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertViolation(t, v.Validate(tc.req), tc.wantField)
		})
	}
}

// assertViolation checks that err is nil when wantField is empty, and otherwise
// that it is a validation error that blames exactly wantField (so a case cannot
// pass for the wrong reason).
func assertViolation(t *testing.T, err error, wantField string) {
	t.Helper()

	if wantField == "" {
		if err != nil {
			t.Fatalf("expected valid message, got: %v", err)
		}
		return
	}

	var verr *protovalidate.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected a validation error on %q, got: %v", wantField, err)
	}
	for _, violation := range verr.Violations {
		if string(violation.FieldDescriptor.Name()) == wantField {
			return
		}
	}
	t.Fatalf("expected a violation on %q, got: %v", wantField, err)
}
