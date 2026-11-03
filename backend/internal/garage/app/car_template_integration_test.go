package app_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/app"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/garage/domain"
)

var genericTemplate = uuid.MustParse("01a1990b-b4dd-777f-9c5b-e31aaf01355f")

// privateTemplate inserts a template owned by team (with its first version) straight in SQL.
func (h *harness) privateTemplate(t *testing.T, team uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	h.pg.Exec(t, fmt.Sprintf(`
BEGIN;
INSERT INTO setup.sheet_template (id, team_id, slug, make, model, scale, powertrain, name)
  SELECT '%[1]s', '%[2]s', 'private-%[3]s', make, model, scale, powertrain, 'Private'
  FROM setup.sheet_template WHERE id = '%[4]s';
INSERT INTO setup.sheet_template_version (template_id, version, definition) VALUES ('%[1]s', 1, '{}');
COMMIT;`, id, team, id.String()[24:], genericTemplate))
	return id
}

func TestCarSheetTemplateLinkRolesAndTeams(t *testing.T) {
	h := newHarness(t)
	a, b := h.user(t, "a"), h.user(t, "b")
	teamA, teamB := h.team(t, a, "owner"), h.team(t, b, "owner")
	pilot := h.user(t, "pilot")
	h.member(t, teamA, pilot, "pilot")
	private := h.privateTemplate(t, teamA)

	// The catalogue template links; a pilot cannot.
	car, err := h.svc.CreateCar(h.ctx, uid(a), tid(teamA), app.CreateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "Mugen", Model: "MBX8R", SheetTemplateID: &genericTemplate,
	})
	require.NoError(t, err)
	require.Equal(t, genericTemplate, *car.SheetTemplateID)
	upd := app.UpdateCarInput{RequestID: uuid.Must(uuid.NewV7()), CarID: car.ID, Version: car.Version,
		Update: domain.CarUpdate{SheetTemplateSet: true, SheetTemplateID: &private}}
	_, err = h.svc.UpdateCar(h.ctx, uid(pilot), tid(teamA), upd)
	require.ErrorIs(t, err, domain.ErrForbidden)

	// Own private template links.
	linked, err := h.svc.UpdateCar(h.ctx, uid(a), tid(teamA), upd)
	require.NoError(t, err)
	require.Equal(t, private, *linked.SheetTemplateID)

	// Team B cannot link team A's template, neither on create nor on update.
	_, err = h.svc.CreateCar(h.ctx, uid(b), tid(teamB), app.CreateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), Make: "X", Model: "Y", SheetTemplateID: &private,
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
	carB := h.createCar(t, b, teamB, "X", "Y")
	_, err = h.svc.UpdateCar(h.ctx, uid(b), tid(teamB), app.UpdateCarInput{
		RequestID: uuid.Must(uuid.NewV7()), CarID: carB.ID, Version: carB.Version,
		Update: domain.CarUpdate{SheetTemplateSet: true, SheetTemplateID: &private},
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
}
