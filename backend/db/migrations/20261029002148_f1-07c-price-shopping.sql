-- F1-07c: preço pago por compra, motivo de perda, "repor até", unidade fechada e lista de compras.
-- Migração expand (só acrescenta): as colunas novas são anuláveis e stock_movement continua imutável.

CREATE TYPE garage.loss_reason AS ENUM ('broken', 'worn', 'lost', 'expired');

-- stock_movement é só de acrescento: movimentos antigos ficam sem preço nem motivo.
ALTER TABLE garage.stock_movement
  ADD COLUMN unit_cost_cents integer,
  ADD COLUMN loss_reason garage.loss_reason,
  ADD CONSTRAINT stock_movement_unit_cost_ck CHECK (unit_cost_cents IS NULL OR unit_cost_cents >= 0),
  ADD CONSTRAINT stock_movement_unit_cost_reason_ck CHECK (unit_cost_cents IS NULL OR reason = 'purchase'),
  ADD CONSTRAINT stock_movement_loss_reason_ck CHECK (loss_reason IS NULL OR reason = 'lost');

ALTER TABLE garage.part
  ADD COLUMN reorder_to numeric(10,2),
  ADD CONSTRAINT part_reorder_to_ck CHECK (reorder_to IS NULL OR reorder_to >= min_stock),
  ADD CONSTRAINT part_unit_ck CHECK (unit IN ('un', 'ml', 'l', 'g')) NOT VALID;
ALTER TABLE garage.part VALIDATE CONSTRAINT part_unit_ck;

-- Só as exceções à lista calculada ficam guardadas: linhas manuais, quantidades ajustadas e
-- linhas automáticas escondidas (dismissed). Uma linha por peça.
CREATE TABLE garage.shopping_item (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  part_id             uuid        NOT NULL,
  qty                 numeric(10,2) CHECK (qty IS NULL OR qty > 0),   -- NULL = usar a sugestão
  dismissed           boolean     NOT NULL DEFAULT false,
  note                text        CHECK (note IS NULL OR char_length(note) <= 500),
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  UNIQUE (team_id, id),
  UNIQUE (team_id, part_id),
  FOREIGN KEY (team_id, part_id) REFERENCES garage.part (team_id, id) ON DELETE CASCADE
);

-- No esquema inicial estes três vêm de ciclos sobre todas as tabelas; numa migração são explícitos.
CREATE TRIGGER touch BEFORE UPDATE ON garage.shopping_item
  FOR EACH ROW EXECUTE FUNCTION app.touch();
CREATE TRIGGER change_log AFTER INSERT OR UPDATE OR DELETE ON garage.shopping_item
  FOR EACH ROW EXECUTE FUNCTION sync.log_change('id');
ALTER TABLE garage.shopping_item ENABLE ROW LEVEL SECURITY;
CREATE POLICY team_isolation ON garage.shopping_item
  USING (team_id = app.current_team_id()) WITH CHECK (team_id = app.current_team_id());
