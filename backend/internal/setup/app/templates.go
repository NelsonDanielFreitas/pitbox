package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/cursor"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// TemplateRef names a template version. A nil Version means the template's current one.
type TemplateRef struct {
	ID      uuid.UUID
	Version *int32
}

const (
	duplicateTemplateMethod = "/pitbox.setup.v1.SetupService/DuplicateSheetTemplate"
	maxCachedTemplates      = 256
)

type templateKey struct {
	id      uuid.UUID
	version int32
}

// templateCache holds parsed versions of GLOBAL templates only. A version is immutable, so a
// cached one never goes stale; a team's own templates are read inside the transaction every
// time, so the RLS keeps deciding who sees them. It is bounded: when full, nothing new is cached.
type templateCache struct {
	mu sync.RWMutex
	m  map[templateKey]domain.Template
}

func newTemplateCache() *templateCache { return &templateCache{m: map[templateKey]domain.Template{}} }

func (c *templateCache) get(k templateKey) (domain.Template, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.m[k]
	return t, ok
}

func (c *templateCache) put(k templateKey, t domain.Template) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) < maxCachedTemplates {
		c.m[k] = t
	}
}

// loadTemplate returns a parsed, validated template version the team may see. Anything the
// team may not see (another team's private template, a deleted or unknown one) is ErrNotFound.
// version 0 means the current version.
func (s *Service) loadTemplate(ctx context.Context, tx db.DBTX, team db.TeamID, id uuid.UUID, version int32) (domain.Template, error) {
	if version == 0 {
		info, err := s.store.GetSheetTemplate(ctx, tx, uuid.UUID(team), id)
		if err != nil {
			return domain.Template{}, err
		}
		version = info.CurrentVersion
	}
	key := templateKey{id, version}
	if t, ok := s.templates.get(key); ok {
		return t, nil
	}
	row, err := s.store.GetSheetTemplateVersion(ctx, tx, uuid.UUID(team), id, version)
	if err != nil {
		return domain.Template{}, err
	}
	t, err := domain.ParseTemplate(row.Definition)
	if err != nil || t.ID != id.String() || t.Version != int(version) {
		// Stored data that no longer parses is our bug, not the caller's: not a validation error.
		return domain.Template{}, fmt.Errorf("sheet template %s v%d: stored definition is invalid", id, version)
	}
	if row.Global {
		s.templates.put(key, t)
	}
	return t, nil
}

// templateFromData reads the template reference of v2 data and loads that version.
func (s *Service) templateFromData(ctx context.Context, tx db.DBTX, team db.TeamID, data map[string]any) (domain.Template, error) {
	ref, ok := data["template"].(map[string]any)
	if !ok {
		return domain.Template{}, &domain.ValidationError{Path: "template", RuleID: domain.RuleTemplateUnknown}
	}
	idRaw, _ := ref["id"].(string)
	id, err := uuid.Parse(idRaw)
	n, isNum := ref["version"].(float64)
	if err != nil || id == uuid.Nil || !isNum || n < 1 || n > 1<<30 || n != float64(int32(n)) {
		return domain.Template{}, &domain.ValidationError{Path: "template", RuleID: domain.RuleTemplateUnknown}
	}
	return s.loadTemplate(ctx, tx, team, id, int32(n))
}

// ListSheetTemplates lists the global catalogue and the team's templates, by id.
func (s *Service) ListSheetTemplates(ctx context.Context, user db.UserID, team db.TeamID, size int32, token string) ([]domain.SheetTemplateInfo, string, error) {
	var c idCursor
	if err := cursor.Decode(token, &c); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	var out []domain.SheetTemplateInfo
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		var err error
		out, err = s.store.ListSheetTemplates(ctx, tx, uuid.UUID(team), c.ID, limit+1)
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

// GetSheetTemplateVersion returns the raw definition JSON of one version (0 = current).
func (s *Service) GetSheetTemplateVersion(ctx context.Context, user db.UserID, team db.TeamID, id uuid.UUID, version int32) (domain.SheetTemplateVersionRow, error) {
	var out domain.SheetTemplateVersionRow
	err := s.withMemberReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		v := version
		if v == 0 {
			info, err := s.store.GetSheetTemplate(ctx, tx, uuid.UUID(team), id)
			if err != nil {
				return err
			}
			v = info.CurrentVersion
		}
		var err error
		out, err = s.store.GetSheetTemplateVersion(ctx, tx, uuid.UUID(team), id, v)
		return err
	})
	return out, err
}

