package app_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/domain"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/identity/store"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/apperr"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/authz"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/clock"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/outbox"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
)

// fakeAuthz is an in-memory OpenFGA. It keeps the same relations the real model
// has; the use cases only ask for owner/member.
type fakeAuthz struct {
	mu    sync.Mutex
	roles map[string]string // "user|team" -> role
}

func newFakeAuthz() *fakeAuthz { return &fakeAuthz{roles: map[string]string{}} }

func (f *fakeAuthz) key(user, team string) string { return user + "|" + team }

func (f *fakeAuthz) SetTeamRole(_ context.Context, user, team string, role *string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if role == nil || *role == "guest" {
		delete(f.roles, f.key(user, team))
		return nil
	}
	f.roles[f.key(user, team)] = *role
	return nil
}

func (f *fakeAuthz) Check(_ context.Context, user, relation, object string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	role := f.roles[f.key(user, object)]
	allowed := false
	switch relation {
	case "owner":
		allowed = role == "owner"
	case "mechanic":
		allowed = role == "owner" || role == "mechanic"
	case "member":
		allowed = role == "owner" || role == "mechanic" || role == "pilot"
	case "pilot":
		allowed = role == "pilot"
	}
	if !allowed {
		return authz.ErrDenied
	}
	return nil
}

func (f *fakeAuthz) set(user, team, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.roles[f.key(user, team)] = role
}

type harness struct {
	pg    testutil.Postgres
	pool  interface{ Close() }
	svc   *app.Service
	authz *fakeAuthz
	ctx   context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pg := testutil.StartPostgres(t)
	pool, err := db.Open(context.Background(), pg.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	az := newFakeAuthz()
	log := slog.New(slog.DiscardHandler)
	proc, err := outbox.NewProcessor(pool, az, outbox.DefaultOptions(), log, nil, nil)
	require.NoError(t, err)
	svc, err := app.New(app.Deps{
		DB: db.New(pool), Store: store.NewRepo(), Authz: az, Outbox: proc, Clock: clock.Real(),
		Log: log, Mailer: app.NoopMailer{},
	})
	require.NoError(t, err)
	return &harness{pg: pg, pool: pool, svc: svc, authz: az, ctx: context.Background()}
}

func (h *harness) user(t *testing.T, label string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	h.pg.Exec(t, fmt.Sprintf(
		`INSERT INTO identity.user_account (id, idp_subject, email, display_name) VALUES ('%s', '%s', '%s@example.pt', '%s')`,
		id, label+"-"+id.String(), label, label))
	return id
}

func uid(t *testing.T, id uuid.UUID) db.UserID { t.Helper(); return db.UserID(id) }
func tid(t *testing.T, id uuid.UUID) db.TeamID { t.Helper(); return db.TeamID(id) }

func (h *harness) count(t *testing.T, query string) int {
	t.Helper()
	n, err := strconv.Atoi(h.pg.Exec(t, query))
	require.NoError(t, err)
	return n
}

func (h *harness) createTeam(t *testing.T, owner uuid.UUID, name string) domain.Team {
	t.Helper()
	team, err := h.svc.CreateTeam(h.ctx, uid(t, owner), app.CreateTeamInput{
		RequestID: uuid.Must(uuid.NewV7()), Name: name, Category: "1/8 TT nitro", CountryCode: "PT",
	})
	require.NoError(t, err)
	return team
}

func TestTeamLifecycle(t *testing.T) {
	h := newHarness(t)
	owner, mechanic := h.user(t, "owner"), h.user(t, "mechanic")

	team := h.createTeam(t, owner, "Equipa")
	require.Equal(t, int32(1), team.Version)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'team.create'`, team.ID)))
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM platform.outbox WHERE team_id = '%s' AND event_type = 'membership.changed'`, team.ID)))

	teams, next, err := h.svc.ListMyTeams(h.ctx, uid(t, owner), 10, "")
	require.NoError(t, err)
	require.Empty(t, next)
	require.Len(t, teams, 1)
	require.Equal(t, domain.RoleOwner, teams[0].Role)

	got, err := h.svc.GetTeam(h.ctx, uid(t, owner), tid(t, team.ID))
	require.NoError(t, err)
	require.Nil(t, got.PendingTransfer)
	require.Equal(t, int32(1), got.Version)

	name := "Equipa Nova"
	updated, err := h.svc.UpdateTeam(h.ctx, uid(t, owner), tid(t, team.ID), app.UpdateTeamInput{
		RequestID: uuid.Must(uuid.NewV7()), Version: got.Version,
		Update: domain.TeamUpdate{Name: &name, ClubSet: true},
	})
	require.NoError(t, err)
	require.Equal(t, "Equipa Nova", updated.Name)
	require.Nil(t, updated.Club)
	require.Equal(t, int32(2), updated.Version)
	_, err = h.svc.UpdateTeam(h.ctx, uid(t, owner), tid(t, team.ID), app.UpdateTeamInput{
		RequestID: uuid.Must(uuid.NewV7()), Version: 1, Update: domain.TeamUpdate{Name: &name},
	})
	require.ErrorIs(t, err, domain.ErrConflict)

	email := "mechanic@example.pt"
	inv, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic, Email: &email,
	})
	require.NoError(t, err)
	require.Len(t, token, domain.TokenLength)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'invitation.create'`, team.ID)))

	accepted, err := h.svc.AcceptInvitation(h.ctx, uid(t, mechanic), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)
	require.Equal(t, domain.RoleMechanic, accepted.Role)
	require.Equal(t, team.ID, accepted.Team.ID)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'invitation.accepted'`, team.ID)))

	members, _, err := h.svc.ListMembers(h.ctx, uid(t, owner), tid(t, team.ID), 10, "")
	require.NoError(t, err)
	require.Len(t, members, 2)
	var target domain.Member
	for _, m := range members {
		if m.UserID == mechanic {
			target = m
		}
	}
	require.Equal(t, domain.RoleMechanic, target.Role)

	pilot, err := h.svc.ChangeMemberRole(h.ctx, uid(t, owner), tid(t, team.ID), app.ChangeMemberRoleInput{
		RequestID: uuid.Must(uuid.NewV7()), UserID: mechanic, Role: domain.RolePilot, Version: target.Version,
	})
	require.NoError(t, err)
	require.Equal(t, domain.RolePilot, pilot.Role)

	require.NoError(t, h.svc.RemoveMember(h.ctx, uid(t, owner), tid(t, team.ID), app.RemoveMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), UserID: mechanic, Version: pilot.Version,
	}))
	require.Equal(t, 0, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.membership WHERE team_id = '%s' AND user_id = '%s'`, team.ID, mechanic)))
	require.Equal(t, 4, h.count(t, fmt.Sprintf(`SELECT count(*) FROM platform.outbox WHERE team_id = '%s' AND event_type = 'membership.changed'`, team.ID)))
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'member.removed'`, team.ID)))

	// The invitation row is kept for the audit trail, but it is no longer pending.
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.invitation WHERE id = '%s' AND accepted_at IS NOT NULL`, inv.ID)))

	// Every audit row has the author and the request id, and all writes left one.
	require.Equal(t, 0, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND (request_id IS NULL OR actor_id = '')`, team.ID)))
	for _, action := range []string{"team.create", "team.update", "invitation.create", "invitation.accepted", "member.role_changed", "member.removed"} {
		require.GreaterOrEqual(t, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = '%s'`, team.ID, action)), 1, action)
	}
}

func TestCreateTeamIdempotency(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	requestID := uuid.Must(uuid.NewV7())

	first, err := h.svc.CreateTeam(h.ctx, uid(t, owner), app.CreateTeamInput{
		RequestID: requestID, Name: "Equipa", Category: "1/8", CountryCode: "PT",
	})
	require.NoError(t, err)
	second, err := h.svc.CreateTeam(h.ctx, uid(t, owner), app.CreateTeamInput{
		RequestID: requestID, Name: "Equipa", Category: "1/8", CountryCode: "PT",
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.team WHERE created_by = '%s'`, owner)))

	_, err = h.svc.CreateTeam(h.ctx, uid(t, owner), app.CreateTeamInput{
		RequestID: requestID, Name: "Outra", Category: "1/8", CountryCode: "PT",
	})
	require.ErrorIs(t, err, domain.ErrInvalid)
}

