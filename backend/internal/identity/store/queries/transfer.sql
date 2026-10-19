-- name: CancelPendingTransfers :execrows
UPDATE identity.ownership_transfer
SET cancelled_at = now()
WHERE team_id = $1 AND confirmed_at IS NULL AND cancelled_at IS NULL;

-- name: CreateTransfer :one
INSERT INTO identity.ownership_transfer (team_id, from_user_id, to_user_id, created_by, expires_at)
VALUES ($1, $2, $3, $4, LEAST($5::timestamptz, now() + interval '24 hours'))
RETURNING id, team_id, from_user_id, to_user_id, created_by, created_at, expires_at,
          confirmed_at, cancelled_at;

-- name: GetTransferForUpdate :one
SELECT id, team_id, from_user_id, to_user_id, created_by, created_at, expires_at,
       confirmed_at, cancelled_at
FROM identity.ownership_transfer
WHERE team_id = $1 AND id = $2
  AND confirmed_at IS NULL AND cancelled_at IS NULL
FOR UPDATE;

-- name: ConfirmTransfer :execrows
UPDATE identity.ownership_transfer
SET confirmed_at = now()
WHERE team_id = $1 AND id = $2
  AND confirmed_at IS NULL AND cancelled_at IS NULL AND expires_at > now();

-- name: CancelTransfer :execrows
UPDATE identity.ownership_transfer
SET cancelled_at = now()
WHERE team_id = $1 AND id = $2 AND confirmed_at IS NULL AND cancelled_at IS NULL;
