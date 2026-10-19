-- name: GetIdempotencyKey :one
SELECT request_hash, response
FROM platform.idempotency_key
WHERE user_id = $1 AND request_id = $2;

-- name: InsertIdempotencyKey :one
INSERT INTO platform.idempotency_key (request_id, user_id, method, request_hash, status_code)
VALUES ($1, $2, $3, $4, 'OK')
ON CONFLICT (user_id, request_id) DO NOTHING
RETURNING request_id;

-- name: SetIdempotencyResponse :exec
UPDATE platform.idempotency_key
SET response = $3
WHERE user_id = $1 AND request_id = $2;
