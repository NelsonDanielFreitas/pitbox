-- =============================================================================
-- PitBox — Esquema da base de dados
-- PostgreSQL 16 + TimescaleDB (telemetria)
--
-- Organização: um esquema por módulo de domínio (ver Arquitetura Técnica §4 e §7)
--   app          funções e tipos partilhados
--   identity     utilizadores, equipas, membros, convites, dispositivos
--   garage       carros, motores, pneus, peças e stock
--   setup        fichas de setup versionadas e partilha
--   maintenance  intervenções, regras de manutenção, checklists
--   events       pistas, campeonatos, provas, sessões, voltas, finais
--   timing       live timing e resultados externos
--   telemetry    módulos, firmware, sessões e amostras de telemetria
--   sync         sincronização offline (operações, conflitos, change log)
--   platform     idempotência, outbox, preferências de notificação
--   audit        registo de auditoria (só acrescento)
--
-- Convenções
--   * Chaves primárias UUIDv7 (geradas no cliente para permitir criação offline;
--     app.uuid_v7() como valor por omissão no servidor).
--   * Todas as tabelas de equipa têm team_id NOT NULL, RLS ativo e UNIQUE (team_id, id);
--     as FKs entre tabelas de equipa são compostas (team_id, x_id), o que torna
--     impossível ligar um registo a um recurso de outra equipa.
--   * Colunas comuns: created_at, updated_at, version (concorrência otimista),
--     deleted_at (apagamento lógico; purga ao fim de 30 dias), created_by.
--   * Unidades no nome da coluna: _ms, _s, _c, _mm, _deg, _pct, _v, _ml, _cents.
--   * Tempos sempre em timestamptz (UTC).
--
-- Papéis
--   pitbox_owner     dono dos objetos e das políticas do TimescaleDB; LOGIN sem palavra-passe (ADR 0011);
--                    as migrações correm ligadas como pitbox_migrator, que o assume (não usado pela aplicação)
--   pitbox_app       usado pela API/worker/ingest; sem BYPASSRLS
--   pitbox_readonly  análise e suporte de leitura; sujeito a RLS
-- =============================================================================

-- Pré-requisitos (bootstrap, fora das migrações): papéis pitbox_owner/pitbox_app/pitbox_readonly,
-- extensões citext, btree_gist e timescaledb, e a ligação como pitbox_migrator (que assume
-- pitbox_owner em cada sessão). Ver deploy/compose/postgres/init/ e ADR 0010.
-- Sem BEGIN/COMMIT: o Atlas já corre cada migração numa transação.

-- -----------------------------------------------------------------------------
-- Esquemas
-- -----------------------------------------------------------------------------
CREATE SCHEMA IF NOT EXISTS app;
CREATE SCHEMA IF NOT EXISTS identity;
CREATE SCHEMA IF NOT EXISTS garage;
CREATE SCHEMA IF NOT EXISTS setup;
CREATE SCHEMA IF NOT EXISTS maintenance;
CREATE SCHEMA IF NOT EXISTS events;
CREATE SCHEMA IF NOT EXISTS timing;
CREATE SCHEMA IF NOT EXISTS telemetry;
CREATE SCHEMA IF NOT EXISTS sync;
CREATE SCHEMA IF NOT EXISTS platform;
CREATE SCHEMA IF NOT EXISTS audit;

-- =============================================================================
-- app — funções partilhadas
-- =============================================================================

-- UUIDv7: 48 bits de timestamp em ms + aleatório; ordenável no tempo.
CREATE OR REPLACE FUNCTION app.uuid_v7() RETURNS uuid
LANGUAGE sql VOLATILE AS $$
  SELECT encode(
    set_bit(set_bit(
      overlay(uuid_send(gen_random_uuid())
              PLACING substring(int8send((extract(epoch FROM clock_timestamp()) * 1000)::bigint) FROM 3)
              FROM 1 FOR 6),
      52, 1), 53, 1),
    'hex')::uuid
$$;

-- Contexto do pedido, definido pela aplicação no início de cada transação:
--   SET LOCAL app.team_id = '<uuid>';  SET LOCAL app.user_id = '<uuid>';
CREATE OR REPLACE FUNCTION app.current_team_id() RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.team_id', true), '')::uuid
$$;

CREATE OR REPLACE FUNCTION app.current_user_id() RETURNS uuid
LANGUAGE sql STABLE AS $$
  SELECT NULLIF(current_setting('app.user_id', true), '')::uuid
$$;

-- updated_at + version em cada UPDATE.
CREATE OR REPLACE FUNCTION app.touch() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  NEW.version    := OLD.version + 1;
  RETURN NEW;
END
$$;

-- Bloqueia UPDATE/DELETE em tabelas imutáveis (versões de setup, auditoria, eventos de corrida).
CREATE OR REPLACE FUNCTION app.forbid_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'tabela % é só de acrescento: % não é permitido', TG_TABLE_NAME, TG_OP
    USING ERRCODE = 'insufficient_privilege';
END
$$;

-- =============================================================================
-- Tipos enumerados
-- =============================================================================
CREATE TYPE identity.team_role        AS ENUM ('owner', 'mechanic', 'pilot', 'guest');
CREATE TYPE identity.device_platform  AS ENUM ('web', 'android', 'ios');

CREATE TYPE garage.asset_status       AS ENUM ('active', 'spare', 'retired');
CREATE TYPE garage.tyre_status        AS ENUM ('new', 'used', 'worn', 'retired');
CREATE TYPE garage.part_kind          AS ENUM (
  'glow_plug', 'clutch_shoe', 'clutch_spring', 'clutch_bell', 'bearing', 'conrod',
  'piston_sleeve', 'air_filter', 'shock_oil', 'diff_oil', 'fuel', 'spring', 'tyre',
  'insert', 'body', 'wing', 'other');
CREATE TYPE garage.stock_reason       AS ENUM ('purchase', 'consumed', 'installed', 'adjustment', 'lost', 'returned');
CREATE TYPE garage.loss_reason        AS ENUM ('broken', 'worn', 'lost', 'expired');
CREATE TYPE garage.powertrain_family  AS ENUM ('combustion', 'electric');

CREATE TYPE setup.field_type          AS ENUM ('number', 'integer', 'text', 'enum', 'boolean');
CREATE TYPE setup.surface             AS ENUM ('dry', 'damp', 'wet', 'dusty', 'watered', 'rutted', 'mixed');

CREATE TYPE maintenance.target_kind   AS ENUM ('car', 'engine', 'tyre_set');
CREATE TYPE maintenance.metric        AS ENUM ('tanks', 'run_seconds', 'sessions', 'days');
CREATE TYPE maintenance.entry_kind    AS ENUM ('service', 'replacement', 'cleaning', 'tuning', 'inspection', 'break_in');
CREATE TYPE maintenance.checklist_scope AS ENUM ('pre_event', 'pre_session');

CREATE TYPE events.event_status       AS ENUM ('planned', 'registered', 'in_progress', 'completed', 'cancelled');
CREATE TYPE events.session_kind       AS ENUM ('practice', 'timed_practice', 'qualifier', 'final', 'test');
CREATE TYPE events.session_status     AS ENUM ('scheduled', 'ready', 'running', 'finished', 'cancelled');
CREATE TYPE events.data_source        AS ENUM ('import', 'manual', 'telemetry', 'live');
CREATE TYPE events.incident_kind      AS ENUM ('rollover', 'contact', 'flameout', 'off_track', 'failure', 'marshal');
CREATE TYPE events.pit_action         AS ENUM ('refuel', 'tyres', 'glow_plug', 'restart', 'repair', 'air_filter');

CREATE TYPE timing.provider           AS ENUM ('csv_upload', 'liverc', 'myrcm', 'rcm', 'other');
CREATE TYPE timing.import_status      AS ENUM ('pending', 'parsed', 'failed');

CREATE TYPE telemetry.device_status   AS ENUM ('pairing', 'active', 'revoked');
CREATE TYPE telemetry.upload_status   AS ENUM ('uploaded', 'processing', 'ready', 'failed');
CREATE TYPE telemetry.metric          AS ENUM ('head_temp_c', 'rpm', 'rx_voltage_v', 'impact_g');
CREATE TYPE telemetry.release_channel AS ENUM ('stable', 'beta');

CREATE TYPE sync.op_kind              AS ENUM ('create', 'patch', 'append', 'delete');
CREATE TYPE sync.op_status            AS ENUM ('applied', 'merged', 'conflict', 'rejected');

CREATE TYPE platform.notify_channel   AS ENUM ('push', 'email', 'local');

-- =============================================================================
-- identity
-- =============================================================================

-- Espelho mínimo do utilizador do IdP (Zitadel). Credenciais nunca estão aqui.
CREATE TABLE identity.user_account (
  id              uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  idp_subject     text        NOT NULL UNIQUE,                -- claim "sub" do IdP
  email           citext      NOT NULL,
  display_name    text        NOT NULL CHECK (length(display_name) BETWEEN 1 AND 80),
  locale          text        NOT NULL DEFAULT 'pt-PT',
  avatar_object_key text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         integer     NOT NULL DEFAULT 1,
  deleted_at      timestamptz
);
CREATE UNIQUE INDEX user_account_email_uq ON identity.user_account (email) WHERE deleted_at IS NULL;

