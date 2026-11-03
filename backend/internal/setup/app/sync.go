package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// The sync module writes setups through these two appliers. They reuse the rules of the
// normal use cases (schema v1, references to the garage, immutable versions) but not their
// authorization or idempotency: sync has already checked the caller's current role and keeps
// op_id as the idempotency key.
//
// Entities:
//   - "setup.setup": Create only (a setup with its version 1). Renaming is not part of the
//     server API yet, so Patch and Delete are refused.
//   - "setup.setup_version": the entity id is the SETUP id. A Patch with base_version is
//     "save a new version based on version N"; a stale base is a conflict for the mechanic
//     (ADR 0024). Versions are immutable, so there is no Create or Delete.

// maxPullVersions bounds how many versions one Pull row carries for a setup.
const maxPullVersions = 20

type SetupApplier struct{ svc *Service }

func NewSetupApplier(svc *Service) *SetupApplier { return &SetupApplier{svc: svc} }

var _ syncapply.Applier = (*SetupApplier)(nil)

func (*SetupApplier) Entity() string { return "setup.setup" }

func (a *SetupApplier) CurrentVersion(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (int32, error) {
	s, err := a.svc.store.GetSetup(ctx, tx, uuid.UUID(team), id)
	if err != nil {
		return 0, setupSyncErr(err)
	}
	return s.Version, nil
}

func (a *SetupApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	if op.Kind != syncapply.Create {
		return 0, fmt.Errorf("setup operation kind: %w", syncapply.ErrInvalidFields)
	}
	team, user := op.TeamID, op.UserID
	f := syncapply.Fields(op.Fields)
	if err := f.Only("car_id", "name", "notes", "source_setup_id", "source_version", "sheet_template_id", "sheet_template_version"); err != nil {
		return 0, err
	}
	carRaw, err := f.RequiredString("car_id")
	if err != nil {
		return 0, err
	}
	carID, err := uuid.Parse(carRaw)
	if err != nil || carID == uuid.Nil {
		return 0, fmt.Errorf("car id: %w", syncapply.ErrInvalidFields)
	}
	rawName, err := f.RequiredString("name")
	if err != nil {
		return 0, err
	}
	_, rawNotes, err1 := f.String("notes")
	_, srcRaw, err2 := f.String("source_setup_id")
	_, srcVersion, err3 := f.Int32("source_version")
	_, tplRaw, err4 := f.String("sheet_template_id")
	_, tplVersion, err5 := f.Int32("sheet_template_version")
	if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
		return 0, syncapply.ErrInvalidFields
	}
	name, err := domain.NormalizeName(rawName)
	if err != nil {
		return 0, setupSyncErr(err)
	}
	notes, err := domain.NormalizeNotes(rawNotes)
	if err != nil {
		return 0, setupSyncErr(err)
	}

	if ok, err := a.svc.garage.CarExists(ctx, tx, team, carID); err != nil {
		return 0, fmt.Errorf("check car: %w", err)
	} else if !ok {
		return 0, fmt.Errorf("car: %w", syncapply.ErrInvalidRef)
	}

	var src *uuid.UUID
	if srcRaw != nil {
		id, err := uuid.Parse(*srcRaw)
		if err != nil || id == uuid.Nil {
			return 0, fmt.Errorf("source setup id: %w", syncapply.ErrInvalidFields)
		}
		src = &id
	} else if srcVersion != nil {
		return 0, fmt.Errorf("source version without setup: %w", syncapply.ErrInvalidFields)
	}
	var tref *TemplateRef
	if tplRaw != nil {
		id, err := uuid.Parse(*tplRaw)
		if err != nil || id == uuid.Nil {
			return 0, fmt.Errorf("sheet template id: %w", syncapply.ErrInvalidFields)
		}
		if tplVersion != nil && *tplVersion < 1 {
			return 0, fmt.Errorf("sheet template version: %w", syncapply.ErrInvalidFields)
		}
		tref = &TemplateRef{ID: id, Version: tplVersion}
	} else if tplVersion != nil {
		return 0, fmt.Errorf("sheet template version without template: %w", syncapply.ErrInvalidFields)
	}
	initial, err := a.svc.prepareInitial(ctx, tx, team, tref, src, srcVersion)
	if err != nil {
		return 0, setupSyncRefErr(err)
	}

	created, err := a.svc.store.CreateSetupWithID(ctx, tx, uuid.UUID(team), uuid.UUID(user), op.EntityID, carID, name, notes)
	if err != nil {
		return 0, setupSyncErr(err)
	}
	if _, err := a.svc.store.CreateVersion(ctx, tx, uuid.UUID(team), uuid.UUID(user), op.EntityID, 1, initial.schema, initial.raw, initial.basedSetup, initial.basedVersion, nil); err != nil {
		return 0, setupSyncErr(err)
	}
	return created.Version, nil
}

