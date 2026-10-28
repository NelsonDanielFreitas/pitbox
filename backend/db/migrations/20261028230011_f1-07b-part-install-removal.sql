-- F1-07b: destino e motivo de remoção das montagens de peças.
-- Migração aditiva: garage.part_install existe desde o esquema inicial e ainda não tem escritas.

-- lint:allow coluna nula sem default numa tabela vazia e ainda sem produção
ALTER TABLE garage.part_install
  ADD COLUMN removal_destination text;

ALTER TABLE garage.part_install
  ADD CONSTRAINT part_install_removal_ck CHECK (
    CASE
      WHEN removed_at IS NULL THEN removal_destination IS NULL AND removal_reason IS NULL
      WHEN removal_destination = 'scrap' THEN COALESCE(removal_reason IN ('broken', 'worn', 'lost'), false)
      WHEN removal_destination IN ('stock', 'spare') THEN removal_reason IS NULL
      ELSE false
    END
  );
