package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
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

func (s *Service) checkMechanic(ctx context.Context, user, team string) error {
	err := s.authz.Check(ctx, subject(user), relationMechanic, teamObject(team))
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, authz.ErrDenied):
		return fmt.Errorf("authorization check: %w", err)
	}
	memberErr := s.authz.Check(ctx, subject(user), relationMember, teamObject(team))
	switch {
	case memberErr == nil:
		return fmt.Errorf("mechanic role: %w", domain.ErrForbidden)
	case errors.Is(memberErr, authz.ErrDenied):
		return fmt.Errorf("team access: %w", domain.ErrNotFound)
	default:
		return fmt.Errorf("authorization check: %w", memberErr)
	}
}

func (s *Service) withMemberReadTx(ctx context.Context, user db.UserID, team db.TeamID, fn func(context.Context, db.DBTX, string) error) error {
	if err := s.checkMember(ctx, user.String(), team.String()); err != nil {
		return err
	}
	return s.db.WithTeamReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		if m.Role == string("guest") {
			return fmt.Errorf("team access: %w", domain.ErrNotFound)
		}
		return fn(ctx, tx, m.Role)
	})
}

func (s *Service) withMechanicTx(ctx context.Context, user db.UserID, team db.TeamID, fn func(context.Context, db.DBTX, string) error) error {
	if err := s.checkMechanic(ctx, user.String(), team.String()); err != nil {
		return err
	}
	return s.db.WithTeamTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		if m.Role != string("owner") && m.Role != string("mechanic") {
			return fmt.Errorf("mechanic role: %w", domain.ErrForbidden)
		}
		return fn(ctx, tx, m.Role)
	})
}
