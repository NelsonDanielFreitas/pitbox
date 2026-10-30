package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func i32(v int32) *int32 { return &v }
func i64(v int64) *int64 { return &v }

func TestPartSpecsByKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		kind  string
		specs map[string]any
		ok    bool
	}{
		{"oil viscosity", PartShockOil, map[string]any{"viscosity_cst": 7000.0}, true},
		{"oil viscosity zero", PartShockOil, map[string]any{"viscosity_cst": 0.0}, false},
		{"oil viscosity too high", PartDiffOil, map[string]any{"viscosity_cst": 100_001.0}, false},
		{"oil viscosity text", PartShockOil, map[string]any{"viscosity_cst": "7000"}, false},
		{"fuel", PartFuel, map[string]any{"nitro_pct": 25.0, "oil_pct": 9.0}, true},
		{"fuel nitro too high", PartFuel, map[string]any{"nitro_pct": 41.0}, false},
		{"fuel oil negative", PartFuel, map[string]any{"oil_pct": -1.0}, false},
		{"plug", PartGlowPlug, map[string]any{"heat_range": "medium"}, true},
		{"plug empty text", PartGlowPlug, map[string]any{"heat_range": ""}, false},
		{"plug long text", PartGlowPlug, map[string]any{"heat_range": string(make([]byte, 41))}, false},
		{"spring", PartSpring, map[string]any{"rate": "3.2"}, true},
		{"tyre compound", PartTyre, map[string]any{"compound": "soft"}, true},
		{"tyre compound empty", PartTyre, map[string]any{"compound": ""}, false},
		{"tyre insert key", PartTyre, map[string]any{"insert_type": "foam"}, false},
		{"insert type", PartInsert, map[string]any{"insert_type": "foam"}, true},
		{"insert compound key", PartInsert, map[string]any{"compound": "soft"}, false},
		{"insert type long", PartInsert, map[string]any{"insert_type": string(make([]byte, 41))}, false},
		{"unknown key", PartFuel, map[string]any{"colour": "red"}, false},
		{"key of another kind", PartGlowPlug, map[string]any{"nitro_pct": 20.0}, false},
		{"kind without specs", PartBearing, map[string]any{"size": "5x10"}, false},
		{"kind without specs, empty", PartBearing, map[string]any{}, true},
		{"nil specs", PartOther, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewPart(tc.kind, nil, nil, "Peça", "un", tc.specs, 0, nil, "EUR", false, nil)
			if tc.ok && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted or wrong error: %v", err)
			}
		})
	}
}

func TestPartUpdateRevalidatesSpecsWhenKindChanges(t *testing.T) {
	t.Parallel()
	oil := Part{Kind: PartShockOil, Unit: "ml", Specs: map[string]any{"viscosity_cst": 7000.0}}
	other := PartBearing
	if _, err := ApplyPartUpdate(oil, PartUpdate{Kind: &other}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("kind change with incompatible specs = %v", err)
	}
	fuel := PartFuel
	if _, err := ApplyPartUpdate(oil, PartUpdate{Kind: &fuel}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("viscosity on fuel = %v", err)
	}
	bad := "kg"
	if _, err := ApplyPartUpdate(oil, PartUpdate{Unit: &bad}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unit kg = %v", err)
	}
	// An update that touches neither kind nor specs does not revalidate stored specs.
	legacy := Part{Kind: PartBearing, Unit: "un", Specs: map[string]any{"size": "5x10"}}
	desc := "Rolamento"
	if _, err := ApplyPartUpdate(legacy, PartUpdate{Description: &desc}); err != nil {
		t.Fatalf("unrelated update: %v", err)
	}
}

