package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

const (
	tyreCreateMethod = "/pitbox.garage.v1.TyreService/CreateTyreSet"
	tyreUpdateMethod = "/pitbox.garage.v1.TyreService/UpdateTyreSet"
	tyreDeleteMethod = "/pitbox.garage.v1.TyreService/DeleteTyreSet"
)

type CreateTyreSetInput struct {
	RequestID  uuid.UUID
	Label      string
	Make       string
	Model      string
	Compound   *string
	InsertType *string
	GluedOn    *string
	Status     string
	WearNotes  *string
}

type UpdateTyreSetInput struct {
	RequestID uuid.UUID
	TyreSetID uuid.UUID
	Version   int32
	Update    domain.TyreUpdate
}

func (s *Service) CreateTyreSet(ctx context.Context, user db.UserID, team db.TeamID, in CreateTyreSetInput) (domain.TyreSet, error) {
	t, err := domain.NewTyreSet(in.Label, in.Make, in.Model, in.Compound, in.InsertType, in.GluedOn, in.Status, in.WearNotes)
	if err != nil {
		return domain.TyreSet{}, err
	}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.TyreSet{}, err
	}
	var out domain.TyreSet
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, tyreCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.TyreSet](response)
			return err
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, tyreCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.TyreSet](response)
			return err
		}
		created, err := s.store.CreateTyreSet(ctx, tx, uuid.UUID(team), uuid.UUID(user), t)
		if err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, created); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "tyre.create", "tyre_set", created.ID.String(), nil, tyreAudit(created), in.RequestID.String()); err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, err
}

func (s *Service) GetTyreSet(ctx context.Context, user db.UserID, team db.TeamID, id uuid.UUID) (domain.TyreSet, error) {
	var out domain.TyreSet
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		out, err = s.store.GetTyreSet(ctx, tx, uuid.UUID(team), id)
		return err
	})
	return out, err
}

func (s *Service) ListTyreSets(ctx context.Context, user db.UserID, team db.TeamID, size int32, token string) ([]domain.TyreSet, string, error) {
	var c idCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	var out []domain.TyreSet
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		out, err = s.store.ListTyreSets(ctx, tx, uuid.UUID(team), c.ID, limit+1)
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

func (s *Service) UpdateTyreSet(ctx context.Context, user db.UserID, team db.TeamID, in UpdateTyreSetInput) (domain.TyreSet, error) {
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.TyreSet{}, err
	}
	var out domain.TyreSet
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, tyreUpdateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.TyreSet](response)
			return err
		}
		current, err := s.store.GetTyreSet(ctx, tx, uuid.UUID(team), in.TyreSetID)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrConflict
		}
		next, err := domain.ApplyTyreUpdate(current, in.Update)
		if err != nil {
			return err
		}
		next.Version = in.Version
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, tyreUpdateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.TyreSet](response)
			return err
		}
		updated, err := s.store.UpdateTyreSet(ctx, tx, uuid.UUID(team), next)
		if err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, updated); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "tyre.update", "tyre_set", updated.ID.String(), tyreAudit(current), tyreAudit(updated), in.RequestID.String()); err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *Service) DeleteTyreSet(ctx context.Context, user db.UserID, team db.TeamID, requestID, id uuid.UUID, version int32) error {
	in := struct {
		RequestID uuid.UUID `json:"request_id"`
		TyreSetID uuid.UUID `json:"tyre_set_id"`
		Version   int32     `json:"version"`
	}{requestID, id, version}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return err
	}
	return s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		_, replayed, err := s.replay(ctx, tx, uuid.UUID(user), requestID, tyreDeleteMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		current, err := s.store.GetTyreSet(ctx, tx, uuid.UUID(team), id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return domain.ErrConflict
		}
		if _, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), requestID, tyreDeleteMethod, hash); err != nil {
			return err
		} else if replayed {
			return nil
		}
		if err := s.store.SoftDeleteTyreSet(ctx, tx, uuid.UUID(team), id, version); err != nil {
			return err
		}
		if err := s.idempotency.Save(ctx, tx, uuid.UUID(user), requestID, nil); err != nil {
			return err
		}
		return s.audit(ctx, tx, "tyre.delete", "tyre_set", id.String(), tyreAudit(current), nil, requestID.String())
	})
}

func tyreAudit(t domain.TyreSet) map[string]any {
	return map[string]any{"label": t.Label, "make": t.Make, "model": t.Model, "status": t.Status, "version": t.Version}
}
