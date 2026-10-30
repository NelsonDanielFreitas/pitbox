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

// Appliers for engines, tyre sets and parts (F1-13, ADR 0025). Like CarApplier they reuse the
// domain rules of the normal use cases but not their authorization or idempotency: the sync
// module has already checked the caller's CURRENT role and keeps op_id as the idempotency key.
//
// Not handled here (later F1-13 parts): stock movements (so a part created with an initial
// quantity gets that quantity from its own "purchase" movement), engine mounts, part
// installs and the shopping list. A tyre set made from stock parts (glue) is created without
// the part references; the movements that consume the parts arrive with the stock appliers.

// SyncAppliers are all the garage entities that sync. An entity that is not here is not
// syncable (the allow-list of ADR 0025); the sync module never learns their names otherwise.
func SyncAppliers() []syncapply.Applier {
	return []syncapply.Applier{
		NewCarApplier(), NewEngineApplier(), NewTyreSetApplier(), NewPartApplier(),
		NewStockMovementApplier(), NewShoppingItemApplier(),
		NewEngineMountApplier(), NewPartInstallApplier(),
	}
}

// patchString reads a required string of a Patch: set but null is invalid (it cannot be cleared).
func patchString(f syncapply.Fields, key string) (*string, error) {
	set, v, err := f.String(key)
	if err != nil {
		return nil, err
	}
	if set && v == nil {
		return nil, fmt.Errorf("field %s cannot be cleared: %w", key, syncapply.ErrInvalidFields)
	}
	return v, nil
}

