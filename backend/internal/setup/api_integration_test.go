package setup_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/db"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/platform/testutil"
	"github.com/NelsonDanielFreitas/pitbox/backend/internal/setup"
)

func TestCatalogueVersionAndBudget(t *testing.T) {
	pg := testutil.StartPostgres(t)
	pool, err := db.Open(context.Background(), pg.URL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ctx := context.Background()

	r := setup.NewCatalogueReader(pool)
	v1, err := r.Version(ctx)
	require.NoError(t, err)
	v1b, err := r.Version(ctx)
	require.NoError(t, err)
	require.Equal(t, v1, v1b, "the version is stable while the catalogue does not change")

	snap, err := r.Snapshot(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, snap)
	require.False(t, snap[0].Truncated)
	require.NotEmpty(t, snap[0].Versions)

	// The 2 MiB cap: a budget smaller than one definition lists the template without versions.
	small, err := setup.NewCatalogueReader(pool).WithBudget(10).Snapshot(ctx)
	require.NoError(t, err)
	require.True(t, small[0].Truncated)
	require.Empty(t, small[0].Versions)
	require.Equal(t, int(2<<20), setup.MaxCatalogueBytes)

	// A new global template changes the version.
	pg.Exec(t, `
BEGIN;
INSERT INTO setup.sheet_template (id, slug, make, model, scale, powertrain, name)
  SELECT app.uuid_v7(), 'other-model', make, model, scale, powertrain, 'Other' FROM setup.sheet_template LIMIT 1;
INSERT INTO setup.sheet_template_version (template_id, version, definition)
  SELECT id, 1, '{}' FROM setup.sheet_template WHERE slug = 'other-model';
COMMIT;`)
	v2, err := r.Version(ctx)
	require.NoError(t, err)
	require.NotEqual(t, v1, v2)
}
