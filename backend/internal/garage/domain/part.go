package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	PartGlowPlug     = "glow_plug"
	PartClutchShoe   = "clutch_shoe"
	PartClutchSpring = "clutch_spring"
	PartClutchBell   = "clutch_bell"
	PartBearing      = "bearing"
	PartConrod       = "conrod"
	PartPistonSleeve = "piston_sleeve"
	PartAirFilter    = "air_filter"
	PartShockOil     = "shock_oil"
	PartDiffOil      = "diff_oil"
	PartFuel         = "fuel"
	PartSpring       = "spring"
	PartTyre         = "tyre"
	PartInsert       = "insert"
	PartBody         = "body"
	PartWing         = "wing"
	PartOther        = "other"

	StockPurchase   = "purchase"
	StockConsumed   = "consumed"
	StockInstalled  = "installed"
	StockAdjustment = "adjustment"
	StockLost       = "lost"
	StockReturned   = "returned"
)

var partKinds = map[string]struct{}{
	PartGlowPlug: {}, PartClutchShoe: {}, PartClutchSpring: {}, PartClutchBell: {},
	PartBearing: {}, PartConrod: {}, PartPistonSleeve: {}, PartAirFilter: {},
	PartShockOil: {}, PartDiffOil: {}, PartFuel: {}, PartSpring: {}, PartTyre: {},
	PartInsert: {}, PartBody: {}, PartWing: {}, PartOther: {},
}

var stockReasons = map[string]struct{}{
	StockPurchase: {}, StockConsumed: {}, StockInstalled: {}, StockAdjustment: {},
	StockLost: {}, StockReturned: {},
}

type Part struct {
	ID             uuid.UUID
	Kind           string
	Brand          *string
	Reference      *string
	Description    string
	Unit           string
	Specs          map[string]any
	MinStockCenti  int64
	UnitCostCents  *int32
	Currency       string
	HasServiceLife bool
	QtyCenti       int64
	BelowMin       bool
	CreatedAt      time.Time
	Version        int32
}

func NewPart(kind string, brand, reference *string, description, unit string, specs map[string]any, minStockCenti int64, unitCostCents *int32, currency string, hasServiceLife bool) (Part, error) {
	if _, ok := partKinds[kind]; !ok {
		return Part{}, fmt.Errorf("%w: part kind", ErrInvalid)
	}
	description, err := requiredString(description, maxDescription)
	if err != nil {
		return Part{}, err
	}
	brand, err = optionalString(brand, maxBrandRef)
	if err != nil {
		return Part{}, err
	}
	reference, err = optionalString(reference, maxBrandRef)
	if err != nil {
		return Part{}, err
	}
	unit = trimOr(unit, "un")
	if _, err := requiredString(unit, maxUnit); err != nil {
		return Part{}, err
	}
	specs, err = validateSpecs(specs)
	if err != nil {
		return Part{}, err
	}
	if minStockCenti < 0 {
		return Part{}, fmt.Errorf("%w: min stock", ErrInvalid)
	}
	if unitCostCents != nil && *unitCostCents < 0 {
		return Part{}, fmt.Errorf("%w: unit cost", ErrInvalid)
	}
	currency = trimOr(currency, "EUR")
	if !validCurrency(currency) {
		return Part{}, fmt.Errorf("%w: currency", ErrInvalid)
	}
	return Part{
		Kind: kind, Brand: brand, Reference: reference, Description: description, Unit: unit,
		Specs: specs, MinStockCenti: minStockCenti, UnitCostCents: unitCostCents,
		Currency: currency, HasServiceLife: hasServiceLife,
	}, nil
}

type PartUpdate struct {
	Kind          *string
	BrandSet      bool
	Brand         *string
	ReferenceSet  bool
	Reference     *string
	Description   *string
	Unit          *string
	SpecsSet      bool
	Specs         map[string]any
	MinSet        bool
	MinStockCenti int64
	CostSet       bool
	UnitCostCents *int32
	Currency      *string
	ServiceLife   *bool
}