func TestCreateTeamOwnershipLimit(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	firstRequest := uuid.Must(uuid.NewV7())
	firstInput := app.CreateTeamInput{
		RequestID: firstRequest, Name: "Equipa 1", Category: "1/8", CountryCode: "PT",
	}

	first, err := h.svc.CreateTeam(h.ctx, uid(t, owner), firstInput)
	require.NoError(t, err)
	for i := 2; i <= domain.MaxOwnedTeams; i++ {
		h.createTeam(t, owner, fmt.Sprintf("Equipa %d", i))
	}
	require.Equal(t, domain.MaxOwnedTeams, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.team WHERE created_by = '%s' AND deleted_at IS NULL`, owner)))

	// A live idempotency key still replays at the limit.
	replay, err := h.svc.CreateTeam(h.ctx, uid(t, owner), firstInput)
	require.NoError(t, err)
	require.Equal(t, first.ID, replay.ID)

	requestID := uuid.Must(uuid.NewV7())
	_, err = h.svc.CreateTeam(h.ctx, uid(t, owner), app.CreateTeamInput{
		RequestID: requestID, Name: "Equipa 11", Category: "1/8", CountryCode: "PT",
	})
	require.ErrorIs(t, err, domain.ErrTeamLimit)
	require.ErrorIs(t, err, apperr.ErrFailedPrecondition)

	require.Equal(t, domain.MaxOwnedTeams, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.team WHERE created_by = '%s' AND deleted_at IS NULL`, owner)))
	require.Equal(t, 0, h.count(t, fmt.Sprintf(`SELECT count(*) FROM platform.idempotency_key WHERE user_id = '%s' AND request_id = '%s'`, owner, requestID)))
}

func TestTeamLimitCountsOnlyOwnedActiveTeams(t *testing.T) {
	h := newHarness(t)
	owner, other := h.user(t, "owner"), h.user(t, "other")

	// Being a mechanic in ten teams does not count towards the owner limit.
	for i := 1; i <= domain.MaxOwnedTeams; i++ {
		h.createTeam(t, other, fmt.Sprintf("Equipa de outro %d", i))
	}
	h.pg.Exec(t, fmt.Sprintf(
		`INSERT INTO identity.membership (team_id, user_id, role)
		 SELECT id, '%s', 'mechanic' FROM identity.team WHERE created_by = '%s'`,
		owner, other))

	// A deleted team does not count either.
	deleted := h.createTeam(t, owner, "Equipa apagada")
	h.pg.Exec(t, fmt.Sprintf(`UPDATE identity.team SET deleted_at = now() WHERE id = '%s'`, deleted.ID))

	for i := 1; i <= domain.MaxOwnedTeams; i++ {
		h.createTeam(t, owner, fmt.Sprintf("Equipa própria %d", i))
	}
	_, err := h.svc.CreateTeam(h.ctx, uid(t, owner), app.CreateTeamInput{
		RequestID: uuid.Must(uuid.NewV7()), Name: "Equipa a mais", Category: "1/8", CountryCode: "PT",
	})
	require.ErrorIs(t, err, domain.ErrTeamLimit)
}

