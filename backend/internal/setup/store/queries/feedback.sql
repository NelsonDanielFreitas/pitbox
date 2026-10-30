-- Driver feedback on a setup version (ADR 0026). Append only: no update, no delete.
-- The device chose the id (UUIDv7, the sync entity id); the author is always the caller.
-- name: CreateFeedbackWithID :one
INSERT INTO setup.setup_feedback (
  id, team_id, setup_id, version, author_id,
  steering_entry, steering_mid, steering_exit, traction, jump_stability, braking,
  engine_low_end, engine_top_end, track_zone, note, created_at
)
VALUES ($1, $2, $3, $4, $5,
        sqlc.narg('steering_entry')::smallint, sqlc.narg('steering_mid')::smallint,
        sqlc.narg('steering_exit')::smallint, sqlc.narg('traction')::smallint,
        sqlc.narg('jump_stability')::smallint, sqlc.narg('braking')::smallint,
        sqlc.narg('engine_low_end')::smallint, sqlc.narg('engine_top_end')::smallint,
        sqlc.narg('track_zone'), sqlc.narg('note'), $6)
RETURNING id;

-- No lock: Pull reads in a read-only transaction.
-- name: GetFeedback :one
SELECT id, setup_id, version, author_id, steering_entry, steering_mid, steering_exit, traction,
       jump_stability, braking, engine_low_end, engine_top_end, track_zone, note, created_at
FROM setup.setup_feedback
WHERE team_id = $1 AND id = $2;
