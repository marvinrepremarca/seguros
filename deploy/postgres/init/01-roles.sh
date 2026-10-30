#!/usr/bin/env bash
# Init de PostgreSQL (solo desarrollo; se ejecuta una vez, con el volumen vacío).
# - seguros_migrator: dueño de la BD y de los esquemas; lo usa cmd/migrate.
# - seguros_app: rol de la aplicación, sin BYPASSRLS ni DDL; sus GRANT los dan las migraciones por esquema.
# - keycloak: BD propia para Keycloak.
set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v migrator_pw="$SEGUROS_MIGRATOR_PASSWORD" \
  -v app_pw="$SEGUROS_APP_PASSWORD" \
  -v keycloak_pw="$KEYCLOAK_DB_PASSWORD" <<'SQL'
CREATE ROLE seguros_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'migrator_pw';
CREATE ROLE seguros_app      LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS NOINHERIT PASSWORD :'app_pw';
CREATE ROLE keycloak         LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD :'keycloak_pw';

CREATE DATABASE seguros  OWNER seguros_migrator;
CREATE DATABASE keycloak OWNER keycloak;

REVOKE ALL ON DATABASE seguros  FROM PUBLIC;
REVOKE ALL ON DATABASE keycloak FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE seguros TO seguros_app;
SQL

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname seguros <<'SQL'
-- Nadie crea objetos en public; cada módulo tiene su esquema creado por el migrator.
REVOKE ALL ON SCHEMA public FROM PUBLIC;
ALTER SCHEMA public OWNER TO seguros_migrator;
SQL
