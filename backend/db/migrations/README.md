# migrations

Migrações versionadas aplicadas pelo Atlas Community (`atlas migrate apply --env local`), escritas **à mão** (`make db-new name=...`): o `migrate diff` e o `migrate lint` são Pro e não se usam (ADR 0010).

- Cada alteração toca aqui **e** em `../schema/schema.sql`; `make db-test` falha se divergirem.
- Sem `BEGIN`/`COMMIT` (o Atlas envolve cada ficheiro numa transação). Para `CREATE INDEX CONCURRENTLY` é preciso a diretiva de transação do Atlas: confirmar o nome na documentação da versão fixada antes de usar.
- Nunca editar uma migração já aplicada (`atlas.sum`). Operações destrutivas só com `-- lint:allow <motivo>`.
- Papéis e extensões não vivem aqui (bootstrap em `deploy/compose/postgres/init/`).
