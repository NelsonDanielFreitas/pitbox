-- F1-07d: um conjunto de pneus colado a partir do stock guarda as peças de onde veio.
-- Migração expand (só acrescenta): as colunas são anuláveis, porque um conjunto comprado já
-- colado continua a poder ser criado sem passar pelo stock.

ALTER TABLE garage.tyre_set
  ADD COLUMN tyre_part_id uuid,
  ADD COLUMN insert_part_id uuid,
  ADD CONSTRAINT tyre_set_tyre_part_fk FOREIGN KEY (team_id, tyre_part_id)
    REFERENCES garage.part (team_id, id) ON DELETE SET NULL (tyre_part_id),
  ADD CONSTRAINT tyre_set_insert_part_fk FOREIGN KEY (team_id, insert_part_id)
    REFERENCES garage.part (team_id, id) ON DELETE SET NULL (insert_part_id),
  ADD CONSTRAINT tyre_set_parts_differ_ck CHECK (
    tyre_part_id IS NULL OR insert_part_id IS NULL OR tyre_part_id <> insert_part_id
  );