// DuplicateSheetTemplate copies a global template into the team (ADR 0030, D3). Mechanic or
// owner; idempotent by request id; audited.
func (s *Service) DuplicateSheetTemplate(ctx context.Context, user db.UserID, team db.TeamID, requestID, templateID uuid.UUID) (domain.SheetTemplateInfo, error) {
	hash, err := hashInput(uuid.UUID(team), struct {
		TemplateID uuid.UUID `json:"template_id"`
	}{templateID})
	if err != nil {
		return domain.SheetTemplateInfo{}, fmt.Errorf("hash duplicate template: %w", err)
	}
	var out domain.SheetTemplateInfo
	err = s.withMechanicTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ string) error {
		response, replayed, err := s.claimOrReplay(ctx, tx, uuid.UUID(user), requestID, duplicateTemplateMethod, hash)
		if err != nil {
			return err
		}
		if replayed {
			out, err = decodeResource[domain.SheetTemplateInfo](response)
			return err
		}
		out, err = s.duplicate(ctx, tx, team, user, templateID, uuid.Nil)
		if err != nil {
			return err
		}
		if err := s.saveResource(ctx, tx, uuid.UUID(user), requestID, out); err != nil {
			return err
		}
		return s.audit(ctx, tx, "setup.sheet_template.duplicate", "sheet_template", out.ID.String(), nil,
			map[string]any{"based_on_id": templateID.String(), "slug": out.Slug}, requestID.String())
	})
	return out, err
}

// duplicate copies the current version of a global template under newID (a fresh UUIDv7 when
// newID is Nil). Shared by the use case and the sync applier.
func (s *Service) duplicate(ctx context.Context, tx db.DBTX, team db.TeamID, user db.UserID, srcID, newID uuid.UUID) (domain.SheetTemplateInfo, error) {
	src, err := s.store.GetSheetTemplate(ctx, tx, uuid.UUID(team), srcID)
	if err != nil {
		return domain.SheetTemplateInfo{}, err
	}
	if !src.Global {
		return domain.SheetTemplateInfo{}, fmt.Errorf("only global templates can be duplicated: %w", domain.ErrInvalid)
	}
	if newID == uuid.Nil {
		if newID, err = uuid.NewV7(); err != nil {
			return domain.SheetTemplateInfo{}, fmt.Errorf("new template id: %w", err)
		}
	}
	tpl, err := s.loadTemplate(ctx, tx, team, srcID, src.CurrentVersion)
	if err != nil {
		return domain.SheetTemplateInfo{}, err
	}
	dup, err := domain.DuplicateTemplate(tpl, newID)
	if err != nil {
		return domain.SheetTemplateInfo{}, err
	}
	raw, err := json.Marshal(dup)
	if err != nil {
		return domain.SheetTemplateInfo{}, fmt.Errorf("encode template: %w", err)
	}
	info := domain.SheetTemplateInfo{
		ID: newID, Slug: dup.Slug, Make: dup.Make, Model: dup.Model, Scale: dup.Scale,
		Powertrain: dup.Powertrain, Name: src.Name, SourceURL: src.SourceURL, BasedOnID: &srcID,
	}
	return s.store.CreateTeamSheetTemplate(ctx, tx, uuid.UUID(team), uuid.UUID(user), info, raw)
}
