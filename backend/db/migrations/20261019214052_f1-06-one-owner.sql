-- atlas:txmode none
-- F1-06: no máximo um dono por equipa. O índice é criado CONCURRENTLY, por isso
-- este ficheiro não pode correr numa transação.
-- Se a criação falhar a meio, o Postgres deixa o índice INVALID e o IF NOT EXISTS
-- salta-o na tentativa seguinte: nesse caso, DROP INDEX CONCURRENTLY
-- membership_one_owner_uq e voltar a aplicar.

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS membership_one_owner_uq
  ON identity.membership (team_id)
  WHERE role = 'owner';
