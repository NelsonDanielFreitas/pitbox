package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

const (
	shoppingSetMethod    = "/pitbox.garage.v1.ShoppingService/SetShoppingItem"
	shoppingRemoveMethod = "/pitbox.garage.v1.ShoppingService/RemoveShoppingItem"
)

type SetShoppingItemInput struct {
	RequestID       uuid.UUID
	PartID          uuid.UUID
	QtyCenti        *int64
	Note            *string
	ExpectedVersion int32
}

type RemoveShoppingItemInput struct {
	RequestID uuid.UUID
	PartID    uuid.UUID
}

// ListShoppingList returns the computed list. Pilots read it, but without any cost.
func (s *Service) ListShoppingList(ctx context.Context, user db.UserID, team db.TeamID, size int32, token string) ([]domain.ShoppingLine, string, error) {
	var c idCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	var out []domain.ShoppingLine
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, role string) error {
		var err error
		out, err = s.store.ListShoppingList(ctx, tx, uuid.UUID(team), c.ID, limit+1)
		if err == nil && role != "owner" && role != "mechanic" {
			for i := range out {
				out[i].UnitCostCents = nil
			}
		}
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > int(limit) {
		out = out[:limit]
		next = cursor.Encode(idCursor{ID: out[len(out)-1].PartID})
	}
	return out, next, nil
}

// SetShoppingItem adds a manual line, adjusts a line or brings back a hidden one. It
// replaces the stored quantity and note: an absent quantity means "use the suggestion".
func (s *Service) SetShoppingItem(ctx context.Context, user db.UserID, team db.TeamID, in SetShoppingItemInput) (domain.ShoppingLine, error) {
	qty, note, err := domain.NewShoppingItemQty(in.QtyCenti, in.Note)
	if err != nil {
		return domain.ShoppingLine{}, err
	}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.ShoppingLine{}, err
	}
	var out domain.ShoppingLine
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, shoppingSetMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.ShoppingLine](response)
			return err
		}
		if err := s.store.LockPartForStock(ctx, tx, uuid.UUID(team), in.PartID); err != nil {
			return err
		}
		part, err := s.store.GetPart(ctx, tx, uuid.UUID(team), in.PartID)
		if err != nil {
			return err
		}
		item, found, err := s.store.GetShoppingItemForUpdate(ctx, tx, uuid.UUID(team), in.PartID)
		if err != nil {
			return err
		}
		if (found && in.ExpectedVersion != item.Version) || (!found && in.ExpectedVersion != 0) {
			return domain.ErrConflict
		}
		suggested := domain.SuggestedQty(part.QtyCenti, part.MinStockCenti, part.ReorderToCenti)
		effective := suggested
		if qty != nil {
			effective = *qty
		}
		if effective <= 0 { // a line that is not below its minimum needs a quantity
			return domain.ErrInvalid
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, shoppingSetMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.ShoppingLine](response)
			return err
		}
		next := domain.ShoppingItem{PartID: in.PartID, QtyCenti: qty, Note: note, Version: item.Version}
		var stored domain.ShoppingItem
		if found {
			stored, err = s.store.UpdateShoppingItem(ctx, tx, uuid.UUID(team), next)
		} else {
			stored, err = s.store.CreateShoppingItem(ctx, tx, uuid.UUID(team), uuid.UUID(user), next)
		}
		if err != nil {
			return err
		}
		out = domain.ShoppingLine{
			PartID: in.PartID, QtyCenti: effective, SuggestedQtyCenti: suggested, BelowMin: part.BelowMin,
			Note: stored.Note, ItemVersion: stored.Version, UnitCostCents: part.UnitCostCents,
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, out); err != nil {
			return err
		}
		var before any
		if found {
			before = shoppingAudit(item)
		}
		return s.audit(ctx, tx, "shopping.set", "part", in.PartID.String(), before, shoppingAudit(stored), in.RequestID.String())
	})
	return out, err
}

// RemoveShoppingItem deletes a manual line, or hides an automatic one until the stock is
// back at the minimum. A part that is not on the list is not found.
func (s *Service) RemoveShoppingItem(ctx context.Context, user db.UserID, team db.TeamID, in RemoveShoppingItemInput) error {
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return err
	}
	return s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		_, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, shoppingRemoveMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		if err := s.store.LockPartForStock(ctx, tx, uuid.UUID(team), in.PartID); err != nil {
			return err
		}
		part, err := s.store.GetPart(ctx, tx, uuid.UUID(team), in.PartID)
		if err != nil {
			return err
		}
		item, found, err := s.store.GetShoppingItemForUpdate(ctx, tx, uuid.UUID(team), in.PartID)
		if err != nil {
			return err
		}
		if (!part.BelowMin && !found) || (found && item.Dismissed) {
			return domain.ErrNotFound
		}
		if _, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, shoppingRemoveMethod, hash); err != nil {
			return err
		} else if replayed {
			return nil
		}
		var after any
		if part.BelowMin {
			hidden := domain.ShoppingItem{PartID: in.PartID, Dismissed: true, Version: item.Version}
			if found {
				_, err = s.store.UpdateShoppingItem(ctx, tx, uuid.UUID(team), hidden)
			} else {
				_, err = s.store.CreateShoppingItem(ctx, tx, uuid.UUID(team), uuid.UUID(user), hidden)
			}
			after = map[string]any{"dismissed": true}
		} else {
			_, err = s.store.DeleteShoppingItemForPart(ctx, tx, uuid.UUID(team), in.PartID)
		}
		if err != nil {
			return err
		}
		if err := s.idempotency.Save(ctx, tx, uuid.UUID(user), in.RequestID, nil); err != nil {
			return err
		}
		var before any
		if found {
			before = shoppingAudit(item)
		}
		return s.audit(ctx, tx, "shopping.remove", "part", in.PartID.String(), before, after, in.RequestID.String())
	})
}

func shoppingAudit(i domain.ShoppingItem) map[string]any {
	m := map[string]any{"dismissed": i.Dismissed, "version": i.Version}
	if i.QtyCenti != nil {
		m["qty_centi"] = *i.QtyCenti
	}
	return m
}
