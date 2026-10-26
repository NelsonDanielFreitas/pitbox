-- The stock level aggregate and the part cascade both read stock_movement by part.
CREATE INDEX IF NOT EXISTS stock_movement_part_idx
  ON stock_movement (team_id, part_id);
