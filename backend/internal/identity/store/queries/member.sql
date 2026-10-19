-- name: CreateMembership :one
INSERT INTO identity.membership (team_id, user_id, role, invited_by)
VALUES ($1, $2, $3::identity.team_role, $4)
RETURNING team_id, user_id, role::text AS role, invited_by, joined_at, version;

-- name: GetMember :one
SELECT m.team_id, m.user_id, u.display_name, m.role::text AS role, m.joined_at, m.version, m.invited_by
FROM identity.membership m
JOIN identity.user_account u ON u.id = m.user_id AND u.deleted_at IS NULL
WHERE m.team_id = $1 AND m.user_id = $2;

-- name: ListMembers :many
SELECT m.team_id, m.user_id, u.display_name, m.role::text AS role, m.joined_at, m.version, m.invited_by
FROM identity.membership m
JOIN identity.user_account u ON u.id = m.user_id AND u.deleted_at IS NULL
WHERE m.team_id = $1
  AND (m.joined_at > $2 OR (m.joined_at = $2 AND m.user_id > $3))
ORDER BY m.joined_at, m.user_id
LIMIT $4;

-- name: UpdateMembershipRole :one
UPDATE identity.membership
SET role = $1::identity.team_role
WHERE team_id = $2 AND user_id = $3 AND version = $4
RETURNING team_id, user_id, role::text AS role, joined_at, version;

-- name: DeleteMembership :execrows
DELETE FROM identity.membership
WHERE team_id = $1 AND user_id = $2 AND version = $3;

-- name: DeleteOwnMembership :execrows
DELETE FROM identity.membership
WHERE team_id = $1 AND user_id = $2;