CREATE TABLE identity.team (
  id              uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  name            text        NOT NULL CHECK (length(name) BETWEEN 1 AND 80),
  category        text        NOT NULL DEFAULT '1/8 TT nitro' CHECK (length(category) BETWEEN 1 AND 40),
  club            text CHECK (club IS NULL OR length(club) BETWEEN 1 AND 80),
  country_code    char(2)     NOT NULL DEFAULT 'PT',
  created_by      uuid        NOT NULL REFERENCES identity.user_account (id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         integer     NOT NULL DEFAULT 1,
  deleted_at      timestamptz
);

CREATE TABLE identity.membership (
  team_id         uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  user_id         uuid        NOT NULL REFERENCES identity.user_account (id) ON DELETE CASCADE,
  role            identity.team_role NOT NULL,
  invited_by      uuid        REFERENCES identity.user_account (id),
  joined_at       timestamptz NOT NULL DEFAULT now(),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         integer     NOT NULL DEFAULT 1,
  PRIMARY KEY (team_id, user_id)
);
CREATE INDEX membership_user_idx ON identity.membership (user_id);
CREATE UNIQUE INDEX membership_one_owner_uq ON identity.membership (team_id) WHERE role = 'owner';
COMMENT ON TABLE identity.membership IS
  'Fonte de verdade dos papéis; espelhada nas relações do OpenFGA pelo worker (outbox).';

CREATE TABLE identity.invitation (
  id              uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id         uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  email           citext,                                     -- NULL = convite por link
  role            identity.team_role NOT NULL CHECK (role <> 'owner'),
  token_hash      bytea       NOT NULL UNIQUE,                -- SHA-256 do token; o token nunca é guardado
  expires_at      timestamptz NOT NULL,
  accepted_at     timestamptz,
  accepted_by     uuid        REFERENCES identity.user_account (id),
  revoked_at      timestamptz,
  created_by      uuid        NOT NULL REFERENCES identity.user_account (id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT invitation_token_hash_len CHECK (length(token_hash) = 32),
  CONSTRAINT invitation_expires_after_created CHECK (expires_at > created_at),
  CHECK (expires_at <= created_at + interval '7 days'),
  CONSTRAINT invitation_team_id_uq UNIQUE (team_id, id)
);
CREATE INDEX invitation_team_idx ON identity.invitation (team_id) WHERE accepted_at IS NULL AND revoked_at IS NULL;

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

-- Dispositivos da app (telemóvel/tablet) com chave pública para assinar operações offline.
CREATE TABLE identity.device_key (
  id              uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id         uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  user_id         uuid        NOT NULL REFERENCES identity.user_account (id) ON DELETE CASCADE,
  device_name     text        NOT NULL,
  platform        identity.device_platform NOT NULL,
  public_key      bytea       NOT NULL,                       -- ECDSA P-256, formato SPKI DER
  key_algorithm   text        NOT NULL DEFAULT 'ES256',
  last_seen_at    timestamptz,
  revoked_at      timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (team_id, id)
);
CREATE INDEX device_key_user_idx ON identity.device_key (user_id) WHERE revoked_at IS NULL;

CREATE TABLE identity.offline_pass (
  id              uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id         uuid        NOT NULL,
  user_id         uuid        NOT NULL REFERENCES identity.user_account (id) ON DELETE CASCADE,
  device_key_id   uuid        NOT NULL,
  role            identity.team_role NOT NULL,
  issued_at       timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  revoked_at      timestamptz,
  FOREIGN KEY (team_id, device_key_id) REFERENCES identity.device_key (team_id, id) ON DELETE CASCADE,
  CHECK (expires_at > issued_at AND expires_at <= issued_at + interval '72 hours')
);
CREATE INDEX offline_pass_device_idx ON identity.offline_pass (device_key_id, expires_at);

-- Códigos de recuperação de MFA: mostrados uma vez ao utilizador, guardados só como HMAC.
-- key_id identifica a chave do anel que produziu o HMAC; permite retirar chaves antigas.
CREATE TABLE identity.recovery_code (
  id          uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  user_id     uuid        NOT NULL REFERENCES identity.user_account (id) ON DELETE CASCADE,
  code_hash   bytea       NOT NULL CHECK (length(code_hash) = 32),
  used_at     timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  key_id      text        NOT NULL,
  UNIQUE (user_id, code_hash),
  CONSTRAINT recovery_code_key_id_valid CHECK (key_id ~ '^[a-z0-9][a-z0-9-]{0,15}$')
);
CREATE INDEX recovery_code_user_idx ON identity.recovery_code (user_id) WHERE used_at IS NULL;

-- Acesso temporário de suporte a dados de uma equipa (visível ao dono, auditado).
CREATE TABLE identity.support_grant (
  id              uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id         uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  admin_subject   text        NOT NULL,                       -- "sub" do administrador no IdP
  ticket_ref      text        NOT NULL,
  reason          text        NOT NULL,
  starts_at       timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  revoked_at      timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CHECK (expires_at > starts_at AND expires_at <= starts_at + interval '24 hours')
);

-- =============================================================================
-- events (pistas primeiro: são referenciadas por setup e sessões)
-- =============================================================================

-- team_id NULL = pista do catálogo global (gerida pela plataforma).
CREATE TABLE events.track (
  id              uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id         uuid        REFERENCES identity.team (id) ON DELETE CASCADE,
  name            text        NOT NULL,
  city            text,
  country_code    char(2)     NOT NULL DEFAULT 'PT',
  latitude        numeric(9,6) CHECK (latitude  BETWEEN -90  AND 90),
  longitude       numeric(9,6) CHECK (longitude BETWEEN -180 AND 180),
  surface_type    text,                                       -- terra, argila, relva sintética...
  length_m        integer     CHECK (length_m > 0),
  notes           text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         integer     NOT NULL DEFAULT 1,
  deleted_at      timestamptz
);
CREATE INDEX track_team_idx ON events.track (team_id);

-- =============================================================================
-- garage
-- =============================================================================

-- Catálogos globais de referência (ADR 0016): sem team_id e sem RLS, só de leitura
-- para a aplicação; as migrações revistas escrevem. A versão que a app consome é um
-- hash do conteúdo calculado na leitura por garage/api.go.
CREATE TABLE garage.scale (
  code        text        PRIMARY KEY CHECK (code ~ '^1/[1-9][0-9]?$'),
  name        text        NOT NULL CHECK (length(name) BETWEEN 1 AND 40),
  active      boolean     NOT NULL DEFAULT true,
  sort_order  smallint    NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE garage.powertrain (
  code        text        PRIMARY KEY CHECK (code ~ '^[a-z][a-z0-9_]{0,31}$'),
  family      garage.powertrain_family NOT NULL,
  name        text        NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
  active      boolean     NOT NULL DEFAULT true,
  sort_order  smallint    NOT NULL DEFAULT 0,
  created_at  timestamptz NOT NULL DEFAULT now()
);

-- Na v0.1 só existe 1/8 nitro; outras escalas/motorizações entram na Fase 6.
INSERT INTO garage.scale (code, name, active, sort_order) VALUES ('1/8', '1/8', true, 10);
INSERT INTO garage.powertrain (code, family, name, active, sort_order)
VALUES ('nitro', 'combustion', 'Nitro', true, 10);

CREATE TABLE garage.car (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  make                text        NOT NULL,
  model               text        NOT NULL,
  model_year          smallint    CHECK (model_year BETWEEN 1990 AND 2100),
  serial_number       text,
  nickname            text,
  transponder_number  text,                                   -- MyLaps; usado para casar resultados
  status              garage.asset_status NOT NULL DEFAULT 'active',
  base_setup_id       uuid,                                   -- FK adicionada depois de setup.setup
  photo_object_key    text,
  notes               text,
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  scale               text        NOT NULL DEFAULT '1/8' CONSTRAINT car_scale_fk REFERENCES garage.scale (code),
  powertrain          text        NOT NULL DEFAULT 'nitro' CONSTRAINT car_powertrain_fk REFERENCES garage.powertrain (code),
  -- Ficha do fabricante (F2-00): sem FK porque uma FK ignora a RLS; ver o trigger sheet_template_check.
  sheet_template_id   uuid,
  UNIQUE (team_id, id)
);
CREATE UNIQUE INDEX car_transponder_uq ON garage.car (team_id, transponder_number)
  WHERE transponder_number IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE garage.engine (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  make                text        NOT NULL,
  model               text        NOT NULL,
  serial_number       text,
  purchased_on        date,
  status              garage.asset_status NOT NULL DEFAULT 'active',
  -- Contadores mantidos pela aplicação a partir de sessões e rodagens (eventos de domínio).
  tanks_total         numeric(8,2) NOT NULL DEFAULT 0 CHECK (tanks_total >= 0),
  run_seconds_total   integer     NOT NULL DEFAULT 0 CHECK (run_seconds_total >= 0),
  sessions_total      integer     NOT NULL DEFAULT 0 CHECK (sessions_total >= 0),
  notes               text,
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, id)
);

-- Histórico motor ↔ carro. Um motor só pode estar num carro de cada vez, e vice-versa.
CREATE TABLE garage.engine_mount (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  engine_id           uuid        NOT NULL,
  car_id              uuid        NOT NULL,
  mounted_at          timestamptz NOT NULL DEFAULT now(),
  unmounted_at        timestamptz,
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (team_id, engine_id) REFERENCES garage.engine (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, car_id)    REFERENCES garage.car (team_id, id)    ON DELETE CASCADE,
  CHECK (unmounted_at IS NULL OR unmounted_at > mounted_at),
  EXCLUDE USING gist (engine_id WITH =, tstzrange(mounted_at, unmounted_at) WITH &&),
  EXCLUDE USING gist (car_id    WITH =, tstzrange(mounted_at, unmounted_at) WITH &&)
);

CREATE TABLE garage.tyre_set (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  label               text        NOT NULL,                   -- marca escrita nas jantes, ex.: "J2"
  make                text        NOT NULL,
  model               text        NOT NULL,
  compound            text,
  insert_type         text,
  glued_on            date,
  status              garage.tyre_status NOT NULL DEFAULT 'new',
  sessions_total      integer     NOT NULL DEFAULT 0 CHECK (sessions_total >= 0),
  run_seconds_total   integer     NOT NULL DEFAULT 0 CHECK (run_seconds_total >= 0),
  wear_notes          text,
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  tyre_part_id        uuid,                                   -- peças de onde o conjunto foi colado
  insert_part_id      uuid,
  UNIQUE (team_id, id),
  CONSTRAINT tyre_set_parts_differ_ck CHECK (
    tyre_part_id IS NULL OR insert_part_id IS NULL OR tyre_part_id <> insert_part_id
  )
);
CREATE UNIQUE INDEX tyre_set_label_uq ON garage.tyre_set (team_id, label)
  WHERE deleted_at IS NULL AND status <> 'retired';

-- Catálogo de peças e consumíveis da equipa.
CREATE TABLE garage.part (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  kind                garage.part_kind NOT NULL,
  brand               text,
  reference           text,                                   -- referência do fabricante
  description         text        NOT NULL,
  unit                text        NOT NULL DEFAULT 'un',      -- un, ml, l, g
  specs               jsonb       NOT NULL DEFAULT '{}'::jsonb, -- ex.: {"viscosity_cst": 7000}
  min_stock           numeric(10,2) NOT NULL DEFAULT 0 CHECK (min_stock >= 0),
  unit_cost_cents     integer     CHECK (unit_cost_cents >= 0),
  currency            char(3)     NOT NULL DEFAULT 'EUR',
  has_service_life    boolean     NOT NULL DEFAULT false,     -- vela, sapatas: contam uso quando montadas
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  reorder_to          numeric(10,2),                          -- "repor até"; NULL = repor até min_stock
  UNIQUE (team_id, id),
  CHECK (jsonb_typeof(specs) = 'object'),
  CONSTRAINT part_reorder_to_ck CHECK (reorder_to IS NULL OR reorder_to >= min_stock),
  CONSTRAINT part_unit_ck CHECK (unit IN ('un', 'ml', 'l', 'g'))
);
CREATE INDEX part_team_kind_idx ON garage.part (team_id, kind) WHERE deleted_at IS NULL;

-- tyre_set é criada antes de part: as ligações ao stock de onde o conjunto foi colado ficam aqui.
ALTER TABLE garage.tyre_set
  ADD CONSTRAINT tyre_set_tyre_part_fk FOREIGN KEY (team_id, tyre_part_id)
    REFERENCES garage.part (team_id, id) ON DELETE SET NULL (tyre_part_id),
  ADD CONSTRAINT tyre_set_insert_part_fk FOREIGN KEY (team_id, insert_part_id)
    REFERENCES garage.part (team_id, id) ON DELETE SET NULL (insert_part_id);

-- Movimentos de stock (só acrescento). O stock atual é a soma — ver garage.stock_level.
CREATE TABLE garage.stock_movement (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  part_id             uuid        NOT NULL,
  qty_delta           numeric(10,2) NOT NULL CHECK (qty_delta <> 0),
  reason              garage.stock_reason NOT NULL,
  maintenance_entry_id uuid,                                  -- FK adicionada depois de maintenance
  note                text,
  occurred_at         timestamptz NOT NULL DEFAULT now(),
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  unit_cost_cents     integer,                                -- preço pago, só em compras
  loss_reason         garage.loss_reason,                     -- só em perdas
  FOREIGN KEY (team_id, part_id) REFERENCES garage.part (team_id, id) ON DELETE CASCADE,
  CONSTRAINT stock_movement_unit_cost_ck CHECK (unit_cost_cents IS NULL OR unit_cost_cents >= 0),
  CONSTRAINT stock_movement_unit_cost_reason_ck CHECK (unit_cost_cents IS NULL OR reason = 'purchase'),
  CONSTRAINT stock_movement_loss_reason_ck CHECK (loss_reason IS NULL OR reason = 'lost')
);
CREATE INDEX stock_movement_part_idx ON garage.stock_movement (team_id, part_id, occurred_at);

-- Peças com vida útil montadas num carro ou num motor.
CREATE TABLE garage.part_install (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  part_id             uuid        NOT NULL,
  car_id              uuid,
  engine_id           uuid,
  installed_at        timestamptz NOT NULL DEFAULT now(),
  removed_at          timestamptz,
  removal_reason      text,
  run_seconds         integer     NOT NULL DEFAULT 0 CHECK (run_seconds >= 0),
  sessions            integer     NOT NULL DEFAULT 0 CHECK (sessions >= 0),
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  removal_destination text,
  FOREIGN KEY (team_id, part_id)   REFERENCES garage.part (team_id, id)   ON DELETE CASCADE,
  FOREIGN KEY (team_id, car_id)    REFERENCES garage.car (team_id, id)    ON DELETE CASCADE,
  FOREIGN KEY (team_id, engine_id) REFERENCES garage.engine (team_id, id) ON DELETE CASCADE,
  CHECK (num_nonnulls(car_id, engine_id) = 1),
  CHECK (removed_at IS NULL OR removed_at > installed_at),
  CONSTRAINT part_install_removal_ck CHECK (
    CASE
      WHEN removed_at IS NULL THEN removal_destination IS NULL AND removal_reason IS NULL
      WHEN removal_destination = 'scrap' THEN COALESCE(removal_reason IN ('broken', 'worn', 'lost'), false)
      WHEN removal_destination IN ('stock', 'spare') THEN removal_reason IS NULL
      ELSE false
    END
  )
);
CREATE INDEX part_install_active_idx ON garage.part_install (team_id, part_id) WHERE removed_at IS NULL;

-- Lista de compras de equipa: só as exceções à lista calculada (manuais, quantidade ajustada,
-- automáticas escondidas). Uma linha por peça.
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

-- =============================================================================
-- setup
-- =============================================================================

-- Campos personalizados por equipa (os campos-base vêm do esquema JSON versionado da app).
CREATE TABLE setup.field_definition (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  key                 text        NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{1,62}$'),
  label               text        NOT NULL,
  group_key           text        NOT NULL,                   -- diffs, shocks, geometry, engine...
  data_type           setup.field_type NOT NULL,
  unit                text,
  min_value           numeric,
  max_value           numeric,
  options             jsonb,                                  -- para data_type = 'enum'
  sort_order          integer     NOT NULL DEFAULT 0,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, key),
  CHECK (min_value IS NULL OR max_value IS NULL OR min_value <= max_value),
  CHECK (data_type <> 'enum' OR jsonb_typeof(options) = 'array')
);

-- Ficha "viva" de setup; o conteúdo está em setup.setup_version.
CREATE TABLE setup.setup (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  car_id              uuid        NOT NULL,
  name                text        NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  current_version     integer     NOT NULL DEFAULT 1 CHECK (current_version >= 1),
  notes               text,
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, car_id) REFERENCES garage.car (team_id, id) ON DELETE CASCADE
);
CREATE INDEX setup_car_idx ON setup.setup (team_id, car_id) WHERE deleted_at IS NULL;

