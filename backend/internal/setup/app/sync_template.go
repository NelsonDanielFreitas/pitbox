package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// TemplateApplier handles "setup.sheet_template": the TEAM's templates (the global catalogue
// travels in the Pull catalogue snapshot instead). Operations:
//   - Create: duplicate a global template under the id the device chose. Fields: based_on_id.
//     The slug and the definition are derived from the global one and the new id (the device
//     derives the same), so a client cannot send either.
//   - Patch: rename. Fields: name.
//   - Delete: soft delete.
//
// A global template is never writable: it reads as not found here.
type TemplateApplier struct{ svc *Service }

func NewTemplateApplier(svc *Service) *TemplateApplier { return &TemplateApplier{svc: svc} }

var _ syncapply.Applier = (*TemplateApplier)(nil)

func (*TemplateApplier) Entity() string { return "setup.sheet_template" }

func (a *TemplateApplier) own(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (domain.SheetTemplateInfo, error) {
	info, err := a.svc.store.GetSheetTemplate(ctx, tx, uuid.UUID(team), id)
	if err != nil {
		return domain.SheetTemplateInfo{}, setupSyncErr(err)
	}
	if info.Global {
		return domain.SheetTemplateInfo{}, syncapply.ErrNotFound
	}
	return info, nil
}

func (a *TemplateApplier) CurrentVersion(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (int32, error) {
	info, err := a.own(ctx, tx, team, id)
	if err != nil {
		return 0, err
	}
	return info.Version, nil
}

func (a *TemplateApplier) Apply(ctx context.Context, tx db.DBTX, op syncapply.Op) (int32, error) {
	f := syncapply.Fields(op.Fields)
	switch op.Kind {
	case syncapply.Create:
		if err := f.Only("based_on_id"); err != nil {
			return 0, err
		}
		raw, err := f.RequiredString("based_on_id")
		if err != nil {
			return 0, err
		}
		src, err := uuid.Parse(raw)
		if err != nil || src == uuid.Nil || op.EntityID == uuid.Nil {
			return 0, fmt.Errorf("based_on_id: %w", syncapply.ErrInvalidFields)
		}
		created, err := a.svc.duplicate(ctx, tx, op.TeamID, op.UserID, src, op.EntityID)
		if err != nil {
			return 0, setupSyncRefErr(err)
		}
		return created.Version, nil
	case syncapply.Patch:
		if err := f.Only("name"); err != nil {
			return 0, err
		}
		name, err := f.RequiredString("name")
		if err != nil {
			return 0, err
		}
		if name, err = normalizeTemplateName(name); err != nil {
			return 0, setupSyncErr(err)
		}
		return a.update(ctx, tx, op, name, nil)
	case syncapply.Delete:
		cur, err := a.own(ctx, tx, op.TeamID, op.EntityID)
		if err != nil {
			return 0, err
		}
		now := time.Now().UTC()
		return a.update(ctx, tx, op, cur.Name, &now)
	default:
		return 0, fmt.Errorf("template operation kind: %w", syncapply.ErrInvalidFields)
	}
}

func (a *TemplateApplier) update(ctx context.Context, tx db.DBTX, op syncapply.Op, name string, deletedAt *time.Time) (int32, error) {
	cur, err := a.own(ctx, tx, op.TeamID, op.EntityID)
	if err != nil {
		return 0, err
	}
	if cur.Version != op.ExpectedVersion {
		return 0, syncapply.ErrStale
	}
	updated, err := a.svc.store.UpdateTeamSheetTemplate(ctx, tx, uuid.UUID(op.TeamID), op.EntityID, op.ExpectedVersion, name, deletedAt)
	if err != nil {
		return 0, setupSyncErr(err)
	}
	return updated.Version, nil
}

func normalizeTemplateName(v string) (string, error) {
	n, err := domain.NormalizeName(v)
	if err != nil {
		return "", err
	}
	return n, nil
}

type templateWire struct {
	ID             string         `json:"id"`
	Slug           string         `json:"slug"`
	Make           string         `json:"make"`
	Model          string         `json:"model"`
	Scale          string         `json:"scale"`
	Powertrain     string         `json:"powertrain"`
	Name           string         `json:"name"`
	SourceURL      *string        `json:"source_url"`
	CurrentVersion int32          `json:"current_version"`
	BasedOnID      *string        `json:"based_on_id"`
	Version        int32          `json:"version"`
	Versions       []templateWver `json:"versions"`
}

type templateWver struct {
	Version    int32           `json:"version"`
	Definition json.RawMessage `json:"definition"`
}

// Read returns the template with its versions (a team template has one in this release).
func (a *TemplateApplier) Read(ctx context.Context, tx db.DBTX, team db.TeamID, _ string, id uuid.UUID) (json.RawMessage, bool, error) {
	info, err := a.svc.store.GetSheetTemplate(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if info.Global {
		return nil, false, nil
	}
	row, err := a.svc.store.GetSheetTemplateVersion(ctx, tx, uuid.UUID(team), id, info.CurrentVersion)
	if err != nil {
		return nil, false, err
	}
	w := templateWire{
		ID: info.ID.String(), Slug: info.Slug, Make: info.Make, Model: info.Model, Scale: info.Scale,
		Powertrain: info.Powertrain, Name: info.Name, SourceURL: info.SourceURL,
		CurrentVersion: info.CurrentVersion, Version: info.Version,
		Versions: []templateWver{{Version: row.Version, Definition: row.Definition}},
	}
	if info.BasedOnID != nil {
		s := info.BasedOnID.String()
		w.BasedOnID = &s
	}
	raw, err := json.Marshal(w)
	return raw, err == nil, err
}