type setupWire struct {
	ID             string    `json:"id"`
	CarID          string    `json:"car_id"`
	Name           string    `json:"name"`
	CurrentVersion int32     `json:"current_version"`
	Notes          *string   `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Version        int32     `json:"version"`
}

func (a *SetupApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, _ string, id uuid.UUID) (json.RawMessage, bool, error) {
	s, err := a.svc.store.GetSetup(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := json.Marshal(setupWire{ID: s.ID, CarID: s.CarID, Name: s.Name, CurrentVersion: s.CurrentVersion,
		Notes: s.Notes, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, Version: s.Version})
	return raw, err == nil, err
}

// VersionApplier handles "setup.setup_version".
type VersionApplier struct{ svc *Service }

func NewVersionApplier(svc *Service) *VersionApplier { return &VersionApplier{svc: svc} }

var _ syncapply.Applier = (*VersionApplier)(nil)

func (*VersionApplier) Entity() string { return "setup.setup_version" }

// AtomicPatch: a version is its data. If the data clashes with a newer version, the summary
// alone is not a version.
func (*VersionApplier) AtomicPatch() bool { return true }

// CurrentVersion is the setup's current VERSION NUMBER: what a device's base_version means.
func (a *VersionApplier) CurrentVersion(ctx context.Context, tx db.DBTX, team db.TeamID, setupID uuid.UUID) (int32, error) {
	s, err := a.svc.store.GetSetup(ctx, tx, uuid.UUID(team), setupID)
	if err != nil {
		return 0, setupSyncErr(err)
	}
	return s.CurrentVersion, nil
}

func (a *VersionApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	if op.Kind != syncapply.Patch {
		return 0, fmt.Errorf("version operation kind: %w", syncapply.ErrInvalidFields)
	}
	f := syncapply.Fields(op.Fields)
	if err := f.Only("data", "change_summary", "schema_version"); err != nil {
		return 0, err
	}
	data, err := f.Object("data")
	if err != nil {
		return 0, err
	}
	_, rawSummary, err := f.String("change_summary")
	if err != nil {
		return 0, err
	}
	// An operation queued before F2-00 has no schema_version: it is v1.
	_, rawSchema, err := f.Int32("schema_version")
	if err != nil {
		return 0, err
	}
	schema := int32(0)
	if rawSchema != nil {
		schema = *rawSchema
	}
	schema = normalizeSchema(schema)
	if schema != domain.SchemaVersion && schema != domain.SchemaVersionV2 {
		return 0, fmt.Errorf("schema version: %w", syncapply.ErrInvalidFields)
	}
	summary, err := domain.NormalizeChangeSummary(rawSummary)
	if err != nil {
		return 0, setupSyncErr(err)
	}

	team, user, setupID, base := uuid.UUID(op.TeamID), uuid.UUID(op.UserID), op.EntityID, op.ExpectedVersion
	current, err := a.svc.store.LockSetup(ctx, tx, team, setupID)
	if err != nil {
		return 0, setupSyncErr(err)
	}
	if current.CurrentVersion != base {
		return 0, syncapply.ErrStale
	}
	baseVersion, err := a.svc.store.GetVersion(ctx, tx, team, setupID, base)
	if err != nil {
		return 0, setupSyncErr(err)
	}
	if baseVersion.Ref.SchemaVersion == domain.SchemaVersionV2 && schema != domain.SchemaVersionV2 {
		return 0, fmt.Errorf("a template setup cannot go back to schema v1: %w", syncapply.ErrInvalidFields)
	}
	raw, err := a.svc.marshalVersionData(ctx, tx, op.TeamID, schema, data)
	if err != nil {
		return 0, setupSyncRefErr(err)
	}
	if err := a.svc.validateChangedReferences(ctx, tx, op.TeamID, baseVersion.Data, data); err != nil {
		return 0, setupSyncRefErr(err)
	}
	next := base + 1
	if _, err := a.svc.store.CreateVersion(ctx, tx, team, user, setupID, next, schema, raw, &setupID, &base, summary); err != nil {
		return 0, setupSyncErr(err)
	}
	if _, err := a.svc.store.UpdateCurrentVersion(ctx, tx, team, setupID, next, base); err != nil {
		return 0, setupSyncErr(err)
	}
	return next, nil
}

type versionWire struct {
	Version       int32          `json:"version"`
	SchemaVersion int32          `json:"schema_version"`
	BasedOnSetup  *string        `json:"based_on_setup_id"`
	BasedOnNumber *int32         `json:"based_on_version"`
	ChangeSummary *string        `json:"change_summary"`
	CreatedAt     time.Time      `json:"created_at"`
	Data          map[string]any `json:"data"`
}

// Read returns the most recent versions of the setup (up to maxPullVersions). The change log
// names the setup, not the version, so the row carries the versions themselves.
func (a *VersionApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, _ string, setupID uuid.UUID) (json.RawMessage, bool, error) {
	if _, err := a.svc.store.GetSetup(ctx, tx, uuid.UUID(team), setupID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	refs, err := a.svc.store.ListVersions(ctx, tx, uuid.UUID(team), setupID, math.MaxInt32, maxPullVersions)
	if err != nil {
		return nil, false, err
	}
	out := make([]versionWire, 0, len(refs))
	for _, r := range refs {
		v, err := a.svc.store.GetVersion(ctx, tx, uuid.UUID(team), setupID, r.Version)
		if err != nil {
			return nil, false, err
		}
		out = append(out, versionWire{Version: r.Version, SchemaVersion: r.SchemaVersion,
			BasedOnSetup: r.BasedOnSetupID, BasedOnNumber: r.BasedOnVersion, ChangeSummary: r.ChangeSummary,
			CreatedAt: r.CreatedAt, Data: v.Data})
	}
	raw, err := json.Marshal(struct {
		SetupID  string        `json:"setup_id"`
		Versions []versionWire `json:"versions"`
	}{setupID.String(), out})
	return raw, err == nil, err
}

// setupSyncErr turns setup domain errors into the sync contract.
func setupSyncErr(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return syncapply.ErrNotFound
	case errors.Is(err, domain.ErrConflict):
		return syncapply.ErrStale
	case errors.Is(err, domain.ErrInvalid):
		return syncapply.ErrInvalidFields
	default:
		return err
	}
}

// setupSyncRefErr is for lookups of something the data refers to: missing means a bad
// reference, not a missing entity.
func setupSyncRefErr(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return syncapply.ErrInvalidRef
	}
	return setupSyncErr(err)
}
