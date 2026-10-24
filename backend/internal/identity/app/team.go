package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/audit"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox"
)

const createTeamMethod = "/pitbox.identity.v1.TeamService/CreateTeam"

// CreateTeamInput is the validated input of CreateTeam.
type CreateTeamInput struct {
	RequestID   uuid.UUID
	Name        string
	Category    string
	Club        *string
	CountryCode string
}

// UpdateTeamInput is the input of UpdateTeam. Update selects the fields in the
// request's FieldMask; only the club may be cleared.
type UpdateTeamInput struct {
	RequestID uuid.UUID
	Version   int32
	Update    domain.TeamUpdate
}

// CreateTeam creates a team and makes the caller its owner. It is idempotent per
// request_id: a retry with the same body returns the same team, and a different
// body or method is INVALID_ARGUMENT. A user may own at most MaxOwnedTeams
// teams; a live idempotency replay still works at the limit. A strong factor is
// required by the transport.
func (s *Service) CreateTeam(ctx context.Context, user db.UserID, in CreateTeamInput) (domain.Team, error) {
	team, err := domain.NewTeam(in.Name, in.Category, in.Club, in.CountryCode)
	if err != nil {
		return domain.Team{}, err
	}
	hash, err := hashCreateTeam(in)
	if err != nil {
		return domain.Team{}, fmt.Errorf("hash create team: %w", err)
	}

	var result domain.Team
	err = s.db.WithNewTeamTx(ctx, user, func(ctx context.Context, tx db.DBTX, teamID db.TeamID) error {
		userID := uuid.UUID(user)
		if err := s.store.LockUserForOwnership(ctx, tx, userID); err != nil {
			return err
		}
		existingMethod, existingHash, existingResponse, err := s.store.GetIdempotencyKey(ctx, tx, userID, in.RequestID)
		switch {
		case err == nil:
			return s.replayCreateTeam(ctx, tx, existingMethod, existingHash, existingResponse, hash, &result)
		case !errors.Is(err, errNotFound):
			return err
		}

		owned, err := s.store.CountOwnedTeams(ctx, tx, userID)
		if err != nil {
			return err
		}
		if owned >= domain.MaxOwnedTeams {
			return domain.ErrTeamLimit
		}

		inserted, err := s.store.InsertIdempotencyKey(ctx, tx, in.RequestID, userID, createTeamMethod, hash)
		if err != nil {
			return err
		}
		if !inserted {
			// Defensive: the account lock above already serialises this user's
			// CreateTeam calls, so a lost claim should not happen. If it does,
			// replay the committed team; nothing of this transaction is kept.
			existingMethod, existingHash, existingResponse, err := s.store.GetIdempotencyKey(ctx, tx, userID, in.RequestID)
			if err != nil {
				return err
			}
			return s.replayCreateTeam(ctx, tx, existingMethod, existingHash, existingResponse, hash, &result)
		}

		team.ID = uuid.UUID(teamID)
		team.CreatedBy = userID
		created, err := s.store.CreateTeam(ctx, tx, team)
		if err != nil {
			return err
		}
		if _, err := s.store.CreateMembership(ctx, tx, domain.Member{
			TeamID: created.ID, UserID: userID, Role: domain.RoleOwner,
		}); err != nil {
			return err
		}
		owner := string(domain.RoleOwner)
		if err := outbox.Record(ctx, tx, outbox.MembershipChanged(created.ID, userID, &owner)); err != nil {
			return err
		}
		if err := audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "team.create", ObjectType: "team", ObjectID: created.ID.String(),
			After:     map[string]any{"name": created.Name, "category": created.Category, "country_code": created.CountryCode},
			RequestID: in.RequestID.String(),
		}); err != nil {
			return err
		}
		if err := s.store.SetIdempotencyResponse(ctx, tx, userID, in.RequestID, created.ID[:]); err != nil {
			return err
		}
		result = created
		return nil
	})
	if err != nil {
		return domain.Team{}, err
	}
	s.syncRole(ctx, uuid.UUID(user), result.ID)
	return result, nil
}

