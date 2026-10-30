package garage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
)

// CarApplier applies offline operations on garage.car for the sync module. It reuses the
// domain rules of the normal use cases (validation, catalogue, open mounts), but not their
// authorization or idempotency: the sync module has already checked the caller's current
// role and keeps op_id as the idempotency key.
type CarApplier struct{ repo *store.Repo }

func NewCarApplier() *CarApplier { return &CarApplier{repo: store.NewRepo()} }

var _ syncapply.Applier = (*CarApplier)(nil)

func (*CarApplier) Entity() string { return "garage.car" }

var carFields = []string{
	"make", "model", "model_year", "serial_number", "nickname", "transponder_number",
	"status", "scale", "powertrain", "notes",
}

func (a *CarApplier) CurrentVersion(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (int32, error) {
	c, err := a.repo.GetCar(ctx, tx, uuid.UUID(team), id)
	if err != nil {
		return 0, carSyncErr(err)
	}
	return c.Version, nil
}

func (a *CarApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	team, user := uuid.UUID(op.TeamID), uuid.UUID(op.UserID)
	fields := syncapply.Fields(op.Fields)
	switch op.Kind {
	case syncapply.Create:
		return a.create(ctx, tx, team, user, op.EntityID, fields)
	case syncapply.Patch:
		return a.patch(ctx, tx, team, op.EntityID, op.ExpectedVersion, fields)
	case syncapply.Delete:
		return a.delete(ctx, tx, team, op.EntityID, op.ExpectedVersion)
	default: // Append makes no sense for a car
		return 0, fmt.Errorf("car operation kind: %w", syncapply.ErrInvalidFields)
	}
}

func (a *CarApplier) create(ctx context.Context, tx db.DBTX, team, user, id uuid.UUID, f syncapply.Fields) (int32, error) {
	if err := f.Only(carFields...); err != nil {
		return 0, err
	}
	make, err := f.RequiredString("make")
	if err != nil {
		return 0, err
	}
	model, err := f.RequiredString("model")
	if err != nil {
		return 0, err
	}
	_, year, err := f.Int32("model_year")
	if err != nil {
		return 0, err
	}
	_, serial, err1 := f.String("serial_number")
	_, nick, err2 := f.String("nickname")
	_, transponder, err3 := f.String("transponder_number")
	_, status, err4 := f.String("status")
	_, scale, err5 := f.String("scale")
	_, powertrain, err6 := f.String("powertrain")
	_, notes, err7 := f.String("notes")
	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7); err != nil {
		return 0, syncapply.ErrInvalidFields
	}
	car, err := domain.NewCar(make, model, year, serial, nick, transponder,
		deref(status), deref(scale), deref(powertrain), notes)
	if err != nil {
		return 0, carSyncErr(err)
	}
	if err := a.repo.ActiveCatalog(ctx, tx, car.Scale, car.Powertrain); err != nil {
		return 0, carSyncErr(err)
	}
	created, err := a.repo.CreateCarWithID(ctx, tx, team, user, id, car)
	if err != nil {
		return 0, carSyncErr(err)
	}
	return created.Version, nil
}

