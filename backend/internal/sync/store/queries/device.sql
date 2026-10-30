-- name: InsertDeviceKey :one
INSERT INTO identity.device_key (team_id, user_id, device_name, platform, public_key)
VALUES ($1, $2, $3, sqlc.arg(platform)::identity.device_platform, $4)
RETURNING id;

-- name: GetDeviceKey :one
SELECT id, user_id, public_key, revoked_at
FROM identity.device_key
WHERE team_id = $1 AND id = $2;

-- name: CountActiveDevices :one
SELECT count(*) FROM identity.device_key
WHERE team_id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: TouchDeviceKey :exec
UPDATE identity.device_key SET last_seen_at = now()
WHERE team_id = $1 AND id = $2;

-- name: RevokeDeviceKey :one
UPDATE identity.device_key SET revoked_at = now()
WHERE team_id = $1 AND id = $2 AND revoked_at IS NULL
RETURNING id;

-- name: RevokeDevicePasses :exec
UPDATE identity.offline_pass SET revoked_at = now()
WHERE team_id = $1 AND device_key_id = $2 AND revoked_at IS NULL;

-- name: InsertOfflinePass :one
INSERT INTO identity.offline_pass (team_id, user_id, device_key_id, role, expires_at)
VALUES ($1, $2, $3, sqlc.arg(role)::identity.team_role, now() + make_interval(secs => sqlc.arg(ttl_seconds)::double precision))
RETURNING id, issued_at, expires_at;

-- name: GetOfflinePass :one
SELECT id, user_id, device_key_id, role::text AS role, issued_at, expires_at, revoked_at
FROM identity.offline_pass
WHERE team_id = $1 AND id = $2;

-- name: GetDeviceKeyByPublicKey :one
SELECT id FROM identity.device_key
WHERE team_id = $1 AND user_id = $2 AND public_key = $3 AND revoked_at IS NULL;
