#!/bin/sh
# Corre uma só vez, no primeiro arranque do volume (docker-entrypoint-initdb.d).
# O superutilizador "postgres" só serve para isto; nada o usa depois.
# Mudar uma palavra-passe no .env depois disto NÃO tem efeito: usar `make reset`.
#
# Papéis (coerentes com backend/db/schema/schema.sql, que só cria os de grupo se não existirem):
#   grupo, NOLOGIN : pitbox_owner (dono dos objetos), pitbox_app, pitbox_readonly (NOBYPASSRLS)
#   login          : pitbox_migrator (IN ROLE pitbox_owner) para as migrações (Atlas)
#                    pitbox_api      (IN ROLE pitbox_app)   para API/worker/ingest
#                    openfga, zitadel (um por serviço)
# Bases: pitbox (dados + esquema fga do OpenFGA) e zitadel. O schema.sql entra no F0-06, não aqui.
set -eu

psql -v ON_ERROR_STOP=1 --username postgres --dbname postgres \
  -v migrator_pw="$PITBOX_MIGRATOR_PASSWORD" \
  -v api_pw="$PITBOX_API_PASSWORD" \
  -v zitadel_pw="$ZITADEL_DB_PASSWORD" \
  -v openfga_pw="$OPENFGA_DB_PASSWORD" <<'SQL'
CREATE ROLE pitbox_owner    NOLOGIN NOSUPERUSER NOBYPASSRLS;
CREATE ROLE pitbox_app      NOLOGIN NOSUPERUSER NOBYPASSRLS;
CREATE ROLE pitbox_readonly NOLOGIN NOSUPERUSER NOBYPASSRLS;

CREATE ROLE pitbox_migrator LOGIN NOSUPERUSER NOBYPASSRLS IN ROLE pitbox_owner PASSWORD :'migrator_pw';
CREATE ROLE pitbox_api      LOGIN NOSUPERUSER NOBYPASSRLS IN ROLE pitbox_app   PASSWORD :'api_pw';
CREATE ROLE openfga         LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD :'openfga_pw';
CREATE ROLE zitadel         LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD :'zitadel_pw';

CREATE DATABASE pitbox  OWNER pitbox_owner;
CREATE DATABASE zitadel OWNER zitadel;

REVOKE ALL ON DATABASE pitbox  FROM PUBLIC;
REVOKE ALL ON DATABASE zitadel FROM PUBLIC;
GRANT CONNECT ON DATABASE pitbox TO pitbox_app, pitbox_readonly, openfga;
SQL

# TimescaleDB exige superutilizador para CREATE EXTENSION.
psql -v ON_ERROR_STOP=1 --username postgres --dbname pitbox <<'SQL'
CREATE EXTENSION IF NOT EXISTS timescaledb;

REVOKE CREATE ON SCHEMA public FROM PUBLIC;

-- OpenFGA: esquema próprio, dono openfga; sem permissões em mais nenhum esquema.
CREATE SCHEMA fga AUTHORIZATION openfga;
ALTER ROLE openfga IN DATABASE pitbox SET search_path = fga;
SQL
