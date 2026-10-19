package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// Store is the identity persistence surface used by the use cases. It is
// implemented by identity/store; every method runs in the transaction the use
// case opened.
type Store interface {
	CreateTeam(ctx context.Context, tx db.DBTX, t domain.Team) (domain.Team, error)
	GetTeam(ctx context.Context, tx db.DBTX, teamID uuid.UUID) (domain.Team, error)
	ListMyTeams(ctx context.Context, tx db.DBTX, userID uuid.UUID, after uuid.UUID, limit int32) ([]domain.TeamMembership, error)
	UpdateTeam(ctx context.Context, tx db.DBTX, t domain.Team) (domain.Team, error)

	CreateMembership(ctx context.Context, tx db.DBTX, m domain.Member) (domain.Member, error)
	GetMember(ctx context.Context, tx db.DBTX, teamID, userID uuid.UUID) (domain.Member, error)
	ListMembers(ctx context.Context, tx db.DBTX, teamID uuid.UUID, afterJoined time.Time, afterUser uuid.UUID, limit int32) ([]domain.Member, error)
	UpdateMemberRole(ctx context.Context, tx db.DBTX, teamID, userID uuid.UUID, role domain.Role, version int32) (domain.Member, error)
	DeleteMember(ctx context.Context, tx db.DBTX, teamID, userID uuid.UUID, version int32) error
	DeleteOwnMembership(ctx context.Context, tx db.DBTX, teamID, userID uuid.UUID) error

	CountPendingInvitations(ctx context.Context, tx db.DBTX, teamID uuid.UUID) (int64, error)
	CreateInvitation(ctx context.Context, tx db.DBTX, inv domain.Invitation) (domain.Invitation, error)
	GetInvitation(ctx context.Context, tx db.DBTX, teamID, id uuid.UUID) (domain.Invitation, error)
	ListInvitations(ctx context.Context, tx db.DBTX, teamID uuid.UUID, after uuid.UUID, limit int32) ([]domain.Invitation, error)
	RevokeInvitation(ctx context.Context, tx db.DBTX, teamID, id uuid.UUID) error
	AcceptInvitation(ctx context.Context, tx db.DBTX, tokenHash []byte) (domain.InvitationAcceptance, error)

	CancelPendingTransfers(ctx context.Context, tx db.DBTX, teamID uuid.UUID) error
	CreateTransfer(ctx context.Context, tx db.DBTX, t domain.OwnershipTransfer) (domain.OwnershipTransfer, error)
	GetTransferForUpdate(ctx context.Context, tx db.DBTX, teamID, id uuid.UUID) (domain.OwnershipTransfer, error)
	ConfirmTransfer(ctx context.Context, tx db.DBTX, teamID, id uuid.UUID) error
	CancelTransfer(ctx context.Context, tx db.DBTX, teamID, id uuid.UUID) error

	GetIdempotencyKey(ctx context.Context, tx db.DBTX, userID, requestID uuid.UUID) (requestHash, response []byte, err error)
	InsertIdempotencyKey(ctx context.Context, tx db.DBTX, requestID, userID uuid.UUID, method string, requestHash []byte) (bool, error)
	SetIdempotencyResponse(ctx context.Context, tx db.DBTX, userID, requestID uuid.UUID, response []byte) error
}

// Authorizer is the slice of OpenFGA the use cases need: a Check before touching
// data and the post-commit tuple sync that keeps a new membership usable before
// the worker drains the outbox.
type Authorizer interface {
	Check(ctx context.Context, user, relation, object string) error
	SetTeamRole(ctx context.Context, user, team string, role *string) error
}

// Mailer sends an invitation. The real provider is F5; the fake never logs the
// email or the token.
type Mailer interface {
	Invitation(ctx context.Context, to, token string) error
}
