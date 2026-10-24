-- name: GetIdempotencyKey :one
SELECT method, request_hash, response
FROM platform.idempotency_key
WHERE user_id = $1 AND request_id = $2 AND expires_at > now();

-- name: InsertIdempotencyKey :one
INSERT INTO platform.idempotency_key (request_id, user_id, method, request_hash, status_code)
VALUES ($1, $2, $3, $4, 'OK')
ON CONFLICT (user_id, request_id) DO UPDATE
SET method       = EXCLUDED.method,
    request_hash = EXCLUDED.request_hash,
    response     = NULL,
    status_code  = 'OK',
    created_at   = now(),
    expires_at   = now() + interval '7 days'
WHERE platform.idempotency_key.expires_at <= now()
RETURNING request_id;

-- name: SetIdempotencyResponse :exec
UPDATE platform.idempotency_key
SET response = $3
WHERE user_id = $1 AND request_id = $2;
