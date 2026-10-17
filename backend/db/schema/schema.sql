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
  CHECK (expires_at <= created_at + interval '7 days')
);
CREATE INDEX invitation_team_idx ON identity.invitation (team_id) WHERE accepted_at IS NULL AND revoked_at IS NULL;

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

-- Códigos de recuperação de MFA: mostrados uma vez ao utilizador, guardados só como hash.
CREATE TABLE identity.recovery_code (
  id          uuid        PRIMARY KEY DEFAULT app.uuid_v7(),
  user_id     uuid        NOT NULL REFERENCES identity.user_account (id) ON DELETE CASCADE,
  code_hash   bytea       NOT NULL CHECK (length(code_hash) = 32),
  used_at     timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, code_hash)
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
  UNIQUE (team_id, id)
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
  UNIQUE (team_id, id),
  CHECK (jsonb_typeof(specs) = 'object')
);
CREATE INDEX part_team_kind_idx ON garage.part (team_id, kind) WHERE deleted_at IS NULL;

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
  FOREIGN KEY (team_id, part_id) REFERENCES garage.part (team_id, id) ON DELETE CASCADE
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
  FOREIGN KEY (team_id, part_id)   REFERENCES garage.part (team_id, id)   ON DELETE CASCADE,
  FOREIGN KEY (team_id, car_id)    REFERENCES garage.car (team_id, id)    ON DELETE CASCADE,
  FOREIGN KEY (team_id, engine_id) REFERENCES garage.engine (team_id, id) ON DELETE CASCADE,
  CHECK (num_nonnulls(car_id, engine_id) = 1),
  CHECK (removed_at IS NULL OR removed_at > installed_at)
);
CREATE INDEX part_install_active_idx ON garage.part_install (team_id, part_id) WHERE removed_at IS NULL;

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
  FOREIGN KEY (team_id, setup_id) REFERENCES setup.setup (team_id, id) ON DELETE CASCADE
);

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
  FOREIGN KEY (team_id, device_key_id) REFERENCES identity.device_key (team_id, id)
);
CREATE INDEX operation_team_received_idx ON sync.operation (team_id, received_at DESC);

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
  changed_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX change_log_team_seq_idx ON sync.change_log (team_id, seq);

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
  response            bytea,                                  -- resposta serializada (Protobuf)
  status_code         text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  expires_at          timestamptz NOT NULL DEFAULT now() + interval '7 days',
  PRIMARY KEY (user_id, request_id)                       -- o request_id só é único por utilizador
);
CREATE INDEX idempotency_key_expiry_idx ON platform.idempotency_key (expires_at);

-- Outbox transacional: eventos de domínio publicados pelo worker (River).
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
  attempts            smallint    NOT NULL DEFAULT 0
);
CREATE INDEX outbox_unpublished_idx ON platform.outbox (id) WHERE published_at IS NULL;

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
    ['garage','part_install','id'],
    ['setup','field_definition','id'], ['setup','setup','id'], ['setup','setup_version','setup_id'],
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
      AND (c.table_schema, c.table_name) NOT IN (('identity','membership'), ('events','track'), ('audit','audit_log'), ('platform','outbox'))
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
REVOKE UPDATE, DELETE ON garage.stock_movement      FROM pitbox_app;
REVOKE INSERT, UPDATE, DELETE ON telemetry.firmware_release FROM pitbox_app;
REVOKE SELECT ON platform.idempotency_key           FROM pitbox_readonly;
REVOKE SELECT ON platform.push_subscription         FROM pitbox_readonly;   -- endpoints e chaves de push
REVOKE SELECT ON platform.outbox                    FROM pitbox_readonly;   -- eventos de todas as equipas, sem RLS

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
CREATE OR REPLACE FUNCTION identity.replace_recovery_codes(p_user_id uuid, p_code_hashes bytea[])
  RETURNS void
  LANGUAGE plpgsql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
  invalid int;
BEGIN
  SELECT count(*) INTO invalid
  FROM unnest(p_code_hashes) AS h
  WHERE length(h) != 32;
  IF invalid > 0 THEN
    RAISE EXCEPTION 'replace_recovery_codes: invalid hash length' USING ERRCODE = '22023';
  END IF;

  DELETE FROM identity.recovery_code
  WHERE user_id = p_user_id AND used_at IS NULL;

  INSERT INTO identity.recovery_code (user_id, code_hash)
  SELECT p_user_id, h
  FROM unnest(p_code_hashes) AS h;
END
$$;

REVOKE ALL ON FUNCTION identity.replace_recovery_codes(uuid, bytea[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.replace_recovery_codes(uuid, bytea[]) TO pitbox_app;
COMMENT ON FUNCTION identity.replace_recovery_codes(uuid, bytea[]) IS
  'Substitui os códigos de recuperação não usados de um utilizador. SECURITY DEFINER, só pitbox_app.';

-- identity.consume_recovery_code — consome um código de recuperação se existir e não estiver usado.
CREATE OR REPLACE FUNCTION identity.consume_recovery_code(p_user_id uuid, p_code_hash bytea)
  RETURNS boolean
  LANGUAGE sql
  SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
  WITH upd AS (
    UPDATE identity.recovery_code
    SET used_at = now()
    WHERE user_id = p_user_id
      AND code_hash = p_code_hash
      AND used_at IS NULL
    RETURNING id
  )
  SELECT count(*) > 0 FROM upd;
$$;

REVOKE ALL ON FUNCTION identity.consume_recovery_code(uuid, bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION identity.consume_recovery_code(uuid, bytea) TO pitbox_app;
COMMENT ON FUNCTION identity.consume_recovery_code(uuid, bytea) IS
  'Consome um código de recuperação de MFA. SECURITY DEFINER, só pitbox_app.';

-- =============================================================================
-- TimescaleDB: hypertable e retenção da telemetria
-- =============================================================================
-- Sem compressão (ADR 0011): o TimescaleDB não suporta RLS em chunks comprimidos e
-- telemetry.sample tem de ter RLS. A extensão faz parte do bootstrap e é obrigatória.
SELECT create_hypertable('telemetry.sample', 'ts', chunk_time_interval => interval '1 day');
SELECT add_retention_policy('telemetry.sample', interval '24 months');
