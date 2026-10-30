-- F1-13: non-repudiation of offline operations and a gap-free Pull cursor.
--
-- device_signature: ECDSA P-256 (IEEE P1363, 64 bytes) over the canonical
-- operation. sync.operation has no writers before F1-13, so it is empty and
-- the column can be NOT NULL from the start.
ALTER TABLE sync.operation
  ADD COLUMN device_signature bytea NOT NULL,
  ADD CONSTRAINT operation_signature_len CHECK (length(device_signature) = 64),
  -- Version of the entity after this operation. Together with fields it tells a
  -- later patch which fields changed since its base_version (field-level merge).
  ADD COLUMN result_version integer;

-- Merge lookup: operations on one entity, newest version first.
-- lint:allow sync.operation has no rows before F1-13, so the plain index is instant.
CREATE INDEX operation_entity_version_idx ON sync.operation (team_id, entity, entity_id, result_version DESC)
  WHERE result_version IS NOT NULL;

-- xid: transaction that wrote the change. seq is an IDENTITY and transactions
-- commit out of order, so a client that stored cursor=N could skip a row with
-- seq<N that commits later. Pull only returns rows whose transaction is older
-- than pg_snapshot_xmin(pg_current_snapshot()), i.e. already finished.
ALTER TABLE sync.change_log
  ADD COLUMN xid xid8 NOT NULL DEFAULT pg_current_xact_id();
