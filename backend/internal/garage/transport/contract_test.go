package transport

import (
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

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

func TestPartQuantityContract(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	create := func() *garagev1.CreatePartRequest {
		return &garagev1.CreatePartRequest{
			TeamId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61", RequestId: validRequestID,
			Kind: garagev1.PartKind_PART_KIND_GLOW_PLUG, Description: "Vela", Unit: "un",
		}
	}
	for _, req := range []*garagev1.CreatePartRequest{
		func() *garagev1.CreatePartRequest { r := create(); r.MinStockCenti = 10_000_000_000; return r }(),
		func() *garagev1.CreatePartRequest { r := create(); r.InitialQtyCenti = 10_000_000_000; return r }(),
	} {
		if v.Validate(req) == nil {
			t.Fatalf("oversized quantity accepted: %+v", req)
		}
	}
	movement := func() *garagev1.AddStockMovementRequest {
		return &garagev1.AddStockMovementRequest{
			TeamId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61", RequestId: validRequestID,
			PartId: "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62", Reason: garagev1.StockReason_STOCK_REASON_PURCHASE,
		}
	}
	ok := movement()
	ok.QtyDeltaCenti = 9_999_999_999
	if err := v.Validate(ok); err != nil {
		t.Fatalf("max delta refused: %v", err)
	}
	tooBig := movement()
	tooBig.QtyDeltaCenti = 10_000_000_000
	if v.Validate(tooBig) == nil {
		t.Fatal("oversized delta accepted")
	}
	tooSmall := movement()
	tooSmall.QtyDeltaCenti = -10_000_000_000
	if v.Validate(tooSmall) == nil {
		t.Fatal("oversized negative delta accepted")
	}
}

func TestPartInstallContract(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	const team, part, car, engine = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61", "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62",
		"0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d63", "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d64"
	install := func(mutate func(*garagev1.InstallPartRequest)) *garagev1.InstallPartRequest {
		r := &garagev1.InstallPartRequest{
			TeamId: team, RequestId: validRequestID, PartId: part,
			Target: &garagev1.InstallPartRequest_EngineId{EngineId: engine},
		}
		if mutate != nil {
			mutate(r)
		}
		return r
	}
	remove := func(mutate func(*garagev1.RemovePartRequest)) *garagev1.RemovePartRequest {
		r := &garagev1.RemovePartRequest{
			TeamId: team, RequestId: validRequestID, InstallId: part,
			Destination: garagev1.RemovalDestination_REMOVAL_DESTINATION_STOCK,
		}
		if mutate != nil {
			mutate(r)
		}
		return r
	}
	list := func(mutate func(*garagev1.ListPartInstallsRequest)) *garagev1.ListPartInstallsRequest {
		r := &garagev1.ListPartInstallsRequest{TeamId: team, Filter: &garagev1.ListPartInstallsRequest_CarId{CarId: car}}
		if mutate != nil {
			mutate(r)
		}
		return r
	}
	past := timestamppb.New(time.Now().Add(-time.Hour))
	future := timestamppb.New(time.Now().Add(time.Hour))

	valid := []proto.Message{
		install(nil),
		install(func(r *garagev1.InstallPartRequest) { r.Target = &garagev1.InstallPartRequest_CarId{CarId: car} }),
		install(func(r *garagev1.InstallPartRequest) { r.InstalledAt = past }),
		remove(nil),
		remove(func(r *garagev1.RemovePartRequest) {
			r.Destination = garagev1.RemovalDestination_REMOVAL_DESTINATION_SCRAP
			r.Reason = garagev1.RemovalReason_REMOVAL_REASON_WORN
		}),
		list(nil),
		list(func(r *garagev1.ListPartInstallsRequest) {
			r.Filter = &garagev1.ListPartInstallsRequest_PartId{PartId: part}
		}),
	}
	for i, msg := range valid {
		if err := v.Validate(msg); err != nil {
			t.Errorf("valid message %d refused: %v", i, err)
		}
	}
	invalid := map[string]proto.Message{
		"install without target":     install(func(r *garagev1.InstallPartRequest) { r.Target = nil }),
		"install bad target id":      install(func(r *garagev1.InstallPartRequest) { r.Target = &garagev1.InstallPartRequest_CarId{CarId: "x"} }),
		"install time in the future": install(func(r *garagev1.InstallPartRequest) { r.InstalledAt = future }),
		"install note too long":      install(func(r *garagev1.InstallPartRequest) { r.Note = proto.String(strings.Repeat("a", 501)) }),
		"install request missing":    install(func(r *garagev1.InstallPartRequest) { r.RequestId = "" }),
		"remove destination missing": remove(func(r *garagev1.RemovePartRequest) { r.Destination = 0 }),
		"remove destination unknown": remove(func(r *garagev1.RemovePartRequest) { r.Destination = 99 }),
		"scrap without reason": remove(func(r *garagev1.RemovePartRequest) {
			r.Destination = garagev1.RemovalDestination_REMOVAL_DESTINATION_SCRAP
		}),
		"reason without scrap": remove(func(r *garagev1.RemovePartRequest) { r.Reason = garagev1.RemovalReason_REMOVAL_REASON_LOST }),
		"scrap reason unknown": remove(func(r *garagev1.RemovePartRequest) {
			r.Destination = garagev1.RemovalDestination_REMOVAL_DESTINATION_SCRAP
			r.Reason = 99
		}),
		"remove note too long": remove(func(r *garagev1.RemovePartRequest) { r.Note = proto.String(strings.Repeat("a", 501)) }),
		"list without filter":  list(func(r *garagev1.ListPartInstallsRequest) { r.Filter = nil }),
		"list page too large":  list(func(r *garagev1.ListPartInstallsRequest) { r.PageSize = 201 }),
		"list token too long":  list(func(r *garagev1.ListPartInstallsRequest) { r.PageToken = strings.Repeat("a", 513) }),
	}
	for name, msg := range invalid {
		if v.Validate(msg) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPriceLossReasonAndUnitContract(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}
	const team, part = "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d61", "0190f2a4-7c3e-7b1a-8d2f-3c5e9a1b4d62"
	movement := func(mutate func(*garagev1.AddStockMovementRequest)) *garagev1.AddStockMovementRequest {
		r := &garagev1.AddStockMovementRequest{
			TeamId: team, RequestId: validRequestID, PartId: part, QtyDeltaCenti: 100,
			Reason: garagev1.StockReason_STOCK_REASON_PURCHASE,
		}
		mutate(r)
		return r
	}
	lost := func(r *garagev1.AddStockMovementRequest) {
		r.Reason, r.QtyDeltaCenti = garagev1.StockReason_STOCK_REASON_LOST, -100
	}
	createPart := func(mutate func(*garagev1.CreatePartRequest)) *garagev1.CreatePartRequest {
		r := &garagev1.CreatePartRequest{
			TeamId: team, RequestId: validRequestID, Kind: garagev1.PartKind_PART_KIND_FUEL, Description: "Combustível", Unit: "l",
		}
		mutate(r)
		return r
	}
	valid := map[string]proto.Message{
		"purchase with price": movement(func(r *garagev1.AddStockMovementRequest) { r.UnitCostCents = proto.Int32(950) }),
		"purchase free":       movement(func(r *garagev1.AddStockMovementRequest) { r.UnitCostCents = proto.Int32(0) }),
		"lost with reason": movement(func(r *garagev1.AddStockMovementRequest) {
			lost(r)
			r.LossReason = garagev1.LossReason_LOSS_REASON_EXPIRED
		}),
		"unit g":          createPart(func(r *garagev1.CreatePartRequest) { r.Unit = "g" }),
		"unit default":    createPart(func(r *garagev1.CreatePartRequest) { r.Unit = "" }),
		"reorder_to":      createPart(func(r *garagev1.CreatePartRequest) { r.ReorderToCenti = proto.Int64(2500) }),
		"list":            &garagev1.ListShoppingListRequest{TeamId: team, PageSize: 200},
		"set line":        &garagev1.SetShoppingItemRequest{TeamId: team, RequestId: validRequestID, PartId: part, QtyCenti: proto.Int64(400)},
		"set line update": &garagev1.SetShoppingItemRequest{TeamId: team, RequestId: validRequestID, PartId: part, ExpectedVersion: 2},
		"remove line":     &garagev1.RemoveShoppingItemRequest{TeamId: team, RequestId: validRequestID, PartId: part},
	}
	for name, msg := range valid {
		if err := v.Validate(msg); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	invalid := map[string]proto.Message{
		"price on a consumption": movement(func(r *garagev1.AddStockMovementRequest) {
			r.Reason, r.QtyDeltaCenti, r.UnitCostCents = garagev1.StockReason_STOCK_REASON_CONSUMED, -100, proto.Int32(950)
		}),
		"negative price":         movement(func(r *garagev1.AddStockMovementRequest) { r.UnitCostCents = proto.Int32(-1) }),
		"lost without reason":    movement(lost),
		"reason on a purchase":   movement(func(r *garagev1.AddStockMovementRequest) { r.LossReason = garagev1.LossReason_LOSS_REASON_BROKEN }),
		"unknown loss reason":    movement(func(r *garagev1.AddStockMovementRequest) { lost(r); r.LossReason = 99 }),
		"unit kg":                createPart(func(r *garagev1.CreatePartRequest) { r.Unit = "kg" }),
		"unit upper case":        createPart(func(r *garagev1.CreatePartRequest) { r.Unit = "L" }),
		"reorder_to negative":    createPart(func(r *garagev1.CreatePartRequest) { r.ReorderToCenti = proto.Int64(-1) }),
		"reorder_to too large":   createPart(func(r *garagev1.CreatePartRequest) { r.ReorderToCenti = proto.Int64(10_000_000_000) }),
		"list page too large":    &garagev1.ListShoppingListRequest{TeamId: team, PageSize: 201},
		"list without team":      &garagev1.ListShoppingListRequest{},
		"set line zero quantity": &garagev1.SetShoppingItemRequest{TeamId: team, RequestId: validRequestID, PartId: part, QtyCenti: proto.Int64(0)},
		"set line long note":     &garagev1.SetShoppingItemRequest{TeamId: team, RequestId: validRequestID, PartId: part, Note: proto.String(strings.Repeat("a", 501))},
		"set line bad version":   &garagev1.SetShoppingItemRequest{TeamId: team, RequestId: validRequestID, PartId: part, ExpectedVersion: -1},
		"set line no request":    &garagev1.SetShoppingItemRequest{TeamId: team, PartId: part},
		"remove line bad part":   &garagev1.RemoveShoppingItemRequest{TeamId: team, RequestId: validRequestID, PartId: "x"},
	}
	for name, msg := range invalid {
		if v.Validate(msg) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
