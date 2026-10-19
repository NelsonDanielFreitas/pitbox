package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/store/sqlcgen"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
)

// Repo is the F1-06 persistence. Every method receives the team-scoped
// transaction (or the user transaction for ListMyTeams) and never opens its own.
type Repo struct{}

// NewRepo returns the identity repository.
func NewRepo() *Repo { return &Repo{} }

func (Repo) CreateTeam(ctx context.Context, tx db.DBTX, t domain.Team) (domain.Team, error) {
	row, err := sqlcgen.New(tx).CreateTeam(ctx, sqlcgen.CreateTeamParams{
		ID:          t.ID,
		Name:        t.Name,
		Category:    t.Category,
		Club:        t.Club,
		CountryCode: t.CountryCode,
		CreatedBy:   t.CreatedBy,
	})
	if err != nil {
		return domain.Team{}, fmt.Errorf("create team: %w", err)
	}
	return teamFromCreate(row), nil
}

func (Repo) GetTeam(ctx context.Context, tx db.DBTX, teamID uuid.UUID) (domain.Team, error) {
	q := sqlcgen.New(tx)
	row, err := q.GetTeam(ctx, teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Team{}, fmt.Errorf("get team: %w", apperr.ErrNotFound)
	}
	if err != nil {
		return domain.Team{}, fmt.Errorf("get team: %w", err)
	}
	team := teamFromGet(row)
	transfer, err := q.GetPendingTransfer(ctx, teamID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// no pending transfer
	case err != nil:
		return domain.Team{}, fmt.Errorf("get pending transfer: %w", err)
	default:
		t := transferFromModel(transfer)
		team.PendingTransfer = &t
	}
	return team, nil
}