func (a *CarApplier) patch(ctx context.Context, tx db.DBTX, team, id uuid.UUID, expected int32, f syncapply.Fields) (int32, error) {
	if err := f.Only(carFields...); err != nil {
		return 0, err
	}
	current, err := a.repo.GetCar(ctx, tx, team, id)
	if err != nil {
		return 0, carSyncErr(err)
	}
	if current.Version != expected {
		return 0, syncapply.ErrStale
	}
	var u domain.CarUpdate
	var errs []error
	collect := func(err error) { errs = append(errs, err) }

	_, u.Make, err = f.String("make")
	collect(err)
	_, u.Model, err = f.String("model")
	collect(err)
	u.ModelYearSet, u.ModelYear, err = f.Int32("model_year")
	collect(err)
	u.SerialNumberSet, u.SerialNumber, err = f.String("serial_number")
	collect(err)
	u.NicknameSet, u.Nickname, err = f.String("nickname")
	collect(err)
	u.TransponderSet, u.TransponderNumber, err = f.String("transponder_number")
	collect(err)
	_, u.Status, err = f.String("status")
	collect(err)
	_, u.Scale, err = f.String("scale")
	collect(err)
	_, u.Powertrain, err = f.String("powertrain")
	collect(err)
	u.NotesSet, u.Notes, err = f.String("notes")
	collect(err)
	if err := errors.Join(errs...); err != nil {
		return 0, syncapply.ErrInvalidFields
	}

	next, err := domain.ApplyCarUpdate(current, u)
	if err != nil {
		return 0, carSyncErr(err)
	}
	if next.Scale != current.Scale || next.Powertrain != current.Powertrain {
		if err := a.repo.ActiveCatalog(ctx, tx, next.Scale, next.Powertrain); err != nil {
			return 0, carSyncErr(err)
		}
	}
	if next.Status == domain.AssetRetired {
		if _, open, err := a.repo.OpenMountForCar(ctx, tx, team, id); err != nil {
			return 0, err
		} else if open {
			return 0, fmt.Errorf("retire a mounted car: %w", syncapply.ErrInvalidRef)
		}
	}
	next.Version = expected
	updated, err := a.repo.UpdateCar(ctx, tx, team, next)
	if err != nil {
		return 0, carSyncErr(err)
	}
	return updated.Version, nil
}

func (a *CarApplier) delete(ctx context.Context, tx db.DBTX, team, id uuid.UUID, expected int32) (int32, error) {
	if err := a.repo.LockCar(ctx, tx, team, id); err != nil {
		return 0, carSyncErr(err)
	}
	current, err := a.repo.GetCar(ctx, tx, team, id)
	if err != nil {
		return 0, carSyncErr(err)
	}
	if current.Version != expected {
		return 0, syncapply.ErrStale
	}
	if _, open, err := a.repo.OpenMountForCar(ctx, tx, team, id); err != nil {
		return 0, err
	} else if open {
		return 0, fmt.Errorf("delete a mounted car: %w", syncapply.ErrInvalidRef)
	}
	if err := a.repo.SoftDeleteCar(ctx, tx, team, id, expected); err != nil {
		return 0, carSyncErr(err)
	}
	return expected + 1, nil
}

// carWire is what a device receives for a car. A car has no cost or role-dependent field.
type carWire struct {
	ID                uuid.UUID `json:"id"`
	Make              string    `json:"make"`
	Model             string    `json:"model"`
	ModelYear         *int32    `json:"model_year"`
	SerialNumber      *string   `json:"serial_number"`
	Nickname          *string   `json:"nickname"`
	TransponderNumber *string   `json:"transponder_number"`
	Status            string    `json:"status"`
	Scale             string    `json:"scale"`
	Powertrain        string    `json:"powertrain"`
	Notes             *string   `json:"notes"`
	CreatedAt         time.Time `json:"created_at"`
	Version           int32     `json:"version"`
}

func (a *CarApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, _ string, id uuid.UUID) (json.RawMessage, bool, error) {
	c, err := a.repo.GetCar(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := json.Marshal(carWire{
		ID: c.ID, Make: c.Make, Model: c.Model, ModelYear: c.ModelYear, SerialNumber: c.SerialNumber,
		Nickname: c.Nickname, TransponderNumber: c.TransponderNumber, Status: c.Status, Scale: c.Scale,
		Powertrain: c.Powertrain, Notes: c.Notes, CreatedAt: c.CreatedAt, Version: c.Version,
	})
	return raw, err == nil, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// carSyncErr turns garage domain errors into the sync contract. Anything else is left
// alone (a database failure is an error of the whole Push, not a rejection).
func carSyncErr(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return syncapply.ErrNotFound
	case errors.Is(err, domain.ErrConflict):
		return syncapply.ErrStale
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, domain.ErrAlreadyExists):
		return syncapply.ErrInvalidFields
	case errors.Is(err, domain.ErrCatalogInactive):
		return syncapply.ErrCatalogRetired
	case errors.Is(err, domain.ErrOpenMount), errors.Is(err, domain.ErrMountConflict), errors.Is(err, domain.ErrInstallConflict):
		return syncapply.ErrInvalidRef
	default:
		return err
	}
}
