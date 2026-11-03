package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// initialVersion is the data of version 1 of a new setup.
type initialVersion struct {
	raw          []byte
	schema       int32
	basedSetup   *uuid.UUID
	basedVersion *int32
}

// prepareInitial builds version 1 for CreateSetup and the sync applier.
//
//   - Without a template and without a source: an empty v1 sheet (the behaviour before F2-00,
//     which clients that do not know templates still rely on).
//   - Without a template, copying a v1 source: a v1 copy. Copying a v2 source: a v2 sheet of
//     the source's own template version.
//   - With a template: an empty v2 sheet for it. Copying needs the same template (v2 source) or
//     the generic one (v1 source, converted in memory); anything else cannot be mapped field by
//     field and is refused.
//
// A template or source the team may not see is ErrNotFound.
func (s *Service) prepareInitial(ctx context.Context, tx db.DBTX, team db.TeamID, tref *TemplateRef, srcSetup *uuid.UUID, srcVersion *int32) (initialVersion, error) {
	var source *domain.Version
	var out initialVersion
	if srcSetup != nil {
		want := int32(0)
		if srcVersion != nil {
			want = *srcVersion
		}
		v, err := s.store.GetVersion(ctx, tx, uuid.UUID(team), *srcSetup, want)
		if err != nil {
			return out, err
		}
		source = &v
		out.basedSetup, out.basedVersion = srcSetup, &v.Ref.Version
	}

	if tref == nil {
		switch {
		case source == nil || source.Ref.SchemaVersion == domain.SchemaVersion:
			data := map[string]any{}
			if source != nil {
				data = source.Data
			}
			raw, err := domain.MarshalV1(data)
			out.raw, out.schema = raw, domain.SchemaVersion
			return out, err
		case source.Ref.SchemaVersion == domain.SchemaVersionV2:
			t, err := s.templateFromData(ctx, tx, team, source.Data)
			if err != nil {
				return out, err
			}
			return s.finishV2(out, t, source)
		default:
			return out, fmt.Errorf("source schema: %w", domain.ErrInvalid)
		}
	}

	version := int32(0)
	if tref.Version != nil {
		version = *tref.Version
	}
	t, err := s.loadTemplate(ctx, tx, team, tref.ID, version)
	if err != nil {
		return out, err
	}
	return s.finishV2(out, t, source)
}

func (s *Service) finishV2(out initialVersion, t domain.Template, source *domain.Version) (initialVersion, error) {
	data := map[string]any{
		"template": map[string]any{"id": t.ID, "version": float64(t.Version)},
		"values":   map[string]any{},
	}
	if source != nil {
		switch source.Ref.SchemaVersion {
		case domain.SchemaVersionV2:
			ref, _ := source.Data["template"].(map[string]any)
			if id, _ := ref["id"].(string); id != t.ID {
				return out, fmt.Errorf("copy across templates: %w", domain.ErrInvalid)
			}
			if values, ok := source.Data["values"].(map[string]any); ok {
				data["values"] = values
			}
			if refs, ok := source.Data["refs"].(map[string]any); ok {
				data["refs"] = refs
			}
		case domain.SchemaVersion:
			if t.ID != domain.GenericTemplateID {
				return out, fmt.Errorf("copy a v1 setup into a specific template: %w", domain.ErrInvalid)
			}
			values, refs := domain.V1ToValues(source.Data)
			data["values"] = values
			if len(refs) > 0 {
				data["refs"] = refs
			}
		default:
			return out, fmt.Errorf("source schema: %w", domain.ErrInvalid)
		}
	}
	raw, err := domain.MarshalV2(t, data)
	out.raw, out.schema = raw, domain.SchemaVersionV2
	return out, err
}

// marshalVersionData validates the data of a new version for its schema and returns the JSON to
// store. v2 loads the template named in the data, which the team must be allowed to see.
func (s *Service) marshalVersionData(ctx context.Context, tx db.DBTX, team db.TeamID, schema int32, data map[string]any) ([]byte, error) {
	switch schema {
	case 0, domain.SchemaVersion:
		return domain.MarshalV1(data)
	case domain.SchemaVersionV2:
		t, err := s.templateFromData(ctx, tx, team, data)
		if err != nil {
			return nil, err
		}
		return domain.MarshalV2(t, data)
	default:
		return nil, &domain.ValidationError{Path: "schema_version", RuleID: domain.RuleSchemaVersion}
	}
}

// normalizeSchema maps the request value (0 = unset) to the schema it means.
func normalizeSchema(v int32) int32 {
	if v == 0 {
		return domain.SchemaVersion
	}
	return v
}

// view turns a stored version into something CompareVersions understands.
func (s *Service) view(ctx context.Context, tx db.DBTX, team db.TeamID, generic *domain.Template, v domain.Version) (domain.VersionView, error) {
	if v.Ref.SchemaVersion == domain.SchemaVersionV2 {
		t, err := s.templateFromData(ctx, tx, team, v.Data)
		if err != nil {
			return domain.VersionView{}, err
		}
		return domain.ViewFromV2(t, v.Data), nil
	}
	if generic.ID == "" {
		g, err := domain.GenericTemplateFromV1()
		if err != nil {
			return domain.VersionView{}, err
		}
		*generic = g
	}
	return domain.ViewFromV1(*generic, v.Data), nil
}

// referenceIDs reads the engine and tyre-set references of either schema: v1 keeps them inside
// the sheet groups, v2 in data.refs (they are not template fields).
func referenceIDs(data map[string]any) (engine, tyres string) {
	if _, isV2 := data["template"]; isV2 {
		refs, _ := data["refs"].(map[string]any)
		engine, _ = refs["engine_id"].(string)
		tyres, _ = refs["tyre_set_id"].(string)
		return engine, tyres
	}
	return referenceID(data, "engine", "engine_id"), referenceID(data, "tyres", "tyre_set_id")
}
