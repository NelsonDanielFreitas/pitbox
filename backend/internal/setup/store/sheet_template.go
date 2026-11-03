package store

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"context"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/store/sqlcgen"
)

// info is the common shape of the template rows sqlc generates per query.
type info struct {
	ID             uuid.UUID
	TeamID         *uuid.UUID
	Slug           string
	Make           string
	Model          string
	Scale          string
	Powertrain     string
	Name           string
	SourceUrl      *string
	CurrentVersion int32
	BasedOnID      *uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int32
}

func (i info) domain() domain.SheetTemplateInfo {
	return domain.SheetTemplateInfo{
		ID: i.ID, Global: i.TeamID == nil, Slug: i.Slug, Make: i.Make, Model: i.Model, Scale: i.Scale,
		Powertrain: i.Powertrain, Name: i.Name, SourceURL: i.SourceUrl, CurrentVersion: i.CurrentVersion,
		BasedOnID: i.BasedOnID, CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt, Version: i.Version,
	}
}

// ListSheetTemplates returns the global catalogue and the team's own, by id, after the cursor.
func (Repo) ListSheetTemplates(ctx context.Context, tx db.DBTX, team, after uuid.UUID, limit int32) ([]domain.SheetTemplateInfo, error) {
	rows, err := sqlcgen.New(tx).ListSheetTemplates(ctx, sqlcgen.ListSheetTemplatesParams{TeamID: &team, ID: after, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list sheet templates: %w", err)
	}
	out := make([]domain.SheetTemplateInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, info(r).domain())
	}
	return out, nil
}

func (Repo) GetSheetTemplate(ctx context.Context, tx db.DBTX, team, id uuid.UUID) (domain.SheetTemplateInfo, error) {
	r, err := sqlcgen.New(tx).GetSheetTemplate(ctx, sqlcgen.GetSheetTemplateParams{TeamID: &team, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SheetTemplateInfo{}, fmt.Errorf("get sheet template: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.SheetTemplateInfo{}, fmt.Errorf("get sheet template: %w", err)
	}
	return info(r).domain(), nil
}

func (Repo) GetSheetTemplateVersion(ctx context.Context, tx db.DBTX, team, id uuid.UUID, version int32) (domain.SheetTemplateVersionRow, error) {
	r, err := sqlcgen.New(tx).GetSheetTemplateVersion(ctx, sqlcgen.GetSheetTemplateVersionParams{TeamID: &team, TemplateID: id, Version: version})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SheetTemplateVersionRow{}, fmt.Errorf("get sheet template version: %w", domain.ErrNotFound)
	}
	if err != nil {
		return domain.SheetTemplateVersionRow{}, fmt.Errorf("get sheet template version: %w", err)
	}
	return domain.SheetTemplateVersionRow{TemplateID: r.TemplateID, Version: r.Version, Definition: r.Definition, CreatedAt: r.CreatedAt, Global: r.IsGlobal}, nil
}

// CreateTeamSheetTemplate stores a team template and its version 1 in the caller's
// transaction (the current_version foreign key is deferred, so the order is free).
func (Repo) CreateTeamSheetTemplate(ctx context.Context, tx db.DBTX, team, user uuid.UUID, t domain.SheetTemplateInfo, definition []byte) (domain.SheetTemplateInfo, error) {
	q := sqlcgen.New(tx)
	r, err := q.CreateTeamSheetTemplate(ctx, sqlcgen.CreateTeamSheetTemplateParams{
		ID: t.ID, TeamID: &team, Slug: t.Slug, Make: t.Make, Model: t.Model, Scale: t.Scale,
		Powertrain: t.Powertrain, Name: t.Name, SourceUrl: t.SourceURL, CurrentVersion: 1,
		BasedOnID: t.BasedOnID, CreatedBy: &user,
	})
	if err != nil {
		return domain.SheetTemplateInfo{}, mapError("create sheet template", err)
	}
	if err := q.CreateSheetTemplateVersion(ctx, sqlcgen.CreateSheetTemplateVersionParams{TemplateID: t.ID, Version: 1, Definition: definition}); err != nil {
		return domain.SheetTemplateInfo{}, mapError("create sheet template version", err)
	}
	return info(r).domain(), nil
}

// UpdateTeamSheetTemplate renames or soft-deletes a team template (optimistic concurrency).
func (Repo) UpdateTeamSheetTemplate(ctx context.Context, tx db.DBTX, team, id uuid.UUID, expected int32, name string, deletedAt *time.Time) (domain.SheetTemplateInfo, error) {
	r, err := sqlcgen.New(tx).UpdateTeamSheetTemplate(ctx, sqlcgen.UpdateTeamSheetTemplateParams{
		TeamID: &team, ID: id, Version: expected, Name: name, DeletedAt: deletedAt,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SheetTemplateInfo{}, domain.ErrConflict
	}
	if err != nil {
		return domain.SheetTemplateInfo{}, mapError("update sheet template", err)
	}
	return info(r).domain(), nil
}
