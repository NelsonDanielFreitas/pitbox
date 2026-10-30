package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func gluePart(kind string, qty int64) Part {
	brand := "Sworkz"
	return Part{ID: uuid.New(), Kind: kind, Brand: &brand, Description: "Pneus 1/8", QtyCenti: qty}
}

func glueInput(tyre, insert Part) GlueInput {
	return GlueInput{TyrePartID: tyre.ID, InsertPartID: insert.ID, Label: "J4"}
}

func TestNewGlueDefaultsAndMovements(t *testing.T) {
	t.Parallel()
	tyre, insert := gluePart(PartTyre, 800), gluePart(PartInsert, 400)
	tyre.Specs = map[string]any{"compound": "soft"}
	insert.Specs = map[string]any{"insert_type": "foam"}
	now := time.Date(2026, 10, 30, 23, 30, 0, 0, time.UTC)

	g, err := NewGlue(glueInput(tyre, insert), tyre, insert, now)
	if err != nil {
		t.Fatal(err)
	}
	if g.Set.Make != "Sworkz" || g.Set.Model != "Pneus 1/8" || *g.Set.Compound != "soft" || *g.Set.InsertType != "foam" {
		t.Fatalf("defaults not copied from the parts: %+v", g.Set)
	}
	if g.Set.GluedOn == nil || g.Set.GluedOn.Format(time.DateOnly) != "2026-10-30" || g.Set.Status != TyreNew {
		t.Fatalf("glued date or status: %+v", g.Set)
	}
	if *g.Set.TyrePartID != tyre.ID || *g.Set.InsertPartID != insert.ID {
		t.Fatal("set is not linked to its parts")
	}
	if g.TyreMovement.QtyDeltaCenti != -400 || g.InsertMovement.QtyDeltaCenti != -400 ||
		g.TyreMovement.Reason != StockConsumed || g.InsertMovement.Reason != StockConsumed ||
		g.TyreMovement.PartID != tyre.ID || g.InsertMovement.PartID != insert.ID {
		t.Fatalf("movements: %+v %+v", g.TyreMovement, g.InsertMovement)
	}
}

func TestNewGlueRequestOverridesPartDefaults(t *testing.T) {
	t.Parallel()
	tyre, insert := gluePart(PartTyre, 800), gluePart(PartInsert, 800)
	tyre.Specs = map[string]any{"compound": "soft"}
	in := glueInput(tyre, insert)
	make, model, compound, qty := "Matrix", "Z", "medium", int32(8)
	in.Make, in.Model, in.Compound, in.TyreQty = &make, &model, &compound, qty
	g, err := NewGlue(in, tyre, insert, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if g.Set.Make != "Matrix" || g.Set.Model != "Z" || *g.Set.Compound != "medium" || g.TyreMovement.QtyDeltaCenti != -800 {
		t.Fatalf("overrides ignored: %+v %+v", g.Set, g.TyreMovement)
	}
}

func TestNewGlueRefusals(t *testing.T) {
	t.Parallel()
	now := time.Now()
	tests := []struct {
		name   string
		mutate func(in *GlueInput, tyre, insert *Part)
		want   error
	}{
		{"short tyre stock", func(_ *GlueInput, tyre, _ *Part) { tyre.QtyCenti = 300 }, ErrInsufficientStock},
		{"short insert stock", func(_ *GlueInput, _, insert *Part) { insert.QtyCenti = 399 }, ErrInsufficientStock},
		{"negative stock", func(_ *GlueInput, tyre, _ *Part) { tyre.QtyCenti = -100 }, ErrInsufficientStock},
		{"tyre part has the wrong kind", func(_ *GlueInput, tyre, _ *Part) { tyre.Kind = PartBearing }, ErrInvalid},
		{"insert part has the wrong kind", func(_ *GlueInput, _, insert *Part) { insert.Kind = PartTyre }, ErrInvalid},
		{"swapped parts", func(in *GlueInput, _, _ *Part) { in.TyrePartID, in.InsertPartID = in.InsertPartID, in.TyrePartID }, ErrInvalid},
		{"same part twice", func(in *GlueInput, _, _ *Part) { in.InsertPartID = in.TyrePartID }, ErrInvalid},
		{"quantity too high", func(in *GlueInput, _, _ *Part) { in.TyreQty = MaxGlueQty + 1 }, ErrInvalid},
		{"negative quantity", func(in *GlueInput, _, _ *Part) { in.InsertQty = -1 }, ErrInvalid},
		{"no make anywhere", func(_ *GlueInput, tyre, _ *Part) { tyre.Brand = nil }, ErrInvalid},
		{"empty label", func(in *GlueInput, _, _ *Part) { in.Label = " " }, ErrInvalid},
		{"bad date", func(in *GlueInput, _, _ *Part) { bad := "2026-13-45"; in.GluedOn = &bad }, ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tyre, insert := gluePart(PartTyre, 800), gluePart(PartInsert, 800)
			in := glueInput(tyre, insert)
			tc.mutate(&in, &tyre, &insert)
			if _, err := NewGlue(in, tyre, insert, now); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}
