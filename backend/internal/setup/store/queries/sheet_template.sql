-- Sheet templates (F2-00, ADR 0030). A row with team_id NULL is the global catalogue; the RLS
-- already limits reads to the global rows plus the caller's team, and every query repeats the
-- team filter (defence in depth).

-- name: ListSheetTemplates :many
SELECT id, team_id, slug, make, model, scale, powertrain, name, source_url, current_version, based_on_id,
       created_at, updated_at, version
FROM setup.sheet_template
WHERE (team_id IS NULL OR team_id = $1) AND deleted_at IS NULL AND id > $2
ORDER BY id
LIMIT $3;

-- name: GetSheetTemplate :one
SELECT id, team_id, slug, make, model, scale, powertrain, name, source_url, current_version, based_on_id,
       created_at, updated_at, version
FROM setup.sheet_template
WHERE (team_id IS NULL OR team_id = $1) AND id = $2 AND deleted_at IS NULL;

-- name: GetSheetTemplateVersion :one
SELECT template_id, version, definition, created_at, (team_id IS NULL)::boolean AS is_global
FROM setup.sheet_template_version
WHERE (team_id IS NULL OR team_id = $1) AND template_id = $2 AND version = $3;

-- A team copy of a global template. The device chose the id (sync entity id); the team is the caller's.
-- name: CreateTeamSheetTemplate :one
INSERT INTO setup.sheet_template (id, team_id, slug, make, model, scale, powertrain, name, source_url,
                                  current_version, based_on_id, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING id, team_id, slug, make, model, scale, powertrain, name, source_url, current_version, based_on_id,
          created_at, updated_at, version;

-- team_id is set by the database from the template; it is not a parameter.
-- name: CreateSheetTemplateVersion :exec
INSERT INTO setup.sheet_template_version (template_id, version, definition)
VALUES ($1, $2, $3);

-- Optimistic concurrency, as the other mutable entities.
-- name: UpdateTeamSheetTemplate :one
UPDATE setup.sheet_template
SET name = $4, deleted_at = sqlc.narg('deleted_at')
WHERE team_id = $1 AND id = $2 AND version = $3
RETURNING id, team_id, slug, make, model, scale, powertrain, name, source_url, current_version, based_on_id,
          created_at, updated_at, version;

-- The global catalogue for the sync snapshot. No team context: it is reference data
-- (the RLS shows team_id NULL rows to everybody).
-- name: ListGlobalSheetTemplates :many
SELECT id, slug, make, model, scale, powertrain, name, source_url, current_version
FROM setup.sheet_template
WHERE team_id IS NULL AND deleted_at IS NULL
ORDER BY id;

-- The digest lets the catalogue version be computed without reading every definition.
-- name: ListGlobalSheetTemplateDigests :many
SELECT v.template_id, v.version, sha256(convert_to(v.definition::text, 'UTF8')) AS digest,
       octet_length(v.definition::text) AS size
FROM setup.sheet_template_version v
JOIN setup.sheet_template t ON t.id = v.template_id AND t.team_id IS NULL AND t.deleted_at IS NULL
WHERE v.team_id IS NULL
ORDER BY v.template_id, v.version;

-- name: ListGlobalSheetTemplateDefinitions :many
SELECT v.template_id, v.version, v.definition
FROM setup.sheet_template_version v
JOIN setup.sheet_template t ON t.id = v.template_id AND t.team_id IS NULL AND t.deleted_at IS NULL
WHERE v.team_id IS NULL
ORDER BY v.template_id, v.version;
