package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// validateChangedReferences checks garage references only when they are new or
// different. A reference that was valid when the setup was recorded stays valid
// history even if the engine/tyre is later retired or deleted.
func (s *Service) validateChangedReferences(ctx context.Context, tx db.DBTX, team db.TeamID, before, after map[string]any) error {
	if err := s.checkReference(ctx, tx, team, before, after, "engine", "engine_id", s.garage.EngineExists); err != nil {
		return err
	}
	return s.checkReference(ctx, tx, team, before, after, "tyres", "tyre_set_id", s.garage.TyreSetExists)
}

func (s *Service) checkReference(
	ctx context.Context, tx db.DBTX, team db.TeamID, before, after map[string]any,
	group, key string, exists func(context.Context, db.DBTX, db.TeamID, uuid.UUID) (bool, error),
) error {
	oldID := referenceID(before, group, key)
	newID := referenceID(after, group, key)
	if newID == "" || newID == oldID {
		return nil
	}
	id, err := uuid.Parse(newID)
	if err != nil || id == uuid.Nil {
		return domain.ErrInvalid
	}
	ok, err := exists(ctx, tx, team, id)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrNotFound
	}
	return nil
}

func referenceID(data map[string]any, group, key string) string {
	g, ok := data[group].(map[string]any)
	if !ok {
		return ""
	}
	id, _ := g[key].(string)
	return id
}