func TestCreateTeamIdempotencyExpired(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	requestID := uuid.Must(uuid.NewV7())
	in := app.CreateTeamInput{
		RequestID: requestID, Name: "Equipa", Category: "1/8", CountryCode: "PT",
	}

	first, err := h.svc.CreateTeam(h.ctx, uid(t, owner), in)
	require.NoError(t, err)
	h.pg.Exec(t, fmt.Sprintf(
		`UPDATE platform.idempotency_key
		 SET created_at = now() - interval '8 days', expires_at = now() - interval '1 day'
		 WHERE user_id = '%s' AND request_id = '%s'`,
		owner, requestID))

	second, err := h.svc.CreateTeam(h.ctx, uid(t, owner), in)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, 2, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.team WHERE created_by = '%s'`, owner)))

	// The expired key was taken over: the next retry replays the new team.
	replay, err := h.svc.CreateTeam(h.ctx, uid(t, owner), in)
	require.NoError(t, err)
	require.Equal(t, second.ID, replay.ID)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(
		`SELECT count(*) FROM platform.idempotency_key
		 WHERE user_id = '%s' AND request_id = '%s' AND expires_at > now() AND response IS NOT NULL`,
		owner, requestID)))
}

func TestCreateTeamIdempotencyRejectsAnotherMethod(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	requestID := uuid.Must(uuid.NewV7())
	in := app.CreateTeamInput{
		RequestID: requestID, Name: "Equipa", Category: "1/8", CountryCode: "PT",
	}

	_, err := h.svc.CreateTeam(h.ctx, uid(t, owner), in)
	require.NoError(t, err)
	h.pg.Exec(t, fmt.Sprintf(
		`UPDATE platform.idempotency_key SET method = '/pitbox.setup.v1.SetupService/CreateSetup'
		 WHERE user_id = '%s' AND request_id = '%s'`,
		owner, requestID))

	_, err = h.svc.CreateTeam(h.ctx, uid(t, owner), in)
	require.ErrorIs(t, err, domain.ErrInvalid)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.team WHERE created_by = '%s'`, owner)))
}

