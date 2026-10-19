-- atlas:txmode none
-- F1-05b: lease/backoff por evento e ordem por chave (ADR 0015).
-- O índice é criado CONCURRENTLY, por isso o ficheiro não pode correr numa transação.
-- Se a criação concorrente falhar a meio, o Postgres deixa o índice INVALID e o
-- IF NOT EXISTS passa a saltá-lo na tentativa seguinte: nesse caso,
-- DROP INDEX CONCURRENTLY outbox_unpublished_key_idx e voltar a aplicar
-- (ver docs/runbooks/outbox-atraso-e-falhas.md).

ALTER TABLE platform.outbox ADD COLUMN IF NOT EXISTS locked_until timestamptz;

CREATE INDEX CONCURRENTLY IF NOT EXISTS outbox_unpublished_key_idx
  ON platform.outbox (team_id, aggregate, aggregate_id, id)
  WHERE published_at IS NULL;
