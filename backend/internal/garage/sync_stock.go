package garage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
)

// Appliers for stock movements and the shopping list (F1-13, ADR 0025). Stock is the sum
// of append-only movements, so two devices that each consume or buy never conflict: every
// movement is just added. The effects a movement has on the rest (the "stock low" event,
// the shopping list) run here exactly as in the AddStockMovement use case.

const (
	// occurred_at comes from the device (the clock is the user's, not trusted for ordering):
	// a value from the future or from long ago is replaced by the server time.
	movementMaxFuture = 5 * time.Minute
	movementMaxAge    = 90 * 24 * time.Hour
)

// ---------------------------------------------------------------------- stock movements

type StockMovementApplier struct {
	repo *store.Repo
	now  func() time.Time
}

func NewStockMovementApplier() *StockMovementApplier {
	return &StockMovementApplier{repo: store.NewRepo(), now: time.Now}
}

var _ syncapply.Applier = (*StockMovementApplier)(nil)

func (*StockMovementApplier) Entity() string { return "garage.stock_movement" }

var movementFields = []string{
	"part_id", "qty_delta_centi", "reason", "note", "occurred_at", "unit_cost_cents", "loss_reason",
}

// CurrentVersion: a movement is never edited, so there is no version to compare.
func (*StockMovementApplier) CurrentVersion(context.Context, db.DBTX, db.TeamID, uuid.UUID) (int32, error) {
	return 0, nil
}

func (a *StockMovementApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	if op.Kind != syncapply.Append {
		return 0, fmt.Errorf("stock movements are append only: %w", syncapply.ErrInvalidFields)
	}
	f := syncapply.Fields(op.Fields)
	if err := f.Only(movementFields...); err != nil {
		return 0, err
	}
	partRaw, err := f.RequiredString("part_id")
	if err != nil {
		return 0, err
	}
	partID, err := uuid.Parse(partRaw)
	if err != nil {
		return 0, fmt.Errorf("part_id: %w", syncapply.ErrInvalidFields)
	}
	reason, err := f.RequiredString("reason")
	if err != nil {
		return 0, err
	}
	_, qty, err1 := f.Int64("qty_delta_centi")
	_, note, err2 := f.String("note")
	_, occurred, err3 := f.String("occurred_at")
	_, cost, err4 := f.Int32("unit_cost_cents")
	_, loss, err5 := f.String("loss_reason")
	if err := errors.Join(err1, err2, err3, err4, err5); err != nil || qty == nil {
		return 0, syncapply.ErrInvalidFields
	}
	now := a.now()
	at := now
	if occurred != nil {
		t, err := time.Parse(time.RFC3339Nano, *occurred)
		if err != nil {
			return 0, fmt.Errorf("occurred_at: %w", syncapply.ErrInvalidFields)
		}
		if t.Before(now.Add(movementMaxFuture)) && t.After(now.Add(-movementMaxAge)) {
			at = t
		}
	}
	m, err := domain.NewStockMovement(partID, *qty, reason, note, at, domain.MovementDetails{
		UnitCostCents: cost, LossReason: deref(loss),
	})
	if err != nil {
		return 0, carSyncErr(err)
	}
	team, user := uuid.UUID(op.TeamID), uuid.UUID(op.UserID)
	// The lock serialises movements of one part, so "before" below is exact.
	if err := a.repo.LockPartForStock(ctx, tx, team, partID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return 0, fmt.Errorf("part of the movement: %w", syncapply.ErrInvalidRef)
		}
		return 0, err
	}
	before, err := a.repo.PartStockCenti(ctx, tx, team, partID)
	if err != nil {
		return 0, err
	}
	created, err := a.repo.CreateStockMovementWithID(ctx, tx, team, user, op.EntityID, m)
	if err != nil {
		return 0, carSyncErr(err)
	}
	if err := app.AfterSyncedMovement(ctx, tx, a.repo, team, partID, created.ID, before, reason == domain.StockPurchase); err != nil {
		return 0, err
	}
	return 1, nil
}

type movementWire struct {
	PartID        uuid.UUID `json:"part_id"`
	QtyDeltaCenti int64     `json:"qty_delta_centi"`
	Reason        string    `json:"reason"`
	Note          *string   `json:"note"`
	OccurredAt    time.Time `json:"occurred_at"`
	UnitCostCents *int32    `json:"unit_cost_cents,omitempty"`
	LossReason    *string   `json:"loss_reason"`
}

