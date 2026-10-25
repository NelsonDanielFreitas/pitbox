package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPartQuantityLimits(t *testing.T) {
	t.Parallel()
	part, err := NewPart(PartGlowPlug, nil, nil, "Vela", "un", nil, MaxStockCenti, nil, "EUR", false)
	if err != nil || part.MinStockCenti != MaxStockCenti {
		t.Fatalf("max min_stock: %+v, %v", part, err)
	}
	if _, err := NewPart(PartGlowPlug, nil, nil, "Vela", "un", nil, MaxStockCenti+1, nil, "EUR", false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("min_stock above max = %v", err)
	}
	if _, err := NewStockMovement(uuid.New(), MaxStockCenti, StockPurchase, nil, partTime()); err != nil {
		t.Fatalf("max purchase: %v", err)
	}
	if _, err := NewStockMovement(uuid.New(), MaxStockCenti+1, StockPurchase, nil, partTime()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("purchase above max = %v", err)
	}
	if _, err := NewStockMovement(uuid.New(), -MaxStockCenti, StockConsumed, nil, partTime()); err != nil {
		t.Fatalf("max consumption: %v", err)
	}
	if _, err := NewStockMovement(uuid.New(), -MaxStockCenti-1, StockConsumed, nil, partTime()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("consumption below min = %v", err)
	}
}

func TestCarUpdateTrimsCatalogueCodes(t *testing.T) {
	t.Parallel()
	car := Car{Scale: "1/8", Powertrain: "nitro"}
	scale, powertrain := " 1/10 ", " electric_test "
	updated, err := ApplyCarUpdate(car, CarUpdate{Scale: &scale, Powertrain: &powertrain})
	if err != nil {
		t.Fatalf("ApplyCarUpdate: %v", err)
	}
	if updated.Scale != "1/10" || updated.Powertrain != "electric_test" {
		t.Fatalf("not trimmed: %+v", updated)
	}
	bad := "1/8"
	if _, err := ApplyCarUpdate(car, CarUpdate{Powertrain: &bad}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid powertrain = %v", err)
	}
}

func partTime() (t time.Time) { return time.Unix(1, 0).UTC() }
