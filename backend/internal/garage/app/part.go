package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox"
)

const (
	partCreateMethod  = "/pitbox.garage.v1.PartService/CreatePart"
	partUpdateMethod  = "/pitbox.garage.v1.PartService/UpdatePart"
	partDeleteMethod  = "/pitbox.garage.v1.PartService/DeletePart"
	stockMethod       = "/pitbox.garage.v1.PartService/AddStockMovement"
	stockLowEvent     = "garage.stock_low"
	stockLowAggregate = "garage.part"
)

type CreatePartInput struct {
	RequestID       uuid.UUID
	Kind            string
	Brand           *string
	Reference       *string
	Description     string
	Unit            string
	Specs           map[string]any
	MinStockCenti   int64
	UnitCostCents   *int32
	Currency        string
	HasServiceLife  bool
	InitialQtyCenti int64
	ReorderToCenti  *int64
}

type UpdatePartInput struct {
	RequestID uuid.UUID
	PartID    uuid.UUID
	Version   int32
	Update    domain.PartUpdate
}

type AddStockMovementInput struct {
	RequestID     uuid.UUID
	PartID        uuid.UUID
	QtyDeltaCenti int64
	Reason        string
	Note          *string
	UnitCostCents *int32
	LossReason    string
}

type stockResult struct {
	Part     domain.Part          `json:"part"`
	Movement domain.StockMovement `json:"movement,omitempty"`
}

func (s *Service) CreatePart(ctx context.Context, user db.UserID, team db.TeamID, in CreatePartInput) (domain.Part, error) {
	p, err := domain.NewPart(in.Kind, in.Brand, in.Reference, in.Description, in.Unit, in.Specs,
		in.MinStockCenti, in.UnitCostCents, in.Currency, in.HasServiceLife, in.ReorderToCenti)
	if err != nil {
		return domain.Part{}, err
	}
	if in.InitialQtyCenti < 0 || in.InitialQtyCenti > domain.MaxStockCenti {
		return domain.Part{}, fmt.Errorf("%w: initial quantity", domain.ErrInvalid)
	}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.Part{}, err
	}
	var out domain.Part
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, partCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Part](response)
			return err
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, partCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Part](response)
			return err
		}
		created, err := s.store.CreatePart(ctx, tx, uuid.UUID(team), uuid.UUID(user), p)
		if err != nil {
			return err
		}
		var initialMovementID uuid.UUID
		if in.InitialQtyCenti > 0 {
			// The initial stock was bought at the unit cost given for the part.
			movement, err := domain.NewStockMovement(created.ID, in.InitialQtyCenti, domain.StockPurchase, nil, s.now(),
				domain.MovementDetails{UnitCostCents: in.UnitCostCents})
			if err != nil {
				return err
			}
			storedMovement, err := s.store.CreateStockMovement(ctx, tx, uuid.UUID(team), uuid.UUID(user), movement)
			if err != nil {
				return err
			}
			initialMovementID = storedMovement.ID
		}
		created, err = s.store.GetPart(ctx, tx, uuid.UUID(team), created.ID)
		if err != nil {
			return err
		}
		if created.MinStockCenti > 0 && created.QtyCenti < created.MinStockCenti {
			if err := recordStockLow(ctx, tx, uuid.UUID(team), created, initialMovementID); err != nil {
				return err
			}
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, created); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "part.create", "part", created.ID.String(), nil, partAudit(created), in.RequestID.String()); err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, err
}

func (s *Service) GetPart(ctx context.Context, user db.UserID, team db.TeamID, id uuid.UUID) (domain.Part, error) {
	var out domain.Part
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, role string) error {
		var err error
		out, err = s.store.GetPart(ctx, tx, uuid.UUID(team), id)
		if err == nil && role != "owner" && role != "mechanic" {
			out.UnitCostCents = nil
		}
		return err
	})
	return out, err
}

func (s *Service) ListParts(ctx context.Context, user db.UserID, team db.TeamID, size int32, token string) ([]domain.Part, string, error) {
	var c idCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	var out []domain.Part
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, role string) error {
		var err error
		out, err = s.store.ListParts(ctx, tx, uuid.UUID(team), c.ID, limit+1)
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
		next = cursor.Encode(idCursor{ID: out[len(out)-1].ID})
	}
	return out, next, nil
}

