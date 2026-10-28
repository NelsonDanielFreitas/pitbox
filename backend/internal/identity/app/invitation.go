package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/audit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox"
)

// InviteMemberInput is the input of InviteMember.
type InviteMemberInput struct {
	RequestID uuid.UUID
	Role      domain.Role
	Email     *string
}

// InviteMember creates an invitation by email or link. It is intentionally not
// idempotent; the plaintext token is returned once and only its hash is stored.
// Email invitations keep returning the token until the real mailer ships (#124).
func (s *Service) InviteMember(ctx context.Context, user db.UserID, teamID db.TeamID, in InviteMemberInput) (domain.Invitation, string, error) {
	if err := domain.AssignableRole(in.Role); err != nil {
		return domain.Invitation{}, "", err
	}
	email, err := domain.InvitationEmail(in.Email)
	if err != nil {
		return domain.Invitation{}, "", err
	}
	token, hash, err := domain.NewInvitationToken()
	if err != nil {
		return domain.Invitation{}, "", err
	}

	var created domain.Invitation
	err = s.withOwnerTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		// Serialise invitations of this team: the count-then-insert below would
		// otherwise let two concurrent requests both pass at 49 pending.
		if err := s.store.LockTeamForInvites(ctx, tx, uuid.UUID(teamID)); err != nil {
			return err
		}
		pending, err := s.store.CountPendingInvitations(ctx, tx, uuid.UUID(teamID))
		if err != nil {
			return err
		}
		if pending >= domain.MaxPendingInvitations {
			return domain.ErrInvitationLimit
		}
		created, err = s.store.CreateInvitation(ctx, tx, domain.Invitation{
			TeamID: uuid.UUID(teamID), Email: email, Role: in.Role, TokenHash: hash,
			ExpiresAt: domain.InvitationExpiry(s.clock.Now()), CreatedBy: uuid.UUID(user),
		})
		if err != nil {
			return err
		}
		return audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "invitation.create", ObjectType: "invitation", ObjectID: created.ID.String(),
			After: map[string]any{"role": string(in.Role), "has_email": email != nil}, RequestID: in.RequestID.String(),
		})
	})
	if err != nil {
		return domain.Invitation{}, "", err
	}
	// The email is sent after commit; a mailer failure does not invalidate the
	// invitation (the owner still has the token and can share the link).
	if email != nil {
		s.sendInvitation(ctx, *email, token)
	}
	return created, token, nil
}

// ListInvitations lists a team's not accepted and not revoked invitations. It is
// owner-only because an invitation can contain personal data.
func (s *Service) ListInvitations(ctx context.Context, user db.UserID, teamID db.TeamID, size int32, pageToken string) ([]domain.Invitation, string, error) {
	var cursor invitationsCursor
	if err := decodeCursor(pageToken, &cursor); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	var invitations []domain.Invitation
	err := s.withOwnerReadTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX) error {
		var err error
		invitations, err = s.store.ListInvitations(ctx, tx, uuid.UUID(teamID), cursor.ID, limit+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(invitations) > int(limit) {
		invitations = invitations[:limit]
		next = encodeCursor(invitationsCursor{ID: invitations[len(invitations)-1].ID})
	}
	return invitations, next, nil
}

// AcceptInvitation consumes the token. The team comes from the invitation, not
// from the request. The same error is returned for a missing, expired, revoked,
// used or tampered invitation.
func (s *Service) AcceptInvitation(ctx context.Context, user db.UserID, requestID uuid.UUID, token string) (domain.TeamMembership, error) {
	hash, err := domain.HashInvitationToken(token)
	if err != nil {
		return domain.TeamMembership{}, err
	}

	var acceptance domain.InvitationAcceptance
	var membership domain.TeamMembership
	err = s.db.WithJoinTeamTx(ctx, user, func(ctx context.Context, tx db.DBTX) (db.TeamID, error) {
		var err error
		acceptance, err = s.store.AcceptInvitation(ctx, tx, hash)
		if err != nil {
			return db.TeamID{}, err
		}
		return db.TeamID(acceptance.TeamID), nil
	}, func(ctx context.Context, tx db.DBTX, m db.Membership) error {
		team, err := s.store.GetTeam(ctx, tx, acceptance.TeamID)
		if err != nil {
			return err
		}
		membership = domain.TeamMembership{Team: team, Role: domain.Role(m.Role)}
		if !acceptance.Consumed {
			// Already a member (or already accepted by this user): nothing
			// changed, so there is no new event to publish.
			return nil
		}
		role := string(acceptance.Role)
		if err := outbox.Record(ctx, tx, outbox.MembershipChanged(acceptance.TeamID, uuid.UUID(user), &role)); err != nil {
			return err
		}
		return audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "invitation.accepted", ObjectType: "member", ObjectID: uuid.UUID(user).String(),
			After: map[string]any{"role": role}, RequestID: requestID.String(),
		})
	})
	if err != nil {
		return domain.TeamMembership{}, err
	}
	s.syncRole(ctx, uuid.UUID(user), acceptance.TeamID)
	return membership, nil
}

// RevokeInvitation revokes a pending invitation. Only the owner may call it.
func (s *Service) RevokeInvitation(ctx context.Context, user db.UserID, teamID db.TeamID, invitationID uuid.UUID, requestID uuid.UUID) error {
	return s.withOwnerTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		if err := s.store.RevokeInvitation(ctx, tx, uuid.UUID(teamID), invitationID); err != nil {
			return err
		}
		return audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "invitation.revoked", ObjectType: "invitation", ObjectID: invitationID.String(),
			RequestID: requestID.String(),
		})
	})
}
