-- F1-06b: as chaves de idempotência expiram e nunca podem ter timestamps invertidos.
ALTER TABLE platform.idempotency_key
  ADD CONSTRAINT idempotency_expires_after_created
  CHECK (expires_at > created_at) NOT VALID;

ALTER TABLE platform.idempotency_key
  VALIDATE CONSTRAINT idempotency_expires_after_created;
