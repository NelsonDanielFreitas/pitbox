package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

const (
	installMethod = "/pitbox.garage.v1.PartService/InstallPart"
	removeMethod  = "/pitbox.garage.v1.PartService/RemovePart"
)

type InstallPartInput struct {
	RequestID   uuid.UUID
	PartID      uuid.UUID
	CarID       *uuid.UUID
	EngineID    *uuid.UUID
	InstalledAt *time.Time
	Note        *string
}

type RemovePartInput struct {
	RequestID   uuid.UUID
	InstallID   uuid.UUID
	Destination string
	Reason      string
	Note        *string
}

type installResult struct {
	Install domain.PartInstall `json:"install"`
	Part    domain.Part        `json:"part"`
}

// InstallPart installs one unit of a part that has a service life. The installation row
// and its `installed` stock movement are written in the same transaction.
func (s *Service) InstallPart(ctx context.Context, user db.UserID, team db.TeamID, in InstallPartInput) (domain.PartInstall, domain.Part, error) {
	install, err := domain.NewPartInstall(in.PartID, in.CarID, in.EngineID, in.InstalledAt, s.now())
	if err != nil {
		return domain.PartInstall{}, domain.Part{}, err
	}
	movement, err := domain.NewStockMovement(in.PartID, -domain.UnitCenti, domain.StockInstalled, in.Note, s.now(), domain.MovementDetails{})
	if err != nil {
		return domain.PartInstall{}, domain.Part{}, err
	}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.PartInstall{}, domain.Part{}, err
	}
	var result installResult
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, installMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			result, err = decodeResource[installResult](response)
			return err
		}
		// Fixed lock order (part, then target) so concurrent installs cannot deadlock.
		if err := s.store.LockPartForStock(ctx, tx, uuid.UUID(team), in.PartID); err != nil {
			return err
		}
		if err := s.lockInstallTarget(ctx, tx, uuid.UUID(team), install); err != nil {
			return err
		}
		before, err := s.store.GetPart(ctx, tx, uuid.UUID(team), in.PartID)
		if err != nil {
			return err
		}
		if !before.HasServiceLife {
			return domain.ErrInstallConflict
		}
		if err := s.installable(ctx, tx, uuid.UUID(team), install); err != nil {
			return err
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, installMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			result, err = decodeResource[installResult](response)
			return err
		}
		created, err := s.store.CreatePartInstall(ctx, tx, uuid.UUID(team), uuid.UUID(user), install)
		if err != nil {
			return err
		}
		stock, err := s.store.CreateStockMovement(ctx, tx, uuid.UUID(team), uuid.UUID(user), movement)
		if err != nil {
			return err
		}
		part, err := s.store.GetPart(ctx, tx, uuid.UUID(team), in.PartID)
		if err != nil {
			return err
		}
		if before.QtyCenti >= part.MinStockCenti && part.QtyCenti < part.MinStockCenti {
			if err := recordStockLow(ctx, tx, uuid.UUID(team), part, stock.ID); err != nil {
				return err
			}
		}
		if err := s.afterStockChange(ctx, tx, uuid.UUID(team), part, false); err != nil {
			return err
		}
		result = installResult{Install: created, Part: part}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, result); err != nil {
			return err
		}
		after := installAudit(created)
		after["movement_id"] = stock.ID.String()
		return s.audit(ctx, tx, "part.install", "part", in.PartID.String(), nil, after, in.RequestID.String())
	})
	return result.Install, result.Part, err
}

