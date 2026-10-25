package transport

import (
	"strings"
	"testing"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	garagev1 "github.com/NelsonDanielFreitas/pitbox/backend/gen/pitbox/garage/v1"
)

const validRequestID = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d60"

func TestCreateCarContract(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	base := func() *garagev1.CreateCarRequest {
		return &garagev1.CreateCarRequest{
			TeamId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61", RequestId: validRequestID,
			Make: "Mugen", Model: "MBX8R", Status: garagev1.AssetStatus_ASSET_STATUS_ACTIVE,
			Scale: "1/8", Powertrain: "nitro",
		}
	}
	tests := []struct {
		name, field string
		mutate      func(*garagev1.CreateCarRequest)
	}{
		{name: "valid"},
		{name: "team missing", field: "team_id", mutate: func(r *garagev1.CreateCarRequest) { r.TeamId = "" }},
		{name: "request missing", field: "request_id", mutate: func(r *garagev1.CreateCarRequest) { r.RequestId = "" }},
		{name: "make empty", field: "make", mutate: func(r *garagev1.CreateCarRequest) { r.Make = "" }},
		{name: "model too long", field: "model", mutate: func(r *garagev1.CreateCarRequest) { r.Model = strings.Repeat("a", 81) }},
		{name: "year too old", field: "model_year", mutate: func(r *garagev1.CreateCarRequest) { r.ModelYear = proto.Int32(1989) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := base()
			if tc.mutate != nil {
				tc.mutate(req)
			}
			err := v.Validate(req)
			if tc.field == "" {
				if err != nil {
					t.Fatalf("valid request refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want violation on %s", tc.field)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("violation %v does not mention %s", err, tc.field)
			}
		})
	}
}

func TestListCarsAndUpdateCarContract(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []*garagev1.ListCarsRequest{
		{TeamId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61", PageSize: 201},
		{TeamId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61", PageToken: strings.Repeat("x", 513)},
	} {
		if v.Validate(req) == nil {
			t.Fatalf("invalid list request accepted: %+v", req)
		}
	}
	update := &garagev1.UpdateCarRequest{
		TeamId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61", RequestId: validRequestID,
		Version: 1, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"notes"}},
		CarId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62",
	}
	if err := v.Validate(update); err != nil {
		t.Fatalf("valid update refused: %v", err)
	}
	update.CarId = ""
	if v.Validate(update) == nil {
		t.Fatal("update without car_id accepted")
	}
}
