package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

var errNotFound = apperr.ErrNotFound

const (
	relationMember = "member"
	relationOwner  = "owner"
)

func subject(id string) string { return "user:" + id }
func object(id string) string  { return "team:" + id }

// checkMember authorizes by the member relation. A guest is refused by the
// OpenFGA model, and the NOT_FOUND (instead of PERMISSION_DENIED) keeps team
// ids from being probed. An OpenFGA failure is fail closed: the request is
// refused, never allowed.
func (s *Service) checkMember(ctx context.Context, user, teamID string) error {
	err := s.authz.Check(ctx, subject(user), relationMember, object(teamID))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, authz.ErrDenied):
		return fmt.Errorf("team access: %w", errNotFound)
	default:
		return fmt.Errorf("authorization check: %w", err)
	}
}

// checkOwner distinguishes "member without the role" (PERMISSION_DENIED) from
// "not a member" (NOT_FOUND) by falling back to a member Check.
func (s *Service) checkOwner(ctx context.Context, user, teamID string) error {
	err := s.authz.Check(ctx, subject(user), relationOwner, object(teamID))
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, authz.ErrDenied):
		return fmt.Errorf("authorization check: %w", err)
	}
	memberErr := s.authz.Check(ctx, subject(user), relationMember, object(teamID))
	switch {
	case memberErr == nil:
		return fmt.Errorf("owner role: %w", domain.ErrForbidden)
	case errors.Is(memberErr, authz.ErrDenied):
		return fmt.Errorf("team access: %w", errNotFound)
	default:
		return fmt.Errorf("authorization check: %w", memberErr)
	}
}

// withOwnerTx runs an owner-only write. The membership table is checked again
// inside the transaction, and its role must be owner: OpenFGA is a synchronized
// copy, the database is the source of truth.
func (s *Service) withOwnerTx(ctx context.Context, user db.UserID, team db.TeamID, fn func(context.Context, db.DBTX, db.Membership) error) error {
	if err := s.checkOwner(ctx, user.String(), team.String()); err != nil {
		return err
	}
	return s.db.WithTeamTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		if m.Role != string(domain.RoleOwner) {
			return fmt.Errorf("owner role: %w", domain.ErrForbidden)
		}
		return fn(ctx, tx, m)
	})
}

// withMemberTx runs a member-only write, checking both barriers.
func (s *Service) withMemberTx(ctx context.Context, user db.UserID, team db.TeamID, fn func(context.Context, db.DBTX, db.Membership) error) error {
	if err := s.checkMember(ctx, user.String(), team.String()); err != nil {
		return err
	}
	return s.db.WithTeamTx(ctx, user, team, fn)
}

// withMemberReadTx is withMemberTx for NO_SIDE_EFFECTS RPCs: the transaction is
// read-only.
func (s *Service) withMemberReadTx(ctx context.Context, user db.UserID, team db.TeamID, fn func(context.Context, db.DBTX) error) error {
	if err := s.checkMember(ctx, user.String(), team.String()); err != nil {
		return err
	}
	return s.db.WithTeamReadTx(ctx, user, team, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		return fn(ctx, tx)
	})
}
