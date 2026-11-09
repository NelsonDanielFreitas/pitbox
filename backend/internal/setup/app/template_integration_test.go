package app_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup/domain"
)

// The generic 1/8 nitro template seeded by the migration (ADR 0030).
var genericTemplate = uuid.MustParse("01a1990b-b4dd-777f-9c5b-e31aaf01355f")

func TestDuplicateTemplateRolesIdempotencyAndAudit(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	pilot := h.user(t, "pilot")
	h.member(t, team, pilot, "pilot")
	reqID := uuid.Must(uuid.NewV7())

	_, _, err := h.svc.ListSheetTemplates(h.ctx, uid(pilot), tid(team), 50, "")
	require.NoError(t, err, "a pilot may read the catalogue")

	_, err = h.svc.DuplicateSheetTemplate(h.ctx, uid(pilot), tid(team), uuid.Must(uuid.NewV7()), genericTemplate)
	require.ErrorIs(t, err, domain.ErrForbidden)

	first, err := h.svc.DuplicateSheetTemplate(h.ctx, uid(owner), tid(team), reqID, genericTemplate)
	require.NoError(t, err)
	require.NotNil(t, first.BasedOnID)
	again, err := h.svc.DuplicateSheetTemplate(h.ctx, uid(owner), tid(team), reqID, genericTemplate)
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID, "same request id must not duplicate")
	require.Equal(t, "1", h.pg.Exec(t, fmt.Sprintf(
		`SELECT count(*) FROM setup.sheet_template WHERE team_id = '%s'`, team)))
	require.Equal(t, "1", h.pg.Exec(t, fmt.Sprintf(
		`SELECT count(*) FROM audit.audit_log WHERE team_id = '%s' AND action = 'setup.sheet_template.duplicate' AND actor_id = '%s' AND request_id = '%s'`,
		team, owner, reqID)))

	// A team template cannot be duplicated again (only the catalogue can).
	_, err = h.svc.DuplicateSheetTemplate(h.ctx, uid(owner), tid(team), uuid.Must(uuid.NewV7()), first.ID)
	require.ErrorIs(t, err, domain.ErrInvalid)
}

func TestTemplateIsolationBetweenTeams(t *testing.T) {
	h := newHarness(t)
	a, b := h.user(t, "a"), h.user(t, "b")
	teamA, teamB := h.team(t, a, "owner"), h.team(t, b, "owner")
	carB := h.car(t, teamB)
	private, err := h.svc.DuplicateSheetTemplate(h.ctx, uid(a), tid(teamA), uuid.Must(uuid.NewV7()), genericTemplate)
	require.NoError(t, err)

	// B asks for A's template through B's own team: not found, not forbidden.
	_, err = h.svc.GetSheetTemplateVersion(h.ctx, uid(b), tid(teamB), private.ID, 1)
	require.ErrorIs(t, err, domain.ErrNotFound)
	// B saves a setup using A's template.
	_, err = h.svc.CreateSetup(h.ctx, uid(b), tid(teamB), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: carB, Name: "x",
		Template: &app.TemplateRef{ID: private.ID},
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
	// B asks for A's team id: not a member.
	_, err = h.svc.GetSheetTemplateVersion(h.ctx, uid(b), tid(teamA), private.ID, 1)
	require.ErrorIs(t, err, domain.ErrNotFound)
	// B's listing never contains A's template.
	list, _, err := h.svc.ListSheetTemplates(h.ctx, uid(b), tid(teamB), 200, "")
	require.NoError(t, err)
	for _, it := range list {
		require.NotEqual(t, private.ID, it.ID)
	}
}

func TestSetupSchemaV1AndV2(t *testing.T) {
	h := newHarness(t)
	owner := h.user(t, "owner")
	team := h.team(t, owner, "owner")
	car := h.car(t, team)

	// An old queue: a v1 setup and a v1 version still apply after the upgrade.
	old, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "v1"})
	require.NoError(t, err)
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: uuid.MustParse(old.ID), BaseVersion: 1,
		Data: map[string]any{"diffs": map[string]any{"front_cst": float64(500)}},
	})
	require.NoError(t, err)

	// A v2 setup on the generic template.
	v2, err := h.svc.CreateSetup(h.ctx, uid(owner), tid(team), app.CreateSetupInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: car, Name: "v2",
		Template: &app.TemplateRef{ID: genericTemplate},
	})
	require.NoError(t, err)
	id := uuid.MustParse(v2.ID)
	tpl := map[string]any{"id": genericTemplate.String(), "version": float64(1)}

	// The client may not send canonical: the server computes it.
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
		RequestID: uuid.Must(uuid.NewV7()), SetupID: id, BaseVersion: 1, SchemaVersion: 2,
		Data: map[string]any{"template": tpl, "values": map[string]any{"diffs_front_cst": float64(500)},
			"canonical": map[string]any{"diffs": map[string]any{"front_cst": float64(1)}}},
	})
	require.ErrorIs(t, err, domain.ErrInvalid)

	// An unknown key and an out-of-range value are refused too.
	for _, vals := range []map[string]any{{"nope": float64(1)}, {"diffs_front_cst": float64(1)}} {
		_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), app.CreateVersionInput{
			RequestID: uuid.Must(uuid.NewV7()), SetupID: id, BaseVersion: 1, SchemaVersion: 2,
			Data: map[string]any{"template": tpl, "values": vals},
		})
		require.ErrorIs(t, err, domain.ErrInvalid)
	}

	req := uuid.Must(uuid.NewV7())
	in := app.CreateVersionInput{
		RequestID: req, SetupID: id, BaseVersion: 1, SchemaVersion: 2,
		Data: map[string]any{"template": tpl, "values": map[string]any{"diffs_front_cst": float64(500)}},
	}
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), in)
	require.NoError(t, err)
	_, err = h.svc.CreateVersion(h.ctx, uid(owner), tid(team), in)
	require.NoError(t, err, "resending the same request is a replay")
	require.Equal(t, "2", h.pg.Exec(t, fmt.Sprintf(
		`SELECT count(*) FROM setup.setup_version WHERE setup_id = '%s'`, id)))
	// The server filled the canonical value and the generated column.
	require.Equal(t, "500", h.pg.Exec(t, fmt.Sprintf(
		`SELECT data->'canonical'->>'diffs.front_cst' FROM setup.setup_version WHERE setup_id = '%s' AND version = 2`, id)))
}
