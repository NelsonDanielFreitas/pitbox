-- F1-13: a Pull page used to apply a child before its parent when the server listed it first
-- (a setup updated after its version arrives after it), and driver feedback whose version was
-- missing at that moment was dropped while the cursor moved on. Forget the cursors so the next
-- cycle pulls everything again; every apply is idempotent (INSERT OR IGNORE, upserts), and an
-- entity with pending operations is held, not overwritten. Forward-only.
DELETE FROM app_meta WHERE key LIKE 'sync.cursor.%';
