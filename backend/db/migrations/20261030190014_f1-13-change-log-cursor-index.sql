-- atlas:txmode none

-- F1-13: the Pull pages by (xid, seq) (ADR 0025). CONCURRENTLY, so this file cannot run
-- inside a transaction. If it fails half-way Postgres leaves the index INVALID and
-- IF NOT EXISTS skips it next time: DROP INDEX CONCURRENTLY change_log_team_xid_seq_idx
-- and apply again.
CREATE INDEX CONCURRENTLY IF NOT EXISTS change_log_team_xid_seq_idx
  ON sync.change_log (team_id, xid, seq);