func (Repo) ListMyTeams(ctx context.Context, tx db.DBTX, userID uuid.UUID, after uuid.UUID, limit int32) ([]domain.TeamMembership, error) {
	rows, err := sqlcgen.New(tx).ListMyTeams(ctx, sqlcgen.ListMyTeamsParams{UserID: userID, ID: after, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list my teams: %w", err)
	}
	out := make([]domain.TeamMembership, 0, len(rows))
	for _, row := range rows {
		role, err := domain.ParseRole(row.Role)
		if err != nil {
			return nil, fmt.Errorf("list my teams: %w", err)
		}
		out = append(out, domain.TeamMembership{
			Team: domain.Team{
				ID: row.ID, Name: row.Name, Category: row.Category, Club: row.Club,
				CountryCode: row.CountryCode, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, Version: row.Version,
			},
			Role: role,
		})
	}
	return out, nil
}

func (Repo) UpdateTeam(ctx context.Context, tx db.DBTX, t domain.Team) (domain.Team, error) {
	row, err := sqlcgen.New(tx).UpdateTeam(ctx, sqlcgen.UpdateTeamParams{
		Name: t.Name, Category: t.Category, Club: t.Club, CountryCode: t.CountryCode,
		ID: t.ID, Version: t.Version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Team{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Team{}, fmt.Errorf("update team: %w", err)
	}
	return teamFromUpdate(row), nil
}

func (Repo) CreateMembership(ctx context.Context, tx db.DBTX, m domain.Member) (domain.Member, error) {
	row, err := sqlcgen.New(tx).CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		TeamID: m.TeamID, UserID: m.UserID, Column3: sqlcgen.IdentityTeamRole(m.Role), InvitedBy: m.InvitedBy,
	})
	if err != nil {
		return domain.Member{}, fmt.Errorf("create membership: %w", err)
	}
	role, err := domain.ParseRole(row.Role)
	if err != nil {
		return domain.Member{}, fmt.Errorf("create membership: %w", err)
	}
	return domain.Member{
		TeamID: row.TeamID, UserID: row.UserID, Role: role, JoinedAt: row.JoinedAt,
		Version: row.Version, InvitedBy: row.InvitedBy,
	}, nil
}

func (Repo) GetMember(ctx context.Context, tx db.DBTX, teamID, userID uuid.UUID) (domain.Member, error) {
	row, err := sqlcgen.New(tx).GetMember(ctx, sqlcgen.GetMemberParams{TeamID: teamID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Member{}, fmt.Errorf("get member: %w", apperr.ErrNotFound)
	}
	if err != nil {
		return domain.Member{}, fmt.Errorf("get member: %w", err)
	}
	role, err := domain.ParseRole(row.Role)
	if err != nil {
		return domain.Member{}, fmt.Errorf("get member: %w", err)
	}
	return domain.Member{
		TeamID: row.TeamID, UserID: row.UserID, DisplayName: row.DisplayName, Role: role,
		JoinedAt: row.JoinedAt, Version: row.Version, InvitedBy: row.InvitedBy,
	}, nil
}

func (Repo) ListMembers(ctx context.Context, tx db.DBTX, teamID uuid.UUID, afterJoined time.Time, afterUser uuid.UUID, limit int32) ([]domain.Member, error) {
	rows, err := sqlcgen.New(tx).ListMembers(ctx, sqlcgen.ListMembersParams{
		TeamID: teamID, JoinedAt: afterJoined, UserID: afterUser, Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	out := make([]domain.Member, 0, len(rows))
	for _, row := range rows {
		role, err := domain.ParseRole(row.Role)
		if err != nil {
			return nil, fmt.Errorf("list members: %w", err)
		}
		out = append(out, domain.Member{
			TeamID: row.TeamID, UserID: row.UserID, DisplayName: row.DisplayName, Role: role,
			JoinedAt: row.JoinedAt, Version: row.Version, InvitedBy: row.InvitedBy,
		})
	}
	return out, nil
}

func (Repo) UpdateMemberRole(ctx context.Context, tx db.DBTX, teamID, userID uuid.UUID, role domain.Role, version int32) (domain.Member, error) {
	row, err := sqlcgen.New(tx).UpdateMembershipRole(ctx, sqlcgen.UpdateMembershipRoleParams{
		Column1: sqlcgen.IdentityTeamRole(role), TeamID: teamID, UserID: userID, Version: version,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Member{}, domain.ErrConflict
	}
	if err != nil {
		return domain.Member{}, fmt.Errorf("update member role: %w", err)
	}
	parsed, err := domain.ParseRole(row.Role)
	if err != nil {
		return domain.Member{}, fmt.Errorf("update member role: %w", err)
	}
	return domain.Member{TeamID: row.TeamID, UserID: row.UserID, Role: parsed, JoinedAt: row.JoinedAt, Version: row.Version}, nil
}

func (Repo) DeleteMember(ctx context.Context, tx db.DBTX, teamID, userID uuid.UUID, version int32) error {
	n, err := sqlcgen.New(tx).DeleteMembership(ctx, sqlcgen.DeleteMembershipParams{TeamID: teamID, UserID: userID, Version: version})
	if err != nil {
		return fmt.Errorf("delete member: %w", err)
	}
	if n == 0 {
		return domain.ErrConflict
	}
	return nil
}

func (Repo) DeleteOwnMembership(ctx context.Context, tx db.DBTX, teamID, userID uuid.UUID) error {
	n, err := sqlcgen.New(tx).DeleteOwnMembership(ctx, sqlcgen.DeleteOwnMembershipParams{TeamID: teamID, UserID: userID})
	if err != nil {
		return fmt.Errorf("leave team: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("leave team: %w", apperr.ErrNotFound)
	}
	return nil
}

func (Repo) CountPendingInvitations(ctx context.Context, tx db.DBTX, teamID uuid.UUID) (int64, error) {
	n, err := sqlcgen.New(tx).CountPendingInvitations(ctx, teamID)
	if err != nil {
		return 0, fmt.Errorf("count pending invitations: %w", err)
	}
	return n, nil
}

func (Repo) CreateInvitation(ctx context.Context, tx db.DBTX, inv domain.Invitation) (domain.Invitation, error) {
	email := ""
	if inv.Email != nil {
		email = *inv.Email
	}
	row, err := sqlcgen.New(tx).CreateInvitation(ctx, sqlcgen.CreateInvitationParams{
		TeamID: inv.TeamID, Column2: email, Column3: sqlcgen.IdentityTeamRole(inv.Role),
		TokenHash: inv.TokenHash, Column5: inv.ExpiresAt, CreatedBy: inv.CreatedBy,
	})
	if err != nil {
		return domain.Invitation{}, fmt.Errorf("create invitation: %w", err)
	}
	return invitationFromCreate(row)
}

func (Repo) GetInvitation(ctx context.Context, tx db.DBTX, teamID, id uuid.UUID) (domain.Invitation, error) {
	row, err := sqlcgen.New(tx).GetInvitation(ctx, sqlcgen.GetInvitationParams{TeamID: teamID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Invitation{}, fmt.Errorf("get invitation: %w", apperr.ErrNotFound)
	}
	if err != nil {
		return domain.Invitation{}, fmt.Errorf("get invitation: %w", err)
	}
	return invitationFromGet(row)
}

func (Repo) ListInvitations(ctx context.Context, tx db.DBTX, teamID uuid.UUID, after uuid.UUID, limit int32) ([]domain.Invitation, error) {
	rows, err := sqlcgen.New(tx).ListInvitations(ctx, sqlcgen.ListInvitationsParams{TeamID: teamID, ID: after, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	out := make([]domain.Invitation, 0, len(rows))
	for _, row := range rows {
		inv, err := invitationFromList(row)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, nil
}

func (Repo) RevokeInvitation(ctx context.Context, tx db.DBTX, teamID, id uuid.UUID) error {
	n, err := sqlcgen.New(tx).RevokeInvitation(ctx, sqlcgen.RevokeInvitationParams{TeamID: teamID, ID: id})
	if err != nil {
		return fmt.Errorf("revoke invitation: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("revoke invitation: %w", apperr.ErrNotFound)
	}
	return nil
}

// AcceptInvitation calls the narrow SECURITY DEFINER function: the RLS hides the
// invitation (and refuses the membership) because the caller is not a member yet.
func (Repo) AcceptInvitation(ctx context.Context, tx db.DBTX, tokenHash []byte) (domain.InvitationAcceptance, error) {
	var teamID uuid.UUID
	var role string
	var consumed bool
	err := tx.QueryRow(ctx, `SELECT out_team_id, out_role::text, out_consumed FROM identity.accept_invitation($1)`, tokenHash).
		Scan(&teamID, &role, &consumed)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "PB010" {
			return domain.InvitationAcceptance{}, domain.ErrInvalidInvitation
		}
		return domain.InvitationAcceptance{}, fmt.Errorf("accept invitation: %w", err)
	}
	parsed, err := domain.ParseRole(role)
	if err != nil {
		return domain.InvitationAcceptance{}, fmt.Errorf("accept invitation: %w", err)
	}
	return domain.InvitationAcceptance{TeamID: teamID, Role: parsed, Consumed: consumed}, nil
}

func (Repo) CancelPendingTransfers(ctx context.Context, tx db.DBTX, teamID uuid.UUID) error {
	if _, err := sqlcgen.New(tx).CancelPendingTransfers(ctx, teamID); err != nil {
		return fmt.Errorf("cancel pending transfers: %w", err)
	}
	return nil
}

func (Repo) CreateTransfer(ctx context.Context, tx db.DBTX, t domain.OwnershipTransfer) (domain.OwnershipTransfer, error) {
	row, err := sqlcgen.New(tx).CreateTransfer(ctx, sqlcgen.CreateTransferParams{
		TeamID: t.TeamID, FromUserID: t.FromUserID, ToUserID: t.ToUserID, CreatedBy: t.CreatedBy, Column5: t.ExpiresAt,
	})
	if err != nil {
		return domain.OwnershipTransfer{}, fmt.Errorf("create transfer: %w", err)
	}
	return transferFromModel(row), nil
}

func (Repo) GetTransferForUpdate(ctx context.Context, tx db.DBTX, teamID, id uuid.UUID) (domain.OwnershipTransfer, error) {
	row, err := sqlcgen.New(tx).GetTransferForUpdate(ctx, sqlcgen.GetTransferForUpdateParams{TeamID: teamID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.OwnershipTransfer{}, domain.ErrTransferInvalid
	}
	if err != nil {
		return domain.OwnershipTransfer{}, fmt.Errorf("get transfer: %w", err)
	}
	return transferFromModel(row), nil
}

func (Repo) ConfirmTransfer(ctx context.Context, tx db.DBTX, teamID, id uuid.UUID) error {
	n, err := sqlcgen.New(tx).ConfirmTransfer(ctx, sqlcgen.ConfirmTransferParams{TeamID: teamID, ID: id})
	if err != nil {
		return fmt.Errorf("confirm transfer: %w", err)
	}
	if n == 0 {
		return domain.ErrTransferInvalid
	}
	return nil
}

func (Repo) CancelTransfer(ctx context.Context, tx db.DBTX, teamID, id uuid.UUID) error {
	n, err := sqlcgen.New(tx).CancelTransfer(ctx, sqlcgen.CancelTransferParams{TeamID: teamID, ID: id})
	if err != nil {
		return fmt.Errorf("cancel transfer: %w", err)
	}
	if n == 0 {
		return domain.ErrTransferInvalid
	}
	return nil
}

func (Repo) GetIdempotencyKey(ctx context.Context, tx db.DBTX, userID, requestID uuid.UUID) ([]byte, []byte, error) {
	row, err := sqlcgen.New(tx).GetIdempotencyKey(ctx, sqlcgen.GetIdempotencyKeyParams{UserID: userID, RequestID: requestID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get idempotency key: %w", err)
	}
	return row.RequestHash, row.Response, nil
}

func (Repo) InsertIdempotencyKey(ctx context.Context, tx db.DBTX, requestID, userID uuid.UUID, method string, requestHash []byte) (bool, error) {
	_, err := sqlcgen.New(tx).InsertIdempotencyKey(ctx, sqlcgen.InsertIdempotencyKeyParams{
		RequestID: requestID, UserID: userID, Method: method, RequestHash: requestHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert idempotency key: %w", err)
	}
	return true, nil
}

func (Repo) SetIdempotencyResponse(ctx context.Context, tx db.DBTX, userID, requestID uuid.UUID, response []byte) error {
	if err := sqlcgen.New(tx).SetIdempotencyResponse(ctx, sqlcgen.SetIdempotencyResponseParams{
		UserID: userID, RequestID: requestID, Response: response,
	}); err != nil {
		return fmt.Errorf("set idempotency response: %w", err)
	}
	return nil
}

// --- mapping ---------------------------------------------------------------

func teamFromCreate(r sqlcgen.CreateTeamRow) domain.Team {
	return domain.Team{ID: r.ID, Name: r.Name, Category: r.Category, Club: r.Club, CountryCode: r.CountryCode,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, Version: r.Version}
}

func teamFromGet(r sqlcgen.GetTeamRow) domain.Team {
	return domain.Team{ID: r.ID, Name: r.Name, Category: r.Category, Club: r.Club, CountryCode: r.CountryCode,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, Version: r.Version}
}

func teamFromUpdate(r sqlcgen.UpdateTeamRow) domain.Team {
	return domain.Team{ID: r.ID, Name: r.Name, Category: r.Category, Club: r.Club, CountryCode: r.CountryCode,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, Version: r.Version}
}

func transferFromModel(r sqlcgen.IdentityOwnershipTransfer) domain.OwnershipTransfer {
	return domain.OwnershipTransfer{ID: r.ID, TeamID: r.TeamID, FromUserID: r.FromUserID, ToUserID: r.ToUserID,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
		ConfirmedAt: r.ConfirmedAt, CancelledAt: r.CancelledAt}
}

func invitationFromCreate(r sqlcgen.CreateInvitationRow) (domain.Invitation, error) {
	role, err := domain.ParseRole(r.Role)
	if err != nil {
		return domain.Invitation{}, err
	}
	return domain.Invitation{ID: r.ID, TeamID: r.TeamID, Email: r.Email, Role: role, ExpiresAt: r.ExpiresAt,
		AcceptedAt: r.AcceptedAt, RevokedAt: r.RevokedAt, CreatedAt: r.CreatedAt}, nil
}

func invitationFromGet(r sqlcgen.GetInvitationRow) (domain.Invitation, error) {
	role, err := domain.ParseRole(r.Role)
	if err != nil {
		return domain.Invitation{}, err
	}
	return domain.Invitation{ID: r.ID, TeamID: r.TeamID, Email: r.Email, Role: role, ExpiresAt: r.ExpiresAt,
		AcceptedAt: r.AcceptedAt, RevokedAt: r.RevokedAt, CreatedAt: r.CreatedAt}, nil
}

func invitationFromList(r sqlcgen.ListInvitationsRow) (domain.Invitation, error) {
	role, err := domain.ParseRole(r.Role)
	if err != nil {
		return domain.Invitation{}, err
	}
	return domain.Invitation{ID: r.ID, TeamID: r.TeamID, Email: r.Email, Role: role, ExpiresAt: r.ExpiresAt,
		AcceptedAt: r.AcceptedAt, RevokedAt: r.RevokedAt, CreatedAt: r.CreatedAt}, nil
}