func (s *Service) UpdatePart(ctx context.Context, user db.UserID, team db.TeamID, in UpdatePartInput) (domain.Part, error) {
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.Part{}, err
	}
	var out domain.Part
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, partUpdateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Part](response)
			return err
		}
		current, err := s.store.GetPart(ctx, tx, uuid.UUID(team), in.PartID)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrConflict
		}
		next, err := domain.ApplyPartUpdate(current, in.Update)
		if err != nil {
			return err
		}
		next.Version = in.Version
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, partUpdateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Part](response)
			return err
		}
		updated, err := s.store.UpdatePart(ctx, tx, uuid.UUID(team), next)
		if err != nil {
			return err
		}
		updated, err = s.store.GetPart(ctx, tx, uuid.UUID(team), updated.ID)
		if err != nil {
			return err
		}
		if current.QtyCenti >= current.MinStockCenti && updated.QtyCenti < updated.MinStockCenti {
			if err := recordStockLow(ctx, tx, uuid.UUID(team), updated, uuid.Nil); err != nil {
				return err
			}
		}
		// A lower minimum can put the part back above it: a hidden line must not stay hidden.
		if err := s.afterStockChange(ctx, tx, uuid.UUID(team), updated, false); err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, updated); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "part.update", "part", updated.ID.String(), partAudit(current), partAudit(updated), in.RequestID.String()); err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *Service) DeletePart(ctx context.Context, user db.UserID, team db.TeamID, requestID, id uuid.UUID, version int32) error {
	in := struct {
		RequestID uuid.UUID `json:"request_id"`
		PartID    uuid.UUID `json:"part_id"`
		Version   int32     `json:"version"`
	}{requestID, id, version}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return err
	}
	return s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		_, replayed, err := s.replay(ctx, tx, uuid.UUID(user), requestID, partDeleteMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		current, err := s.store.GetPart(ctx, tx, uuid.UUID(team), id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return domain.ErrConflict
		}
		if _, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), requestID, partDeleteMethod, hash); err != nil {
			return err
		} else if replayed {
			return nil
		}
		if err := s.store.SoftDeletePart(ctx, tx, uuid.UUID(team), id, version); err != nil {
			return err
		}
		// The soft delete never fires the foreign key cascade, so the line goes explicitly.
		if _, err := s.store.DeleteShoppingItemForPart(ctx, tx, uuid.UUID(team), id); err != nil {
			return err
		}
		if err := s.idempotency.Save(ctx, tx, uuid.UUID(user), requestID, nil); err != nil {
			return err
		}
		return s.audit(ctx, tx, "part.delete", "part", id.String(), partAudit(current), nil, requestID.String())
	})
}

func (s *Service) AddStockMovement(ctx context.Context, user db.UserID, team db.TeamID, in AddStockMovementInput) (domain.Part, domain.StockMovement, error) {
	movement, err := domain.NewStockMovement(in.PartID, in.QtyDeltaCenti, in.Reason, in.Note, s.now(),
		domain.MovementDetails{UnitCostCents: in.UnitCostCents, LossReason: in.LossReason})
	if err != nil {
		return domain.Part{}, domain.StockMovement{}, err
	}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.Part{}, domain.StockMovement{}, err
	}
	var result stockResult
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, stockMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			result, err = decodeResource[stockResult](response)
			return err
		}
		if err := s.store.LockPartForStock(ctx, tx, uuid.UUID(team), in.PartID); err != nil {
			return err
		}
		before, err := s.store.PartStockCenti(ctx, tx, uuid.UUID(team), in.PartID)
		if err != nil {
			return err
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, stockMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			result, err = decodeResource[stockResult](response)
			return err
		}
		created, err := s.store.CreateStockMovement(ctx, tx, uuid.UUID(team), uuid.UUID(user), movement)
		if err != nil {
			return err
		}
		part, err := s.store.GetPart(ctx, tx, uuid.UUID(team), in.PartID)
		if err != nil {
			return err
		}
		if before >= part.MinStockCenti && part.QtyCenti < part.MinStockCenti {
			if err := recordStockLow(ctx, tx, uuid.UUID(team), part, created.ID); err != nil {
				return err
			}
		}
		if err := s.afterStockChange(ctx, tx, uuid.UUID(team), part, in.Reason == domain.StockPurchase); err != nil {
			return err
		}
		result = stockResult{Part: part, Movement: created}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, result); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "part.stock_movement", "part", part.ID.String(),
			map[string]any{"qty_centi": before}, map[string]any{"qty_centi": part.QtyCenti, "movement_id": created.ID.String()}, in.RequestID.String()); err != nil {
			return err
		}
		return nil
	})
	return result.Part, result.Movement, err
}

