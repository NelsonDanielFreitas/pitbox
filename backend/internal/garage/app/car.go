package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

const (
	carCreateMethod = "/pitbox.garage.v1.CarService/CreateCar"
	carUpdateMethod = "/pitbox.garage.v1.CarService/UpdateCar"
	carDeleteMethod = "/pitbox.garage.v1.CarService/DeleteCar"
)

type CreateCarInput struct {
	RequestID         uuid.UUID
	Make              string
	Model             string
	ModelYear         *int32
	SerialNumber      *string
	Nickname          *string
	TransponderNumber *string
	Status            string
	Scale             string
	Powertrain        string
	Notes             *string
}

type UpdateCarInput struct {
	RequestID uuid.UUID
	CarID     uuid.UUID
	Version   int32
	Update    domain.CarUpdate
}

type idCursor struct {
	ID uuid.UUID `json:"id"`
}

func (s *Service) CreateCar(ctx context.Context, user db.UserID, team db.TeamID, in CreateCarInput) (domain.Car, error) {
	car, err := domain.NewCar(in.Make, in.Model, in.ModelYear, in.SerialNumber, in.Nickname,
		in.TransponderNumber, in.Status, in.Scale, in.Powertrain, in.Notes)
	if err != nil {
		return domain.Car{}, err
	}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.Car{}, fmt.Errorf("hash create car: %w", err)
	}
	var out domain.Car
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, carCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Car](response)
			return err
		}
		if err := s.store.ActiveCatalog(ctx, tx, car.Scale, car.Powertrain); err != nil {
			return err
		}
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, carCreateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Car](response)
			return err
		}
		created, err := s.store.CreateCar(ctx, tx, uuid.UUID(team), uuid.UUID(user), car)
		if err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, created); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "car.create", "car", created.ID.String(), nil, carAudit(created), in.RequestID.String()); err != nil {
			return err
		}
		out = created
		return nil
	})
	return out, err
}

func (s *Service) GetCar(ctx context.Context, user db.UserID, team db.TeamID, id uuid.UUID) (domain.Car, error) {
	var out domain.Car
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		out, err = s.store.GetCar(ctx, tx, uuid.UUID(team), id)
		return err
	})
	return out, err
}

func (s *Service) ListCars(ctx context.Context, user db.UserID, team db.TeamID, size int32, token string) ([]domain.Car, string, error) {
	var c idCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	var out []domain.Car
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		out, err = s.store.ListCars(ctx, tx, uuid.UUID(team), c.ID, limit+1)
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

func (s *Service) UpdateCar(ctx context.Context, user db.UserID, team db.TeamID, in UpdateCarInput) (domain.Car, error) {
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return domain.Car{}, fmt.Errorf("hash update car: %w", err)
	}
	var out domain.Car
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.replay(ctx, tx, uuid.UUID(user), in.RequestID, carUpdateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Car](response)
			return err
		}
		current, err := s.store.GetCar(ctx, tx, uuid.UUID(team), in.CarID)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrConflict
		}
		next, err := domain.ApplyCarUpdate(current, in.Update)
		if err != nil {
			return err
		}
		if next.Scale != current.Scale || next.Powertrain != current.Powertrain {
			if err := s.store.ActiveCatalog(ctx, tx, next.Scale, next.Powertrain); err != nil {
				return err
			}
		}
		if next.Status == domain.AssetRetired {
			if _, open, err := s.store.OpenMountForCar(ctx, tx, uuid.UUID(team), next.ID); err != nil {
				return err
			} else if open {
				return domain.ErrOpenMount
			}
		}
		next.Version = in.Version
		response, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), in.RequestID, carUpdateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.Car](response)
			return err
		}
		updated, err := s.store.UpdateCar(ctx, tx, uuid.UUID(team), next)
		if err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), in.RequestID, updated); err != nil {
			return err
		}
		if err := s.audit(ctx, tx, "car.update", "car", updated.ID.String(), carAudit(current), carAudit(updated), in.RequestID.String()); err != nil {
			return err
		}
		out = updated
		return nil
	})
	return out, err
}

func (s *Service) DeleteCar(ctx context.Context, user db.UserID, team db.TeamID, requestID uuid.UUID, id uuid.UUID, version int32) error {
	in := struct {
		RequestID uuid.UUID `json:"request_id"`
		CarID     uuid.UUID `json:"car_id"`
		Version   int32     `json:"version"`
	}{requestID, id, version}
	hash, err := hashInput(uuid.UUID(team), in)
	if err != nil {
		return err
	}
	return s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		_, replayed, err := s.replay(ctx, tx, uuid.UUID(user), requestID, carDeleteMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		if err := s.store.LockCar(ctx, tx, uuid.UUID(team), id); err != nil {
			return err
		}
		current, err := s.store.GetCar(ctx, tx, uuid.UUID(team), id)
		if err != nil {
			return err
		}
		if current.Version != version {
			return domain.ErrConflict
		}
		if _, open, err := s.store.OpenMountForCar(ctx, tx, uuid.UUID(team), id); err != nil {
			return err
		} else if open {
			return domain.ErrOpenMount
		}
		if _, replayed, err = s.claimOrReplay(ctx, tx, uuid.UUID(user), requestID, carDeleteMethod, hash); err != nil {
			return err
		} else if replayed {
			return nil
		}
		if err := s.store.SoftDeleteCar(ctx, tx, uuid.UUID(team), id, version); err != nil {
			return err
		}
		if err := s.idempotency.Save(ctx, tx, uuid.UUID(user), requestID, nil); err != nil {
			return err
		}
		return s.audit(ctx, tx, "car.delete", "car", id.String(), carAudit(current), nil, requestID.String())
	})
}

func carAudit(c domain.Car) map[string]any {
	return map[string]any{
		"make": c.Make, "model": c.Model, "status": c.Status, "scale": c.Scale,
		"powertrain": c.Powertrain, "version": c.Version,
	}
}