func dateString(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

// canSeeCosts: purchase prices are hidden from pilots and guests (ADR 0023), also in the Pull.
func canSeeCosts(role string) bool { return role == "owner" || role == "mechanic" }

// ---------------------------------------------------------------------------- engines

type EngineApplier struct{ repo *store.Repo }

func NewEngineApplier() *EngineApplier { return &EngineApplier{repo: store.NewRepo()} }

var _ syncapply.Applier = (*EngineApplier)(nil)

func (*EngineApplier) Entity() string { return "garage.engine" }

var engineFields = []string{"make", "model", "serial_number", "purchased_on", "status", "notes"}

func (a *EngineApplier) CurrentVersion(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (int32, error) {
	e, err := a.repo.GetEngine(ctx, tx, uuid.UUID(team), id)
	if err != nil {
		return 0, carSyncErr(err)
	}
	return e.Version, nil
}

func (a *EngineApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	f := syncapply.Fields(op.Fields)
	if err := f.Only(engineFields...); err != nil {
		return 0, err
	}
	team, user := uuid.UUID(op.TeamID), uuid.UUID(op.UserID)
	switch op.Kind {
	case syncapply.Create:
		make, err := f.RequiredString("make")
		if err != nil {
			return 0, err
		}
		model, err := f.RequiredString("model")
		if err != nil {
			return 0, err
		}
		_, serial, err1 := f.String("serial_number")
		_, purchased, err2 := f.String("purchased_on")
		_, status, err3 := f.String("status")
		_, notes, err4 := f.String("notes")
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		e, err := domain.NewEngine(make, model, serial, purchased, deref(status), notes)
		if err != nil {
			return 0, carSyncErr(err)
		}
		created, err := a.repo.CreateEngineWithID(ctx, tx, team, user, op.EntityID, e)
		if err != nil {
			return 0, carSyncErr(err)
		}
		return created.Version, nil
	case syncapply.Patch:
		current, err := a.repo.GetEngine(ctx, tx, team, op.EntityID)
		if err != nil {
			return 0, carSyncErr(err)
		}
		if current.Version != op.ExpectedVersion {
			return 0, syncapply.ErrStale
		}
		var u domain.EngineUpdate
		var errs [6]error
		u.Make, errs[0] = patchString(f, "make")
		u.Model, errs[1] = patchString(f, "model")
		u.SerialSet, u.SerialNumber, errs[2] = f.String("serial_number")
		u.PurchasedSet, u.PurchasedOn, errs[3] = f.String("purchased_on")
		u.NotesSet, u.Notes, errs[4] = f.String("notes")
		u.Status, errs[5] = patchString(f, "status")
		if err := errors.Join(errs[:]...); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		next, err := domain.ApplyEngineUpdate(current, u)
		if err != nil {
			return 0, carSyncErr(err)
		}
		if next.Status == domain.AssetRetired {
			if _, open, err := a.repo.OpenMountForEngine(ctx, tx, team, next.ID); err != nil {
				return 0, err
			} else if open {
				return 0, fmt.Errorf("retire a mounted engine: %w", syncapply.ErrInvalidRef)
			}
		}
		next.Version = op.ExpectedVersion
		updated, err := a.repo.UpdateEngine(ctx, tx, team, next)
		if err != nil {
			return 0, carSyncErr(err)
		}
		return updated.Version, nil
	default: // Delete and Append are not part of the engine contract yet
		return 0, fmt.Errorf("engine operation kind: %w", syncapply.ErrInvalidFields)
	}
}

type engineWire struct {
	ID           uuid.UUID `json:"id"`
	Make         string    `json:"make"`
	Model        string    `json:"model"`
	SerialNumber *string   `json:"serial_number"`
	PurchasedOn  *string   `json:"purchased_on"`
	Status       string    `json:"status"`
	Notes        *string   `json:"notes"`
	CreatedAt    time.Time `json:"created_at"`
	Version      int32     `json:"version"`
}

func (a *EngineApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, _ string, id uuid.UUID) (json.RawMessage, bool, error) {
	e, err := a.repo.GetEngine(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := json.Marshal(engineWire{ID: e.ID, Make: e.Make, Model: e.Model, SerialNumber: e.SerialNumber,
		PurchasedOn: dateString(e.PurchasedOn), Status: e.Status, Notes: e.Notes, CreatedAt: e.CreatedAt, Version: e.Version})
	return raw, err == nil, err
}

// ---------------------------------------------------------------------------- tyre sets

type TyreSetApplier struct{ repo *store.Repo }

func NewTyreSetApplier() *TyreSetApplier { return &TyreSetApplier{repo: store.NewRepo()} }

var _ syncapply.Applier = (*TyreSetApplier)(nil)

func (*TyreSetApplier) Entity() string { return "garage.tyre_set" }

var tyreFields = []string{"label", "make", "model", "compound", "insert_type", "glued_on", "status", "wear_notes"}

func (a *TyreSetApplier) CurrentVersion(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (int32, error) {
	t, err := a.repo.GetTyreSet(ctx, tx, uuid.UUID(team), id)
	if err != nil {
		return 0, carSyncErr(err)
	}
	return t.Version, nil
}

func (a *TyreSetApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	f := syncapply.Fields(op.Fields)
	if err := f.Only(tyreFields...); err != nil {
		return 0, err
	}
	team, user := uuid.UUID(op.TeamID), uuid.UUID(op.UserID)
	switch op.Kind {
	case syncapply.Create:
		label, err := f.RequiredString("label")
		if err != nil {
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
		_, compound, err1 := f.String("compound")
		_, insert, err2 := f.String("insert_type")
		_, glued, err3 := f.String("glued_on")
		_, status, err4 := f.String("status")
		_, wear, err5 := f.String("wear_notes")
		if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		t, err := domain.NewTyreSet(label, make, model, compound, insert, glued, deref(status), wear)
		if err != nil {
			return 0, carSyncErr(err)
		}
		created, err := a.repo.CreateTyreSetWithID(ctx, tx, team, user, op.EntityID, t)
		if err != nil {
			return 0, carSyncErr(err)
		}
		return created.Version, nil
	case syncapply.Patch:
		current, err := a.repo.GetTyreSet(ctx, tx, team, op.EntityID)
		if err != nil {
			return 0, carSyncErr(err)
		}
		if current.Version != op.ExpectedVersion {
			return 0, syncapply.ErrStale
		}
		var u domain.TyreUpdate
		var errs [8]error
		u.Label, errs[0] = patchString(f, "label")
		u.Make, errs[1] = patchString(f, "make")
		u.Model, errs[2] = patchString(f, "model")
		u.CompoundSet, u.Compound, errs[3] = f.String("compound")
		u.InsertSet, u.InsertType, errs[4] = f.String("insert_type")
		u.GluedSet, u.GluedOn, errs[5] = f.String("glued_on")
		u.Status, errs[6] = patchString(f, "status")
		u.WearSet, u.WearNotes, errs[7] = f.String("wear_notes")
		if err := errors.Join(errs[:]...); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		next, err := domain.ApplyTyreUpdate(current, u)
		if err != nil {
			return 0, carSyncErr(err)
		}
		next.Version = op.ExpectedVersion
		updated, err := a.repo.UpdateTyreSet(ctx, tx, team, next)
		if err != nil {
			return 0, carSyncErr(err)
		}
		return updated.Version, nil
	default:
		return 0, fmt.Errorf("tyre set operation kind: %w", syncapply.ErrInvalidFields)
	}
}

type tyreWire struct {
	ID         uuid.UUID `json:"id"`
	Label      string    `json:"label"`
	Make       string    `json:"make"`
	Model      string    `json:"model"`
	Compound   *string   `json:"compound"`
	InsertType *string   `json:"insert_type"`
	GluedOn    *string   `json:"glued_on"`
	Status     string    `json:"status"`
	WearNotes  *string   `json:"wear_notes"`
	CreatedAt  time.Time `json:"created_at"`
	Version    int32     `json:"version"`
}

func (a *TyreSetApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, _ string, id uuid.UUID) (json.RawMessage, bool, error) {
	t, err := a.repo.GetTyreSet(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := json.Marshal(tyreWire{ID: t.ID, Label: t.Label, Make: t.Make, Model: t.Model, Compound: t.Compound,
		InsertType: t.InsertType, GluedOn: dateString(t.GluedOn), Status: t.Status, WearNotes: t.WearNotes,
		CreatedAt: t.CreatedAt, Version: t.Version})
	return raw, err == nil, err
}

// ---------------------------------------------------------------------------- parts

type PartApplier struct{ repo *store.Repo }

func NewPartApplier() *PartApplier { return &PartApplier{repo: store.NewRepo()} }

var _ syncapply.Applier = (*PartApplier)(nil)

func (*PartApplier) Entity() string { return "garage.part" }

var partFields = []string{
	"kind", "brand", "reference", "description", "unit", "specs", "min_stock_centi",
	"unit_cost_cents", "currency", "has_service_life", "reorder_to_centi",
}

func (a *PartApplier) CurrentVersion(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (int32, error) {
	p, err := a.repo.GetPart(ctx, tx, uuid.UUID(team), id)
	if err != nil {
		return 0, carSyncErr(err)
	}
	return p.Version, nil
}

func (a *PartApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	f := syncapply.Fields(op.Fields)
	if err := f.Only(partFields...); err != nil {
		return 0, err
	}
	team, user := uuid.UUID(op.TeamID), uuid.UUID(op.UserID)
	switch op.Kind {
	case syncapply.Create:
		kind, err := f.RequiredString("kind")
		if err != nil {
			return 0, err
		}
		description, err := f.RequiredString("description")
		if err != nil {
			return 0, err
		}
		_, brand, err1 := f.String("brand")
		_, reference, err2 := f.String("reference")
		_, unit, err3 := f.String("unit")
		_, specs, err4 := f.OptionalObject("specs")
		_, min, err5 := f.Int64("min_stock_centi")
		_, cost, err6 := f.Int32("unit_cost_cents")
		_, currency, err7 := f.String("currency")
		_, service, err8 := f.Bool("has_service_life")
		_, reorder, err9 := f.Int64("reorder_to_centi")
		if err := errors.Join(err1, err2, err3, err4, err5, err6, err7, err8, err9); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		minValue := int64(0)
		if min != nil {
			minValue = *min
		}
		p, err := domain.NewPart(kind, brand, reference, description, deref(unit), specs, minValue, cost,
			deref(currency), service != nil && *service, reorder)
		if err != nil {
			return 0, carSyncErr(err)
		}
		created, err := a.repo.CreatePartWithID(ctx, tx, team, user, op.EntityID, p)
		if err != nil {
			return 0, carSyncErr(err)
		}
		return created.Version, nil
	case syncapply.Patch:
		current, err := a.repo.GetPart(ctx, tx, team, op.EntityID)
		if err != nil {
			return 0, carSyncErr(err)
		}
		if current.Version != op.ExpectedVersion {
			return 0, syncapply.ErrStale
		}
		var u domain.PartUpdate
		var errs [11]error
		u.Kind, errs[0] = patchString(f, "kind")
		u.BrandSet, u.Brand, errs[1] = f.String("brand")
		u.ReferenceSet, u.Reference, errs[2] = f.String("reference")
		u.Description, errs[3] = patchString(f, "description")
		u.Unit, errs[4] = patchString(f, "unit")
		u.SpecsSet, u.Specs, errs[5] = f.OptionalObject("specs")
		var minPtr *int64
		u.MinSet, minPtr, errs[6] = f.Int64("min_stock_centi")
		if u.MinSet && minPtr == nil {
			errs[6] = syncapply.ErrInvalidFields
		} else if minPtr != nil {
			u.MinStockCenti = *minPtr
		}
		u.CostSet, u.UnitCostCents, errs[7] = f.Int32("unit_cost_cents")
		u.Currency, errs[8] = patchString(f, "currency")
		var service *bool
		var serviceSet bool
		serviceSet, service, errs[9] = f.Bool("has_service_life")
		if serviceSet && service == nil {
			errs[9] = syncapply.ErrInvalidFields
		}
		u.ServiceLife = service
		u.ReorderSet, u.ReorderTo, errs[10] = f.Int64("reorder_to_centi")
		if err := errors.Join(errs[:]...); err != nil {
			return 0, syncapply.ErrInvalidFields
		}
		next, err := domain.ApplyPartUpdate(current, u)
		if err != nil {
			return 0, carSyncErr(err)
		}
		next.Version = op.ExpectedVersion
		updated, err := a.repo.UpdatePart(ctx, tx, team, next)
		if err != nil {
			return 0, carSyncErr(err)
		}
		return updated.Version, nil
	case syncapply.Delete:
		current, err := a.repo.GetPart(ctx, tx, team, op.EntityID)
		if err != nil {
			return 0, carSyncErr(err)
		}
		if current.Version != op.ExpectedVersion {
			return 0, syncapply.ErrStale
		}
		if err := a.repo.SoftDeletePart(ctx, tx, team, op.EntityID, op.ExpectedVersion); err != nil {
			return 0, carSyncErr(err)
		}
		// The soft delete never fires the foreign key cascade, so the shopping line goes explicitly.
		if _, err := a.repo.DeleteShoppingItemForPart(ctx, tx, team, op.EntityID); err != nil {
			return 0, err
		}
		return op.ExpectedVersion + 1, nil
	default:
		return 0, fmt.Errorf("part operation kind: %w", syncapply.ErrInvalidFields)
	}
}

type partWire struct {
	ID             uuid.UUID      `json:"id"`
	Kind           string         `json:"kind"`
	Brand          *string        `json:"brand"`
	Reference      *string        `json:"reference"`
	Description    string         `json:"description"`
	Unit           string         `json:"unit"`
	Specs          map[string]any `json:"specs"`
	MinStockCenti  int64          `json:"min_stock_centi"`
	UnitCostCents  *int32         `json:"unit_cost_cents,omitempty"`
	Currency       string         `json:"currency"`
	HasServiceLife bool           `json:"has_service_life"`
	ReorderToCenti *int64         `json:"reorder_to_centi"`
	CreatedAt      time.Time      `json:"created_at"`
	Version        int32          `json:"version"`
}

// Read hides the purchase price from pilots and guests: the field is left out, not zeroed.
func (a *PartApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, role string, id uuid.UUID) (json.RawMessage, bool, error) {
	p, err := a.repo.GetPart(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	w := partWire{ID: p.ID, Kind: p.Kind, Brand: p.Brand, Reference: p.Reference, Description: p.Description,
		Unit: p.Unit, Specs: p.Specs, MinStockCenti: p.MinStockCenti, Currency: p.Currency,
		HasServiceLife: p.HasServiceLife, ReorderToCenti: p.ReorderToCenti, CreatedAt: p.CreatedAt, Version: p.Version}
	if canSeeCosts(role) {
		w.UnitCostCents = p.UnitCostCents
	}
	raw, err := json.Marshal(w)
	return raw, err == nil, err
}