func (u PartUpdate) Empty() bool {
	return u.Kind == nil && !u.BrandSet && !u.ReferenceSet && u.Description == nil && u.Unit == nil &&
		!u.SpecsSet && !u.MinSet && !u.CostSet && u.Currency == nil && u.ServiceLife == nil
}

func ApplyPartUpdate(p Part, u PartUpdate) (Part, error) {
	if u.Empty() {
		return Part{}, fmt.Errorf("%w: empty update mask", ErrInvalid)
	}
	var err error
	if u.Kind != nil {
		if _, ok := partKinds[*u.Kind]; !ok {
			return Part{}, fmt.Errorf("%w: part kind", ErrInvalid)
		}
		p.Kind = *u.Kind
	}
	if u.BrandSet {
		p.Brand, err = optionalString(u.Brand, maxBrandRef)
		if err != nil {
			return Part{}, err
		}
	}
	if u.ReferenceSet {
		p.Reference, err = optionalString(u.Reference, maxBrandRef)
		if err != nil {
			return Part{}, err
		}
	}
	if u.Description != nil {
		p.Description, err = requiredString(*u.Description, maxDescription)
		if err != nil {
			return Part{}, err
		}
	}
	if u.Unit != nil {
		p.Unit, err = requiredString(*u.Unit, maxUnit)
		if err != nil {
			return Part{}, err
		}
	}
	if u.SpecsSet {
		p.Specs, err = validateSpecs(u.Specs)
		if err != nil {
			return Part{}, err
		}
	}
	if u.MinSet {
		if u.MinStockCenti < 0 {
			return Part{}, fmt.Errorf("%w: min stock", ErrInvalid)
		}
		p.MinStockCenti = u.MinStockCenti
	}
	if u.CostSet {
		if u.UnitCostCents != nil && *u.UnitCostCents < 0 {
			return Part{}, fmt.Errorf("%w: unit cost", ErrInvalid)
		}
		p.UnitCostCents = u.UnitCostCents
	}
	if u.Currency != nil {
		if !validCurrency(*u.Currency) {
			return Part{}, fmt.Errorf("%w: currency", ErrInvalid)
		}
		p.Currency = *u.Currency
	}
	if u.ServiceLife != nil {
		p.HasServiceLife = *u.ServiceLife
	}
	return p, nil
}

func validCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, c := range s {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// StockMovement is an append-only stock delta. Its sign must match the reason.
type StockMovement struct {
	ID            uuid.UUID
	PartID        uuid.UUID
	QtyDeltaCenti int64
	Reason        string
	Note          *string
	OccurredAt    time.Time
}

func NewStockMovement(partID uuid.UUID, qtyDeltaCenti int64, reason string, note *string, occurredAt time.Time) (StockMovement, error) {
	if _, ok := stockReasons[reason]; !ok {
		return StockMovement{}, fmt.Errorf("%w: stock reason", ErrInvalid)
	}
	if qtyDeltaCenti == 0 {
		return StockMovement{}, fmt.Errorf("%w: stock quantity", ErrInvalid)
	}
	switch reason {
	case StockPurchase, StockReturned:
		if qtyDeltaCenti <= 0 {
			return StockMovement{}, fmt.Errorf("%w: stock sign", ErrInvalid)
		}
	case StockConsumed, StockInstalled, StockLost:
		if qtyDeltaCenti >= 0 {
			return StockMovement{}, fmt.Errorf("%w: stock sign", ErrInvalid)
		}
	}
	note, err := optionalString(note, maxNote)
	if err != nil {
		return StockMovement{}, err
	}
	if occurredAt.IsZero() {
		return StockMovement{}, fmt.Errorf("%w: stock time", ErrInvalid)
	}
	return StockMovement{PartID: partID, QtyDeltaCenti: qtyDeltaCenti, Reason: reason, Note: note, OccurredAt: occurredAt.UTC()}, nil
}

func ValidPartKind(kind string) bool      { _, ok := partKinds[kind]; return ok }
func ValidStockReason(reason string) bool { _, ok := stockReasons[reason]; return ok }
func NormalizePartText(s string) string   { return strings.TrimSpace(s) }
