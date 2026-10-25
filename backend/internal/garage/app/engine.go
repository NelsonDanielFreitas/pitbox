package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

const (
	engineCreateMethod = "/pitbox.garage.v1.EngineService/CreateEngine"
	engineUpdateMethod = "/pitbox.garage.v1.EngineService/UpdateEngine"
	engineDeleteMethod = "/pitbox.garage.v1.EngineService/DeleteEngine"
	mountMethod        = "/pitbox.garage.v1.EngineService/MountEngine"
	unmountMethod      = "/pitbox.garage.v1.EngineService/UnmountEngine"
	moveMethod         = "/pitbox.garage.v1.EngineService/MoveEngine"
)

type CreateEngineInput struct {
	RequestID    uuid.UUID
	Make         string
	Model        string
	SerialNumber *string
	PurchasedOn  *string
	Status       string
	Notes        *string
}

type UpdateEngineInput struct {
	RequestID uuid.UUID
	EngineID  uuid.UUID
	Version   int32
	Update    domain.EngineUpdate
}

func (s *Service) CreateEngine(ctx context.Context, user db.UserID, team db.TeamID, in CreateEngineInput) (domain.Engine, error) {
	e, err := domain.NewEngine(in.Make, in.Model, in.SerialNumber, in.PurchasedOn, in.Status, in.Notes)
	if err != nil {
		return domain.Engine{}, err
	}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.Engine{}, err
	}
	var out domain.Engine
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, engineCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Engine](response)
			return err
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, engineCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Engine](response)
			return err
		}
		created, err := s.store.CreateEngine(ctx, tx, uuid.UUID(team), uuid.UUID(user), e)
		if err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, created); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "engine.create", "engine", created.ID.String(), nil, engineAudit(created), in.RequestID.String()); err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, err
}

func (s *Service) GetEngine(ctx context.Context, user db.UserID, team db.TeamID, id uuid.UUID) (domain.Engine, error) {
	var out domain.Engine
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		out, err = s.store.GetEngine(ctx, tx, uuid.UUID(team), id)
		return err
	})
	return out, err
}

func (s *Service) ListEngines(ctx context.Context, user db.UserID, team db.TeamID, size int32, token string) ([]domain.Engine, string, error) {
	var c idCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	var out []domain.Engine
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		out, err = s.store.ListEngines(ctx, tx, uuid.UUID(team), c.ID, limit+1)
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

func (s *Service) UpdateEngine(ctx context.Context, user db.UserID, team db.TeamID, in UpdateEngineInput) (domain.Engine, error) {
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.Engine{}, err
	}
	var out domain.Engine
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, engineUpdateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Engine](response)
			return err
		}
		current, err := s.store.GetEngine(ctx, tx, uuid.UUID(team), in.EngineID)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrConflict
		}
		next, err := domain.ApplyEngineUpdate(current, in.Update)
		if err != nil {
			return err
		}
		next.Version = in.Version
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, engineUpdateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Engine](response)
			return err
		}
		updated, err := s.store.UpdateEngine(ctx, tx, uuid.UUID(team), next)
		if err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, updated); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "engine.update", "engine", updated.ID.String(), engineAudit(current), engineAudit(updated), in.RequestID.String()); err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *Service) DeleteEngine(ctx context.Context, user db.UserID, team db.TeamID, requestID, id uuid.UUID, version int32) error {
	in := struct {
		RequestID uuid.UUID `json:"request_id"`
		EngineID  uuid.UUID `json:"engine_id"`
		Version   int32     `json:"version"`
	}{requestID, id, version}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return err
	}
	return s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		_, replayed, err := s.replay(ctx, tx, uuid.UUID(user), requestID, engineDeleteMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		if err := s.store.LockEngine(ctx, tx, uuid.UUID(team), id); err != nil {
			return err
		}
		current, err := s.store.GetEngine(ctx, tx, uuid.UUID(team), id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return domain.ErrConflict
		}
		if _, open, err := s.store.OpenMountForEngine(ctx, tx, uuid.UUID(team), id); err != nil {
			return err
		} else if open {
			return domain.ErrOpenMount
		}
		if _, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), requestID, engineDeleteMethod, hash); err != nil {
			return err
		} else if replayed {
			return nil
		}
		if err := s.store.SoftDeleteEngine(ctx, tx, uuid.UUID(team), id, version); err != nil {
			return err
		}
		if err := s.idempotency.Save(ctx, tx, uuid.UUID(user), requestID, nil); err != nil {
			return err
		}
		return s.audit(ctx, tx, "engine.delete", "engine", id.String(), engineAudit(current), nil, requestID.String())
	})
}

func (s *Service) MountEngine(ctx context.Context, user db.UserID, team db.TeamID, requestID, engineID, carID uuid.UUID) (domain.EngineMount, error) {
	in := struct {
		RequestID uuid.UUID `json:"request_id"`
		EngineID  uuid.UUID `json:"engine_id"`
		CarID     uuid.UUID `json:"car_id"`
	}{requestID, engineID, carID}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.EngineMount{}, err
	}
	var out domain.EngineMount
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), requestID, mountMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.EngineMount](response)
			return err
		}
		if err := s.lockMountAssets(ctx, tx, uuid.UUID(team), engineID, carID); err != nil {
			return err
		}
		if err := s.mountable(ctx, tx, uuid.UUID(team), engineID, carID); err != nil {
			return err
		}
		if _, open, err := s.store.OpenMountForEngine(ctx, tx, uuid.UUID(team), engineID); err != nil {
			return err
		} else if open {
			return domain.ErrMountConflict
		}
		if _, open, err := s.store.OpenMountForCar(ctx, tx, uuid.UUID(team), carID); err != nil {
			return err
		} else if open {
			return domain.ErrMountConflict
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), requestID, mountMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.EngineMount](response)
			return err
		}
		created, err := s.store.CreateMount(ctx, tx, uuid.UUID(team), uuid.UUID(user), domain.EngineMount{
			EngineID: engineID, CarID: carID, MountedAt: s.now(),
		})
		if err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), requestID, created); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "engine.mount", "engine", engineID.String(), nil, mountAudit(created), requestID.String()); err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, err
}

