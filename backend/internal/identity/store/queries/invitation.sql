-- name: CountPendingInvitations :one
SELECT count(*)
FROM identity.invitation
WHERE team_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now();

-- name: CreateInvitation :one
INSERT INTO identity.invitation (team_id, email, role, token_hash, expires_at, created_by)
VALUES ($1, NULLIF($2::citext, ''::citext), $3::identity.team_role, $4, LEAST($5::timestamptz, now() + interval '7 days'), $6)
RETURNING id, team_id, email, role::text AS role, expires_at, accepted_at, revoked_at, created_at;

-- name: GetInvitation :one
SELECT id, team_id, email, role::text AS role, expires_at, accepted_at, revoked_at, created_at
FROM identity.invitation
WHERE team_id = $1 AND id = $2;

-- name: ListInvitations :many
SELECT id, team_id, email, role::text AS role, expires_at, accepted_at, revoked_at, created_at
FROM identity.invitation
WHERE team_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now() AND id > $2
ORDER BY id
LIMIT $3;

-- name: RevokeInvitation :execrows
UPDATE identity.invitation
SET revoked_at = now()
WHERE team_id = $1 AND id = $2 AND accepted_at IS NULL AND revoked_at IS NULL;