func (s *Service) ListStockMovements(ctx context.Context, user db.UserID, team db.TeamID, partID uuid.UUID, size int32, token string) ([]domain.StockMovement, string, error) {
	var c idCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	after := c.ID
	if after == uuid.Nil {
		after = maxCursorUUID
	}
	var out []domain.StockMovement
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, role string) error {
		if _, err := s.store.GetPart(ctx, tx, uuid.UUID(team), partID); err != nil {
			return err
		}
		var err error
		out, err = s.store.ListStockMovements(ctx, tx, uuid.UUID(team), partID, after, limit+1)
		if err == nil && role != "owner" && role != "mechanic" {
			for i := range out {
				out[i].UnitCostCents = nil // the price paid is not for pilots
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
		next = cursor.Encode(idCursor{ID: out[len(out)-1].ID})
	}
	return out, next, nil
}

// afterStockChange keeps the shopping list in step with the stock inside the same
// transaction: a purchase removes the part's line, and a hidden line is dropped once the
// part is back at its minimum so the alert works again next time.
func (s *Service) afterStockChange(ctx context.Context, tx db.DBTX, team uuid.UUID, part domain.Part, purchase bool) error {
	return afterStockChange(ctx, tx, s.store, team, part, purchase)
}

func afterStockChange(ctx context.Context, tx db.DBTX, st Store, team uuid.UUID, part domain.Part, purchase bool) error {
	if purchase {
		if _, err := st.DeleteShoppingItemForPart(ctx, tx, team, part.ID); err != nil {
			return err
		}
		return nil
	}
	if !part.BelowMin {
		return st.ClearDismissedShoppingItem(ctx, tx, team, part.ID)
	}
	return nil
}

// AfterSyncedMovement is what AddStockMovement does after inserting a movement, for a
// movement that arrived through sync: it raises the "stock low" event when the part crossed
// its minimum, then keeps the shopping list in step. `before` is the part's stock (in
// hundredths) read under the part lock before the insert.
func AfterSyncedMovement(ctx context.Context, tx db.DBTX, st Store, team, partID, movementID uuid.UUID, before int64, purchase bool) error {
	part, err := st.GetPart(ctx, tx, team, partID)
	if err != nil {
		return err
	}
	if before >= part.MinStockCenti && part.QtyCenti < part.MinStockCenti {
		if err := recordStockLow(ctx, tx, team, part, movementID); err != nil {
			return err
		}
	}
	return afterStockChange(ctx, tx, st, team, part, purchase)
}

func recordStockLow(ctx context.Context, tx db.DBTX, team uuid.UUID, part domain.Part, movementID uuid.UUID) error {
	return outbox.Record(ctx, tx, outbox.Event{
		TeamID:      team,
		Aggregate:   stockLowAggregate,
		AggregateID: part.ID,
		Type:        stockLowEvent,
		Payload: struct {
			PartID        uuid.UUID `json:"part_id"`
			QtyCenti      int64     `json:"qty_centi"`
			MinStockCenti int64     `json:"min_stock_centi"`
			MovementID    uuid.UUID `json:"movement_id"`
		}{PartID: part.ID, QtyCenti: part.QtyCenti, MinStockCenti: part.MinStockCenti, MovementID: movementID},
	})
}

func partAudit(p domain.Part) map[string]any {
	m := map[string]any{
		"kind": p.Kind, "description": p.Description, "unit": p.Unit,
		"min_stock_centi": p.MinStockCenti, "currency": p.Currency, "version": p.Version,
	}
	if p.UnitCostCents != nil {
		m["unit_cost_cents"] = *p.UnitCostCents
	}
	return m
}