func TestInvitationFailuresAreIndistinguishable(t *testing.T) {
	h := newHarness(t)
	owner, userA, userB := h.user(t, "owner"), h.user(t, "a"), h.user(t, "b")
	team := h.createTeam(t, owner, "Equipa")

	email := "a@example.pt"
	inv, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot, Email: &email,
	})
	require.NoError(t, err)

	// Wrong account: the email does not match.
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, userB), uuid.Must(uuid.NewV7()), token)
	require.ErrorIs(t, err, domain.ErrInvalidInvitation)
	require.Equal(t, 0, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.invitation WHERE id = '%s' AND accepted_at IS NOT NULL`, inv.ID)))

	// Tampered token.
	tampered := "A" + token[1:]
	if tampered == token {
		tampered = "B" + token[1:]
	}
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, userA), uuid.Must(uuid.NewV7()), tampered)
	require.ErrorIs(t, err, domain.ErrInvalidInvitation)

	// Revoked.
	require.NoError(t, h.svc.RevokeInvitation(h.ctx, uid(t, owner), tid(t, team.ID), inv.ID, uuid.Must(uuid.NewV7())))
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, userA), uuid.Must(uuid.NewV7()), token)
	require.ErrorIs(t, err, domain.ErrInvalidInvitation)

	// Used by another account.
	inv2, token2, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, userA), uuid.Must(uuid.NewV7()), token2)
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, userB), uuid.Must(uuid.NewV7()), token2)
	require.ErrorIs(t, err, domain.ErrInvalidInvitation)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.invitation WHERE id = '%s' AND accepted_by = '%s'`, inv2.ID, userA)))

	// Expired.
	inv3, token3, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
	})
	require.NoError(t, err)
	h.pg.Exec(t, fmt.Sprintf(`UPDATE identity.invitation SET created_at = now() - interval '8 days', expires_at = now() - interval '1 day' WHERE id = '%s'`, inv3.ID))
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, userB), uuid.Must(uuid.NewV7()), token3)
	require.ErrorIs(t, err, domain.ErrInvalidInvitation)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'invitation.revoked'`, team.ID)))
}

func TestAcceptIsIdempotentAndDoesNotChangeAnExistingRole(t *testing.T) {
	h := newHarness(t)
	owner, user := h.user(t, "owner"), h.user(t, "user")
	team := h.createTeam(t, owner, "Equipa")

	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
	})
	require.NoError(t, err)
	first, err := h.svc.AcceptInvitation(h.ctx, uid(t, user), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)
	require.Equal(t, domain.RolePilot, first.Role)
	second, err := h.svc.AcceptInvitation(h.ctx, uid(t, user), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)
	require.Equal(t, first.Team.ID, second.Team.ID)
	require.Equal(t, domain.RolePilot, second.Role)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.membership WHERE team_id = '%s' AND user_id = '%s'`, team.ID, user)))

	// Already a member by another route: the invitation is not consumed and the
	// role does not change.
	inv, token2, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	third, err := h.svc.AcceptInvitation(h.ctx, uid(t, user), uuid.Must(uuid.NewV7()), token2)
	require.NoError(t, err)
	require.Equal(t, domain.RolePilot, third.Role)
	require.Equal(t, 0, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.invitation WHERE id = '%s' AND accepted_at IS NOT NULL`, inv.ID)))
}

func TestAuthorizationAndLastOwner(t *testing.T) {
	h := newHarness(t)
	owner, mechanic, stranger := h.user(t, "owner"), h.user(t, "mechanic"), h.user(t, "stranger")
	team := h.createTeam(t, owner, "Equipa")

	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, mechanic), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)

	// Mechanic cannot invite: member passes, owner does not.
	_, _, err = h.svc.InviteMember(h.ctx, uid(t, mechanic), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
	})
	require.ErrorIs(t, err, domain.ErrForbidden)

	// A user outside the team gets NOT_FOUND on every kind of RPC.
	_, err = h.svc.GetTeam(h.ctx, uid(t, stranger), tid(t, team.ID))
	require.ErrorIs(t, err, apperr.ErrNotFound, "not a member is NOT_FOUND")
	_, _, err = h.svc.ListMembers(h.ctx, uid(t, stranger), tid(t, team.ID), 10, "")
	require.ErrorIs(t, err, apperr.ErrNotFound)
	// Invitations are owner-only: a mechanic is a member without the role.
	_, _, err = h.svc.ListInvitations(h.ctx, uid(t, mechanic), tid(t, team.ID), 10, "")
	require.ErrorIs(t, err, domain.ErrForbidden)

	// A guest can leave: LeaveTeam does not use OpenFGA.
	guest := h.user(t, "guest")
	h.pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.membership (team_id, user_id, role) VALUES ('%s', '%s', 'guest')`, team.ID, guest))
	require.NoError(t, h.svc.LeaveTeam(h.ctx, uid(t, guest), tid(t, team.ID), uuid.Must(uuid.NewV7())))
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'member.left'`, team.ID)))

	// The last owner cannot leave, be removed or be demoted.
	require.ErrorIs(t, h.svc.LeaveTeam(h.ctx, uid(t, owner), tid(t, team.ID), uuid.Must(uuid.NewV7())), domain.ErrOwnerRequired)
	members, _, err := h.svc.ListMembers(h.ctx, uid(t, owner), tid(t, team.ID), 10, "")
	require.NoError(t, err)
	var ownerMember domain.Member
	for _, m := range members {
		if m.UserID == owner {
			ownerMember = m
		}
	}
	require.ErrorIs(t, h.svc.RemoveMember(h.ctx, uid(t, owner), tid(t, team.ID), app.RemoveMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), UserID: owner, Version: ownerMember.Version,
	}), domain.ErrOwnerRequired)
	_, err = h.svc.ChangeMemberRole(h.ctx, uid(t, owner), tid(t, team.ID), app.ChangeMemberRoleInput{
		RequestID: uuid.Must(uuid.NewV7()), UserID: owner, Role: domain.RolePilot, Version: ownerMember.Version,
	})
	require.ErrorIs(t, err, domain.ErrOwnerRequired)

	// A demoted mechanic whose old OpenFGA tuple still says owner is refused by
	// the role read inside the transaction.
	h.authz.set("user:"+mechanic.String(), "team:"+team.ID.String(), "owner")
	require.ErrorIs(t, func() error {
		_, _, err := h.svc.InviteMember(h.ctx, uid(t, mechanic), tid(t, team.ID), app.InviteMemberInput{
			RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
		})
		return err
	}(), domain.ErrForbidden)
	// The same tuple was not forged: the fake Check(owner) passed but the DB role
	// is mechanic. Restore the honest value for the rest of the test.
	h.authz.set("user:"+mechanic.String(), "team:"+team.ID.String(), "mechanic")
}

func TestOwnershipTransfer(t *testing.T) {
	h := newHarness(t)
	owner, mechanic := h.user(t, "owner"), h.user(t, "mechanic")
	team := h.createTeam(t, owner, "Equipa")
	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, mechanic), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)

	transfer, err := h.svc.InitiateTransfer(h.ctx, uid(t, owner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: mechanic,
	})
	require.NoError(t, err)
	got, err := h.svc.GetTeam(h.ctx, uid(t, mechanic), tid(t, team.ID))
	require.NoError(t, err)
	require.NotNil(t, got.PendingTransfer)
	require.Equal(t, transfer.ID, got.PendingTransfer.ID)

	membership, err := h.svc.AcceptTransfer(h.ctx, uid(t, mechanic), tid(t, team.ID), uuid.Must(uuid.NewV7()), transfer.ID)
	require.NoError(t, err)
	require.Equal(t, domain.RoleOwner, membership.Role)
	old, err := h.svc.GetTeam(h.ctx, uid(t, owner), tid(t, team.ID))
	require.NoError(t, err)
	require.Nil(t, old.PendingTransfer)

	var oldRole, newRole string
	h.pg.Exec(t, fmt.Sprintf(`SELECT role::text FROM identity.membership WHERE team_id = '%s' AND user_id = '%s'`, team.ID, owner))
	oldRole = h.pg.Exec(t, fmt.Sprintf(`SELECT role::text FROM identity.membership WHERE team_id = '%s' AND user_id = '%s'`, team.ID, owner))
	newRole = h.pg.Exec(t, fmt.Sprintf(`SELECT role::text FROM identity.membership WHERE team_id = '%s' AND user_id = '%s'`, team.ID, mechanic))
	require.Equal(t, "mechanic", oldRole)
	require.Equal(t, "owner", newRole)
	require.Equal(t, 4, h.count(t, fmt.Sprintf(`SELECT count(*) FROM platform.outbox WHERE team_id = '%s' AND event_type = 'membership.changed'`, team.ID)))
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'ownership.transfer_initiated'`, team.ID)))
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'ownership.transferred'`, team.ID)))
}

func TestExpiredTransferDoesNotBlockANewOne(t *testing.T) {
	h := newHarness(t)
	owner, mechanic := h.user(t, "owner"), h.user(t, "mechanic")
	team := h.createTeam(t, owner, "Equipa")
	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, mechanic), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)

	first, err := h.svc.InitiateTransfer(h.ctx, uid(t, owner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: mechanic,
	})
	require.NoError(t, err)
	h.pg.Exec(t, fmt.Sprintf(`UPDATE identity.ownership_transfer SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE id = '%s'`, first.ID))

	second, err := h.svc.InitiateTransfer(h.ctx, uid(t, owner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: mechanic,
	})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.ownership_transfer WHERE team_id = '%s' AND confirmed_at IS NULL AND cancelled_at IS NULL`, team.ID)))
}

