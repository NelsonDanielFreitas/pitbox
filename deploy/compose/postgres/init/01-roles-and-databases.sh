#!/bin/sh
# Corre uma só vez, no primeiro arranque do volume (docker-entrypoint-initdb.d).
# O superutilizador "postgres" só serve para isto; nada o usa depois.
# Mudar uma palavra-passe no .env depois disto NÃO tem efeito: usar `make reset`.
#
# Papéis (coerentes com backend/db/schema/schema.sql, que só cria os de grupo se não existirem):
#   grupo, NOLOGIN : pitbox_app, pitbox_readonly (NOBYPASSRLS)
#   dono           : pitbox_owner (dono dos objetos). LOGIN mas SEM palavra-passe: o TimescaleDB só
#                    corre as políticas (retenção) de uma hypertable se o dono puder fazer login
#                    ("permission denied to start background process"). Sem palavra-passe, nenhuma
#                    ligação por TCP o autentica; só o servidor o usa, em processos internos (ADR 0011).
#   login          : pitbox_migrator (IN ROLE pitbox_owner) para as migrações (Atlas)
#                    pitbox_api      (IN ROLE pitbox_app)   para API/worker/ingest
#                    openfga, zitadel (um por serviço)
# Bases: pitbox (dados + esquema fga do OpenFGA) e zitadel.
# Este script é o BOOTSTRAP (corre como superutilizador): papéis e extensões. As migrações
# (backend/db/migrations) correm depois como pitbox_migrator, que assume pitbox_owner em
# cada sessão (ALTER ROLE ... SET role), por isso os objetos ficam sempre com dono pitbox_owner.
# O pitbox_migrator não é superutilizador nem tem CREATEROLE (ADR 0010).
set -eu

psql -v ON_ERROR_STOP=1 --username postgres --dbname postgres \
  -v migrator_pw="$PITBOX_MIGRATOR_PASSWORD" \
  -v api_pw="$PITBOX_API_PASSWORD" \
  -v zitadel_pw="$ZITADEL_DB_PASSWORD" \
  -v openfga_pw="$OPENFGA_DB_PASSWORD" <<'SQL'
CREATE ROLE pitbox_owner    LOGIN NOSUPERUSER NOBYPASSRLS;   -- sem PASSWORD, de propósito
CREATE ROLE pitbox_app      NOLOGIN NOSUPERUSER NOBYPASSRLS;
CREATE ROLE pitbox_readonly NOLOGIN NOSUPERUSER NOBYPASSRLS;

CREATE ROLE pitbox_migrator LOGIN NOSUPERUSER NOBYPASSRLS IN ROLE pitbox_owner PASSWORD :'migrator_pw';
CREATE ROLE pitbox_api      LOGIN NOSUPERUSER NOBYPASSRLS IN ROLE pitbox_app   PASSWORD :'api_pw';
CREATE ROLE openfga         LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD :'openfga_pw';
CREATE ROLE zitadel         LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD :'zitadel_pw';

-- Sem isto, os objetos ficariam com dono pitbox_migrator (pertencer a um papel herda
-- privilégios, mas não muda o current_user).
ALTER ROLE pitbox_migrator SET role = 'pitbox_owner';

CREATE DATABASE pitbox  OWNER pitbox_owner;
CREATE DATABASE zitadel OWNER zitadel;

REVOKE ALL ON DATABASE pitbox  FROM PUBLIC;
REVOKE ALL ON DATABASE zitadel FROM PUBLIC;
GRANT CONNECT ON DATABASE pitbox TO pitbox_app, pitbox_readonly, openfga;
SQL

# TimescaleDB exige superutilizador para CREATE EXTENSION; citext e btree_gist ficam aqui por coerência.
psql -v ON_ERROR_STOP=1 --username postgres --dbname pitbox <<'SQL'
CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS btree_gist;

REVOKE CREATE ON SCHEMA public FROM PUBLIC;

-- OpenFGA: esquema próprio, dono openfga; sem permissões em mais nenhum esquema.
CREATE SCHEMA fga AUTHORIZATION openfga;
ALTER ROLE openfga IN DATABASE pitbox SET search_path = fga;
SQL
