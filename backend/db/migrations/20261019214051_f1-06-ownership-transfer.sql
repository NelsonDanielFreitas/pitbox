-- F1-06: transferência de propriedade em dois passos (24 h, confirmação do destinatário).
CREATE TABLE identity.ownership_transfer (
  id              uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id         uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  from_user_id    uuid        NOT NULL,
  to_user_id      uuid        NOT NULL,
  created_by      uuid        NOT NULL REFERENCES identity.user_account (id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  confirmed_at    timestamptz,
  cancelled_at    timestamptz,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, from_user_id) REFERENCES identity.membership (team_id, user_id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, to_user_id)   REFERENCES identity.membership (team_id, user_id) ON DELETE CASCADE,
  CHECK (from_user_id <> to_user_id),
  CHECK (expires_at > created_at AND expires_at <= created_at + interval '24 hours'),
  CHECK (confirmed_at IS NULL OR cancelled_at IS NULL)
);

CREATE UNIQUE INDEX ownership_transfer_pending_uq
  ON identity.ownership_transfer (team_id)
  WHERE confirmed_at IS NULL AND cancelled_at IS NULL;

ALTER TABLE identity.ownership_transfer ENABLE ROW LEVEL SECURITY;
CREATE POLICY team_isolation ON identity.ownership_transfer
  USING (team_id = app.current_team_id())
  WITH CHECK (team_id = app.current_team_id());