func TestRemovingTheRecipientCancelsThePendingTransfer(t *testing.T) {
	h := newHarness(t)
	owner, mechanic := h.user(t, "owner"), h.user(t, "mechanic")
	team := h.createTeam(t, owner, "Equipa")
	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, mechanic), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)
	_, err = h.svc.InitiateTransfer(h.ctx, uid(t, owner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: mechanic,
	})
	require.NoError(t, err)

	members, _, err := h.svc.ListMembers(h.ctx, uid(t, owner), tid(t, team.ID), 10, "")
	require.NoError(t, err)
	var target domain.Member
	for _, m := range members {
		if m.UserID == mechanic {
			target = m
		}
	}
	require.NoError(t, h.svc.RemoveMember(h.ctx, uid(t, owner), tid(t, team.ID), app.RemoveMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), UserID: mechanic, Version: target.Version,
	}))
	require.Equal(t, 0, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.ownership_transfer WHERE team_id = '%s'`, team.ID)))
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'ownership.transfer_cancelled'`, team.ID)))
}

func TestInvitationLimit(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.createTeam(t, owner, "Equipa")

	for i := 0; i < 50; i++ {
		_, _, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
			RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
		})
		require.NoError(t, err)
	}
	_, _, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
	})
	require.ErrorIs(t, err, domain.ErrInvitationLimit)
}

func TestExpiredInvitationDoesNotBlockANewOne(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.createTeam(t, owner, "Equipa")
	inv, _, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
	})
	require.NoError(t, err)
	h.pg.Exec(t, fmt.Sprintf(`UPDATE identity.invitation SET created_at = now() - interval '8 days', expires_at = now() - interval '1 day' WHERE id = '%s'`, inv.ID))
	// Expired invitations do not count towards the limit.
	for i := 0; i < 50; i++ {
		_, _, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
			RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
		})
		require.NoError(t, err)
	}
}

func TestPagination(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	first := h.createTeam(t, owner, "A")
	second := h.createTeam(t, owner, "B")

	page, next, err := h.svc.ListMyTeams(h.ctx, uid(t, owner), 1, "")
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.NotEmpty(t, next)
	page2, next2, err := h.svc.ListMyTeams(h.ctx, uid(t, owner), 1, next)
	require.NoError(t, err)
	require.Len(t, page2, 1)
	require.Empty(t, next2)
	require.NotEqual(t, first.ID, second.ID)
	require.NotEqual(t, page[0].Team.ID, page2[0].Team.ID)
}

var _ = errors.Is
var _ = time.Now