// Read hides the price paid from pilots and guests (ADR 0023): the field is left out.
func (a *StockMovementApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, role string, id uuid.UUID) (json.RawMessage, bool, error) {
	m, err := a.repo.GetStockMovement(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	w := movementWire{PartID: m.PartID, QtyDeltaCenti: m.QtyDeltaCenti, Reason: m.Reason, Note: m.Note, OccurredAt: m.OccurredAt}
	if m.LossReason != "" {
		w.LossReason = &m.LossReason
	}
	if canSeeCosts(role) {
		w.UnitCostCents = m.UnitCostCents
	}
	raw, err := json.Marshal(w)
	return raw, err == nil, err
}

// ---------------------------------------------------------------------- shopping list

type ShoppingItemApplier struct{ repo *store.Repo }

func NewShoppingItemApplier() *ShoppingItemApplier {
	return &ShoppingItemApplier{repo: store.NewRepo()}
}

var _ syncapply.Applier = (*ShoppingItemApplier)(nil)

func (*ShoppingItemApplier) Entity() string { return "garage.shopping_item" }

var shoppingFields = []string{"part_id", "qty_centi", "dismissed", "note"}

func (a *ShoppingItemApplier) CurrentVersion(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (int32, error) {
	item, err := a.repo.GetShoppingItemByID(ctx, tx, uuid.UUID(team), id)
	if err != nil {
		return 0, carSyncErr(err)
	}
	return item.Version, nil
}

func (a *ShoppingItemApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	f := syncapply.Fields(op.Fields)
	if err := f.Only(shoppingFields...); err != nil {
		return 0, err
	}
	team, user := uuid.UUID(op.TeamID), uuid.UUID(op.UserID)
	switch op.Kind {
	case syncapply.Create:
		partRaw, err := f.RequiredString("part_id")
		if err != nil {
			return 0, err
		}
		partID, err := uuid.Parse(partRaw)
		if err != nil {
			return 0, fmt.Errorf("part_id: %w", syncapply.ErrInvalidFields)
		}
		_, qty, err1 := f.Int64("qty_centi")
		_, dismissed, err2 := f.Bool("dismissed")
		_, note, err3 := f.String("note")
		if err := errors.Join(err1, err2, err3); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		item := domain.ShoppingItem{PartID: partID, Dismissed: dismissed != nil && *dismissed}
		if item.QtyCenti, item.Note, err = domain.NewShoppingItemQty(qty, note); err != nil {
			return 0, carSyncErr(err)
		}
		if err := a.checkEffectiveQty(ctx, tx, team, item); err != nil {
			return 0, err
		}
		created, err := a.repo.CreateShoppingItemWithID(ctx, tx, team, user, op.EntityID, item)
		if err != nil {
			return 0, carSyncErr(err)
		}
		return created.Version, nil
	case syncapply.Patch:
		current, err := a.repo.GetShoppingItemByID(ctx, tx, team, op.EntityID)
		if err != nil {
			return 0, carSyncErr(err)
		}
		if current.Version != op.ExpectedVersion {
			return 0, syncapply.ErrStale
		}
		if _, set := f["part_id"]; set {
			return 0, fmt.Errorf("a shopping line cannot change part: %w", syncapply.ErrInvalidFields)
		}
		next := current
		var errs [3]error
		if set, qty, err := f.Int64("qty_centi"); err != nil {
			errs[0] = err
		} else if set {
			next.QtyCenti = qty
		}
		if set, dismissed, err := f.Bool("dismissed"); err != nil || (set && dismissed == nil) {
			errs[1] = syncapply.ErrInvalidFields
		} else if set {
			next.Dismissed = *dismissed
		}
		if set, note, err := f.String("note"); err != nil {
			errs[2] = err
		} else if set {
			next.Note = note
		}
		if err := errors.Join(errs[:]...); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		var verr error
		if next.QtyCenti, next.Note, verr = domain.NewShoppingItemQty(next.QtyCenti, next.Note); verr != nil {
			return 0, carSyncErr(verr)
		}
		if err := a.checkEffectiveQty(ctx, tx, team, next); err != nil {
			return 0, err
		}
		updated, err := a.repo.UpdateShoppingItemByID(ctx, tx, team, next)
		if err != nil {
			return 0, carSyncErr(err)
		}
		return updated.Version, nil
	case syncapply.Delete:
		if err := a.repo.DeleteShoppingItemByID(ctx, tx, team, op.EntityID, op.ExpectedVersion); err != nil {
			return 0, carSyncErr(err)
		}
		return op.ExpectedVersion + 1, nil
	default:
		return 0, fmt.Errorf("shopping item operation kind: %w", syncapply.ErrInvalidFields)
	}
}

// checkEffectiveQty is the rule of SetShoppingItem: a visible line needs a positive quantity,
// either its own or the suggestion, and the part must exist. A hidden line needs neither.
func (a *ShoppingItemApplier) checkEffectiveQty(ctx context.Context, tx db.DBTX, team uuid.UUID, item domain.ShoppingItem) error {
	part, err := a.repo.GetPart(ctx, tx, team, item.PartID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("part of the shopping line: %w", syncapply.ErrInvalidRef)
		}
		return err
	}
	if item.Dismissed {
		return nil
	}
	effective := domain.SuggestedQty(part.QtyCenti, part.MinStockCenti, part.ReorderToCenti)
	if item.QtyCenti != nil {
		effective = *item.QtyCenti
	}
	if effective <= 0 {
		return fmt.Errorf("shopping quantity: %w", syncapply.ErrInvalidFields)
	}
	return nil
}

type shoppingWire struct {
	PartID    uuid.UUID `json:"part_id"`
	QtyCenti  *int64    `json:"qty_centi"`
	Dismissed bool      `json:"dismissed"`
	Note      *string   `json:"note"`
	Version   int32     `json:"version"`
}

func (a *ShoppingItemApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, _ string, id uuid.UUID) (json.RawMessage, bool, error) {
	item, err := a.repo.GetShoppingItemByID(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := json.Marshal(shoppingWire{PartID: item.PartID, QtyCenti: item.QtyCenti, Dismissed: item.Dismissed, Note: item.Note, Version: item.Version})
	return raw, err == nil, err
}
