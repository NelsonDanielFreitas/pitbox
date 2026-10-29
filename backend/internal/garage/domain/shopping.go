package domain

import (
	"fmt"

	"github.com/google/uuid"
)

// ShoppingItem is a stored exception to the computed shopping list: a manual line, an
// adjusted quantity or an automatic line the mechanic hid. There is one per part.
type ShoppingItem struct {
	ID        uuid.UUID
	PartID    uuid.UUID
	QtyCenti  *int64 // nil: use the suggestion
	Dismissed bool
	Note      *string
	Version   int32
}

// ShoppingLine is one line of the computed list.
type ShoppingLine struct {
	PartID            uuid.UUID
	QtyCenti          int64
	SuggestedQtyCenti int64
	BelowMin          bool
	Note              *string
	ItemVersion       int32 // 0: nothing stored for this line
	UnitCostCents     *int32
}

// EstimatedCostCents is the quantity times the part's unit cost, or nil without a cost.
func (l ShoppingLine) EstimatedCostCents() *int64 {
	if l.UnitCostCents == nil {
		return nil
	}
	v := l.QtyCenti * int64(*l.UnitCostCents) / UnitCenti
	return &v
}

// SuggestedQty is how much to buy to bring the stock up to reorderTo, or to the minimum
// when there is no restock target. It is never negative.
func SuggestedQty(qtyCenti, minStockCenti int64, reorderToCenti *int64) int64 {
	target := minStockCenti
	if reorderToCenti != nil {
		target = *reorderToCenti
	}
	if d := target - qtyCenti; d > 0 {
		return d
	}
	return 0
}

// NewShoppingItemQty validates the quantity and note of a line being set.
func NewShoppingItemQty(qtyCenti *int64, note *string) (*int64, *string, error) {
	if qtyCenti != nil && (*qtyCenti <= 0 || *qtyCenti > MaxStockCenti) {
		return nil, nil, fmt.Errorf("%w: shopping quantity", ErrInvalid)
	}
	note, err := optionalString(note, maxNote)
	if err != nil {
		return nil, nil, err
	}
	return qtyCenti, note, nil
}
