-- name: CreateShareLink :one
INSERT INTO setup.share_link (
  team_id, setup_id, pinned_version, token_hash, expires_at, created_by
)
VALUES (
  $1, $2, $3, $4,
  CASE
    WHEN sqlc.narg('ttl')::text IS NULL THEN NULL
    ELSE now() + sqlc.narg('ttl')::text::interval
  END,
  $5
)
RETURNING id, setup_id, pinned_version, expires_at, revoked_at, last_used_at,
          use_count, created_at;

-- name: ListShareLinks :many
SELECT id, setup_id, pinned_version, expires_at, revoked_at, last_used_at,
       use_count, created_at
FROM setup.share_link
WHERE team_id = $1 AND setup_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 100;

-- name: CountActiveShareLinks :one
SELECT count(*)::integer
FROM setup.share_link
WHERE team_id = $1 AND setup_id = $2
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- name: GetShareLink :one
SELECT id, setup_id, pinned_version, expires_at, revoked_at, last_used_at,
       use_count, created_at
FROM setup.share_link
WHERE team_id = $1 AND setup_id = $2 AND id = $3;

-- name: RevokeShareLink :one
UPDATE setup.share_link
SET revoked_at = now()
WHERE team_id = $1 AND setup_id = $2 AND id = $3 AND revoked_at IS NULL
RETURNING id;
