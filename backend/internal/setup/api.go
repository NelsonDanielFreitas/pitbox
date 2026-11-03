// Package setup is the setup module. Other modules use only this file.
package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/store/sqlcgen"
)

// TemplateDirectory answers whether a sheet template is usable by a team: a global one, or one
// of the team's own, not deleted. The caller passes its own transaction, so the RLS context is
// the caller's; the database trigger on garage.car is the second barrier.
type TemplateDirectory struct{ repo *store.Repo }

func NewTemplateDirectory() *TemplateDirectory { return &TemplateDirectory{repo: store.NewRepo()} }

func (d *TemplateDirectory) Visible(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID) (bool, error) {
	_, err := d.repo.GetSheetTemplate(ctx, tx, uuid.UUID(team), id)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// MaxCatalogueBytes is the budget for the template definitions in a Pull snapshot (D4). Past
// it a template is listed without its definitions and the app asks for them with
// GetSheetTemplateVersion when it has a network.
const MaxCatalogueBytes = 2 << 20

// CatalogueTemplate is one global template in the Pull snapshot.
type CatalogueTemplate struct {
	ID             string             `json:"id"`
	Slug           string             `json:"slug"`
	Make           string             `json:"make"`
	Model          string             `json:"model"`
	Scale          string             `json:"scale"`
	Powertrain     string             `json:"powertrain"`
	Name           string             `json:"name"`
	SourceURL      *string            `json:"source_url"`
	CurrentVersion int32              `json:"current_version"`
	Truncated      bool               `json:"truncated"`
	Versions       []CatalogueVersion `json:"versions"`
}

type CatalogueVersion struct {
	Version    int32           `json:"version"`
	Definition json.RawMessage `json:"definition"`
}

// CatalogueReader reads the global templates for the sync snapshot. There is no team
// context: they are reference data, the same for everybody.
type CatalogueReader struct {
	db     sqlcgen.DBTX
	budget int
}

func NewCatalogueReader(q sqlcgen.DBTX) *CatalogueReader {
	return &CatalogueReader{db: q, budget: MaxCatalogueBytes}
}

// WithBudget is for tests.
func (r *CatalogueReader) WithBudget(n int) *CatalogueReader { r.budget = n; return r }

// Version is a hash of everything the snapshot would contain, computed from digests so the
// Pull does not read every definition each time.
func (r *CatalogueReader) Version(ctx context.Context) (string, error) {
	q := sqlcgen.New(r.db)
	templates, err := q.ListGlobalSheetTemplates(ctx)
	if err != nil {
		return "", fmt.Errorf("list global sheet templates: %w", err)
	}
	digests, err := q.ListGlobalSheetTemplateDigests(ctx)
	if err != nil {
		return "", fmt.Errorf("list global sheet template digests: %w", err)
	}
	h := sha256.New()
	field := func(v string) {
		_, _ = h.Write([]byte(strconv.Itoa(len(v))))
		_, _ = h.Write([]byte(":"))
		_, _ = h.Write([]byte(v))
	}
	field("sheet_templates")
	field(strconv.Itoa(len(templates)))
	for _, t := range templates {
		for _, v := range []string{t.ID.String(), t.Slug, t.Make, t.Model, t.Scale, t.Powertrain, t.Name, strconv.Itoa(int(t.CurrentVersion))} {
			field(v)
		}
		if t.SourceUrl != nil {
			field(*t.SourceUrl)
		} else {
			field("-")
		}
	}
	for _, d := range digests {
		field(d.TemplateID.String())
		field(strconv.Itoa(int(d.Version)))
		field(hex.EncodeToString(d.Digest))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Snapshot returns the global templates in id order. Definitions are included while they fit
// in the budget; the first template that would overflow it, and every one after it, is listed
// with Truncated set and no versions. The result is deterministic for the same data.
func (r *CatalogueReader) Snapshot(ctx context.Context) ([]CatalogueTemplate, error) {
	q := sqlcgen.New(r.db)
	templates, err := q.ListGlobalSheetTemplates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list global sheet templates: %w", err)
	}
	defs, err := q.ListGlobalSheetTemplateDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list global sheet template definitions: %w", err)
	}
	byID := map[uuid.UUID][]CatalogueVersion{}
	for _, d := range defs {
		byID[d.TemplateID] = append(byID[d.TemplateID], CatalogueVersion{Version: d.Version, Definition: d.Definition})
	}
	used, full := 0, false
	out := make([]CatalogueTemplate, 0, len(templates))
	for _, t := range templates {
		item := CatalogueTemplate{
			ID: t.ID.String(), Slug: t.Slug, Make: t.Make, Model: t.Model, Scale: t.Scale,
			Powertrain: t.Powertrain, Name: t.Name, SourceURL: t.SourceUrl, CurrentVersion: t.CurrentVersion,
			Versions: []CatalogueVersion{},
		}
		size := 0
		for _, v := range byID[t.ID] {
			size += len(v.Definition)
		}
		if full || used+size > r.budget {
			full = true
			item.Truncated = true
		} else {
			item.Versions = byID[t.ID]
			used += size
		}
		out = append(out, item)
	}
	return out, nil
}