func TestStrangerGetsNotFoundEverywhere(t *testing.T) {
	h := newHarness(t)
	owner, stranger := h.user(t, "owner"), h.user(t, "stranger")
	team := h.createTeam(t, owner, "Equipa")
	random := uuid.Must(uuid.NewV7())
	name := "x"
	checks := []struct {
		name string
		call func() error
	}{
		{"GetTeam", func() error { _, err := h.svc.GetTeam(h.ctx, uid(t, stranger), tid(t, team.ID)); return err }},
		{"UpdateTeam", func() error {
			_, err := h.svc.UpdateTeam(h.ctx, uid(t, stranger), tid(t, team.ID), app.UpdateTeamInput{
				RequestID: uuid.Must(uuid.NewV7()), Version: 1, Update: domain.TeamUpdate{Name: &name},
			})
			return err
		}},
		{"ListMembers", func() error {
			_, _, err := h.svc.ListMembers(h.ctx, uid(t, stranger), tid(t, team.ID), 10, "")
			return err
		}},
		{"ListInvitations", func() error {
			_, _, err := h.svc.ListInvitations(h.ctx, uid(t, stranger), tid(t, team.ID), 10, "")
			return err
		}},
		{"InviteMember", func() error {
			_, _, err := h.svc.InviteMember(h.ctx, uid(t, stranger), tid(t, team.ID), app.InviteMemberInput{
				RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
			})
			return err
		}},
		{"ChangeMemberRole", func() error {
			_, err := h.svc.ChangeMemberRole(h.ctx, uid(t, stranger), tid(t, team.ID), app.ChangeMemberRoleInput{
				RequestID: uuid.Must(uuid.NewV7()), UserID: random, Role: domain.RolePilot, Version: 1,
			})
			return err
		}},
		{"RemoveMember", func() error {
			return h.svc.RemoveMember(h.ctx, uid(t, stranger), tid(t, team.ID), app.RemoveMemberInput{
				RequestID: uuid.Must(uuid.NewV7()), UserID: random, Version: 1,
			})
		}},
		{"RevokeInvitation", func() error {
			return h.svc.RevokeInvitation(h.ctx, uid(t, stranger), tid(t, team.ID), random, uuid.Must(uuid.NewV7()))
		}},
		{"InitiateTransfer", func() error {
			_, err := h.svc.InitiateTransfer(h.ctx, uid(t, stranger), tid(t, team.ID), app.InitiateTransferInput{
				RequestID: uuid.Must(uuid.NewV7()), ToUserID: random,
			})
			return err
		}},
		{"AcceptTransfer", func() error {
			_, err := h.svc.AcceptTransfer(h.ctx, uid(t, stranger), tid(t, team.ID), uuid.Must(uuid.NewV7()), random)
			return err
		}},
		{"CancelTransfer", func() error {
			return h.svc.CancelTransfer(h.ctx, uid(t, stranger), tid(t, team.ID), uuid.Must(uuid.NewV7()), random)
		}},
		{"LeaveTeam", func() error {
			return h.svc.LeaveTeam(h.ctx, uid(t, stranger), tid(t, team.ID), uuid.Must(uuid.NewV7()))
		}},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, tc.call(), apperr.ErrNotFound)
		})
	}
}

func TestMemberAndInvitationPagination(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.createTeam(t, owner, "Equipa")

	for i := 0; i < 2; i++ {
		user := h.user(t, fmt.Sprintf("member%d", i))
		_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
			RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
		})
		require.NoError(t, err)
		_, err = h.svc.AcceptInvitation(h.ctx, uid(t, user), uuid.Must(uuid.NewV7()), token)
		require.NoError(t, err)
	}

	seen := map[uuid.UUID]bool{}
	token := ""
	for {
		members, next, err := h.svc.ListMembers(h.ctx, uid(t, owner), tid(t, team.ID), 1, token)
		require.NoError(t, err)
		require.LessOrEqual(t, len(members), 1)
		for _, m := range members {
			seen[m.UserID] = true
		}
		if next == "" {
			break
		}
		token = next
	}
	require.Len(t, seen, 3) // owner + two members

	for i := 0; i < 3; i++ {
		_, _, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
			RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
		})
		require.NoError(t, err)
	}
	invitations := 0
	token = ""
	for {
		page, next, err := h.svc.ListInvitations(h.ctx, uid(t, owner), tid(t, team.ID), 1, token)
		require.NoError(t, err)
		invitations += len(page)
		if next == "" {
			break
		}
		token = next
	}
	require.Equal(t, 3, invitations)
}

func TestCancelTransfer(t *testing.T) {
	h := newHarness(t)
	owner, mechanic := h.user(t, "owner"), h.user(t, "mechanic")
	team := h.createTeam(t, owner, "Equipa")
	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, mechanic), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)

	first, err := h.svc.InitiateTransfer(h.ctx, uid(t, owner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: mechanic,
	})
	require.NoError(t, err)
	require.NoError(t, h.svc.CancelTransfer(h.ctx, uid(t, owner), tid(t, team.ID), uuid.Must(uuid.NewV7()), first.ID))
	got, err := h.svc.GetTeam(h.ctx, uid(t, owner), tid(t, team.ID))
	require.NoError(t, err)
	require.Nil(t, got.PendingTransfer)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'ownership.transfer_cancelled'`, team.ID)))
	require.ErrorIs(t, h.svc.CancelTransfer(h.ctx, uid(t, owner), tid(t, team.ID), uuid.Must(uuid.NewV7()), first.ID), apperr.ErrNotFound)

	// The recipient may also cancel the next one.
	second, err := h.svc.InitiateTransfer(h.ctx, uid(t, owner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: mechanic,
	})
	require.NoError(t, err)
	require.NoError(t, h.svc.CancelTransfer(h.ctx, uid(t, mechanic), tid(t, team.ID), uuid.Must(uuid.NewV7()), second.ID))
	require.Equal(t, 2, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'ownership.transfer_cancelled'`, team.ID)))
}

func TestConcurrentInvitationAcceptance(t *testing.T) {
	h := newHarness(t)
	owner, first, second := h.user(t, "owner"), h.user(t, "first"), h.user(t, "second")
	team := h.createTeam(t, owner, "Equipa")

	// Two different users race for the same link invitation: one enters.
	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
	})
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, user := range []uuid.UUID{first, second} {
		wg.Add(1)
		go func(i int, user uuid.UUID) {
			defer wg.Done()
			_, errs[i] = h.svc.AcceptInvitation(h.ctx, uid(t, user), uuid.Must(uuid.NewV7()), token)
		}(i, user)
	}
	wg.Wait()
	successes, invalid := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrInvalidInvitation):
			invalid++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, invalid)
	require.Equal(t, 2, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.membership WHERE team_id = '%s'`, team.ID)))

	// The same user twice in parallel: both succeed and there is one membership.
	fresh := h.user(t, "fresh")
	_, token2, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
	})
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = h.svc.AcceptInvitation(h.ctx, uid(t, fresh), uuid.Must(uuid.NewV7()), token2)
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.membership WHERE team_id = '%s' AND user_id = '%s'`, team.ID, fresh)))
}