func (s *Service) UnmountEngine(ctx context.Context, user db.UserID, team db.TeamID, requestID, engineID uuid.UUID) error {
	hash, err := hashInput(uuid.UUID(team), struct {
		RequestID uuid.UUID `json:"request_id"`
		EngineID  uuid.UUID `json:"engine_id"`
	}{requestID, engineID})
	if err != nil {
		return err
	}
	return s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		_, replayed, err := s.replay(ctx, tx, uuid.UUID(user), requestID, unmountMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		if err := s.store.LockEngine(ctx, tx, uuid.UUID(team), engineID); err != nil {
			return err
		}
		current, open, err := s.store.OpenMountForEngine(ctx, tx, uuid.UUID(team), engineID)
		if err != nil {
			return err
		}
		if !open {
			return domain.ErrMountConflict
		}
		if _, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), requestID, unmountMethod, hash); err != nil {
			return err
		} else if replayed {
			return nil
		}
		if err := s.store.CloseMount(ctx, tx, uuid.UUID(team), current.ID, s.now()); err != nil {
			return err
		}
		if err := s.idempotency.Save(ctx, tx, uuid.UUID(user), requestID, nil); err != nil {
			return err
		}
		return s.audit(ctx, tx, "engine.unmount", "engine", engineID.String(), mountAudit(current), nil, requestID.String())
	})
}

func (s *Service) MoveEngine(ctx context.Context, user db.UserID, team db.TeamID, requestID, engineID, carID uuid.UUID) (domain.EngineMount, error) {
	in := struct {
		RequestID uuid.UUID `json:"request_id"`
		EngineID  uuid.UUID `json:"engine_id"`
		CarID     uuid.UUID `json:"car_id"`
	}{requestID, engineID, carID}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.EngineMount{}, err
	}
	var out domain.EngineMount
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), requestID, moveMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.EngineMount](response)
			return err
		}
		if err := s.lockMountAssets(ctx, tx, uuid.UUID(team), engineID, carID); err != nil {
			return err
		}
		if err := s.mountable(ctx, tx, uuid.UUID(team), engineID, carID); err != nil {
			return err
		}
		current, open, err := s.store.OpenMountForEngine(ctx, tx, uuid.UUID(team), engineID)
		if err != nil {
			return err
		}
		if open && current.CarID == carID {
			out = current
			return nil
		}
		if _, open, err := s.store.OpenMountForCar(ctx, tx, uuid.UUID(team), carID); err != nil {
			return err
		} else if open {
			return domain.ErrMountConflict
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), requestID, moveMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.EngineMount](response)
			return err
		}
		at := s.now()
		if open {
			if err := s.store.CloseMount(ctx, tx, uuid.UUID(team), current.ID, at); err != nil {
				return err
			}
		}
		created, err := s.store.CreateMount(ctx, tx, uuid.UUID(team), uuid.UUID(user), domain.EngineMount{
			EngineID: engineID, CarID: carID, MountedAt: at,
		})
		if err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), requestID, created); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "engine.move", "engine", engineID.String(), nil, mountAudit(created), requestID.String()); err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, err
}

func (s *Service) ListEngineMounts(ctx context.Context, user db.UserID, team db.TeamID, engineID uuid.UUID, size int32, token string) ([]domain.EngineMount, string, error) {
	var c idCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	after := c.ID
	if after == uuid.Nil {
		after = maxCursorUUID
	}
	var out []domain.EngineMount
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		if _, err := s.store.GetEngine(ctx, tx, uuid.UUID(team), engineID); err != nil {
			return err
		}
		var err error
		out, err = s.store.ListMountsForEngine(ctx, tx, uuid.UUID(team), engineID, after, limit+1)
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

func (s *Service) lockMountAssets(ctx context.Context, tx db.DBTX, team, engineID, carID uuid.UUID) error {
	if err := s.store.LockEngine(ctx, tx, team, engineID); err != nil {
		return err
	}
	return s.store.LockCar(ctx, tx, team, carID)
}

func (s *Service) mountable(ctx context.Context, tx db.DBTX, team, engineID, carID uuid.UUID) error {
	e, err := s.store.GetEngine(ctx, tx, team, engineID)
	if err != nil {
		return err
	}
	c, err := s.store.GetCar(ctx, tx, team, carID)
	if err != nil {
		return err
	}
	if e.Status == domain.AssetRetired || c.Status == domain.AssetRetired {
		return domain.ErrMountConflict
	}
	return nil
}

func engineAudit(e domain.Engine) map[string]any {
	return map[string]any{"make": e.Make, "model": e.Model, "status": e.Status, "version": e.Version}
}

func mountAudit(m domain.EngineMount) map[string]any {
	return map[string]any{"engine_id": m.EngineID.String(), "car_id": m.CarID.String(), "mounted_at": m.MountedAt}
}
