package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/audit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox"
)

// InitiateTransferInput is the input of InitiateTransfer.
type InitiateTransferInput struct {
	RequestID uuid.UUID
	ToUserID  uuid.UUID
}

// InitiateTransfer starts a two-sided ownership transfer. Only the owner may do
// it; the recipient must be a mechanic or a pilot. A previous pending transfer
// (even an expired one) is cancelled in the same transaction.
func (s *Service) InitiateTransfer(ctx context.Context, user db.UserID, teamID db.TeamID, in InitiateTransferInput) (domain.OwnershipTransfer, error) {
	var created domain.OwnershipTransfer
	err := s.withOwnerTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		target, err := s.store.GetMember(ctx, tx, uuid.UUID(teamID), in.ToUserID)
		if err != nil {
			return err
		}
		if !domain.TransferTarget(target.Role) {
			return domain.ErrTransferTarget
		}
		if err := s.store.CancelPendingTransfers(ctx, tx, uuid.UUID(teamID)); err != nil {
			return err
		}
		created, err = s.store.CreateTransfer(ctx, tx, domain.OwnershipTransfer{
			TeamID: uuid.UUID(teamID), FromUserID: uuid.UUID(user), ToUserID: in.ToUserID,
			CreatedBy: uuid.UUID(user), ExpiresAt: domain.TransferExpiry(s.clock.Now()),
		})
		if err != nil {
			return err
		}
		return audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "ownership.transfer_initiated", ObjectType: "ownership_transfer", ObjectID: created.ID.String(),
			After: map[string]any{"to_user_id": in.ToUserID.String(), "expires_at": created.ExpiresAt}, RequestID: in.RequestID.String(),
		})
	})
	return created, err
}

// AcceptTransfer confirms a pending transfer. Only its recipient may accept, and
// the recipient is a member (WithTeamTx). The old owner is demoted before the new
// one is promoted, so the one-owner index never sees two owners.
func (s *Service) AcceptTransfer(ctx context.Context, user db.UserID, teamID db.TeamID, requestID, transferID uuid.UUID) (domain.TeamMembership, error) {
	var membership domain.TeamMembership
	var oldOwnerID uuid.UUID
	err := s.withMemberTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		transfer, err := s.store.GetTransferForUpdate(ctx, tx, uuid.UUID(teamID), transferID)
		if err != nil {
			return err
		}
		if transfer.ToUserID != uuid.UUID(user) {
			return domain.ErrForbidden
		}
		if !transfer.Pending(s.clock.Now()) {
			return domain.ErrTransferInvalid
		}

		oldOwner, err := s.store.GetMember(ctx, tx, uuid.UUID(teamID), transfer.FromUserID)
		if err != nil {
			return err
		}
		newOwner, err := s.store.GetMember(ctx, tx, uuid.UUID(teamID), transfer.ToUserID)
		if err != nil {
			return err
		}
		if oldOwner.Role != domain.RoleOwner {
			return domain.ErrOwnerRequired
		}
		if !domain.TransferTarget(newOwner.Role) {
			return domain.ErrTransferTarget
		}

		oldUpdated, err := s.store.UpdateMemberRole(ctx, tx, uuid.UUID(teamID), oldOwner.UserID, domain.RoleMechanic, oldOwner.Version)
		if err != nil {
			return err
		}
		newUpdated, err := s.store.UpdateMemberRole(ctx, tx, uuid.UUID(teamID), newOwner.UserID, domain.RoleOwner, newOwner.Version)
		if err != nil {
			return err
		}
		if err := s.store.ConfirmTransfer(ctx, tx, uuid.UUID(teamID), transferID); err != nil {
			return err
		}

		oldRole, newRole := string(domain.RoleMechanic), string(domain.RoleOwner)
		if err := outbox.Record(ctx, tx, outbox.MembershipChanged(uuid.UUID(teamID), oldOwner.UserID, &oldRole)); err != nil {
			return err
		}
		if err := outbox.Record(ctx, tx, outbox.MembershipChanged(uuid.UUID(teamID), newOwner.UserID, &newRole)); err != nil {
			return err
		}

		team, err := s.store.GetTeam(ctx, tx, uuid.UUID(teamID))
		if err != nil {
			return err
		}
		membership = domain.TeamMembership{Team: team, Role: domain.RoleOwner}
		oldOwnerID = oldOwner.UserID

		return audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "ownership.transferred", ObjectType: "team", ObjectID: uuid.UUID(teamID).String(),
			Before:    map[string]any{"owner": oldOwner.UserID.String(), "owner_role": string(oldOwner.Role), "recipient": newOwner.UserID.String(), "recipient_role": string(newOwner.Role)},
			After:     map[string]any{"owner": newOwner.UserID.String(), "owner_role": string(newUpdated.Role), "recipient": oldUpdated.UserID.String(), "recipient_role": string(oldUpdated.Role)},
			RequestID: requestID.String(),
		})
	})
	if err != nil {
		return domain.TeamMembership{}, err
	}

	s.syncRole(ctx, oldOwnerID, uuid.UUID(teamID))
	s.syncRole(ctx, uuid.UUID(user), uuid.UUID(teamID))
	return membership, nil
}

// CancelTransfer cancels a pending transfer. The owner or the recipient may do
// it; the transfer row decides which one the caller is.
func (s *Service) CancelTransfer(ctx context.Context, user db.UserID, teamID db.TeamID, requestID, transferID uuid.UUID) error {
	return s.withMemberTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		transfer, err := s.store.GetTransferForUpdate(ctx, tx, uuid.UUID(teamID), transferID)
		if err != nil {
			return err
		}
		userID := uuid.UUID(user)
		if transfer.FromUserID != userID && transfer.ToUserID != userID {
			return domain.ErrForbidden
		}
		if err := s.store.CancelTransfer(ctx, tx, uuid.UUID(teamID), transferID); err != nil {
			return err
		}
		return audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "ownership.transfer_cancelled", ObjectType: "ownership_transfer", ObjectID: transferID.String(),
			RequestID: requestID.String(),
		})
	})
}

// cancelPendingTransferFor cancels the pending transfer when userID is its
// recipient. It is used when the recipient is removed, leaves or is demoted to
// guest, so the row is cancelled and audited explicitly (the FK cascade is only
// the safety net) and a guest cannot accept an old transfer.
func (s *Service) cancelPendingTransferFor(ctx context.Context, tx db.DBTX, teamID, userID, requestID uuid.UUID, reason string) error {
	transfer, err := s.store.GetPendingTransfer(ctx, tx, teamID)
	switch {
	case errors.Is(err, apperr.ErrNotFound):
		return nil
	case err != nil:
		return err
	}
	if transfer.ToUserID != userID {
		return nil
	}
	if err := s.store.CancelTransfer(ctx, tx, teamID, transfer.ID); err != nil {
		return err
	}
	return audit.RecordTeam(ctx, tx, audit.TeamEvent{
		Action: "ownership.transfer_cancelled", ObjectType: "ownership_transfer", ObjectID: transfer.ID.String(),
		After: map[string]any{"reason": reason}, RequestID: requestID.String(),
	})
}
