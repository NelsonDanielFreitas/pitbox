package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/syncapply"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/sync/domain"
)

const (
	relationMember   = "member"
	relationMechanic = "mechanic"
)

type Authorizer interface {
	Check(ctx context.Context, user, relation, object string) error
}

func subject(id string) string    { return "user:" + id }
func teamObject(id string) string { return "team:" + id }

// checkMember: another team (or no membership) is NOT_FOUND, never PERMISSION_DENIED.
func (s *Service) checkMember(ctx context.Context, user, team string) error {
	err := s.authz.Check(ctx, subject(user), relationMember, teamObject(team))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, authz.ErrDenied):
		return fmt.Errorf("team access: %w", domain.ErrNotFound)
	default:
		return fmt.Errorf("authorization check: %w", err)
	}
}

// checkMechanic: a member without the mechanic relation is PERMISSION_DENIED.
func (s *Service) checkMechanic(ctx context.Context, user, team string) error {
	err := s.authz.Check(ctx, subject(user), relationMechanic, teamObject(team))
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, authz.ErrDenied):
		return fmt.Errorf("authorization check: %w", err)
	}
	if memberErr := s.checkMember(ctx, user, team); memberErr != nil {
		return memberErr
	}
	return fmt.Errorf("mechanic role: %w", domain.ErrForbidden)
}

func isWriter(role string) bool { return role == "owner" || role == "mechanic" }

// canPush: who may open a Push at all. A pilot may, but each operation is then checked
// against its entity (mayWrite); guests never write.
func canPush(role string) bool { return isWriter(role) || role == "pilot" }

// mayWrite: owner and mechanic write everything; another role only the entities whose
// Applier says so (syncapply.RoleGated). An unknown entity (nil applier) is refused later.
func mayWrite(role string, a syncapply.Applier) bool {
	if isWriter(role) {
		return true
	}
	g, ok := a.(syncapply.RoleGated)
	return ok && g.CanWrite(role)
}

// withMemberTx runs fn for any member except guests, who get NOT_FOUND like outsiders.
func (s *Service) withMemberTx(ctx context.Context, user db.UserID, team db.TeamID, fn func(context.Context, db.DBTX, string) error) error {
	if err := s.checkMember(ctx, user.String(), team.String()); err != nil {
		return err
	}
	return s.db.WithTeamTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		if m.Role == "guest" {
			return fmt.Errorf("team access: %w", domain.ErrNotFound)
		}
		return fn(ctx, tx, m.Role)
	})
}

func (s *Service) withMemberReadTx(ctx context.Context, user db.UserID, team db.TeamID, fn func(context.Context, db.DBTX, string) error) error {
	if err := s.checkMember(ctx, user.String(), team.String()); err != nil {
		return err
	}
	return s.db.WithTeamReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		if m.Role == "guest" {
			return fmt.Errorf("team access: %w", domain.ErrNotFound)
		}
		return fn(ctx, tx, m.Role)
	})
}