func TestPartUnitAndReorderTo(t *testing.T) {
	t.Parallel()
	for _, unit := range []string{"un", "ml", "l", "g", ""} {
		if _, err := NewPart(PartOther, nil, nil, "Peça", unit, nil, 0, nil, "EUR", false, nil); err != nil {
			t.Errorf("unit %q refused: %v", unit, err)
		}
	}
	for _, unit := range []string{"L", "kg", "litro", "XL"} {
		if _, err := NewPart(PartOther, nil, nil, "Peça", unit, nil, 0, nil, "EUR", false, nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("unit %q accepted: %v", unit, err)
		}
	}
	if _, err := NewPart(PartOther, nil, nil, "Peça", "un", nil, 500, nil, "EUR", false, i64(499)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reorder_to below minimum = %v", err)
	}
	if p, err := NewPart(PartOther, nil, nil, "Peça", "un", nil, 500, nil, "EUR", false, i64(500)); err != nil || *p.ReorderToCenti != 500 {
		t.Fatalf("reorder_to equal to minimum: %+v, %v", p, err)
	}
	if _, err := NewPart(PartOther, nil, nil, "Peça", "un", nil, 0, nil, "EUR", false, i64(MaxStockCenti+1)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reorder_to above max = %v", err)
	}

	p := Part{Kind: PartOther, Unit: "un", MinStockCenti: 100, ReorderToCenti: i64(300)}
	if _, err := ApplyPartUpdate(p, PartUpdate{MinSet: true, MinStockCenti: 400}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("raising the minimum above reorder_to = %v", err)
	}
	cleared, err := ApplyPartUpdate(p, PartUpdate{ReorderSet: true})
	if err != nil || cleared.ReorderToCenti != nil {
		t.Fatalf("clearing reorder_to: %+v, %v", cleared, err)
	}
}

func TestStockMovementPriceAndLossReason(t *testing.T) {
	t.Parallel()
	part := uuid.New()
	if m, err := NewStockMovement(part, 400, StockPurchase, nil, partTime(), MovementDetails{UnitCostCents: i32(950)}); err != nil || *m.UnitCostCents != 950 {
		t.Fatalf("purchase with price: %+v, %v", m, err)
	}
	if _, err := NewStockMovement(part, 400, StockPurchase, nil, partTime(), MovementDetails{UnitCostCents: i32(-1)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative price = %v", err)
	}
	if _, err := NewStockMovement(part, -100, StockConsumed, nil, partTime(), MovementDetails{UnitCostCents: i32(950)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("price on a consumption = %v", err)
	}
	if m, err := NewStockMovement(part, -100, StockLost, nil, partTime(), MovementDetails{LossReason: LossExpired}); err != nil || m.LossReason != LossExpired {
		t.Fatalf("lost with reason: %+v, %v", m, err)
	}
	if _, err := NewStockMovement(part, -100, StockLost, nil, partTime(), MovementDetails{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("lost without reason = %v", err)
	}
	if _, err := NewStockMovement(part, -100, StockLost, nil, partTime(), MovementDetails{LossReason: "stolen"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown loss reason = %v", err)
	}
	if _, err := NewStockMovement(part, -100, StockConsumed, nil, partTime(), MovementDetails{LossReason: LossWorn}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("loss reason on a consumption = %v", err)
	}
}

func TestSuggestedQty(t *testing.T) {
	t.Parallel()
	if got := SuggestedQty(300, 1000, nil); got != 700 {
		t.Fatalf("to the minimum = %d", got)
	}
	if got := SuggestedQty(300, 1000, i64(2500)); got != 2200 {
		t.Fatalf("to reorder_to = %d", got)
	}
	if got := SuggestedQty(3000, 1000, nil); got != 0 {
		t.Fatalf("above the target = %d", got)
	}
	if got := SuggestedQty(-200, 0, nil); got != 200 {
		t.Fatalf("negative stock = %d", got)
	}
	cost := int32(1250)
	line := ShoppingLine{QtyCenti: 400, UnitCostCents: &cost}
	if got := line.EstimatedCostCents(); got == nil || *got != 5000 {
		t.Fatalf("estimated cost = %v", got)
	}
	if (ShoppingLine{QtyCenti: 400}).EstimatedCostCents() != nil {
		t.Fatal("cost without a unit cost")
	}
}
