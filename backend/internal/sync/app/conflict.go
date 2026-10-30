package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
)

const (
	ResolveServer = "server"
	ResolveClient = "client"
	ResolveManual = "manual"

	// deletedField is the pseudo field of a delete that clashed with a later change.
	deletedField = "_deleted"
)

// ListConflicts lists the open conflicts of the team for the mechanic to decide.
func (s *Service) ListConflicts(ctx context.Context, user db.UserID, team db.TeamID) ([]domain.Conflict, error) {
	if err := s.checkMechanic(ctx, user.String(), team.String()); err != nil {
		return nil, err
	}
	var out []domain.Conflict
	err := s.db.WithTeamReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		if !isWriter(m.Role) {
			return fmt.Errorf("mechanic role: %w", domain.ErrForbidden)
		}
		var err error
		out, err = s.store.OpenConflicts(ctx, tx, uuid.UUID(team))
		return err
	})
	return out, err
}

// ResolveConflict settles one field. "server" keeps the current value; "client" writes the
// device's value; "manual" writes the value the mechanic typed. Resolving twice returns the
// same conflict without writing again.
func (s *Service) ResolveConflict(ctx context.Context, user db.UserID, team db.TeamID, id uuid.UUID, resolution string, value json.RawMessage) (domain.Conflict, error) {
	if resolution == ResolveManual && (len(value) == 0 || !json.Valid(value)) {
		return domain.Conflict{}, fmt.Errorf("manual value: %w", domain.ErrInvalid)
	}
	if err := s.checkMechanic(ctx, user.String(), team.String()); err != nil {
		return domain.Conflict{}, err
	}
	var out domain.Conflict
	err := s.db.WithTeamTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		if !isWriter(m.Role) {
			return fmt.Errorf("mechanic role: %w", domain.ErrForbidden)
		}
		c, found, err := s.store.Conflict(ctx, tx, uuid.UUID(team), id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("conflict: %w", domain.ErrNotFound)
		}
		if c.Resolved {
			out = c
			return nil
		}

		chosen := json.RawMessage(nil)
		switch resolution {
		case ResolveClient:
			chosen = c.ClientValue
		case ResolveManual:
			chosen = value
		}
		if chosen != nil {
			if err := s.writeResolved(ctx, tx, team, user, m.Role, c, chosen); err != nil {
				return err
			}
		}
		res, ok, err := s.store.ResolveConflict(ctx, tx, uuid.UUID(team), id, resolution, chosen, uuid.UUID(user))
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("sync: conflict resolved concurrently")
		}
		out = res
		return audited(ctx, tx, "sync.conflict.resolve", c.Entity, c.EntityID.String(),
			map[string]string{"field": c.Field, "resolution": resolution}, id.String())
	})
	return out, err
}

func (s *Service) writeResolved(ctx context.Context, tx db.DBTX, team db.TeamID, user db.UserID, role string, c domain.Conflict, value json.RawMessage) error {
	a, ok := s.appliers[c.Entity]
	if !ok {
		return fmt.Errorf("conflict entity: %w", domain.ErrInvalid)
	}
	cur, err := a.CurrentVersion(ctx, tx, team, c.EntityID)
	if err != nil {
		return fmt.Errorf("current version: %w", mapResolveErr(err))
	}
	op := syncapply.Op{
		OpID: uuid.New(), TeamID: team, UserID: user, Role: role, EntityID: c.EntityID, ExpectedVersion: cur,
	}
	if c.Field == deletedField {
		op.Kind = syncapply.Delete
	} else {
		op.Kind = syncapply.Patch
		op.Fields = map[string]json.RawMessage{c.Field: value}
	}
	if _, err := a.Apply(ctx, tx, op); err != nil {
		return fmt.Errorf("apply resolution: %w", mapResolveErr(err))
	}
	return nil
}

// mapResolveErr turns the Applier's errors into the public ones. A value that no longer
// validates is the caller's problem (INVALID_ARGUMENT); a lost race is a conflict again.
func mapResolveErr(err error) error {
	switch {
	case errors.Is(err, syncapply.ErrNotFound):
		return domain.ErrNotFound
	case errors.Is(err, syncapply.ErrStale):
		return domain.ErrBusy
	case errors.Is(err, syncapply.ErrForbidden):
		return domain.ErrForbidden
	case errors.Is(err, syncapply.ErrInvalidFields), errors.Is(err, syncapply.ErrInvalidRef), errors.Is(err, syncapply.ErrCatalogRetired):
		return domain.ErrInvalid
	default:
		return err
	}
}