func TestConcurrentCreateTeamIdempotency(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	requestID := uuid.Must(uuid.NewV7())
	in := app.CreateTeamInput{RequestID: requestID, Name: "Equipa", Category: "1/8", CountryCode: "PT"}

	var wg sync.WaitGroup
	ids := make([]uuid.UUID, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var team domain.Team
			team, errs[i] = h.svc.CreateTeam(h.ctx, uid(t, owner), in)
			ids[i] = team.ID
		}(i)
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, ids[0], ids[1])
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.team WHERE created_by = '%s'`, owner)))
}

func TestConcurrentCreateTeamAtOwnershipLimit(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	for i := 1; i < domain.MaxOwnedTeams; i++ {
		h.createTeam(t, owner, fmt.Sprintf("Equipa %d", i))
	}

	requestIDs := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, len(requestIDs))
	for i, requestID := range requestIDs {
		wg.Add(1)
		go func(i int, requestID uuid.UUID) {
			defer wg.Done()
			<-start
			_, errs[i] = h.svc.CreateTeam(h.ctx, uid(t, owner), app.CreateTeamInput{
				RequestID: requestID, Name: fmt.Sprintf("Equipa concorrente %d", i), Category: "1/8", CountryCode: "PT",
			})
		}(i, requestID)
	}
	close(start)
	wg.Wait()

	successes, limited := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrTeamLimit):
			limited++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, limited)
	require.Equal(t, domain.MaxOwnedTeams, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.team WHERE created_by = '%s' AND deleted_at IS NULL`, owner)))
}

func TestAcceptTransferRespectsOwnershipLimit(t *testing.T) {
	h := newHarness(t)
	oldOwner, recipient, other := h.user(t, "old-owner"), h.user(t, "recipient"), h.user(t, "other")
	team := h.createTeam(t, oldOwner, "Equipa")

	_, token, err := h.svc.InviteMember(h.ctx, uid(t, oldOwner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, recipient), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)
	transfer, err := h.svc.InitiateTransfer(h.ctx, uid(t, oldOwner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: recipient,
	})
	require.NoError(t, err)

	owned := make([]domain.Team, 0, domain.MaxOwnedTeams)
	for i := 1; i <= domain.MaxOwnedTeams; i++ {
		owned = append(owned, h.createTeam(t, recipient, fmt.Sprintf("Equipa do destinatário %d", i)))
	}

	_, err = h.svc.AcceptTransfer(h.ctx, uid(t, recipient), tid(t, team.ID), uuid.Must(uuid.NewV7()), transfer.ID)
	require.ErrorIs(t, err, domain.ErrTeamLimit)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(
		`SELECT count(*) FROM identity.ownership_transfer WHERE id = '%s' AND confirmed_at IS NULL AND cancelled_at IS NULL`,
		transfer.ID)))
	require.Equal(t, "owner", h.pg.Exec(t, fmt.Sprintf(
		`SELECT role::text FROM identity.membership WHERE team_id = '%s' AND user_id = '%s'`, team.ID, oldOwner)))
	require.Equal(t, "mechanic", h.pg.Exec(t, fmt.Sprintf(
		`SELECT role::text FROM identity.membership WHERE team_id = '%s' AND user_id = '%s'`, team.ID, recipient)))

	// Free one team through the intended path: transfer its ownership to other.
	free := owned[0]
	_, token, err = h.svc.InviteMember(h.ctx, uid(t, recipient), tid(t, free.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, other), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)
	freeTransfer, err := h.svc.InitiateTransfer(h.ctx, uid(t, recipient), tid(t, free.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: other,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptTransfer(h.ctx, uid(t, other), tid(t, free.ID), uuid.Must(uuid.NewV7()), freeTransfer.ID)
	require.NoError(t, err)

	membership, err := h.svc.AcceptTransfer(h.ctx, uid(t, recipient), tid(t, team.ID), uuid.Must(uuid.NewV7()), transfer.ID)
	require.NoError(t, err)
	require.Equal(t, domain.RoleOwner, membership.Role)
	require.Equal(t, "owner", h.pg.Exec(t, fmt.Sprintf(
		`SELECT role::text FROM identity.membership WHERE team_id = '%s' AND user_id = '%s'`, team.ID, recipient)))
	require.Equal(t, "mechanic", h.pg.Exec(t, fmt.Sprintf(
		`SELECT role::text FROM identity.membership WHERE team_id = '%s' AND user_id = '%s'`, team.ID, oldOwner)))
}

func TestConcurrentAcceptAndCancelTransfer(t *testing.T) {
	h := newHarness(t)
	owner, mechanic := h.user(t, "owner"), h.user(t, "mechanic")
	team := h.createTeam(t, owner, "Equipa")
	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, mechanic), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)
	transfer, err := h.svc.InitiateTransfer(h.ctx, uid(t, owner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: mechanic,
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	var acceptErr, cancelErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, acceptErr = h.svc.AcceptTransfer(h.ctx, uid(t, mechanic), tid(t, team.ID), uuid.Must(uuid.NewV7()), transfer.ID)
	}()
	go func() {
		defer wg.Done()
		cancelErr = h.svc.CancelTransfer(h.ctx, uid(t, owner), tid(t, team.ID), uuid.Must(uuid.NewV7()), transfer.ID)
	}()
	wg.Wait()

	successes := 0
	for _, err := range []error{acceptErr, cancelErr} {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrTransferInvalid):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	require.Equal(t, 1, successes, "exactly one of accept/cancel wins")
	state := h.pg.Exec(t, fmt.Sprintf(`SELECT CASE WHEN confirmed_at IS NOT NULL THEN 'confirmed' ELSE 'cancelled' END FROM identity.ownership_transfer WHERE id = '%s'`, transfer.ID))
	require.Contains(t, []string{"confirmed", "cancelled"}, state)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.membership WHERE team_id = '%s' AND role = 'owner'`, team.ID)))
}

func TestConcurrentInvitationLimit(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.createTeam(t, owner, "Equipa")
	for i := 1; i <= 49; i++ {
		h.pg.Exec(t, fmt.Sprintf(`INSERT INTO identity.invitation (team_id, role, token_hash, expires_at, created_by)
			VALUES ('%s', 'pilot', decode(lpad(to_hex(%d), 64, '0'), 'hex'), now() + interval '1 day', '%s')`, team.ID, i, owner))
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
				RequestID: uuid.Must(uuid.NewV7()), Role: domain.RolePilot,
			})
		}(i)
	}
	wg.Wait()
	successes, limited := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrInvitationLimit):
			limited++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, limited)
	require.Equal(t, 50, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.invitation WHERE team_id = '%s' AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()`, team.ID)))
}