// RemovePart closes an active installation. Destination stock returns one unit to the
// stock; scrap and spare do not. The reason is stored only for scrap.
func (s *Service) RemovePart(ctx context.Context, user db.UserID, team db.TeamID, in RemovePartInput) (domain.PartInstall, domain.Part, error) {
	removal, err := domain.NewRemoval(in.Destination, in.Reason)
	if err != nil {
		return domain.PartInstall{}, domain.Part{}, err
	}
	if _, err := domain.NewStockMovement(uuid.New(), domain.UnitCenti, domain.StockReturned, in.Note, s.now(), domain.MovementDetails{}); err != nil {
		return domain.PartInstall{}, domain.Part{}, err
	}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.PartInstall{}, domain.Part{}, err
	}
	var result installResult
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, removeMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			result, err = decodeResource[installResult](response)
			return err
		}
		current, err := s.store.GetPartInstallForUpdate(ctx, tx, uuid.UUID(team), in.InstallID)
		if err != nil {
			return err
		}
		if !current.Active() {
			return domain.ErrInstallConflict
		}
		if err := s.store.LockPartForStock(ctx, tx, uuid.UUID(team), current.PartID); err != nil {
			return err
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, removeMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			result, err = decodeResource[installResult](response)
			return err
		}
		closed, err := s.store.ClosePartInstall(ctx, tx, uuid.UUID(team), current.ID, removal)
		if err != nil {
			return err
		}
		after := installAudit(closed)
		if removal.Destination == domain.RemovalStock {
			movement, err := domain.NewStockMovement(current.PartID, domain.UnitCenti, domain.StockReturned, in.Note, s.now(), domain.MovementDetails{})
			if err != nil {
				return err
			}
			stock, err := s.store.CreateStockMovement(ctx, tx, uuid.UUID(team), uuid.UUID(user), movement)
			if err != nil {
				return err
			}
			after["movement_id"] = stock.ID.String()
		}
		part, err := s.store.GetPart(ctx, tx, uuid.UUID(team), current.PartID)
		if err != nil {
			return err
		}
		if err := s.afterStockChange(ctx, tx, uuid.UUID(team), part, false); err != nil {
			return err
		}
		result = installResult{Install: closed, Part: part}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, result); err != nil {
			return err
		}
		return s.audit(ctx, tx, "part.remove", "part", current.PartID.String(), installAudit(current), after, in.RequestID.String())
	})
	return result.Install, result.Part, err
}

func (s *Service) ListPartInstalls(ctx context.Context, user db.UserID, team db.TeamID, filter domain.InstallFilter, history bool, size int32, token string) ([]domain.PartInstall, string, error) {
	if !filter.Valid() {
		return nil, "", domain.ErrInvalid
	}
	var c idCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	after := c.ID
	if after == uuid.Nil {
		after = maxCursorUUID
	}
	var out []domain.PartInstall
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		switch {
		case filter.PartID != nil:
			_, err = s.store.GetPart(ctx, tx, uuid.UUID(team), *filter.PartID)
		case filter.CarID != nil:
			_, err = s.store.GetCar(ctx, tx, uuid.UUID(team), *filter.CarID)
		default:
			_, err = s.store.GetEngine(ctx, tx, uuid.UUID(team), *filter.EngineID)
		}
		if err != nil {
			return err
		}
		out, err = s.store.ListPartInstalls(ctx, tx, uuid.UUID(team), filter, history, after, limit+1)
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

func (s *Service) lockInstallTarget(ctx context.Context, tx db.DBTX, team uuid.UUID, in domain.NewInstall) error {
	if in.CarID != nil {
		return s.store.LockCar(ctx, tx, team, *in.CarID)
	}
	return s.store.LockEngine(ctx, tx, team, *in.EngineID)
}

// installable refuses a retired car or engine, like mounting does.
func (s *Service) installable(ctx context.Context, tx db.DBTX, team uuid.UUID, in domain.NewInstall) error {
	status := ""
	if in.CarID != nil {
		c, err := s.store.GetCar(ctx, tx, team, *in.CarID)
		if err != nil {
			return err
		}
		status = c.Status
	} else {
		e, err := s.store.GetEngine(ctx, tx, team, *in.EngineID)
		if err != nil {
			return err
		}
		status = e.Status
	}
	if status == domain.AssetRetired {
		return domain.ErrInstallConflict
	}
	return nil
}

func installAudit(i domain.PartInstall) map[string]any {
	m := map[string]any{"install_id": i.ID.String(), "installed_at": i.InstalledAt}
	if i.CarID != nil {
		m["car_id"] = i.CarID.String()
	}
	if i.EngineID != nil {
		m["engine_id"] = i.EngineID.String()
	}
	if i.RemovedAt != nil {
		m["removed_at"] = *i.RemovedAt
		m["removal_destination"] = i.RemovalDestination
		if i.RemovalReason != "" {
			m["removal_reason"] = i.RemovalReason
		}
	}
	return m
}
