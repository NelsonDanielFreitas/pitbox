-- Changes after a cursor (xid, seq), only from transactions that have already finished
-- (ADR 0025): xid below the oldest transaction still running. Everything that commits later
-- has a bigger xid, so a cursor at (xid, seq) can never skip a row.
-- name: PullChanges :many
SELECT xid::text AS xid, seq, entity, entity_id, op::text AS op
FROM sync.change_log
WHERE team_id = $1
  AND (xid, seq) > (sqlc.arg(after_xid)::text::xid8, sqlc.arg(after_seq)::bigint)
  AND xid < pg_snapshot_xmin(pg_current_snapshot())
ORDER BY xid, seq
LIMIT $2;