func TestDemotingTheRecipientCancelsTheTransfer(t *testing.T) {
	h := newHarness(t)
	owner, mechanic := h.user(t, "owner"), h.user(t, "mechanic")
	team := h.createTeam(t, owner, "Equipa")
	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, mechanic), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)
	transfer, err := h.svc.InitiateTransfer(h.ctx, uid(t, owner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: mechanic,
	})
	require.NoError(t, err)

	members, _, err := h.svc.ListMembers(h.ctx, uid(t, owner), tid(t, team.ID), 10, "")
	require.NoError(t, err)
	var target domain.Member
	for _, m := range members {
		if m.UserID == mechanic {
			target = m
		}
	}
	updated, err := h.svc.ChangeMemberRole(h.ctx, uid(t, owner), tid(t, team.ID), app.ChangeMemberRoleInput{
		RequestID: uuid.Must(uuid.NewV7()), UserID: mechanic, Role: domain.RoleGuest, Version: target.Version,
	})
	require.NoError(t, err)
	require.Equal(t, domain.RoleGuest, updated.Role)
	got, err := h.svc.GetTeam(h.ctx, uid(t, owner), tid(t, team.ID))
	require.NoError(t, err)
	require.Nil(t, got.PendingTransfer, "demoting the recipient cancels the transfer")
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'ownership.transfer_cancelled'`, team.ID)))
	_, err = h.svc.AcceptTransfer(h.ctx, uid(t, mechanic), tid(t, team.ID), uuid.Must(uuid.NewV7()), transfer.ID)
	require.ErrorIs(t, err, domain.ErrTransferInvalid)
}

func TestAcceptTransferRejectsARecipientWhoIsNoLongerEligible(t *testing.T) {
	h := newHarness(t)
	owner, mechanic := h.user(t, "owner"), h.user(t, "mechanic")
	team := h.createTeam(t, owner, "Equipa")
	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, mechanic), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)
	transfer, err := h.svc.InitiateTransfer(h.ctx, uid(t, owner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: mechanic,
	})
	require.NoError(t, err)

	// Simulate an out-of-band demotion (the app's ChangeMemberRole would cancel
	// the transfer; this checks the AcceptTransfer guard too).
	h.pg.Exec(t, fmt.Sprintf(`UPDATE identity.membership SET role = 'guest' WHERE team_id = '%s' AND user_id = '%s'`, team.ID, mechanic))
	_, err = h.svc.AcceptTransfer(h.ctx, uid(t, mechanic), tid(t, team.ID), uuid.Must(uuid.NewV7()), transfer.ID)
	require.ErrorIs(t, err, domain.ErrTransferTarget)
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM identity.membership WHERE team_id = '%s' AND role = 'owner'`, team.ID)))
}

func TestLeavingTheRecipientCancelsTheTransfer(t *testing.T) {
	h := newHarness(t)
	owner, mechanic := h.user(t, "owner"), h.user(t, "mechanic")
	team := h.createTeam(t, owner, "Equipa")
	_, token, err := h.svc.InviteMember(h.ctx, uid(t, owner), tid(t, team.ID), app.InviteMemberInput{
		RequestID: uuid.Must(uuid.NewV7()), Role: domain.RoleMechanic,
	})
	require.NoError(t, err)
	_, err = h.svc.AcceptInvitation(h.ctx, uid(t, mechanic), uuid.Must(uuid.NewV7()), token)
	require.NoError(t, err)
	_, err = h.svc.InitiateTransfer(h.ctx, uid(t, owner), tid(t, team.ID), app.InitiateTransferInput{
		RequestID: uuid.Must(uuid.NewV7()), ToUserID: mechanic,
	})
	require.NoError(t, err)

	require.NoError(t, h.svc.LeaveTeam(h.ctx, uid(t, mechanic), tid(t, team.ID), uuid.Must(uuid.NewV7())))
	require.Equal(t, 1, h.count(t, fmt.Sprintf(`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'ownership.transfer_cancelled'`, team.ID)))
	got, err := h.svc.GetTeam(h.ctx, uid(t, owner), tid(t, team.ID))
	require.NoError(t, err)
	require.Nil(t, got.PendingTransfer)
}