-- Versões imutáveis. "data" segue o esquema JSON da app (schema_version) + campos personalizados.
CREATE TABLE setup.setup_version (
  team_id             uuid        NOT NULL,
  setup_id            uuid        NOT NULL,
  version             integer     NOT NULL CHECK (version >= 1),
  data                jsonb       NOT NULL CHECK (jsonb_typeof(data) = 'object'),
  schema_version      integer     NOT NULL,
  based_on_setup_id   uuid,
  based_on_version    integer,
  change_summary      text,                                   -- "óleo dif. traseiro 5k → 7k"
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  -- Colunas extraídas para análises e filtros (valores numéricos validados pela app).
  diff_front_cst      integer GENERATED ALWAYS AS ((data #>> '{diffs,front_cst}')::integer)  STORED,
  diff_center_cst     integer GENERATED ALWAYS AS ((data #>> '{diffs,center_cst}')::integer) STORED,
  diff_rear_cst       integer GENERATED ALWAYS AS ((data #>> '{diffs,rear_cst}')::integer)   STORED,
  shock_front_oil_cst integer GENERATED ALWAYS AS ((data #>> '{shocks,front,oil_cst}')::integer) STORED,
  shock_rear_oil_cst  integer GENERATED ALWAYS AS ((data #>> '{shocks,rear,oil_cst}')::integer)  STORED,
  clutch_bell_teeth   smallint GENERATED ALWAYS AS ((data #>> '{transmission,clutch_bell_teeth}')::smallint) STORED,
  spur_teeth          smallint GENERATED ALWAYS AS ((data #>> '{transmission,spur_teeth}')::smallint) STORED,
  PRIMARY KEY (setup_id, version),
  UNIQUE (team_id, setup_id, version),
  FOREIGN KEY (team_id, setup_id) REFERENCES setup.setup (team_id, id) ON DELETE CASCADE,
  CHECK ((based_on_setup_id IS NULL) = (based_on_version IS NULL))
);
CREATE INDEX setup_version_data_gin ON setup.setup_version USING gin (data jsonb_path_ops);

-- Feedback do piloto sobre uma versão de setup (ADR 0026). Só de acrescento; o F2 liga-o a uma sessão.
CREATE TABLE setup.setup_feedback (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  setup_id            uuid        NOT NULL,
  version             integer     NOT NULL,
  author_id           uuid        NOT NULL REFERENCES identity.user_account (id),
  steering_entry      smallint    CHECK (steering_entry  BETWEEN 1 AND 5),
  steering_mid        smallint    CHECK (steering_mid    BETWEEN 1 AND 5),
  steering_exit       smallint    CHECK (steering_exit   BETWEEN 1 AND 5),
  traction            smallint    CHECK (traction        BETWEEN 1 AND 5),
  jump_stability      smallint    CHECK (jump_stability  BETWEEN 1 AND 5),
  braking             smallint    CHECK (braking         BETWEEN 1 AND 5),
  engine_low_end      smallint    CHECK (engine_low_end  BETWEEN 1 AND 5),
  engine_top_end      smallint    CHECK (engine_top_end  BETWEEN 1 AND 5),
  track_zone          text        CHECK (track_zone IS NULL OR char_length(btrim(track_zone)) BETWEEN 1 AND 80),
  note                text        CHECK (note IS NULL OR char_length(btrim(note)) BETWEEN 1 AND 2000),
  created_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, setup_id, version)
    REFERENCES setup.setup_version (team_id, setup_id, version) ON DELETE CASCADE,
  CONSTRAINT setup_feedback_content_ck CHECK (
    num_nonnulls(steering_entry, steering_mid, steering_exit, traction, jump_stability,
                 braking, engine_low_end, engine_top_end, note) > 0
  )
);
CREATE INDEX setup_feedback_version_idx ON setup.setup_feedback (team_id, setup_id, version, created_at);

-- Modelos de ficha por carro (F2-00, ADR 0030).
-- Identidade do modelo. team_id NULL = global catalogue (written only by reviewed migrations).
CREATE TABLE setup.sheet_template (
  id              uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id         uuid        REFERENCES identity.team (id) ON DELETE CASCADE,
  slug            text        NOT NULL CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
  make            text        NOT NULL CHECK (char_length(make) BETWEEN 1 AND 80),
  model           text        NOT NULL CHECK (char_length(model) BETWEEN 1 AND 80),
  scale           text        NOT NULL CONSTRAINT sheet_template_scale_fk REFERENCES garage.scale (code),
  powertrain      text        NOT NULL CONSTRAINT sheet_template_powertrain_fk REFERENCES garage.powertrain (code),
  name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
  source_url      text        CHECK (source_url IS NULL OR (source_url ~ '^https://' AND char_length(source_url) <= 500)),
  current_version integer     NOT NULL DEFAULT 1 CHECK (current_version >= 1),
  based_on_id     uuid        REFERENCES setup.sheet_template (id),
  created_by      uuid        REFERENCES identity.user_account (id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  version         integer     NOT NULL DEFAULT 1,
  deleted_at      timestamptz,
  UNIQUE (team_id, id),
  CHECK (based_on_id IS NULL OR team_id IS NOT NULL)
);
CREATE UNIQUE INDEX sheet_template_global_slug ON setup.sheet_template (slug) WHERE team_id IS NULL;
CREATE UNIQUE INDEX sheet_template_team_slug ON setup.sheet_template (team_id, slug) WHERE team_id IS NOT NULL;

-- Immutable versions of a template. team_id is copied from the template by a trigger (for the RLS).
CREATE TABLE setup.sheet_template_version (
  template_id     uuid        NOT NULL REFERENCES setup.sheet_template (id) ON DELETE CASCADE,
  team_id         uuid,
  version         integer     NOT NULL CHECK (version >= 1),
  definition      jsonb       NOT NULL CHECK (jsonb_typeof(definition) = 'object'
                                              AND octet_length(definition::text) <= 262144),
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (template_id, version)
);

-- The current version must exist. Deferred so a template and its first version go in together.
ALTER TABLE setup.sheet_template
  ADD CONSTRAINT sheet_template_current_version_fk FOREIGN KEY (id, current_version)
  REFERENCES setup.sheet_template_version (template_id, version) DEFERRABLE INITIALLY DEFERRED;

-- Favoritos por pista e condição do piso.
CREATE TABLE setup.favorite (
  team_id             uuid        NOT NULL,
  setup_id            uuid        NOT NULL,
  track_id            uuid        NOT NULL REFERENCES events.track (id) ON DELETE CASCADE,
  surface             setup.surface NOT NULL,
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (setup_id, track_id, surface),
  FOREIGN KEY (team_id, setup_id) REFERENCES setup.setup (team_id, id) ON DELETE CASCADE
);

-- Links de partilha só de leitura. Só o hash do token é guardado.
CREATE TABLE setup.share_link (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  setup_id            uuid        NOT NULL,
  pinned_version      integer,                                -- NULL = segue a versão atual
  token_hash          bytea       NOT NULL UNIQUE CHECK (length(token_hash) = 32),
  expires_at          timestamptz,                            -- NULL = sem expiração
  revoked_at          timestamptz,
  last_used_at        timestamptz,
  use_count           integer     NOT NULL DEFAULT 0,
  created_by          uuid        NOT NULL REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT share_link_team_id_uq UNIQUE (team_id, id),
  FOREIGN KEY (team_id, setup_id) REFERENCES setup.setup (team_id, id) ON DELETE CASCADE,
  CONSTRAINT share_link_pinned_version_fk
    FOREIGN KEY (team_id, setup_id, pinned_version)
    REFERENCES setup.setup_version (team_id, setup_id, version) ON DELETE CASCADE,
  CONSTRAINT share_link_expiry_ck
    CHECK (expires_at IS NULL OR expires_at > created_at)
);
CREATE INDEX share_link_setup_idx ON setup.share_link (team_id, setup_id);

ALTER TABLE garage.car
  ADD CONSTRAINT car_base_setup_fk
  FOREIGN KEY (team_id, base_setup_id) REFERENCES setup.setup (team_id, id) ON DELETE SET NULL (base_setup_id);

-- =============================================================================
-- events
-- =============================================================================

CREATE TABLE events.championship (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  name                text        NOT NULL,
  season              smallint    NOT NULL CHECK (season BETWEEN 2000 AND 2100),
  organizer           text,
  scope               text,                                   -- nacional, regional, open
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, id)
);

CREATE TABLE events.event (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  championship_id     uuid,
  track_id            uuid        NOT NULL REFERENCES events.track (id),
  name                text        NOT NULL,
  starts_on           date        NOT NULL,
  ends_on             date        NOT NULL,
  status              events.event_status NOT NULL DEFAULT 'planned',
  regulation_url      text,
  timing_url          text,
  telemetry_allowed   boolean,                                -- NULL = ainda não confirmado no regulamento
  schedule_offset_min integer     NOT NULL DEFAULT 0,         -- atraso global do programa
  notes               text,
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, championship_id) REFERENCES events.championship (team_id, id) ON DELETE SET NULL (championship_id),
  CHECK (ends_on >= starts_on)
);
CREATE INDEX event_team_dates_idx ON events.event (team_id, starts_on DESC) WHERE deleted_at IS NULL;

-- Uma sessão em pista: treino, manga ou final.
CREATE TABLE events.session (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  event_id            uuid,                                   -- NULL = treino fora de prova
  track_id            uuid        NOT NULL REFERENCES events.track (id),
  kind                events.session_kind NOT NULL,
  label               text        NOT NULL,                   -- "Q3", "Final A", "Treino livre 2"
  round_no            smallint,
  status              events.session_status NOT NULL DEFAULT 'scheduled',
  scheduled_at        timestamptz,
  started_at          timestamptz,
  ended_at            timestamptz,
  planned_duration_s  integer     CHECK (planned_duration_s > 0),
  -- Material usado
  car_id              uuid        NOT NULL,
  engine_id           uuid,
  tyre_set_id         uuid,
  setup_id            uuid,
  setup_version       integer,
  transponder_number  text,
  -- Condições
  air_temp_c          numeric(4,1),
  track_temp_c        numeric(4,1),
  humidity_pct        smallint    CHECK (humidity_pct BETWEEN 0 AND 100),
  surface             setup.surface,
  weather_source      events.data_source,
  -- Resultado (resumo; voltas em events.lap)
  laps_completed      smallint    CHECK (laps_completed >= 0),
  total_time_ms       integer     CHECK (total_time_ms >= 0),
  best_lap_ms         integer     CHECK (best_lap_ms > 0),
  final_position      smallint    CHECK (final_position > 0),
  result_source       events.data_source,
  tanks_used          numeric(5,2) CHECK (tanks_used >= 0),
  fuel_left_ml        smallint    CHECK (fuel_left_ml >= 0),
  notes               text,
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, event_id)    REFERENCES events.event (team_id, id)   ON DELETE CASCADE,
  FOREIGN KEY (team_id, car_id)      REFERENCES garage.car (team_id, id),
  FOREIGN KEY (team_id, engine_id)   REFERENCES garage.engine (team_id, id),
  FOREIGN KEY (team_id, tyre_set_id) REFERENCES garage.tyre_set (team_id, id),
  FOREIGN KEY (team_id, setup_id, setup_version) REFERENCES setup.setup_version (team_id, setup_id, version),
  CHECK ((setup_id IS NULL) = (setup_version IS NULL)),
  CHECK (ended_at IS NULL OR started_at IS NULL OR ended_at >= started_at)
);
CREATE INDEX session_event_idx    ON events.session (team_id, event_id, scheduled_at);
CREATE INDEX session_car_time_idx ON events.session (team_id, car_id, started_at DESC);
CREATE INDEX session_setup_idx    ON events.session (team_id, setup_id, setup_version);

-- Feedback do piloto (escalas 1–5).
CREATE TABLE events.driver_feedback (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  session_id          uuid        NOT NULL,
  author_id           uuid        NOT NULL REFERENCES identity.user_account (id),
  steering_entry      smallint    CHECK (steering_entry  BETWEEN 1 AND 5),
  steering_mid        smallint    CHECK (steering_mid    BETWEEN 1 AND 5),
  steering_exit       smallint    CHECK (steering_exit   BETWEEN 1 AND 5),
  traction            smallint    CHECK (traction        BETWEEN 1 AND 5),
  jump_stability      smallint    CHECK (jump_stability  BETWEEN 1 AND 5),
  braking             smallint    CHECK (braking         BETWEEN 1 AND 5),
  engine_low_end      smallint    CHECK (engine_low_end  BETWEEN 1 AND 5),
  engine_top_end      smallint    CHECK (engine_top_end  BETWEEN 1 AND 5),
  track_zone          text,
  note                text,
  voice_object_key    text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  FOREIGN KEY (team_id, session_id) REFERENCES events.session (team_id, id) ON DELETE CASCADE
);
CREATE INDEX driver_feedback_session_idx ON events.driver_feedback (team_id, session_id);

-- Voltas da própria equipa (importadas, manuais ou por telemetria).
CREATE TABLE events.lap (
  team_id             uuid        NOT NULL,
  session_id          uuid        NOT NULL,
  lap_no              smallint    NOT NULL CHECK (lap_no >= 1),
  time_ms             integer     NOT NULL CHECK (time_ms > 0),
  cumulative_ms       integer     CHECK (cumulative_ms > 0),
  position            smallint    CHECK (position > 0),
  source              events.data_source NOT NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (session_id, lap_no),
  FOREIGN KEY (team_id, session_id) REFERENCES events.session (team_id, id) ON DELETE CASCADE
);

CREATE TABLE events.lap_incident (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  session_id          uuid        NOT NULL,
  lap_no              smallint    CHECK (lap_no >= 1),
  kind                events.incident_kind NOT NULL,
  occurred_at         timestamptz,
  note                text,
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (team_id, session_id) REFERENCES events.session (team_id, id) ON DELETE CASCADE
);
CREATE INDEX lap_incident_session_idx ON events.lap_incident (team_id, session_id, lap_no);

-- Plano de estratégia de uma final (um por sessão).
CREATE TABLE events.race_strategy (
  id                    uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id               uuid        NOT NULL,
  session_id            uuid        NOT NULL UNIQUE,
  race_duration_s       integer     NOT NULL CHECK (race_duration_s > 0),
  laps_per_tank_avg     numeric(5,2) NOT NULL CHECK (laps_per_tank_avg > 0),
  laps_per_tank_worst   numeric(5,2) NOT NULL CHECK (laps_per_tank_worst > 0),
  safety_margin_pct     numeric(4,1) NOT NULL DEFAULT 10 CHECK (safety_margin_pct BETWEEN 0 AND 50),
  warning_laps          smallint    NOT NULL DEFAULT 2 CHECK (warning_laps BETWEEN 0 AND 10),
  refuel_s_avg          numeric(5,2) CHECK (refuel_s_avg > 0),
  planned_stops         jsonb       NOT NULL DEFAULT '[]'::jsonb,   -- [{"lap":9,"actions":["refuel"]}, ...]
  scenarios             jsonb       NOT NULL DEFAULT '[]'::jsonb,
  created_by            uuid        REFERENCES identity.user_account (id),
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  version               integer     NOT NULL DEFAULT 1,
  FOREIGN KEY (team_id, session_id) REFERENCES events.session (team_id, id) ON DELETE CASCADE,
  CHECK (laps_per_tank_worst <= laps_per_tank_avg),
  CHECK (jsonb_typeof(planned_stops) = 'array' AND jsonb_typeof(scenarios) = 'array')
);

-- Paragens na box (só acrescento; correções são novas linhas com supersedes_id).
CREATE TABLE events.pit_stop (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  session_id          uuid        NOT NULL,
  seq                 smallint    NOT NULL CHECK (seq >= 1),
  actions             events.pit_action[] NOT NULL CHECK (cardinality(actions) >= 1),
  lap_no              smallint    CHECK (lap_no >= 0),
  in_at               timestamptz NOT NULL,
  out_at              timestamptz,
  unplanned           boolean     NOT NULL DEFAULT false,
  supersedes_id       uuid        REFERENCES events.pit_stop (id),
  recorded_by         uuid        REFERENCES identity.user_account (id),
  sync_op_id          uuid,                                   -- operação offline que a criou
  created_at          timestamptz NOT NULL DEFAULT now(),
  duration_ms         integer GENERATED ALWAYS AS
                        ((extract(epoch FROM (out_at - in_at)) * 1000)::integer) STORED,
  FOREIGN KEY (team_id, session_id) REFERENCES events.session (team_id, id) ON DELETE CASCADE,
  CHECK (out_at IS NULL OR out_at >= in_at)
);
CREATE UNIQUE INDEX pit_stop_seq_uq ON events.pit_stop (session_id, seq) WHERE supersedes_id IS NULL;

-- =============================================================================
-- maintenance
-- =============================================================================

CREATE TABLE maintenance.rule (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  name                text        NOT NULL,                   -- "Rolamentos do motor"
  target_kind         maintenance.target_kind NOT NULL,
  metric              maintenance.metric NOT NULL,
  threshold           numeric(10,2) NOT NULL CHECK (threshold > 0),
  warn_at_pct         smallint    NOT NULL DEFAULT 80 CHECK (warn_at_pct BETWEEN 1 AND 100),
  action              text        NOT NULL,                   -- "Trocar rolamentos"
  part_kind           garage.part_kind,                       -- peça normalmente usada
  active              boolean     NOT NULL DEFAULT true,
  created_by          uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, id)
);

CREATE TABLE maintenance.entry (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  car_id              uuid,
  engine_id           uuid,
  tyre_set_id         uuid,
  rule_id             uuid,                                   -- regra que a intervenção satisfaz
  kind                maintenance.entry_kind NOT NULL,
  title               text        NOT NULL,
  notes               text,
  tanks_at_entry      numeric(8,2),                           -- leitura do contador no momento
  run_seconds_at_entry integer,
  cost_cents          integer     CHECK (cost_cents >= 0),
  photo_object_keys   text[]      NOT NULL DEFAULT '{}',
  performed_at        timestamptz NOT NULL DEFAULT now(),
  performed_by        uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, car_id)      REFERENCES garage.car (team_id, id)      ON DELETE CASCADE,
  FOREIGN KEY (team_id, engine_id)   REFERENCES garage.engine (team_id, id)   ON DELETE CASCADE,
  FOREIGN KEY (team_id, tyre_set_id) REFERENCES garage.tyre_set (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, rule_id)     REFERENCES maintenance.rule (team_id, id) ON DELETE SET NULL (rule_id),
  CHECK (num_nonnulls(car_id, engine_id, tyre_set_id) = 1)
);
CREATE INDEX entry_engine_idx ON maintenance.entry (team_id, engine_id, performed_at DESC) WHERE engine_id IS NOT NULL;
CREATE INDEX entry_car_idx    ON maintenance.entry (team_id, car_id, performed_at DESC)    WHERE car_id IS NOT NULL;
CREATE INDEX entry_rule_idx   ON maintenance.entry (team_id, rule_id, performed_at DESC)   WHERE rule_id IS NOT NULL;

CREATE TABLE maintenance.entry_part (
  team_id             uuid        NOT NULL,
  entry_id            uuid        NOT NULL,
  part_id             uuid        NOT NULL,
  qty                 numeric(10,2) NOT NULL CHECK (qty > 0),
  PRIMARY KEY (entry_id, part_id),
  FOREIGN KEY (team_id, entry_id) REFERENCES maintenance.entry (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, part_id)  REFERENCES garage.part (team_id, id)
);

ALTER TABLE garage.stock_movement
  ADD CONSTRAINT stock_movement_entry_fk
  FOREIGN KEY (team_id, maintenance_entry_id) REFERENCES maintenance.entry (team_id, id) ON DELETE SET NULL (maintenance_entry_id);

CREATE TABLE maintenance.checklist_template (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  scope               maintenance.checklist_scope NOT NULL,
  name                text        NOT NULL,
  is_default          boolean     NOT NULL DEFAULT false,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, id)
);
CREATE UNIQUE INDEX checklist_template_default_uq ON maintenance.checklist_template (team_id, scope)
  WHERE is_default AND deleted_at IS NULL;

CREATE TABLE maintenance.checklist_template_item (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  template_id         uuid        NOT NULL,
  position            smallint    NOT NULL,
  label               text        NOT NULL,
  is_required         boolean     NOT NULL DEFAULT true,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, template_id) REFERENCES maintenance.checklist_template (team_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX checklist_item_position_uq ON maintenance.checklist_template_item (template_id, position)
  WHERE deleted_at IS NULL;

CREATE TABLE maintenance.checklist_run (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  template_id         uuid        NOT NULL,
  event_id            uuid,
  session_id          uuid,
  started_at          timestamptz NOT NULL DEFAULT now(),
  completed_at        timestamptz,
  started_by          uuid        REFERENCES identity.user_account (id),
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, template_id) REFERENCES maintenance.checklist_template (team_id, id),
  FOREIGN KEY (team_id, event_id)    REFERENCES events.event (team_id, id)   ON DELETE CASCADE,
  FOREIGN KEY (team_id, session_id)  REFERENCES events.session (team_id, id) ON DELETE CASCADE,
  CHECK (num_nonnulls(event_id, session_id) = 1)
);

CREATE TABLE maintenance.checklist_run_item (
  team_id             uuid        NOT NULL,
  run_id              uuid        NOT NULL,
  template_item_id    uuid        NOT NULL,
  checked_by          uuid        REFERENCES identity.user_account (id),
  checked_at          timestamptz NOT NULL DEFAULT now(),
  note                text,
  PRIMARY KEY (run_id, template_item_id),
  FOREIGN KEY (team_id, run_id)           REFERENCES maintenance.checklist_run (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, template_item_id) REFERENCES maintenance.checklist_template_item (team_id, id)
);

-- =============================================================================
-- timing
-- =============================================================================

CREATE TABLE timing.provider_link (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  event_id            uuid        NOT NULL,
  provider            timing.provider NOT NULL,
  external_ref        text        NOT NULL,                   -- id/URL da prova no fornecedor
  enabled             boolean     NOT NULL DEFAULT true,
  terms_confirmed_at  timestamptz,                            -- termos do fornecedor verificados
  last_sync_at        timestamptz,
  last_error          text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  UNIQUE (team_id, id),
  UNIQUE (event_id, provider),
  FOREIGN KEY (team_id, event_id) REFERENCES events.event (team_id, id) ON DELETE CASCADE,
  CHECK (provider = 'csv_upload' OR terms_confirmed_at IS NOT NULL OR NOT enabled)
);

-- Resposta bruta guardada no armazenamento de objetos (reprocessável).
CREATE TABLE timing.raw_import (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  provider_link_id    uuid,
  provider            timing.provider NOT NULL,
  source_url          text,
  object_key          text        NOT NULL,
  sha256              bytea       NOT NULL CHECK (length(sha256) = 32),
  size_bytes          integer     NOT NULL CHECK (size_bytes BETWEEN 1 AND 20971520),
  status              timing.import_status NOT NULL DEFAULT 'pending',
  error               text,
  parser_version      text,
  fetched_at          timestamptz NOT NULL DEFAULT now(),
  parsed_at           timestamptz,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, provider_link_id) REFERENCES timing.provider_link (team_id, id) ON DELETE SET NULL (provider_link_id)
);
CREATE INDEX raw_import_status_idx ON timing.raw_import (status, fetched_at) WHERE status = 'pending';

-- Outros pilotos vistos nos resultados (visíveis apenas à equipa).
CREATE TABLE timing.competitor (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  display_name        text        NOT NULL,                   -- como publicado pela organização
  transponder_number  text,
  is_rival            boolean     NOT NULL DEFAULT false,
  notes               text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  UNIQUE (team_id, id)
);
CREATE INDEX competitor_transponder_idx ON timing.competitor (team_id, transponder_number);

CREATE TABLE timing.result (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  session_id          uuid        NOT NULL,
  competitor_id       uuid,                                   -- NULL = a própria equipa
  raw_import_id       uuid,
  position            smallint    CHECK (position > 0),
  laps                smallint    CHECK (laps >= 0),
  total_time_ms       integer     CHECK (total_time_ms >= 0),
  best_lap_ms         integer     CHECK (best_lap_ms > 0),
  created_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, session_id)    REFERENCES events.session (team_id, id)   ON DELETE CASCADE,
  FOREIGN KEY (team_id, competitor_id) REFERENCES timing.competitor (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, raw_import_id) REFERENCES timing.raw_import (team_id, id) ON DELETE SET NULL (raw_import_id)
);
CREATE UNIQUE INDEX result_session_competitor_uq ON timing.result (session_id, competitor_id) NULLS NOT DISTINCT;

CREATE TABLE timing.result_lap (
  team_id             uuid        NOT NULL,
  result_id           uuid        NOT NULL,
  lap_no              smallint    NOT NULL CHECK (lap_no >= 1),
  time_ms             integer     NOT NULL CHECK (time_ms > 0),
  position            smallint    CHECK (position > 0),
  PRIMARY KEY (result_id, lap_no),
  FOREIGN KEY (team_id, result_id) REFERENCES timing.result (team_id, id) ON DELETE CASCADE
);

-- =============================================================================
-- telemetry
-- =============================================================================

-- Catálogo global de firmware (sem dados de equipa; só leitura para a aplicação).
CREATE TABLE telemetry.firmware_release (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  version             text        NOT NULL UNIQUE CHECK (version ~ '^\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$'),
  security_counter    integer     NOT NULL CHECK (security_counter >= 0),   -- anti-rollback
  channel             telemetry.release_channel NOT NULL DEFAULT 'stable',
  min_hardware_rev    text        NOT NULL,
  object_key          text        NOT NULL,
  sha256              bytea       NOT NULL CHECK (length(sha256) = 32),
  signature           bytea       NOT NULL,                   -- Ed25519 sobre o binário
  release_notes       text,
  released_at         timestamptz NOT NULL DEFAULT now(),
  withdrawn_at        timestamptz
);

CREATE TABLE telemetry.device (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  car_id              uuid,
  serial_number       text        NOT NULL UNIQUE,
  hardware_rev        text        NOT NULL,
  firmware_version    text,
  cert_serial         text        UNIQUE,                     -- certificado de cliente X.509 atual
  cert_not_after      timestamptz,
  lora_key_id         text,                                   -- referência à chave de sessão LoRa (no KMS)
  status              telemetry.device_status NOT NULL DEFAULT 'pairing',
  paired_at           timestamptz,
  revoked_at          timestamptz,
  last_seen_at        timestamptz,
  battery_pct         smallint    CHECK (battery_pct BETWEEN 0 AND 100),
  storage_free_mb     integer,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, car_id) REFERENCES garage.car (team_id, id) ON DELETE SET NULL (car_id),
  CHECK (status <> 'revoked' OR revoked_at IS NOT NULL)
);

CREATE TABLE telemetry.pairing_code (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  code_hash           bytea       NOT NULL UNIQUE CHECK (length(code_hash) = 32),
  expires_at          timestamptz NOT NULL,
  used_at             timestamptz,
  device_id           uuid,
  created_by          uuid        NOT NULL REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (team_id, device_id) REFERENCES telemetry.device (team_id, id) ON DELETE CASCADE,
  CHECK (expires_at <= created_at + interval '5 minutes')
);

-- Um ficheiro/gravação do módulo, normalmente associado a uma sessão.
CREATE TABLE telemetry.recording (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  device_id           uuid        NOT NULL,
  session_id          uuid,
  started_at          timestamptz NOT NULL,
  ended_at            timestamptz,
  raw_object_key      text,
  raw_sha256          bytea       CHECK (length(raw_sha256) = 32),
  firmware_version    text,
  sample_count        integer     NOT NULL DEFAULT 0,
  status              telemetry.upload_status NOT NULL DEFAULT 'uploaded',
  error               text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  UNIQUE (team_id, id),
  FOREIGN KEY (team_id, device_id)  REFERENCES telemetry.device (team_id, id) ON DELETE CASCADE,
  FOREIGN KEY (team_id, session_id) REFERENCES events.session (team_id, id)  ON DELETE SET NULL (session_id),
  CHECK (ended_at IS NULL OR ended_at >= started_at)
);
CREATE INDEX recording_session_idx ON telemetry.recording (team_id, session_id);
CREATE UNIQUE INDEX recording_raw_uq ON telemetry.recording (device_id, raw_sha256) WHERE raw_sha256 IS NOT NULL;

-- Amostras (hypertable TimescaleDB quando disponível — ver fim do ficheiro).
CREATE TABLE telemetry.sample (
  team_id             uuid        NOT NULL,
  device_id           uuid        NOT NULL,
  recording_id        uuid        NOT NULL,
  ts                  timestamptz NOT NULL,
  head_temp_c         real,
  rpm                 integer     CHECK (rpm >= 0),
  rx_voltage_v        real,
  accel_x_g           real,
  accel_y_g           real,
  accel_z_g           real,
  gyro_x_dps          real,
  gyro_y_dps          real,
  gyro_z_dps          real,
  throttle_pct        smallint    CHECK (throttle_pct BETWEEN -100 AND 100),
  steering_pct        smallint    CHECK (steering_pct BETWEEN -100 AND 100),
  PRIMARY KEY (device_id, ts),
  FOREIGN KEY (team_id, recording_id) REFERENCES telemetry.recording (team_id, id) ON DELETE CASCADE
);
CREATE INDEX sample_recording_ts_idx ON telemetry.sample (recording_id, ts);

CREATE TABLE telemetry.alert_threshold (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL REFERENCES identity.team (id) ON DELETE CASCADE,
  car_id              uuid,                                   -- NULL = todos os carros da equipa
  metric              telemetry.metric NOT NULL,
  min_value           numeric,
  max_value           numeric,
  enabled             boolean     NOT NULL DEFAULT true,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  version             integer     NOT NULL DEFAULT 1,
  FOREIGN KEY (team_id, car_id) REFERENCES garage.car (team_id, id) ON DELETE CASCADE,
  CHECK (num_nonnulls(min_value, max_value) >= 1)
);
CREATE UNIQUE INDEX alert_threshold_uq ON telemetry.alert_threshold (team_id, car_id, metric) NULLS NOT DISTINCT;

CREATE TABLE telemetry.alert (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  device_id           uuid        NOT NULL,
  recording_id        uuid,
  ts                  timestamptz NOT NULL,
  metric              telemetry.metric NOT NULL,
  value               numeric     NOT NULL,
  threshold           numeric     NOT NULL,
  acknowledged_at     timestamptz,
  acknowledged_by     uuid        REFERENCES identity.user_account (id),
  created_at          timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (team_id, device_id)    REFERENCES telemetry.device (team_id, id)    ON DELETE CASCADE,
  FOREIGN KEY (team_id, recording_id) REFERENCES telemetry.recording (team_id, id) ON DELETE CASCADE
);
CREATE INDEX alert_recording_idx ON telemetry.alert (team_id, recording_id, ts);

-- =============================================================================
-- sync
-- =============================================================================

-- Uma linha por operação recebida de um dispositivo (op_id = chave de idempotência).
CREATE TABLE sync.operation (
  op_id               uuid        PRIMARY KEY,
  team_id             uuid        NOT NULL,
  user_id             uuid        NOT NULL REFERENCES identity.user_account (id),
  device_key_id       uuid        NOT NULL,
  entity              text        NOT NULL,                   -- "events.pit_stop", "setup.setup"...
  entity_id           uuid        NOT NULL,
  kind                sync.op_kind NOT NULL,
  base_version        integer,
  fields              jsonb,
  client_ts           timestamptz NOT NULL,
  received_at         timestamptz NOT NULL DEFAULT now(),
  status              sync.op_status NOT NULL,
  reason              text,                                   -- motivo de rejeição
  device_signature    bytea       NOT NULL,                   -- ECDSA P-256 (P1363) sobre a forma canónica
  CONSTRAINT operation_signature_len CHECK (length(device_signature) = 64),
  result_version      integer,                                -- versão da entidade depois desta operação
  FOREIGN KEY (team_id, device_key_id) REFERENCES identity.device_key (team_id, id)
);
CREATE INDEX operation_team_received_idx ON sync.operation (team_id, received_at DESC);
CREATE INDEX operation_entity_version_idx ON sync.operation (team_id, entity, entity_id, result_version DESC)
  WHERE result_version IS NOT NULL;

CREATE TABLE sync.conflict (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  team_id             uuid        NOT NULL,
  op_id               uuid        NOT NULL REFERENCES sync.operation (op_id) ON DELETE CASCADE,
  entity              text        NOT NULL,
  entity_id           uuid        NOT NULL,
  field               text        NOT NULL,
  server_value        jsonb,
  client_value        jsonb,
  resolution          text        CHECK (resolution IN ('server', 'client', 'manual')),
  resolved_value      jsonb,
  resolved_by         uuid        REFERENCES identity.user_account (id),
  resolved_at         timestamptz,
  created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX conflict_open_idx ON sync.conflict (team_id, created_at) WHERE resolved_at IS NULL;

-- Change log por equipa: o cursor do Pull é "seq".
CREATE TABLE sync.change_log (
  seq                 bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  team_id             uuid        NOT NULL,
  entity              text        NOT NULL,
  entity_id           uuid        NOT NULL,
  op                  char(1)     NOT NULL CHECK (op IN ('I', 'U', 'D')),
  changed_at          timestamptz NOT NULL DEFAULT now(),
  -- Transação que escreveu: o Pull só devolve linhas com xid < xmin do snapshot (sem buracos no cursor).
  xid                 xid8        NOT NULL DEFAULT pg_current_xact_id()
);
CREATE INDEX change_log_team_seq_idx ON sync.change_log (team_id, seq);
CREATE INDEX change_log_team_xid_seq_idx ON sync.change_log (team_id, xid, seq);

-- Regista uma alteração no change log. TG_ARGV[0] = coluna que identifica a entidade.
CREATE OR REPLACE FUNCTION sync.log_change() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  r   jsonb;
  op  char(1);
BEGIN
  IF TG_OP = 'DELETE' THEN r := to_jsonb(OLD); op := 'D';
  ELSE
    r := to_jsonb(NEW);
    op := CASE WHEN TG_OP = 'INSERT' THEN 'I'
               WHEN (r ->> 'deleted_at') IS NOT NULL THEN 'D'
               ELSE 'U' END;
  END IF;
  INSERT INTO sync.change_log (team_id, entity, entity_id, op)
  VALUES ((r ->> 'team_id')::uuid, TG_TABLE_SCHEMA || '.' || TG_TABLE_NAME, (r ->> TG_ARGV[0])::uuid, op);
  RETURN NULL;
END
$$;

-- =============================================================================
-- platform
-- =============================================================================

CREATE TABLE platform.idempotency_key (
  request_id          uuid        NOT NULL,
  user_id             uuid        NOT NULL REFERENCES identity.user_account (id) ON DELETE CASCADE,
  method              text        NOT NULL,                   -- "/pitbox.setup.v1.SetupService/CreateSetup"
  request_hash        bytea       NOT NULL,
  response            bytea,                                  -- envelope JSON versionado da resposta
  status_code         text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  expires_at          timestamptz NOT NULL DEFAULT now() + interval '7 days',
  PRIMARY KEY (user_id, request_id),                      -- o request_id só é único por utilizador
  CONSTRAINT idempotency_expires_after_created CHECK (expires_at > created_at)
);
CREATE INDEX idempotency_key_expiry_idx ON platform.idempotency_key (expires_at);

-- Outbox transacional: eventos de domínio publicados pelo pitbox-worker (poller próprio; ADR 0015).
CREATE TABLE platform.outbox (
  id                  bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  team_id             uuid,
  aggregate           text        NOT NULL,                   -- "garage.engine"
  aggregate_id        uuid        NOT NULL,
  event_type          text        NOT NULL,                   -- "session.finished"
  payload             jsonb       NOT NULL,
  trace_parent        text,                                   -- W3C traceparent
  created_at          timestamptz NOT NULL DEFAULT now(),
  published_at        timestamptz,
  attempts            smallint    NOT NULL DEFAULT 0,
  locked_until        timestamptz
);
CREATE INDEX outbox_unpublished_idx ON platform.outbox (id) WHERE published_at IS NULL;
-- Ordem por chave (ADR 0015): o claim só pega no evento mais antigo por (equipa, agregado, id).
CREATE INDEX outbox_unpublished_key_idx ON platform.outbox (team_id, aggregate, aggregate_id, id) WHERE published_at IS NULL;

CREATE TABLE platform.notification_preference (
  user_id             uuid        NOT NULL REFERENCES identity.user_account (id) ON DELETE CASCADE,
  kind                text        NOT NULL,                   -- "maintenance_due", "low_stock"...
  channel             platform.notify_channel NOT NULL,
  enabled             boolean     NOT NULL DEFAULT true,
  updated_at          timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, kind, channel),
  CHECK (kind NOT LIKE 'security_%' OR enabled)               -- alertas de segurança não se desligam
);

CREATE TABLE platform.push_subscription (
  id                  uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  user_id             uuid        NOT NULL REFERENCES identity.user_account (id) ON DELETE CASCADE,
  platform            identity.device_platform NOT NULL,
  endpoint            text        NOT NULL UNIQUE,            -- Web Push endpoint ou token FCM/APNs
  keys                jsonb,
  created_at          timestamptz NOT NULL DEFAULT now(),
  last_used_at        timestamptz
);

-- =============================================================================
-- audit (só acrescento; exportado diariamente para armazenamento WORM)
-- =============================================================================

CREATE TABLE audit.audit_log (
  id                  bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  team_id             uuid,                                   -- NULL = evento de plataforma
  actor_kind          text        NOT NULL CHECK (actor_kind IN ('user', 'device', 'system', 'admin')),
  actor_id            text        NOT NULL,
  action              text        NOT NULL,                   -- "setup.update", "member.role_changed"
  object_type         text        NOT NULL,
  object_id           text,
  before              jsonb,
  after               jsonb,
  request_id          uuid,
  trace_id            text,
  ip                  inet,
  user_agent          text,
  occurred_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_team_time_idx   ON audit.audit_log (team_id, occurred_at DESC);
CREATE INDEX audit_object_idx      ON audit.audit_log (object_type, object_id);

-- =============================================================================
-- Vistas
-- =============================================================================

CREATE VIEW garage.stock_level WITH (security_invoker = true) AS
SELECT p.team_id,
       p.id                                  AS part_id,
       p.kind,
       p.description,
       p.unit,
       COALESCE(sum(m.qty_delta), 0)         AS qty,
       p.min_stock,
       COALESCE(sum(m.qty_delta), 0) < p.min_stock AS below_min
FROM garage.part p
LEFT JOIN garage.stock_movement m ON m.part_id = p.id AND m.team_id = p.team_id
WHERE p.deleted_at IS NULL
GROUP BY p.team_id, p.id;

-- Estado das regras de manutenção de motores (tanks / run_seconds / sessions / days).
CREATE VIEW maintenance.engine_rule_status WITH (security_invoker = true) AS
WITH last_done AS (
  SELECT DISTINCT ON (e.engine_id, e.rule_id)
         e.team_id, e.engine_id, e.rule_id, e.performed_at, e.tanks_at_entry, e.run_seconds_at_entry
  FROM maintenance.entry e
  WHERE e.engine_id IS NOT NULL AND e.rule_id IS NOT NULL AND e.deleted_at IS NULL
  ORDER BY e.engine_id, e.rule_id, e.performed_at DESC
)
SELECT r.team_id,
       r.id   AS rule_id,
       r.name AS rule_name,
       g.id   AS engine_id,
       r.metric,
       r.threshold,
       CASE r.metric
         WHEN 'tanks'       THEN g.tanks_total - COALESCE(d.tanks_at_entry, 0)
         WHEN 'run_seconds' THEN g.run_seconds_total - COALESCE(d.run_seconds_at_entry, 0)
         WHEN 'sessions'    THEN (SELECT count(*) FROM events.session s
                                  WHERE s.engine_id = g.id AND s.team_id = g.team_id
                                    AND s.status = 'finished' AND s.deleted_at IS NULL
                                    AND s.ended_at > COALESCE(d.performed_at, '-infinity'))
         WHEN 'days'        THEN extract(day FROM now() - COALESCE(d.performed_at, g.created_at))
       END AS used_since_last,
       d.performed_at AS last_done_at
FROM maintenance.rule r
JOIN garage.engine g ON g.team_id = r.team_id AND g.deleted_at IS NULL AND g.status <> 'retired'
LEFT JOIN last_done d ON d.engine_id = g.id AND d.rule_id = r.id
WHERE r.target_kind = 'engine' AND r.active AND r.deleted_at IS NULL;

-- Resumo por sessão para análises (consistência, melhores voltas).
CREATE VIEW events.session_lap_stats WITH (security_invoker = true) AS
SELECT l.team_id,
       l.session_id,
       count(*)                                           AS laps,
       min(l.time_ms)                                     AS best_lap_ms,
       round(avg(l.time_ms))::integer                     AS avg_lap_ms,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY l.time_ms)::integer AS median_lap_ms,
       round(stddev_samp(l.time_ms))::integer             AS stddev_ms,
       round(100.0 * count(*) FILTER (WHERE l.time_ms <= 1.02 * m.best) / count(*), 1) AS consistency_pct
FROM events.lap l
JOIN (SELECT session_id, min(time_ms) AS best FROM events.lap GROUP BY session_id) m USING (session_id)
GROUP BY l.team_id, l.session_id;

-- =============================================================================
-- Triggers: updated_at/version, imutabilidade, change log
-- =============================================================================

-- touch() em todas as tabelas com updated_at + version.
DO $$
DECLARE t record;
BEGIN
  FOR t IN
    SELECT c.table_schema, c.table_name
    FROM information_schema.columns c
    JOIN information_schema.tables tb
      ON tb.table_schema = c.table_schema AND tb.table_name = c.table_name AND tb.table_type = 'BASE TABLE'
    WHERE c.column_name = 'updated_at'
      AND c.table_schema IN ('identity','garage','setup','maintenance','events','timing','telemetry')
      AND EXISTS (SELECT 1 FROM information_schema.columns v
                  WHERE v.table_schema = c.table_schema AND v.table_name = c.table_name AND v.column_name = 'version')
  LOOP
    EXECUTE format('CREATE TRIGGER touch BEFORE UPDATE ON %I.%I FOR EACH ROW EXECUTE FUNCTION app.touch()',
                   t.table_schema, t.table_name);
  END LOOP;
END
$$;

-- Tabelas só de acrescento.
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON setup.setup_version
  FOR EACH ROW EXECUTE FUNCTION app.forbid_mutation();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON setup.setup_feedback
  FOR EACH ROW EXECUTE FUNCTION app.forbid_mutation();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON audit.audit_log
  FOR EACH ROW EXECUTE FUNCTION app.forbid_mutation();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON garage.stock_movement
  FOR EACH ROW EXECUTE FUNCTION app.forbid_mutation();
-- pit_stop: só se permite fechar a paragem (out_at) uma vez; correções = nova linha.
CREATE OR REPLACE FUNCTION events.pit_stop_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'pit_stop é só de acrescento' USING ERRCODE = 'insufficient_privilege';
  END IF;
  IF OLD.out_at IS NOT NULL OR (to_jsonb(NEW) - 'out_at' - 'duration_ms') <> (to_jsonb(OLD) - 'out_at' - 'duration_ms') THEN
    RAISE EXCEPTION 'pit_stop: só out_at pode ser definido, e apenas uma vez' USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END
$$;
CREATE TRIGGER guard BEFORE UPDATE OR DELETE ON events.pit_stop
  FOR EACH ROW EXECUTE FUNCTION events.pit_stop_guard();

-- Change log para as entidades sincronizadas com a app (coluna de identificação em TG_ARGV[0]).
DO $$
DECLARE
  t text[];
BEGIN
  FOREACH t SLICE 1 IN ARRAY ARRAY[
    ['garage','car','id'], ['garage','engine','id'], ['garage','engine_mount','id'],
    ['garage','tyre_set','id'], ['garage','part','id'], ['garage','stock_movement','id'],
    ['garage','part_install','id'], ['garage','shopping_item','id'],
    ['setup','field_definition','id'], ['setup','setup','id'], ['setup','setup_version','setup_id'],
    ['setup','setup_feedback','id'],
    ['setup','favorite','setup_id'],
    ['events','track','id'], ['events','championship','id'], ['events','event','id'],
    ['events','session','id'], ['events','driver_feedback','id'], ['events','lap','session_id'],
    ['events','lap_incident','id'], ['events','race_strategy','id'], ['events','pit_stop','id'],
    ['maintenance','rule','id'], ['maintenance','entry','id'], ['maintenance','entry_part','entry_id'],
    ['maintenance','checklist_template','id'], ['maintenance','checklist_template_item','id'],
    ['maintenance','checklist_run','id'], ['maintenance','checklist_run_item','run_id'],
    ['timing','competitor','id'], ['timing','result','id'],
    ['telemetry','device','id'], ['telemetry','recording','id'], ['telemetry','alert_threshold','id'],
    ['telemetry','alert','id']
  ]
  LOOP
    EXECUTE format(
      'CREATE TRIGGER change_log AFTER INSERT OR UPDATE OR DELETE ON %I.%I FOR EACH ROW EXECUTE FUNCTION sync.log_change(%L)',
      t[1], t[2], t[3]);
  END LOOP;
END
$$;

-- =============================================================================
-- Row-Level Security
-- =============================================================================

-- Política padrão: só a equipa do contexto atual.
DO $$
DECLARE t record;
BEGIN
  FOR t IN
    SELECT c.table_schema, c.table_name
    FROM information_schema.columns c
    JOIN information_schema.tables tb
      ON tb.table_schema = c.table_schema AND tb.table_name = c.table_name AND tb.table_type = 'BASE TABLE'
    WHERE c.column_name = 'team_id'
      AND c.table_schema IN ('identity','garage','setup','maintenance','events','timing','telemetry','sync','audit','platform')
      AND (c.table_schema, c.table_name) NOT IN (('identity','membership'), ('events','track'), ('audit','audit_log'), ('platform','outbox'),
                                                   ('setup','sheet_template'), ('setup','sheet_template_version'))
  LOOP
    EXECUTE format('ALTER TABLE %I.%I ENABLE ROW LEVEL SECURITY', t.table_schema, t.table_name);
    EXECUTE format(
      'CREATE POLICY team_isolation ON %I.%I USING (team_id = app.current_team_id()) WITH CHECK (team_id = app.current_team_id())',
      t.table_schema, t.table_name);
  END LOOP;
END
$$;

-- team: a equipa do contexto, ou equipas de que o utilizador é membro (lista "as minhas equipas").
ALTER TABLE identity.team ENABLE ROW LEVEL SECURITY;
CREATE POLICY team_visible ON identity.team
  USING (id = app.current_team_id()
         OR id IN (SELECT m.team_id FROM identity.membership m WHERE m.user_id = app.current_user_id()))
  WITH CHECK (id = app.current_team_id());

-- membership: membros da equipa atual + as próprias pertenças do utilizador.
ALTER TABLE identity.membership ENABLE ROW LEVEL SECURITY;
CREATE POLICY membership_visible ON identity.membership
  USING (team_id = app.current_team_id() OR user_id = app.current_user_id())
  WITH CHECK (team_id = app.current_team_id());

-- user_account: o próprio utilizador e os colegas da equipa atual.
ALTER TABLE identity.user_account ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_visible ON identity.user_account
  USING (id = app.current_user_id()
         OR id IN (SELECT m.user_id FROM identity.membership m WHERE m.team_id = app.current_team_id()))
  WITH CHECK (id = app.current_user_id());
-- O primeiro registo do utilizador não passa por esta política: é criado por
-- identity.resolve_login (SECURITY DEFINER, depois das permissões), porque antes do primeiro login
-- ainda não existe app.user_id.

-- track: catálogo global (team_id NULL) visível a todos; pistas próprias só da equipa.
ALTER TABLE events.track ENABLE ROW LEVEL SECURITY;
CREATE POLICY track_visible ON events.track
  USING (team_id IS NULL OR team_id = app.current_team_id())
  WITH CHECK (team_id = app.current_team_id());

-- audit_log: leitura só da equipa atual; escrita permitida para a equipa atual.
ALTER TABLE audit.audit_log ENABLE ROW LEVEL SECURITY;
CREATE POLICY audit_team ON audit.audit_log
  USING (team_id = app.current_team_id())
  WITH CHECK (team_id IS NULL OR team_id = app.current_team_id());

-- Tabelas por utilizador.
ALTER TABLE platform.notification_preference ENABLE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON platform.notification_preference
  USING (user_id = app.current_user_id()) WITH CHECK (user_id = app.current_user_id());
ALTER TABLE platform.push_subscription ENABLE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON platform.push_subscription
  USING (user_id = app.current_user_id()) WITH CHECK (user_id = app.current_user_id());
ALTER TABLE platform.idempotency_key ENABLE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON platform.idempotency_key
  USING (user_id = app.current_user_id()) WITH CHECK (user_id = app.current_user_id());

ALTER TABLE identity.recovery_code ENABLE ROW LEVEL SECURITY;
CREATE POLICY own_rows ON identity.recovery_code
  USING (user_id = app.current_user_id()) WITH CHECK (user_id = app.current_user_id());

-- platform.outbox e telemetry.firmware_release: sem RLS; o acesso é limitado por GRANT.
-- Nota: a ingestão de telemetria e o worker definem app.team_id por mensagem, como a API.

-- =============================================================================
-- Permissões
-- =============================================================================

DO $$
DECLARE s text;
BEGIN
  FOREACH s IN ARRAY ARRAY['app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit']
  LOOP
    EXECUTE format('GRANT USAGE ON SCHEMA %I TO pitbox_app, pitbox_readonly', s);
    EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO pitbox_app', s);
    EXECUTE format('GRANT SELECT ON ALL TABLES IN SCHEMA %I TO pitbox_readonly', s);
    EXECUTE format('GRANT USAGE ON ALL SEQUENCES IN SCHEMA %I TO pitbox_app', s);
    EXECUTE format('GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA %I TO pitbox_app, pitbox_readonly', s);
  END LOOP;
END
$$;

-- Objetos criados por migrações futuras (como pitbox_owner) herdam os mesmos privilégios.
-- O que for sensível a pitbox_readonly leva REVOKE explícito na migração; o teste de
-- privilégios falha se a lista de tabelas legíveis mudar sem ser decidido.
DO $$
DECLARE s text;
BEGIN
  FOREACH s IN ARRAY ARRAY['app','identity','garage','setup','maintenance','events','timing','telemetry','sync','platform','audit']
  LOOP
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE pitbox_owner IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pitbox_app', s);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE pitbox_owner IN SCHEMA %I GRANT SELECT ON TABLES TO pitbox_readonly', s);
    EXECUTE format('ALTER DEFAULT PRIVILEGES FOR ROLE pitbox_owner IN SCHEMA %I GRANT USAGE ON SEQUENCES TO pitbox_app', s);
  END LOOP;
END
$$;

-- Restrições adicionais além dos triggers.
REVOKE UPDATE, DELETE ON audit.audit_log            FROM pitbox_app;
REVOKE UPDATE, DELETE ON setup.setup_version        FROM pitbox_app;
REVOKE UPDATE, DELETE ON setup.setup_feedback       FROM pitbox_app;
REVOKE UPDATE, DELETE ON garage.stock_movement      FROM pitbox_app;
REVOKE INSERT, UPDATE, DELETE ON telemetry.firmware_release FROM pitbox_app;
REVOKE SELECT ON platform.idempotency_key           FROM pitbox_readonly;
REVOKE SELECT ON platform.push_subscription         FROM pitbox_readonly;   -- endpoints e chaves de push
REVOKE SELECT ON setup.share_link                   FROM pitbox_readonly;   -- hashes de tokens e metadados de partilha
REVOKE SELECT ON platform.outbox                    FROM pitbox_readonly;   -- eventos de todas as equipas, sem RLS
-- Catálogos globais de referência (ADR 0016): a aplicação só lê; só as migrações escrevem.
REVOKE INSERT, UPDATE, DELETE ON garage.scale, garage.powertrain FROM pitbox_app;
REVOKE INSERT, UPDATE, DELETE ON garage.scale, garage.powertrain FROM pitbox_readonly;

-- =============================================================================
-- identity.resolve_login — primeiro login (F1-03b, ADR 0013)
-- =============================================================================
-- Fica DEPOIS das permissões em bloco acima: o GRANT EXECUTE ON ALL FUNCTIONS daria
-- EXECUTE também a pitbox_readonly. Na migração isto não acontece (a função é posterior).
-- No primeiro login ainda não existe app.user_id, por isso a RLS de user_account esconde
-- a linha por idp_subject e recusa o INSERT. Esta função é o único caminho: corre como
-- pitbox_owner (dono da tabela, isento da RLS enquanto ela não for FORCE) e por isso:
--   * procura SÓ por idp_subject (o "sub" do IdP) e nunca liga contas por email;
--   * devolve apenas o id do utilizador (nada de linhas nem de dados de outros);
--   * só pitbox_app pode executá-la (PUBLIC e pitbox_readonly não);
--   * search_path fixo e nomes qualificados (SECURITY DEFINER sem isto é uma escalada).
-- Erros (SQLSTATE próprios, mensagem fixa sem dados pessoais; a API mapeia-os e mostra ao
-- utilizador uma mensagem genérica):
--   22023  argumento inválido (sub ou email vazios ou demasiado longos)
--   PB001  o email já pertence a outra conta
--   PB002  a conta foi apagada
CREATE OR REPLACE FUNCTION identity.resolve_login(p_sub text, p_email citext, p_display_name text)
RETURNS uuid
LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_id      uuid;
  v_email   public.citext;
  v_name    text;
  v_deleted timestamptz;
  v_new     text;
BEGIN
  IF p_sub IS NULL OR btrim(p_sub) = '' OR length(p_sub) > 255 THEN
    RAISE EXCEPTION 'resolve_login: invalid subject' USING ERRCODE = '22023';
  END IF;
  IF p_email IS NULL OR btrim(p_email::text) = '' OR length(p_email::text) > 320
     OR position('@' IN p_email::text) < 2 THEN
    RAISE EXCEPTION 'resolve_login: invalid email' USING ERRCODE = '22023';
  END IF;
  -- Nome 1..80 caracteres (CHECK da tabela); sem nome, usa a parte local do email.
  v_new := left(btrim(COALESCE(p_display_name, '')), 80);
  IF v_new = '' THEN
    v_new := left(split_part(p_email::text, '@', 1), 80);
  END IF;

  SELECT u.id, u.email, u.display_name, u.deleted_at
    INTO v_id, v_email, v_name, v_deleted
    FROM identity.user_account u
   WHERE u.idp_subject = p_sub
   FOR UPDATE;

  IF FOUND THEN
    IF v_deleted IS NOT NULL THEN
      RAISE EXCEPTION 'resolve_login: account deleted' USING ERRCODE = 'PB002';
    END IF;
    -- lower(): com este search_path os operadores do citext (em public) não são visíveis e a
    -- comparação direta seria sensível a maiúsculas.
    IF lower(v_email::text) <> lower(p_email::text) OR v_name <> v_new THEN
      BEGIN
        UPDATE identity.user_account
           SET email = p_email, display_name = v_new
         WHERE id = v_id;
      EXCEPTION WHEN unique_violation THEN
        RAISE EXCEPTION 'resolve_login: email in use' USING ERRCODE = 'PB001';
      END;
    END IF;
    RETURN v_id;
  END IF;

  BEGIN
    INSERT INTO identity.user_account (idp_subject, email, display_name)
    VALUES (p_sub, p_email, v_new)
    RETURNING id INTO v_id;
  EXCEPTION WHEN unique_violation THEN
    -- Ou o mesmo sub entrou entretanto (primeiros logins em paralelo: devolve o existente),
    -- ou o email é de outra conta.
    SELECT u.id INTO v_id FROM identity.user_account u
     WHERE u.idp_subject = p_sub AND u.deleted_at IS NULL;
    IF NOT FOUND THEN
      RAISE EXCEPTION 'resolve_login: email in use' USING ERRCODE = 'PB001';
    END IF;
  END;
  RETURN v_id;
END
$$;

REVOKE ALL ON FUNCTION identity.resolve_login(text, citext, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.resolve_login(text, citext, text) TO pitbox_app;
COMMENT ON FUNCTION identity.resolve_login(text, citext, text) IS
  'Primeiro login: devolve o id do utilizador do "sub" do IdP, criando-o se não existir. SECURITY DEFINER, só pitbox_app.';

-- identity.is_owner — verifica se um utilizador é dono de alguma equipa (F1-03c).
-- Necessita de SECURITY DEFINER porque a verificação não tem contexto de equipa.
CREATE OR REPLACE FUNCTION identity.is_owner(p_user_id uuid)
  RETURNS boolean
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  RETURN EXISTS (
    SELECT 1 FROM identity.membership
    WHERE user_id = p_user_id AND role = 'owner'
  );
END
$$;

REVOKE ALL ON FUNCTION identity.is_owner(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.is_owner(uuid) TO pitbox_app;
COMMENT ON FUNCTION identity.is_owner(uuid) IS
  'Verifica se o utilizador é dono de alguma equipa. SECURITY DEFINER, só pitbox_app.';

-- identity.replace_recovery_codes — substitui os códigos de recuperação não usados do utilizador.
-- Necessita de SECURITY DEFINER porque a tabela tem RLS por utilizador e a aplicação pode não
-- ter ainda app.user_id definido no contexto do BFF.
CREATE OR REPLACE FUNCTION identity.replace_recovery_codes(
  p_user_id uuid,
  p_code_hashes bytea[],
  p_key_id text
)
  RETURNS void
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  invalid int;
BEGIN
  IF p_key_id IS NULL OR p_key_id !~ '^[a-z0-9][a-z0-9-]{0,15}$' THEN
    RAISE EXCEPTION 'replace_recovery_codes: invalid key id' USING ERRCODE = '22023';
  END IF;
  IF p_code_hashes IS NULL OR cardinality(p_code_hashes) NOT BETWEEN 1 AND 16 THEN
    RAISE EXCEPTION 'replace_recovery_codes: invalid hash count' USING ERRCODE = '22023';
  END IF;
  SELECT count(*) INTO invalid
  FROM unnest(p_code_hashes) AS h
  WHERE h IS NULL OR length(h) != 32;
  IF invalid > 0 THEN
    RAISE EXCEPTION 'replace_recovery_codes: invalid hash length' USING ERRCODE = '22023';
  END IF;

  DELETE FROM identity.recovery_code
  WHERE user_id = p_user_id AND used_at IS NULL;

  INSERT INTO identity.recovery_code (user_id, code_hash, key_id)
  SELECT p_user_id, h, p_key_id
  FROM unnest(p_code_hashes) AS h;
END
$$;

REVOKE ALL ON FUNCTION identity.replace_recovery_codes(uuid, bytea[], text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.replace_recovery_codes(uuid, bytea[], text) TO pitbox_app;
COMMENT ON FUNCTION identity.replace_recovery_codes(uuid, bytea[], text) IS
  'Substitui os códigos de recuperação não usados de um utilizador. SECURITY DEFINER, só pitbox_app.';

-- identity.consume_recovery_code — consome um código de recuperação se algum dos candidatos
-- existir e não estiver usado.
CREATE OR REPLACE FUNCTION identity.consume_recovery_code(p_user_id uuid, p_code_hashes bytea[])
  RETURNS boolean
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  IF p_code_hashes IS NULL OR cardinality(p_code_hashes) NOT BETWEEN 1 AND 16 THEN
    RAISE EXCEPTION 'consume_recovery_code: invalid hash count' USING ERRCODE = '22023';
  END IF;
  IF EXISTS (
    SELECT 1 FROM unnest(p_code_hashes) AS h
    WHERE h IS NULL OR octet_length(h) <> 32
  ) THEN
    RAISE EXCEPTION 'consume_recovery_code: invalid hash length' USING ERRCODE = '22023';
  END IF;

  UPDATE identity.recovery_code
     SET used_at = now()
   WHERE user_id = p_user_id
     AND code_hash = ANY(p_code_hashes)
     AND used_at IS NULL;

  RETURN FOUND;
END
$$;

REVOKE ALL ON FUNCTION identity.consume_recovery_code(uuid, bytea[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.consume_recovery_code(uuid, bytea[]) TO pitbox_app;
COMMENT ON FUNCTION identity.consume_recovery_code(uuid, bytea[]) IS
  'Consome um código de recuperação de MFA. SECURITY DEFINER, só pitbox_app.';

CREATE OR REPLACE FUNCTION identity.accept_invitation(p_token_hash bytea)
  RETURNS TABLE (out_team_id uuid, out_role identity.team_role, out_consumed boolean)
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  v_user     uuid := app.current_user_id();
  v_inv      identity.invitation%ROWTYPE;
  v_email    public.citext;
  v_role     identity.team_role;
  v_consumed boolean := false;
BEGIN
  IF v_user IS NULL THEN
    RAISE EXCEPTION 'accept_invitation: no user context' USING ERRCODE = 'PB010';
  END IF;
  IF p_token_hash IS NULL OR length(p_token_hash) <> 32 THEN
    RAISE EXCEPTION 'accept_invitation: invalid invitation' USING ERRCODE = 'PB010';
  END IF;

  SELECT * INTO v_inv
    FROM identity.invitation i
   WHERE i.token_hash = p_token_hash
   FOR UPDATE;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'accept_invitation: invalid invitation' USING ERRCODE = 'PB010';
  END IF;

  -- Já aceite por este utilizador: devolve a pertença existente sem mudar nada.
  IF v_inv.accepted_at IS NOT NULL THEN
    IF v_inv.accepted_by = v_user THEN
      SELECT m.role INTO v_role
        FROM identity.membership m
       WHERE m.team_id = v_inv.team_id AND m.user_id = v_user;
      IF FOUND THEN
        RETURN QUERY SELECT v_inv.team_id, v_role, false;
        RETURN;
      END IF;
    END IF;
    RAISE EXCEPTION 'accept_invitation: invalid invitation' USING ERRCODE = 'PB010';
  END IF;

  IF v_inv.revoked_at IS NOT NULL OR v_inv.expires_at <= now() THEN
    RAISE EXCEPTION 'accept_invitation: invalid invitation' USING ERRCODE = 'PB010';
  END IF;

  -- Convite por email: tem de coincidir com o email da conta. Com este
  -- search_path os operadores de citext não são visíveis: comparar com lower().
  IF v_inv.email IS NOT NULL THEN
    SELECT u.email INTO v_email
      FROM identity.user_account u
     WHERE u.id = v_user AND u.deleted_at IS NULL;
    IF NOT FOUND OR v_email IS NULL OR lower(v_email::text) <> lower(v_inv.email::text) THEN
      RAISE EXCEPTION 'accept_invitation: invalid invitation' USING ERRCODE = 'PB010';
    END IF;
  END IF;

  -- Já é membro por outra via: não consome o convite nem muda o papel.
  SELECT m.role INTO v_role
    FROM identity.membership m
   WHERE m.team_id = v_inv.team_id AND m.user_id = v_user;
  IF FOUND THEN
    RETURN QUERY SELECT v_inv.team_id, v_role, false;
    RETURN;
  END IF;

  UPDATE identity.invitation
     SET accepted_at = now(), accepted_by = v_user
   WHERE id = v_inv.id;

  INSERT INTO identity.membership (team_id, user_id, role, invited_by)
  VALUES (v_inv.team_id, v_user, v_inv.role, v_inv.created_by);

  v_role := v_inv.role;
  v_consumed := true;
  RETURN QUERY SELECT v_inv.team_id, v_role, v_consumed;
END
$$;

REVOKE ALL ON FUNCTION identity.accept_invitation(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.accept_invitation(bytea) TO pitbox_app;
COMMENT ON FUNCTION identity.accept_invitation(bytea) IS
  'Aceita um convite pelo hash do token, sem contexto de equipa. SECURITY DEFINER, só pitbox_app.';

-- setup.open_share_link — leitura pública de um setup partilhado (F1-08b).
-- Fica depois do bloco de GRANT EXECUTE ON ALL FUNCTIONS para o pitbox_readonly
-- não herdar EXECUTE no schema.sql. A função corre como dona, sem contexto de
-- equipa: a RLS de setup.* não se aplica. Só devolve uma linha quando o link,
-- a equipa, o setup e a versão são válidos; caso contrário devolve zero linhas.
CREATE OR REPLACE FUNCTION setup.open_share_link(p_token_hash bytea)
  RETURNS TABLE (
    name text,
    version integer,
    schema_version integer,
    data jsonb,
    change_summary text,
    version_created_at timestamptz
  )
  LANGUAGE sql
  VOLATILE
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
  WITH opened AS (
    UPDATE setup.share_link l
       SET last_used_at = now(), use_count = use_count + 1
     WHERE l.token_hash = p_token_hash
       AND octet_length(p_token_hash) = 32
       AND l.revoked_at IS NULL
       AND (l.expires_at IS NULL OR l.expires_at > now())
       AND EXISTS (
         SELECT 1
           FROM setup.setup s
           JOIN identity.team t ON t.id = l.team_id AND t.deleted_at IS NULL
           JOIN setup.setup_version v
             ON v.team_id = s.team_id
            AND v.setup_id = s.id
            AND v.version = COALESCE(l.pinned_version, s.current_version)
          WHERE s.team_id = l.team_id
            AND s.id = l.setup_id
            AND s.deleted_at IS NULL
       )
    RETURNING l.team_id, l.setup_id, l.pinned_version
  )
  SELECT s.name, v.version, v.schema_version, v.data, v.change_summary, v.created_at
    FROM opened o
    JOIN setup.setup s ON s.team_id = o.team_id AND s.id = o.setup_id
    JOIN setup.setup_version v
      ON v.team_id = s.team_id
     AND v.setup_id = s.id
     AND v.version = COALESCE(o.pinned_version, s.current_version);
$$;

REVOKE ALL ON FUNCTION setup.open_share_link(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION setup.open_share_link(bytea) TO pitbox_app;
COMMENT ON FUNCTION setup.open_share_link(bytea) IS
  'Abre um link de partilha pelo hash do token, sem contexto de equipa. SECURITY DEFINER, só pitbox_app.';

-- =============================================================================
-- F2-00: modelos de ficha (ADR 0030) — triggers, RLS, vista e link público
-- =============================================================================
-- Depois das permissões em bloco, como na migração. As colunas e tabelas estão acima.
CREATE FUNCTION setup.sheet_template_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    -- A team copy may only be based on a visible global template (RLS hides other teams' ones).
    IF NEW.based_on_id IS NOT NULL AND NOT EXISTS (
         SELECT 1 FROM setup.sheet_template g WHERE g.id = NEW.based_on_id AND g.team_id IS NULL) THEN
      RAISE EXCEPTION 'sheet template not available' USING ERRCODE = 'PB020';
    END IF;
    RETURN NEW;
  END IF;
  IF NEW.id <> OLD.id OR NEW.team_id IS DISTINCT FROM OLD.team_id OR NEW.based_on_id IS DISTINCT FROM OLD.based_on_id
     OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at <> OLD.created_at
     OR NEW.current_version < OLD.current_version THEN
    RAISE EXCEPTION 'sheet_template: identity columns are immutable and current_version only grows'
      USING ERRCODE = 'insufficient_privilege';
  END IF;
  RETURN NEW;
END
$$;

CREATE FUNCTION setup.sheet_template_version_team() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE t uuid;
BEGIN
  SELECT s.team_id INTO t FROM setup.sheet_template s WHERE s.id = NEW.template_id;
  IF NOT FOUND THEN
    RAISE EXCEPTION 'sheet template not available' USING ERRCODE = 'PB020';
  END IF;
  NEW.team_id := t;
  RETURN NEW;
END
$$;

CREATE FUNCTION garage.check_car_sheet_template() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.sheet_template_id IS NULL THEN RETURN NEW; END IF;
  IF TG_OP = 'UPDATE' AND NEW.sheet_template_id IS NOT DISTINCT FROM OLD.sheet_template_id THEN RETURN NEW; END IF;
  IF NOT EXISTS (SELECT 1 FROM setup.sheet_template t
                  WHERE t.id = NEW.sheet_template_id AND t.deleted_at IS NULL
                    AND (t.team_id IS NULL OR t.team_id = NEW.team_id)) THEN
    RAISE EXCEPTION 'sheet template not available' USING ERRCODE = 'PB020';
  END IF;
  RETURN NEW;
END
$$;

REVOKE ALL ON FUNCTION setup.sheet_template_guard() FROM PUBLIC;
REVOKE ALL ON FUNCTION setup.sheet_template_version_team() FROM PUBLIC;
REVOKE ALL ON FUNCTION garage.check_car_sheet_template() FROM PUBLIC;

CREATE TRIGGER guard BEFORE INSERT OR UPDATE ON setup.sheet_template
  FOR EACH ROW EXECUTE FUNCTION setup.sheet_template_guard();
-- touch: criado pelo ciclo genérico acima (a tabela tem updated_at + version).
CREATE TRIGGER team_sync BEFORE INSERT ON setup.sheet_template_version
  FOR EACH ROW EXECUTE FUNCTION setup.sheet_template_version_team();
CREATE TRIGGER immutable BEFORE UPDATE OR DELETE ON setup.sheet_template_version
  FOR EACH ROW EXECUTE FUNCTION app.forbid_mutation();
CREATE TRIGGER sheet_template_check BEFORE INSERT OR UPDATE OF sheet_template_id ON garage.car
  FOR EACH ROW EXECUTE FUNCTION garage.check_car_sheet_template();

-- RLS: everybody reads the global catalogue; a team writes only its own rows. Separate policies per
-- command: with a single FOR ALL policy (as in events.track) an UPDATE could move a global row to
-- the caller's team, because the WITH CHECK only looks at the new row.
ALTER TABLE setup.sheet_template ENABLE ROW LEVEL SECURITY;
CREATE POLICY template_select ON setup.sheet_template
  FOR SELECT USING (team_id IS NULL OR team_id = app.current_team_id());
CREATE POLICY template_insert ON setup.sheet_template
  FOR INSERT WITH CHECK (team_id = app.current_team_id());
CREATE POLICY template_update ON setup.sheet_template
  FOR UPDATE USING (team_id = app.current_team_id()) WITH CHECK (team_id = app.current_team_id());
ALTER TABLE setup.sheet_template_version ENABLE ROW LEVEL SECURITY;
CREATE POLICY template_version_select ON setup.sheet_template_version
  FOR SELECT USING (team_id IS NULL OR team_id = app.current_team_id());
CREATE POLICY template_version_insert ON setup.sheet_template_version
  FOR INSERT WITH CHECK (team_id = app.current_team_id());

REVOKE DELETE ON setup.sheet_template FROM pitbox_app;
REVOKE UPDATE, DELETE ON setup.sheet_template_version FROM pitbox_app;

-- Analysis columns for v1 and v2 (D1). The 7 GENERATED columns stay as they are; v2 rows carry the
-- server-computed data.canonical. security_invoker: without it the view would ignore the RLS.
CREATE VIEW setup.setup_version_metrics WITH (security_invoker = true) AS
SELECT v.team_id, v.setup_id, v.version, v.schema_version,
  COALESCE(v.diff_front_cst, CASE WHEN jsonb_typeof(v.data #> '{canonical,diffs.front_cst}') = 'number'
    THEN (v.data #>> '{canonical,diffs.front_cst}')::numeric::integer END) AS diff_front_cst,
  COALESCE(v.diff_center_cst, CASE WHEN jsonb_typeof(v.data #> '{canonical,diffs.center_cst}') = 'number'
    THEN (v.data #>> '{canonical,diffs.center_cst}')::numeric::integer END) AS diff_center_cst,
  COALESCE(v.diff_rear_cst, CASE WHEN jsonb_typeof(v.data #> '{canonical,diffs.rear_cst}') = 'number'
    THEN (v.data #>> '{canonical,diffs.rear_cst}')::numeric::integer END) AS diff_rear_cst,
  COALESCE(v.shock_front_oil_cst, CASE WHEN jsonb_typeof(v.data #> '{canonical,shocks.front.oil_cst}') = 'number'
    THEN (v.data #>> '{canonical,shocks.front.oil_cst}')::numeric::integer END) AS shock_front_oil_cst,
  COALESCE(v.shock_rear_oil_cst, CASE WHEN jsonb_typeof(v.data #> '{canonical,shocks.rear.oil_cst}') = 'number'
    THEN (v.data #>> '{canonical,shocks.rear.oil_cst}')::numeric::integer END) AS shock_rear_oil_cst,
  COALESCE(v.clutch_bell_teeth, CASE WHEN jsonb_typeof(v.data #> '{canonical,transmission.clutch_bell_teeth}') = 'number'
    THEN (v.data #>> '{canonical,transmission.clutch_bell_teeth}')::numeric::smallint END) AS clutch_bell_teeth,
  COALESCE(v.spur_teeth, CASE WHEN jsonb_typeof(v.data #> '{canonical,transmission.spur_teeth}') = 'number'
    THEN (v.data #>> '{canonical,transmission.spur_teeth}')::numeric::smallint END) AS spur_teeth
FROM setup.setup_version v;

-- setup.open_share_link_v2 — same as open_share_link plus the definition of the template version
-- the shared version was made with (NULL for v1). A new function instead of changing the RETURNS
-- TABLE of the old one, which would need a DROP.
CREATE FUNCTION setup.open_share_link_v2(p_token_hash bytea)
  RETURNS TABLE (
    name text,
    version integer,
    schema_version integer,
    data jsonb,
    change_summary text,
    version_created_at timestamptz,
    template_definition jsonb
  )
  LANGUAGE sql
  VOLATILE
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
  WITH opened AS (
    UPDATE setup.share_link l
       SET last_used_at = now(), use_count = use_count + 1
     WHERE l.token_hash = p_token_hash
       AND octet_length(p_token_hash) = 32
       AND l.revoked_at IS NULL
       AND (l.expires_at IS NULL OR l.expires_at > now())
       AND EXISTS (
         SELECT 1
           FROM setup.setup s
           JOIN identity.team t ON t.id = l.team_id AND t.deleted_at IS NULL
           JOIN setup.setup_version v
             ON v.team_id = s.team_id
            AND v.setup_id = s.id
            AND v.version = COALESCE(l.pinned_version, s.current_version)
          WHERE s.team_id = l.team_id
            AND s.id = l.setup_id
            AND s.deleted_at IS NULL
       )
    RETURNING l.team_id, l.setup_id, l.pinned_version
  )
  SELECT s.name, v.version, v.schema_version, v.data, v.change_summary, v.created_at, tv.definition
    FROM opened o
    JOIN setup.setup s ON s.team_id = o.team_id AND s.id = o.setup_id
    JOIN setup.setup_version v
      ON v.team_id = s.team_id
     AND v.setup_id = s.id
     AND v.version = COALESCE(o.pinned_version, s.current_version)
    LEFT JOIN setup.sheet_template_version tv
      ON v.schema_version = 2
     AND jsonb_typeof(v.data #> '{template,version}') = 'number'
     AND tv.template_id = CASE WHEN (v.data #>> '{template,id}') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                               THEN (v.data #>> '{template,id}')::uuid END
     AND tv.version = CASE WHEN (v.data #>> '{template,version}') ~ '^[0-9]{1,9}$'
                           THEN (v.data #>> '{template,version}')::integer END
     AND (tv.team_id IS NULL OR tv.team_id = s.team_id);
$$;

REVOKE ALL ON FUNCTION setup.open_share_link_v2(bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION setup.open_share_link_v2(bytea) TO pitbox_app;
COMMENT ON FUNCTION setup.open_share_link_v2(bytea) IS
  'Como open_share_link, mais a definição do modelo da versão partilhada. SECURITY DEFINER, só pitbox_app.';

-- Os modelos da equipa sincronizam como as outras entidades; os globais viajam no snapshot do Pull.
CREATE TRIGGER change_log AFTER INSERT OR UPDATE ON setup.sheet_template
  FOR EACH ROW WHEN (NEW.team_id IS NOT NULL) EXECUTE FUNCTION sync.log_change('id');

-- Catálogo global de modelos: os ficheiros em backend/internal/setup/templates são a fonte.
-- BEGIN catálogo de modelos (gerado por scripts/template-migration.py; não editar)
INSERT INTO setup.sheet_template (id, team_id, slug, make, model, scale, powertrain, name, source_url, current_version)
VALUES ('01a1990b-b4dd-777f-9c5b-e31aaf01355f', NULL, 'generic-1-8-nitro', 'Genérico', '1/8 TT nitro', '1/8', 'nitro',
        'Genérico 1/8 TT nitro', NULL, 1)
ON CONFLICT (id) DO UPDATE
  SET current_version = EXCLUDED.current_version, name = EXCLUDED.name, source_url = EXCLUDED.source_url
  WHERE setup.sheet_template.current_version < EXCLUDED.current_version;
INSERT INTO setup.sheet_template_version (template_id, version, definition)
VALUES ('01a1990b-b4dd-777f-9c5b-e31aaf01355f', 1, $tpl${"format":1,"id":"01a1990b-b4dd-777f-9c5b-e31aaf01355f","slug":"generic-1-8-nitro","version":1,"make":"Genérico","model":"1/8 TT nitro","scale":"1/8","powertrain":"nitro","name":{"pt":"Genérico 1/8 TT nitro","en":"Generic 1/8 off-road nitro"},"groups":[{"key":"diffs","label":{"pt":"Diferenciais","en":"Diffs"},"fields":[{"key":"diffs_front_cst","label":{"pt":"Diffs front cst","en":"Diffs front cst"},"type":"int","unit":"cSt","min":100,"max":1000000,"canonical":"diffs.front_cst"},{"key":"diffs_center_cst","label":{"pt":"Diffs center cst","en":"Diffs center cst"},"type":"int","unit":"cSt","min":100,"max":1000000,"canonical":"diffs.center_cst"},{"key":"diffs_rear_cst","label":{"pt":"Diffs rear cst","en":"Diffs rear cst"},"type":"int","unit":"cSt","min":100,"max":1000000,"canonical":"diffs.rear_cst"},{"key":"diffs_front_ml","label":{"pt":"Diffs front ml","en":"Diffs front ml"},"type":"number","unit":"ml","min":0,"max":1000,"canonical":"diffs.front_ml"},{"key":"diffs_center_ml","label":{"pt":"Diffs center ml","en":"Diffs center ml"},"type":"number","unit":"ml","min":0,"max":1000,"canonical":"diffs.center_ml"},{"key":"diffs_rear_ml","label":{"pt":"Diffs rear ml","en":"Diffs rear ml"},"type":"number","unit":"ml","min":0,"max":1000,"canonical":"diffs.rear_ml"},{"key":"diffs_fill_notes","label":{"pt":"Diffs fill notes","en":"Diffs fill notes"},"type":"text","max_len":500,"canonical":"diffs.fill_notes"}]},{"key":"shocks","label":{"pt":"Amortecedores","en":"Shocks"},"fields":[{"key":"shocks_front_oil_cst","label":{"pt":"Shocks front oil cst","en":"Shocks front oil cst"},"type":"int","unit":"cSt","min":100,"max":2000,"canonical":"shocks.front.oil_cst"},{"key":"shocks_front_piston_holes","label":{"pt":"Shocks front piston holes","en":"Shocks front piston holes"},"type":"int","min":1,"max":10,"canonical":"shocks.front.piston_holes"},{"key":"shocks_front_piston_diameter_mm","label":{"pt":"Shocks front piston diameter mm","en":"Shocks front piston diameter mm"},"type":"number","unit":"mm","min":0.1,"max":5,"canonical":"shocks.front.piston_diameter_mm"},{"key":"shocks_front_spring_brand","label":{"pt":"Shocks front spring brand","en":"Shocks front spring brand"},"type":"text","max_len":40,"canonical":"shocks.front.spring_brand"},{"key":"shocks_front_spring_color","label":{"pt":"Shocks front spring color","en":"Shocks front spring color"},"type":"text","max_len":40,"canonical":"shocks.front.spring_color"},{"key":"shocks_front_spring_rate_gf_mm","label":{"pt":"Shocks front spring rate gf mm","en":"Shocks front spring rate gf mm"},"type":"number","unit":"gf/mm","min":0,"max":200,"canonical":"shocks.front.spring_rate_gf_mm"},{"key":"shocks_front_limiters_mm","label":{"pt":"Shocks front limiters mm","en":"Shocks front limiters mm"},"type":"number","unit":"mm","min":0,"max":200,"canonical":"shocks.front.limiters_mm"},{"key":"shocks_front_length_mm","label":{"pt":"Shocks front length mm","en":"Shocks front length mm"},"type":"number","unit":"mm","min":0,"max":200,"canonical":"shocks.front.length_mm"},{"key":"shocks_front_tower_position","label":{"pt":"Shocks front tower position","en":"Shocks front tower position"},"type":"text","max_len":40,"canonical":"shocks.front.tower_position"},{"key":"shocks_front_arm_position","label":{"pt":"Shocks front arm position","en":"Shocks front arm position"},"type":"text","max_len":40,"canonical":"shocks.front.arm_position"},{"key":"shocks_front_membrane_type","label":{"pt":"Shocks front membrane type","en":"Shocks front membrane type"},"type":"text","max_len":40,"canonical":"shocks.front.membrane_type"},{"key":"shocks_rear_oil_cst","label":{"pt":"Shocks rear oil cst","en":"Shocks rear oil cst"},"type":"int","unit":"cSt","min":100,"max":2000,"canonical":"shocks.rear.oil_cst"},{"key":"shocks_rear_piston_holes","label":{"pt":"Shocks rear piston holes","en":"Shocks rear piston holes"},"type":"int","min":1,"max":10,"canonical":"shocks.rear.piston_holes"},{"key":"shocks_rear_piston_diameter_mm","label":{"pt":"Shocks rear piston diameter mm","en":"Shocks rear piston diameter mm"},"type":"number","unit":"mm","min":0.1,"max":5,"canonical":"shocks.rear.piston_diameter_mm"},{"key":"shocks_rear_spring_brand","label":{"pt":"Shocks rear spring brand","en":"Shocks rear spring brand"},"type":"text","max_len":40,"canonical":"shocks.rear.spring_brand"},{"key":"shocks_rear_spring_color","label":{"pt":"Shocks rear spring color","en":"Shocks rear spring color"},"type":"text","max_len":40,"canonical":"shocks.rear.spring_color"},{"key":"shocks_rear_spring_rate_gf_mm","label":{"pt":"Shocks rear spring rate gf mm","en":"Shocks rear spring rate gf mm"},"type":"number","unit":"gf/mm","min":0,"max":200,"canonical":"shocks.rear.spring_rate_gf_mm"},{"key":"shocks_rear_limiters_mm","label":{"pt":"Shocks rear limiters mm","en":"Shocks rear limiters mm"},"type":"number","unit":"mm","min":0,"max":200,"canonical":"shocks.rear.limiters_mm"},{"key":"shocks_rear_length_mm","label":{"pt":"Shocks rear length mm","en":"Shocks rear length mm"},"type":"number","unit":"mm","min":0,"max":200,"canonical":"shocks.rear.length_mm"},{"key":"shocks_rear_tower_position","label":{"pt":"Shocks rear tower position","en":"Shocks rear tower position"},"type":"text","max_len":40,"canonical":"shocks.rear.tower_position"},{"key":"shocks_rear_arm_position","label":{"pt":"Shocks rear arm position","en":"Shocks rear arm position"},"type":"text","max_len":40,"canonical":"shocks.rear.arm_position"},{"key":"shocks_rear_membrane_type","label":{"pt":"Shocks rear membrane type","en":"Shocks rear membrane type"},"type":"text","max_len":40,"canonical":"shocks.rear.membrane_type"}]},{"key":"geometry","label":{"pt":"Geometria","en":"Geometry"},"fields":[{"key":"geometry_front_camber_deg","label":{"pt":"Geometry front camber deg","en":"Geometry front camber deg"},"type":"number","unit":"°","min":-5,"max":2,"canonical":"geometry.front_camber_deg"},{"key":"geometry_rear_camber_deg","label":{"pt":"Geometry rear camber deg","en":"Geometry rear camber deg"},"type":"number","unit":"°","min":-5,"max":2,"canonical":"geometry.rear_camber_deg"},{"key":"geometry_front_toe_deg","label":{"pt":"Geometry front toe deg","en":"Geometry front toe deg"},"type":"number","unit":"°","min":-5,"max":5,"canonical":"geometry.front_toe_deg"},{"key":"geometry_rear_toe_deg","label":{"pt":"Geometry rear toe deg","en":"Geometry rear toe deg"},"type":"number","unit":"°","min":-5,"max":5,"canonical":"geometry.rear_toe_deg"},{"key":"geometry_caster_deg","label":{"pt":"Geometry caster deg","en":"Geometry caster deg"},"type":"number","unit":"°","min":0,"max":30,"canonical":"geometry.caster_deg"},{"key":"geometry_ride_height_mm","label":{"pt":"Geometry ride height mm","en":"Geometry ride height mm"},"type":"number","unit":"mm","min":0,"max":100,"canonical":"geometry.ride_height_mm"},{"key":"geometry_front_droop_mm","label":{"pt":"Geometry front droop mm","en":"Geometry front droop mm"},"type":"number","unit":"mm","min":0,"max":50,"canonical":"geometry.front_droop_mm"},{"key":"geometry_rear_droop_mm","label":{"pt":"Geometry rear droop mm","en":"Geometry rear droop mm"},"type":"number","unit":"mm","min":0,"max":50,"canonical":"geometry.rear_droop_mm"},{"key":"geometry_ackermann","label":{"pt":"Geometry ackermann","en":"Geometry ackermann"},"type":"text","max_len":40,"canonical":"geometry.ackermann"},{"key":"geometry_rear_hub_position","label":{"pt":"Geometry rear hub position","en":"Geometry rear hub position"},"type":"text","max_len":40,"canonical":"geometry.rear_hub_position"}]},{"key":"bars","label":{"pt":"Barras","en":"Bars"},"fields":[{"key":"bars_front_mm","label":{"pt":"Bars front mm","en":"Bars front mm"},"type":"number","unit":"mm","min":0,"max":10,"canonical":"bars.front_mm"},{"key":"bars_front_removed","label":{"pt":"Bars front removed","en":"Bars front removed"},"type":"flag","canonical":"bars.front_removed"},{"key":"bars_rear_mm","label":{"pt":"Bars rear mm","en":"Bars rear mm"},"type":"number","unit":"mm","min":0,"max":10,"canonical":"bars.rear_mm"},{"key":"bars_rear_removed","label":{"pt":"Bars rear removed","en":"Bars rear removed"},"type":"flag","canonical":"bars.rear_removed"}]},{"key":"aero","label":{"pt":"Aerodinâmica","en":"Aero"},"fields":[{"key":"aero_wing_model","label":{"pt":"Aero wing model","en":"Aero wing model"},"type":"text","max_len":80,"canonical":"aero.wing_model"},{"key":"aero_wing_angle_deg","label":{"pt":"Aero wing angle deg","en":"Aero wing angle deg"},"type":"number","unit":"°","min":-10,"max":30,"canonical":"aero.wing_angle_deg"},{"key":"aero_gurney_mm","label":{"pt":"Aero gurney mm","en":"Aero gurney mm"},"type":"number","unit":"mm","min":0,"max":20,"canonical":"aero.gurney_mm"},{"key":"aero_body","label":{"pt":"Aero body","en":"Aero body"},"type":"text","max_len":80,"canonical":"aero.body"}]},{"key":"transmission","label":{"pt":"Transmissão","en":"Transmission"},"fields":[{"key":"transmission_clutch_bell_teeth","label":{"pt":"Transmission clutch bell teeth","en":"Transmission clutch bell teeth"},"type":"int","min":8,"max":60,"canonical":"transmission.clutch_bell_teeth"},{"key":"transmission_spur_teeth","label":{"pt":"Transmission spur teeth","en":"Transmission spur teeth"},"type":"int","min":8,"max":60,"canonical":"transmission.spur_teeth"},{"key":"transmission_clutch_shoe_material","label":{"pt":"Transmission clutch shoe material","en":"Transmission clutch shoe material"},"type":"text","max_len":40,"canonical":"transmission.clutch_shoe_material"},{"key":"transmission_clutch_spring","label":{"pt":"Transmission clutch spring","en":"Transmission clutch spring"},"type":"text","max_len":40,"canonical":"transmission.clutch_spring"},{"key":"transmission_clutch_clearance_mm","label":{"pt":"Transmission clutch clearance mm","en":"Transmission clutch clearance mm"},"type":"number","unit":"mm","min":0,"max":10,"canonical":"transmission.clutch_clearance_mm"}]},{"key":"engine","label":{"pt":"Motor","en":"Engine"},"fields":[{"key":"engine_high_needle_turns","label":{"pt":"Engine high needle turns","en":"Engine high needle turns"},"type":"number","min":0,"max":20,"canonical":"engine.high_needle_turns"},{"key":"engine_low_needle_turns","label":{"pt":"Engine low needle turns","en":"Engine low needle turns"},"type":"number","min":0,"max":20,"canonical":"engine.low_needle_turns"},{"key":"engine_glow_plug_brand","label":{"pt":"Engine glow plug brand","en":"Engine glow plug brand"},"type":"text","max_len":80,"canonical":"engine.glow_plug_brand"},{"key":"engine_glow_plug_temp","label":{"pt":"Engine glow plug temp","en":"Engine glow plug temp"},"type":"text","max_len":40,"canonical":"engine.glow_plug_temp"},{"key":"engine_fuel_brand","label":{"pt":"Engine fuel brand","en":"Engine fuel brand"},"type":"text","max_len":80,"canonical":"engine.fuel_brand"},{"key":"engine_fuel_nitro_pct","label":{"pt":"Engine fuel nitro pct","en":"Engine fuel nitro pct"},"type":"number","unit":"%","min":0,"max":50,"canonical":"engine.fuel_nitro_pct"},{"key":"engine_exhaust","label":{"pt":"Engine exhaust","en":"Engine exhaust"},"type":"text","max_len":80,"canonical":"engine.exhaust"},{"key":"engine_manifold","label":{"pt":"Engine manifold","en":"Engine manifold"},"type":"text","max_len":80,"canonical":"engine.manifold"},{"key":"engine_air_filter","label":{"pt":"Engine air filter","en":"Engine air filter"},"type":"text","max_len":80,"canonical":"engine.air_filter"},{"key":"engine_target_head_temp_c","label":{"pt":"Engine target head temp c","en":"Engine target head temp c"},"type":"number","unit":"°C","min":0,"max":200,"canonical":"engine.target_head_temp_c"}]},{"key":"tyres","label":{"pt":"Pneus","en":"Tyres"},"fields":[{"key":"tyres_glue_pressure_kpa","label":{"pt":"Tyres glue pressure kpa","en":"Tyres glue pressure kpa"},"type":"number","unit":"kPa","min":0,"max":500,"canonical":"tyres.glue_pressure_kpa"},{"key":"tyres_prep_additive","label":{"pt":"Tyres prep additive","en":"Tyres prep additive"},"type":"text","max_len":80,"canonical":"tyres.prep_additive"},{"key":"tyres_prep_time_min","label":{"pt":"Tyres prep time min","en":"Tyres prep time min"},"type":"number","unit":"min","min":0,"max":120,"canonical":"tyres.prep_time_min"}]},{"key":"weight","label":{"pt":"Peso","en":"Weight"},"fields":[{"key":"weight_total_g","label":{"pt":"Weight total g","en":"Weight total g"},"type":"number","unit":"g","min":0,"max":10000,"canonical":"weight.total_g"},{"key":"weight_ballast_g","label":{"pt":"Weight ballast g","en":"Weight ballast g"},"type":"number","unit":"g","min":0,"max":10000,"canonical":"weight.ballast_g"},{"key":"weight_ballast_position","label":{"pt":"Weight ballast position","en":"Weight ballast position"},"type":"text","max_len":80,"canonical":"weight.ballast_position"}]},{"key":"radio","label":{"pt":"Rádio","en":"Radio"},"fields":[{"key":"radio_steering_epa_pct","label":{"pt":"Radio steering epa pct","en":"Radio steering epa pct"},"type":"number","unit":"%","min":0,"max":150,"canonical":"radio.steering_epa_pct"},{"key":"radio_steering_expo_pct","label":{"pt":"Radio steering expo pct","en":"Radio steering expo pct"},"type":"number","unit":"%","min":-100,"max":100,"canonical":"radio.steering_expo_pct"},{"key":"radio_steering_dual_rate_pct","label":{"pt":"Radio steering dual rate pct","en":"Radio steering dual rate pct"},"type":"number","unit":"%","min":0,"max":150,"canonical":"radio.steering_dual_rate_pct"},{"key":"radio_throttle_epa_pct","label":{"pt":"Radio throttle epa pct","en":"Radio throttle epa pct"},"type":"number","unit":"%","min":0,"max":150,"canonical":"radio.throttle_epa_pct"},{"key":"radio_throttle_expo_pct","label":{"pt":"Radio throttle expo pct","en":"Radio throttle expo pct"},"type":"number","unit":"%","min":-100,"max":100,"canonical":"radio.throttle_expo_pct"},{"key":"radio_throttle_dual_rate_pct","label":{"pt":"Radio throttle dual rate pct","en":"Radio throttle dual rate pct"},"type":"number","unit":"%","min":0,"max":150,"canonical":"radio.throttle_dual_rate_pct"},{"key":"radio_brake_pct","label":{"pt":"Radio brake pct","en":"Radio brake pct"},"type":"number","unit":"%","min":0,"max":150,"canonical":"radio.brake_pct"}]}]}$tpl$::jsonb);

INSERT INTO setup.sheet_template (id, team_id, slug, make, model, scale, powertrain, name, source_url, current_version)
VALUES ('01a19e9a-59f5-749a-a5b7-608d6f308fef', NULL, 'mugen-mbx8', 'Mugen', 'MBX8', '1/8', 'nitro',
        'Mugen MBX8 — ficha Ver. 1801E', NULL, 1)
ON CONFLICT (id) DO UPDATE
  SET current_version = EXCLUDED.current_version, name = EXCLUDED.name, source_url = EXCLUDED.source_url
  WHERE setup.sheet_template.current_version < EXCLUDED.current_version;
INSERT INTO setup.sheet_template_version (template_id, version, definition)
VALUES ('01a19e9a-59f5-749a-a5b7-608d6f308fef', 1, $tpl${"format":1,"id":"01a19e9a-59f5-749a-a5b7-608d6f308fef","slug":"mugen-mbx8","version":1,"make":"Mugen","model":"MBX8","sheet_revision":"Ver. 1801E","scale":"1/8","powertrain":"nitro","name":{"pt":"Mugen MBX8 — ficha Ver. 1801E","en":"Mugen MBX8 — sheet Ver. 1801E"},"groups":[{"key":"track_conditions","label":{"pt":"Condições da pista","en":"Track conditions"},"fields":[{"key":"track_size","label":{"pt":"Tamanho","en":"Size"},"type":"multi","options":["Open","Med.","Tight"]},{"key":"track_traction","label":{"pt":"Tração","en":"Traction"},"type":"multi","options":["High","Med.","Low"]},{"key":"track_surface","label":{"pt":"Superfície","en":"Surface"},"type":"multi","options":["Smooth","Med.","Bumpy"]}]},{"key":"front_suspension","label":{"pt":"Suspensão da frente","en":"Front suspension"},"fields":[{"key":"front_rebound_stop","label":{"pt":"Rebound stop (frente)","en":"Front rebound stop"},"type":"number","unit":"mm","min":0,"max":300},{"key":"front_ride_height","label":{"pt":"Altura ao solo (frente)","en":"Front ride height"},"type":"number","unit":"mm","min":0,"max":100,"diagram":{"kind":"length","measure":"ride_height"}},{"key":"front_toe_angle","label":{"pt":"Toe angle (frente)","en":"Front toe angle"},"type":"number","unit":"mm","min":0,"max":100},{"key":"front_kingpin_ball_spacer_upper","label":{"pt":"Kingpin ball spacer, cima","en":"Kingpin ball spacer, upper"},"type":"number","unit":"mm","min":0,"max":10},{"key":"front_kingpin_ball_spacer_lower","label":{"pt":"Kingpin ball spacer, baixo","en":"Kingpin ball spacer, lower"},"type":"number","unit":"mm","min":0,"max":10},{"key":"front_parts_upper_a","label":{"pt":"Peças da frente, cima (coluna da esquerda)","en":"Front parts, upper (left column)"},"type":"multi","options":["E0160","E0159","E2218","E2146","E0239"]},{"key":"front_parts_upper_b","label":{"pt":"Peças da frente, cima (coluna da direita)","en":"Front parts, upper (right column)"},"type":"multi","options":["E2123 (Steel)","E2124 (Steel)","E0220 (CVD)","E2147 (1.5)","E0240 (1mm)","E0241 (2mm)"]},{"key":"front_parts_lower_a","label":{"pt":"Peças da frente, baixo (coluna da esquerda)","en":"Front parts, lower (left column)"},"type":"multi","options":["E0160","E0159"]},{"key":"front_parts_lower_b","label":{"pt":"Peças da frente, baixo (coluna da direita)","en":"Front parts, lower (right column)"},"type":"multi","options":["E2123 (Steel)","E2124 (Steel)"]},{"key":"front_lower_arm_plate","label":{"pt":"Lower arm plate (frente)","en":"Front lower arm plate"},"type":"choice","options":["E2144A,B","E2154 (CFRP 1mm)","E2155 (CFRP 1.2mm)"]},{"key":"front_anti_roll_bar","label":{"pt":"Anti-roll bar (frente)","en":"Front anti-roll bar"},"type":"number","unit":"mm","min":0,"max":10},{"key":"front_upper_arm_spacer","label":{"pt":"Upper arm spacer (frente)","en":"Front upper arm spacer"},"type":"number","unit":"mm","min":0,"max":10},{"key":"front_lower_arm_spacer","label":{"pt":"Lower arm spacer (frente)","en":"Front lower arm spacer"},"type":"number","unit":"mm","min":0,"max":10},{"key":"front_upper_arm_position_a","label":{"pt":"Upper arm position (frente), linha de cima","en":"Front upper arm position, top row"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"front_upper_arm_position_b","label":{"pt":"Upper arm position (frente), linha de baixo","en":"Front upper arm position, bottom row"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"front_lower_arm_position_a","label":{"pt":"Lower arm position, kick up (frente), linha de cima","en":"Front lower arm position (kick up), top row"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"front_lower_arm_position_b","label":{"pt":"Lower arm position, kick up (frente), linha de baixo","en":"Front lower arm position (kick up), bottom row"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}}]},{"key":"front_shocks","label":{"pt":"Amortecedores da frente","en":"Front shocks"},"fields":[{"key":"front_shock_piston","label":{"pt":"Pistão","en":"Piston"},"type":"text","max_len":40},{"key":"front_shock_oil","label":{"pt":"Óleo","en":"Oil"},"type":"text","max_len":40},{"key":"front_shock_spring","label":{"pt":"Mola","en":"Spring"},"type":"text","max_len":40},{"key":"front_shock_position_tower","label":{"pt":"Posição na torre","en":"Shock position (tower)"},"type":"choice","options":["1","2","3","4","5"],"diagram":{"kind":"hole_row","count":5,"orientation":"vertical"},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"front_shock_position_arm","label":{"pt":"Posição no braço","en":"Shock position (arm)"},"type":"choice","options":["1","2"],"diagram":{"kind":"hole_row","count":2},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"front_shock_pivot_ball","label":{"pt":"Pivot ball position","en":"Pivot ball position"},"type":"choice","options":["Front","Rear"]},{"key":"front_damper_stay","label":{"pt":"Damper stay","en":"Damper stay"},"type":"choice","options":["E2562 (Aluminum)","E2565 (CFRP)"]}]},{"key":"steering","label":{"pt":"Direção e frente do chassis","en":"Steering and front plate"},"fields":[{"key":"steering_ackermann","label":{"pt":"Steering ackermann","en":"Steering ackermann"},"type":"choice","options":["1","2","3","4","5"],"diagram":{"kind":"hole_row","count":5,"orientation":"vertical"},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"front_upper_plate","label":{"pt":"Front upper plate","en":"Front upper plate"},"type":"choice","options":["E2148D","E2158 (CFRP)"]},{"key":"servo_saver_arm","label":{"pt":"Servo saver arm","en":"Servo saver arm"},"type":"choice","options":["E2301A","E2310 (Aluminum)"]}]},{"key":"rear_suspension","label":{"pt":"Suspensão de trás","en":"Rear suspension"},"fields":[{"key":"rear_rebound_stop","label":{"pt":"Rebound stop (trás)","en":"Rear rebound stop"},"type":"number","unit":"mm","min":0,"max":300},{"key":"rear_ride_height","label":{"pt":"Altura ao solo (trás)","en":"Rear ride height"},"type":"number","unit":"mm","min":0,"max":100,"diagram":{"kind":"length","measure":"ride_height"}},{"key":"rear_camber_angle","label":{"pt":"Camber angle (trás)","en":"Rear camber angle"},"type":"number","unit":"mm","min":0,"max":100},{"key":"rear_hub_part","label":{"pt":"Peça do hub (trás)","en":"Rear hub part"},"type":"choice","options":["E2131","E2134 (Aluminum)"]},{"key":"rear_parts_a","label":{"pt":"Peças de trás (coluna da esquerda)","en":"Rear parts (left column)"},"type":"multi","options":["E0239","E2219"]},{"key":"rear_parts_b","label":{"pt":"Peças de trás (coluna da direita)","en":"Rear parts (right column)"},"type":"multi","options":["E0240 (1mm)","E0241 (2mm)","E0220 (CVD)"]},{"key":"rear_lower_arm_plate","label":{"pt":"Lower arm plate (trás)","en":"Rear lower arm plate"},"type":"choice","options":["E2145A,B","E2156 (CFRP 1mm)","E2157 (CFRP 1.2mm)"]},{"key":"rear_anti_roll_bar","label":{"pt":"Anti-roll bar (trás)","en":"Rear anti-roll bar"},"type":"number","unit":"mm","min":0,"max":10},{"key":"rear_lower_arm_spacer","label":{"pt":"Lower arm spacer (trás)","en":"Rear lower arm spacer"},"type":"number","unit":"mm","min":0,"max":10},{"key":"rear_upright_spacer","label":{"pt":"Upright spacer (trás)","en":"Rear upright spacer"},"type":"number","unit":"mm","min":0,"max":10},{"key":"rear_hub_position_row","label":{"pt":"Posição do hub (trás), fila horizontal","en":"Rear hub position, horizontal row"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"rear_plate_position","label":{"pt":"Posição da placa (trás), fila vertical","en":"Rear plate position, vertical row"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3,"orientation":"vertical"},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"rear_upper_arm_position_a","label":{"pt":"Upper arm position (trás), coluna da esquerda","en":"Rear upper arm position, left column"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3,"orientation":"vertical"},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"rear_upper_arm_position_b","label":{"pt":"Upper arm position (trás), coluna da direita","en":"Rear upper arm position, right column"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3,"orientation":"vertical"},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"rear_lower_arm_position_anti_squat","label":{"pt":"Lower arm position, anti-squat (trás)","en":"Rear lower arm position (anti-squat)"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"rear_lower_arm_position_toe_a","label":{"pt":"Lower arm position, toe angle (trás), linha de cima","en":"Rear lower arm position (toe angle), top row"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"rear_lower_arm_position_toe_b","label":{"pt":"Lower arm position, toe angle (trás), linha de baixo","en":"Rear lower arm position (toe angle), bottom row"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}}]},{"key":"rear_shocks","label":{"pt":"Amortecedores de trás","en":"Rear shocks"},"fields":[{"key":"rear_shock_piston","label":{"pt":"Pistão","en":"Piston"},"type":"text","max_len":40},{"key":"rear_shock_oil","label":{"pt":"Óleo","en":"Oil"},"type":"text","max_len":40},{"key":"rear_shock_spring","label":{"pt":"Mola","en":"Spring"},"type":"text","max_len":40},{"key":"rear_shock_position_tower","label":{"pt":"Posição na torre","en":"Shock position (tower)"},"type":"choice","options":["1","2","3","4","5"],"diagram":{"kind":"hole_row","count":5,"orientation":"vertical"},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"rear_shock_position_arm","label":{"pt":"Posição no braço","en":"Shock position (arm)"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"rear_shock_pivot_ball","label":{"pt":"Pivot ball position","en":"Pivot ball position"},"type":"choice","options":["Front","Rear"]},{"key":"rear_damper_stay","label":{"pt":"Damper stay","en":"Damper stay"},"type":"choice","options":["E2563 (Aluminum)","E2566 (CFRP)"]}]},{"key":"center_shaft_rods","label":{"pt":"Eixo central e tensores","en":"Center shaft and tension rods"},"fields":[{"key":"center_shaft","label":{"pt":"Center shaft","en":"Center shaft"},"type":"choice","options":["E0223, E0259","E2228, E2229"]},{"key":"tension_rod_front","label":{"pt":"Tension rod (frente)","en":"Tension rod, front"},"type":"choice","options":["E2107A (Long)","E2107B (Short)"]},{"key":"tension_rod_rear","label":{"pt":"Tension rod (trás)","en":"Tension rod, rear"},"type":"choice","options":["E2148C","E0803 (Rubber)"]}]},{"key":"weight","label":{"pt":"Peso","en":"Weight"},"fields":[{"key":"weight_a_g","label":{"pt":"Peso, posição 1","en":"Weight, position 1"},"type":"number","unit":"g","min":0,"max":500},{"key":"weight_b_g","label":{"pt":"Peso, posição 2","en":"Weight, position 2"},"type":"number","unit":"g","min":0,"max":500}]},{"key":"engine","label":{"pt":"Motor","en":"Engine"},"fields":[{"key":"engine_type","label":{"pt":"Tipo","en":"Type"},"type":"text","max_len":60},{"key":"engine_plug","label":{"pt":"Vela","en":"Plug"},"type":"text","max_len":60},{"key":"engine_reducer","label":{"pt":"Reducer","en":"Reducer"},"type":"number","min":0,"max":50},{"key":"engine_muffler","label":{"pt":"Escape","en":"Muffler"},"type":"text","max_len":60},{"key":"engine_fuel","label":{"pt":"Combustível","en":"Fuel"},"type":"text","max_len":60},{"key":"engine_gasket","label":{"pt":"Junta","en":"Gasket"},"type":"text","max_len":60},{"key":"engine_mount","label":{"pt":"Engine mount","en":"Engine mount"},"type":"choice","options":["E0701 (Standard)","Other"]},{"key":"engine_mount_other","label":{"pt":"Engine mount, outro","en":"Engine mount, other"},"type":"text","max_len":60}]},{"key":"diff_gear","label":{"pt":"Diferenciais e relações","en":"Diff. gear"},"table":{"columns":[{"pt":"Relação","en":"Gear"},{"pt":"Óleo","en":"Oil"}],"rows":[{"label":{"pt":"Frente","en":"Front"},"cells":["diff_front_gear","diff_front_oil"]},{"label":{"pt":"Trás","en":"Rear"},"cells":["diff_rear_gear","diff_rear_oil"]},{"label":{"pt":"Centro","en":"Center"},"cells":["diff_center_gear","diff_center_oil"]}]},"fields":[{"key":"diff_front_gear","label":{"pt":"Relação da frente","en":"Front gear"},"type":"choice","options":["44/13T","42/12T"]},{"key":"diff_front_oil","label":{"pt":"Óleo da frente","en":"Front oil"},"type":"int","unit":"cSt","min":100,"max":1000000,"canonical":"diffs.front_cst"},{"key":"diff_rear_gear","label":{"pt":"Relação de trás","en":"Rear gear"},"type":"choice","options":["44/13T","42/12T"]},{"key":"diff_rear_oil","label":{"pt":"Óleo de trás","en":"Rear oil"},"type":"int","unit":"cSt","min":100,"max":1000000,"canonical":"diffs.rear_cst"},{"key":"diff_center_gear","label":{"pt":"Relação do centro","en":"Center gear"},"type":"choice","options":["44T","46T","47T","48T"]},{"key":"diff_center_oil","label":{"pt":"Óleo do centro","en":"Center oil"},"type":"int","unit":"cSt","min":100,"max":1000000,"canonical":"diffs.center_cst"}]},{"key":"clutch","label":{"pt":"Embraiagem","en":"Clutch"},"fields":[{"key":"clutch_type","label":{"pt":"Tipo","en":"Type"},"type":"choice","options":["3P","4P"]},{"key":"clutch_bell","label":{"pt":"Clutch bell","en":"Clutch bell"},"type":"choice","options":["13T","14T"]},{"key":"clutch_shoes","label":{"pt":"Clutch shoes","en":"Clutch shoes"},"type":"multi","options":["C0754","E0704","E0710","E2706"]},{"key":"clutch_spring","label":{"pt":"Clutch spring","en":"Clutch spring"},"type":"choice","options":["0.8","0.9","0.95","1.0","1.05","1.1"]}]},{"key":"tires","label":{"pt":"Pneus","en":"Tires"},"table":{"columns":[{"pt":"Frente","en":"Front"},{"pt":"Trás","en":"Rear"}],"rows":[{"label":{"pt":"Tipo","en":"Type"},"cells":["tire_front_type","tire_rear_type"]},{"label":{"pt":"Composto","en":"Compound"},"cells":["tire_front_compound","tire_rear_compound"]},{"label":{"pt":"Inserts","en":"Inserts"},"cells":["tire_front_inserts","tire_rear_inserts"]},{"label":{"pt":"Jante","en":"Wheel"},"cells":["tire_front_wheel","tire_rear_wheel"]}]},"fields":[{"key":"tire_front_type","label":{"pt":"Tipo (frente)","en":"Front type"},"type":"text","max_len":60},{"key":"tire_rear_type","label":{"pt":"Tipo (trás)","en":"Rear type"},"type":"text","max_len":60},{"key":"tire_front_compound","label":{"pt":"Composto (frente)","en":"Front compound"},"type":"text","max_len":60},{"key":"tire_rear_compound","label":{"pt":"Composto (trás)","en":"Rear compound"},"type":"text","max_len":60},{"key":"tire_front_inserts","label":{"pt":"Inserts (frente)","en":"Front inserts"},"type":"text","max_len":60},{"key":"tire_rear_inserts","label":{"pt":"Inserts (trás)","en":"Rear inserts"},"type":"text","max_len":60},{"key":"tire_front_wheel","label":{"pt":"Jante (frente)","en":"Front wheel"},"type":"text","max_len":60},{"key":"tire_rear_wheel","label":{"pt":"Jante (trás)","en":"Rear wheel"},"type":"text","max_len":60}]},{"key":"body_wing","label":{"pt":"Carroçaria e asa","en":"Body/Wing"},"fields":[{"key":"body","label":{"pt":"Carroçaria","en":"Body"},"type":"text","max_len":60},{"key":"wing","label":{"pt":"Asa","en":"Wing"},"type":"text","max_len":60},{"key":"wing_position","label":{"pt":"Posição da asa (frente-trás)","en":"Wing position (front-rear)"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}},{"key":"wing_mount_hole","label":{"pt":"Furo de fixação da asa","en":"Wing mount hole"},"type":"choice","options":["1","2","3"],"diagram":{"kind":"hole_row","count":3,"orientation":"vertical"},"help":{"pt":"Posições numeradas como na folha: da esquerda para a direita, ou de cima para baixo.","en":"Positions numbered as on the sheet: left to right, or top to bottom."}}]},{"key":"comments","label":{"pt":"Comentários","en":"Comments"},"fields":[{"key":"comments","label":{"pt":"Comentários","en":"Comments"},"type":"text","max_len":500}]}]}$tpl$::jsonb);
-- END catálogo de modelos

-- =============================================================================
-- TimescaleDB: hypertable e retenção da telemetria
-- =============================================================================
-- Sem compressão (ADR 0011): o TimescaleDB não suporta RLS em chunks comprimidos e
-- telemetry.sample tem de ter RLS. A extensão faz parte do bootstrap e é obrigatória.
SELECT create_hypertable('telemetry.sample', 'ts', chunk_time_interval => interval '1 day');
SELECT add_retention_policy('telemetry.sample', interval '24 months');
