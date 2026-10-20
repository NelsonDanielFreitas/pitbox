package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/audit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox"
)

// ChangeMemberRoleInput is the input of ChangeMemberRole.
type ChangeMemberRoleInput struct {
	RequestID uuid.UUID
	UserID    uuid.UUID
	Role      domain.Role
	Version   int32
}

// RemoveMemberInput is the input of RemoveMember.
type RemoveMemberInput struct {
	RequestID uuid.UUID
	UserID    uuid.UUID
	Version   int32
}

// ListMembers lists a team's members with keyset pagination.
func (s *Service) ListMembers(ctx context.Context, user db.UserID, teamID db.TeamID, size int32, pageToken string) ([]domain.Member, string, error) {
	var cursor membersCursor
	if err := decodeCursor(pageToken, &cursor); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	var members []domain.Member
	err := s.withMemberReadTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX) error {
		var err error
		members, err = s.store.ListMembers(ctx, tx, uuid.UUID(teamID), cursor.JoinedAt, cursor.UserID, limit+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(members) > int(limit) {
		members = members[:limit]
		last := members[len(members)-1]
		next = encodeCursor(membersCursor{JoinedAt: last.JoinedAt, UserID: last.UserID})
	}
	return members, next, nil
}

// ChangeMemberRole changes a member's role. Only the owner may do it, the target
// can never become owner, and the current owner cannot be demoted (that is what
// the transfer is for).
func (s *Service) ChangeMemberRole(ctx context.Context, user db.UserID, teamID db.TeamID, in ChangeMemberRoleInput) (domain.Member, error) {
	if err := domain.AssignableRole(in.Role); err != nil {
		return domain.Member{}, err
	}
	var updated domain.Member
	err := s.withOwnerTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		target, err := s.store.GetMember(ctx, tx, uuid.UUID(teamID), in.UserID)
		if err != nil {
			return err
		}
		if target.Role == domain.RoleOwner {
			return domain.ErrOwnerRequired
		}
		updated, err = s.store.UpdateMemberRole(ctx, tx, uuid.UUID(teamID), in.UserID, in.Role, in.Version)
		if err != nil {
			return err
		}
		if !domain.TransferTarget(in.Role) {
			if err := s.cancelPendingTransferFor(ctx, tx, uuid.UUID(teamID), in.UserID, in.RequestID, "recipient_demoted"); err != nil {
				return err
			}
		}
		role := string(in.Role)
		if err := outbox.Record(ctx, tx, outbox.MembershipChanged(uuid.UUID(teamID), in.UserID, &role)); err != nil {
			return err
		}
		return audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "member.role_changed", ObjectType: "member", ObjectID: in.UserID.String(),
			Before: memberAudit(target), After: memberAudit(updated), RequestID: in.RequestID.String(),
		})
	})
	return updated, err
}

// RemoveMember removes a member. The owner cannot remove the owner (the last
// owner rule); removal is a write whose tuple change goes through the outbox.
func (s *Service) RemoveMember(ctx context.Context, user db.UserID, teamID db.TeamID, in RemoveMemberInput) error {
	return s.withOwnerTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		target, err := s.store.GetMember(ctx, tx, uuid.UUID(teamID), in.UserID)
		if err != nil {
			return err
		}
		if target.Role == domain.RoleOwner {
			return domain.ErrOwnerRequired
		}
		if err := s.cancelPendingTransferFor(ctx, tx, uuid.UUID(teamID), in.UserID, in.RequestID, "recipient_removed"); err != nil {
			return err
		}
		if err := s.store.DeleteMember(ctx, tx, uuid.UUID(teamID), in.UserID, in.Version); err != nil {
			return err
		}
		if err := outbox.Record(ctx, tx, outbox.MembershipChanged(uuid.UUID(teamID), in.UserID, nil)); err != nil {
			return err
		}
		return audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "member.removed", ObjectType: "member", ObjectID: in.UserID.String(),
			Before: memberAudit(target), RequestID: in.RequestID.String(),
		})
	})
}

// LeaveTeam removes the caller's own membership. It is the documented exception
// to the Check order: the membership itself authorizes (a guest may leave), so
// there is no OpenFGA Check. The owner cannot leave.
func (s *Service) LeaveTeam(ctx context.Context, user db.UserID, teamID db.TeamID, requestID uuid.UUID) error {
	return s.db.WithTeamTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		if m.Role == string(domain.RoleOwner) {
			return domain.ErrOwnerRequired
		}
		if err := s.cancelPendingTransferFor(ctx, tx, uuid.UUID(teamID), uuid.UUID(user), requestID, "recipient_left"); err != nil {
			return err
		}
		if err := s.store.DeleteOwnMembership(ctx, tx, uuid.UUID(teamID), uuid.UUID(user)); err != nil {
			return err
		}
		if err := outbox.Record(ctx, tx, outbox.MembershipChanged(uuid.UUID(teamID), uuid.UUID(user), nil)); err != nil {
			return err
		}
		return audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "member.left", ObjectType: "member", ObjectID: uuid.UUID(user).String(),
			Before: map[string]any{"role": m.Role}, RequestID: requestID.String(),
		})
	})
}

func memberAudit(m domain.Member) map[string]any {
	return map[string]any{"role": string(m.Role), "version": m.Version}
}
