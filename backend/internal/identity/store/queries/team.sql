-- name: CreateTeam :one
INSERT INTO identity.team (id, name, category, club, country_code, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, name, category, club, country_code, created_by, created_at, version;

-- name: GetTeam :one
SELECT id, name, category, club, country_code, created_by, created_at, version
FROM identity.team
WHERE id = $1 AND deleted_at IS NULL;

-- name: ListMyTeams :many
SELECT t.id, t.name, t.category, t.club, t.country_code, t.created_by, t.created_at, t.version,
       m.role::text AS role
FROM identity.membership m
JOIN identity.team t ON t.id = m.team_id AND t.deleted_at IS NULL
WHERE m.user_id = $1 AND t.id > $2
ORDER BY t.id
LIMIT $3;

-- name: UpdateTeam :one
UPDATE identity.team
SET name = $1, category = $2, club = $3, country_code = $4
WHERE id = $5 AND version = $6 AND deleted_at IS NULL
RETURNING id, name, category, club, country_code, created_by, created_at, version;

-- name: GetPendingTransfer :one
SELECT id, team_id, from_user_id, to_user_id, created_by, created_at, expires_at, confirmed_at, cancelled_at
FROM identity.ownership_transfer
WHERE team_id = $1 AND confirmed_at IS NULL AND cancelled_at IS NULL AND expires_at > now();

-- name: LockTeamForInvites :one
SELECT id FROM identity.team WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE;