func (s *Service) replayCreateTeam(ctx context.Context, tx db.DBTX, existingMethod string, existingHash, existingResponse, wantHash []byte, result *domain.Team) error {
	if existingMethod != createTeamMethod || !bytes.Equal(existingHash, wantHash) {
		return fmt.Errorf("create team: %w", domain.ErrInvalid)
	}
	if len(existingResponse) != 16 {
		return errors.New("store: idempotency response is not a team id")
	}
	id, err := uuid.FromBytes(existingResponse)
	if err != nil {
		return errors.New("store: idempotency response is not a team id")
	}
	team, err := s.store.GetTeam(ctx, tx, id)
	if err != nil {
		return err
	}
	*result = team
	return nil
}

func hashCreateTeam(in CreateTeamInput) ([]byte, error) {
	body := struct {
		Name        string  `json:"name"`
		Category    string  `json:"category"`
		Club        *string `json:"club"`
		CountryCode string  `json:"country_code"`
	}{Name: in.Name, Category: in.Category, Club: in.Club, CountryCode: in.CountryCode}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return sum[:], nil
}

// ListMyTeams lists the caller's teams. It uses WithUserTx: the RLS team_visible
// policy shows exactly the teams the user belongs to.
func (s *Service) ListMyTeams(ctx context.Context, user db.UserID, size int32, pageToken string) ([]domain.TeamMembership, string, error) {
	var cursor teamsCursor
	if err := decodeCursor(pageToken, &cursor); err != nil {
		return nil, "", err
	}
	limit := pageSize(size)
	var teams []domain.TeamMembership
	err := s.db.WithUserTx(ctx, user, func(ctx context.Context, tx db.DBTX) error {
		var err error
		teams, err = s.store.ListMyTeams(ctx, tx, uuid.UUID(user), cursor.ID, limit+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(teams) > int(limit) {
		teams = teams[:limit]
		next = encodeCursor(teamsCursor{ID: teams[len(teams)-1].Team.ID})
	}
	return teams, next, nil
}

// GetTeam returns one team. A guest gets NOT_FOUND (the member Check fails).
func (s *Service) GetTeam(ctx context.Context, user db.UserID, teamID db.TeamID) (domain.Team, error) {
	var team domain.Team
	err := s.withMemberReadTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX) error {
		var err error
		team, err = s.store.GetTeam(ctx, tx, uuid.UUID(teamID))
		return err
	})
	return team, err
}

// UpdateTeam updates the fields selected by the request mask. Only the owner may
// call it; a stale version is ABORTED.
func (s *Service) UpdateTeam(ctx context.Context, user db.UserID, teamID db.TeamID, in UpdateTeamInput) (domain.Team, error) {
	var updated domain.Team
	err := s.withOwnerTx(ctx, user, teamID, func(ctx context.Context, tx db.DBTX, _ db.Membership) error {
		current, err := s.store.GetTeam(ctx, tx, uuid.UUID(teamID))
		if err != nil {
			return err
		}
		next, err := domain.ApplyTeamUpdate(current, in.Update)
		if err != nil {
			return err
		}
		next.Version = in.Version
		updated, err = s.store.UpdateTeam(ctx, tx, next)
		if err != nil {
			return err
		}
		return audit.RecordTeam(ctx, tx, audit.TeamEvent{
			Action: "team.update", ObjectType: "team", ObjectID: updated.ID.String(),
			Before: teamAudit(current), After: teamAudit(updated), RequestID: in.RequestID.String(),
		})
	})
	return updated, err
}

func teamAudit(t domain.Team) map[string]any {
	return map[string]any{
		"name": t.Name, "category": t.Category, "club": t.Club, "country_code": t.CountryCode, "version": t.Version,
	}
}
