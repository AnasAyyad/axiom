#!/bin/sh
set -eu

# Runs only when the official PostgreSQL image initializes an empty data volume.
# Subsequent role/schema changes belong in version-controlled migrations.

psql \
  --set=ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=migrator_user="$POSTGRES_MIGRATOR_USER" \
  --set=runtime_user="$POSTGRES_RUNTIME_USER" \
  --set=recorder_user="$POSTGRES_RECORDER_USER" \
  --set=backup_user="$POSTGRES_BACKUP_USER" \
  --set=readonly_user="$POSTGRES_READONLY_USER" \
  --set=binance_engine_user="$POSTGRES_BINANCE_ENGINE_USER" \
  --set=bybit_engine_user="$POSTGRES_BYBIT_ENGINE_USER" \
  --set=sandbox_qualification_user="$POSTGRES_SANDBOX_QUALIFICATION_USER" <<'SQL'
-- Read role passwords inside PostgreSQL so secret values never appear in the
-- process argument vector or ordinary environment variables. The official
-- image runs this script only while the initial superuser initializes an empty
-- cluster; the mounted files remain the sole provisioning source.
SELECT
  rtrim(pg_read_file('/run/secrets/postgres_migrator_password'), E'\r\n') AS migrator_password,
  rtrim(pg_read_file('/run/secrets/postgres_runtime_password'), E'\r\n') AS runtime_password,
  rtrim(pg_read_file('/run/secrets/postgres_recorder_password'), E'\r\n') AS recorder_password,
  rtrim(pg_read_file('/run/secrets/postgres_backup_password'), E'\r\n') AS backup_password,
  rtrim(pg_read_file('/run/secrets/postgres_readonly_password'), E'\r\n') AS readonly_password,
  rtrim(pg_read_file('/run/secrets/postgres_binance_engine_password'), E'\r\n') AS binance_engine_password,
  rtrim(pg_read_file('/run/secrets/postgres_bybit_engine_password'), E'\r\n') AS bybit_engine_password,
  rtrim(pg_read_file('/run/secrets/postgres_sandbox_qualification_password'), E'\r\n') AS sandbox_qualification_password
\gset

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'migrator_user', :'migrator_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'migrator_user')
\gexec

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'runtime_user', :'runtime_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'runtime_user')
\gexec

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'recorder_user', :'recorder_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'recorder_user')
\gexec

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'backup_user', :'backup_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'backup_user')
\gexec

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'readonly_user', :'readonly_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'readonly_user')
\gexec

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'binance_engine_user', :'binance_engine_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'binance_engine_user')
\gexec
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'bybit_engine_user', :'bybit_engine_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'bybit_engine_user')
\gexec
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'sandbox_qualification_user', :'sandbox_qualification_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'sandbox_qualification_user')
\gexec

SELECT format('ALTER ROLE %I PASSWORD %L', :'migrator_user', :'migrator_password')
\gexec
SELECT format('ALTER ROLE %I PASSWORD %L', :'runtime_user', :'runtime_password')
\gexec
SELECT format('ALTER ROLE %I PASSWORD %L', :'recorder_user', :'recorder_password')
\gexec
SELECT format('ALTER ROLE %I PASSWORD %L', :'backup_user', :'backup_password')
\gexec
SELECT format('ALTER ROLE %I PASSWORD %L', :'readonly_user', :'readonly_password')
\gexec
SELECT format('ALTER ROLE %I PASSWORD %L', :'binance_engine_user', :'binance_engine_password')
\gexec
SELECT format('ALTER ROLE %I PASSWORD %L', :'bybit_engine_user', :'bybit_engine_password')
\gexec
SELECT format('ALTER ROLE %I PASSWORD %L', :'sandbox_qualification_user', :'sandbox_qualification_password')
\gexec

REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT CONNECT ON DATABASE :"DBNAME" TO :"migrator_user", :"runtime_user", :"recorder_user", :"backup_user", :"readonly_user", :"binance_engine_user", :"bybit_engine_user", :"sandbox_qualification_user";
GRANT USAGE, CREATE ON SCHEMA public TO :"migrator_user";
GRANT USAGE ON SCHEMA public TO :"runtime_user", :"recorder_user", :"backup_user", :"readonly_user", :"binance_engine_user", :"bybit_engine_user", :"sandbox_qualification_user";

ALTER DEFAULT PRIVILEGES FOR ROLE :"migrator_user" IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO :"runtime_user";
ALTER DEFAULT PRIVILEGES FOR ROLE :"migrator_user" IN SCHEMA public
  GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO :"runtime_user";
ALTER DEFAULT PRIVILEGES FOR ROLE :"migrator_user" IN SCHEMA public
  GRANT SELECT ON TABLES TO :"readonly_user";
ALTER DEFAULT PRIVILEGES FOR ROLE :"migrator_user" IN SCHEMA public
  GRANT SELECT ON TABLES TO :"backup_user";
ALTER DEFAULT PRIVILEGES FOR ROLE :"migrator_user" IN SCHEMA public
  GRANT SELECT ON SEQUENCES TO :"backup_user";
ALTER DEFAULT PRIVILEGES FOR ROLE :"migrator_user" IN SCHEMA public
  GRANT USAGE ON TYPES TO :"runtime_user", :"recorder_user", :"backup_user", :"readonly_user";
ALTER DEFAULT PRIVILEGES FOR ROLE :"migrator_user" IN SCHEMA public
  GRANT USAGE ON TYPES TO :"binance_engine_user", :"bybit_engine_user";
ALTER DEFAULT PRIVILEGES FOR ROLE :"migrator_user" IN SCHEMA public
  GRANT USAGE ON TYPES TO :"sandbox_qualification_user";
SQL
