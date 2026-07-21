# Axiom single-server Docker Compose deployment

This deployment is deliberately image-based. The repository contains the A1
platform/health source and pinned production Dockerfile; Compose still consumes
an explicit image reference and never pretends an unpublished image exists.

The base Compose project starts PostgreSQL only. V1A application, recorder,
worker, observability, and edge services are profile-gated. The encrypted
backup service arrives in A4; authenticated exchange services are absent.

## 1. Prepare configuration

```bash
cp .env.example .env
mkdir -p .secrets .local/market-data
chmod 700 .secrets
# On Linux, ensure bind-mounted writable paths match APP_UID/APP_GID.
sudo chown -R 10001:10001 .local/market-data
```

Review every `CHANGE_ME` value in `.env`. Keep public ports bound to `127.0.0.1` unless Caddy or another authenticated TLS proxy is active.

Asset eligibility, instruments, datasets, portfolio allocation, valuation, and
reporting policy are not deployment-environment overrides. They belong to the
immutable versioned research configuration selected by `APP_CONFIG_FILE`; a
deployment cannot replace or augment those values through `.env`.

The A2 image includes the reviewed `deploy/config/platform-shadow.json` at
`/etc/axiom/platform.json`. The strict loader validates that complete graph
before opening the database or a listener. A deployment-specific replacement
must be mounted explicitly at an absolute path and selected with
`APP_CONFIG_FILE`; partial environment overlays are rejected.

The B1 recorder may instead select
`deploy/config/platform-shadow-v1b.json`. That immutable graph composes the
compiled Binance and Bybit production-public endpoint sets, three approved
spot instruments per venue, and 15m/1h/4h candles. It contains no secret
references and does not enable authenticated exchange behavior. Other runtime
roles retain the V1A compatibility projection until their sequential V1B
phase is implemented.

If you created `.env` or initialized PostgreSQL before the Axiom naming update, leave those existing database and role names alone for now. Branding does not require deleting or recreating a local database. Fresh setups copied from the current `.env.example` use the `axiom` names; an existing database can be renamed later only through a planned migration/backup procedure.

## 2. Create secret files

Use independent random values. Never reuse a database, session, CSRF, Grafana, alert, or backup secret.

Required for PostgreSQL:

- `.secrets/postgres_owner_password`
- `.secrets/postgres_migrator_password`
- `.secrets/postgres_runtime_password`
- `.secrets/postgres_recorder_password`
- `.secrets/postgres_backup_password`
- `.secrets/backup_encryption_key`
- `.secrets/postgres_readonly_password`

The A11 `api` service exposes redacted public liveness/readiness/build data and
uses the independent health-detail token for authenticated component status.
Only that service receives the bootstrap, CSRF, and session-signing files;
the shadow engine, recorder, workers, and observability services do not.

Required for A11 API startup:

- `.secrets/bootstrap_owner_email`
- `.secrets/bootstrap_owner_password_hash`
- `.secrets/csrf_key`
- `.secrets/session_signing_key`

The bootstrap password file must contain a precomputed Argon2id PHC hash using
64 MiB, three iterations, parallelism one, a 16-byte random salt, and 32-byte
output. Axiom never accepts or stores a bootstrap plaintext password. Generate
it locally without putting the password in shell arguments:

```bash
umask 077
read -r -s -p 'Bootstrap password: ' AXIOM_BOOTSTRAP_PASSWORD; printf '\n'
printf '%s\n' "$AXIOM_BOOTSTRAP_PASSWORD" | \
  go run ./scripts/generate_bootstrap_hash.go > .secrets/bootstrap_owner_password_hash
unset AXIOM_BOOTSTRAP_PASSWORD
printf '%s\n' 'owner@example.invalid' > .secrets/bootstrap_owner_email
openssl rand -base64 48 > .secrets/csrf_key
openssl rand -base64 48 > .secrets/session_signing_key
```

On an empty database, missing, empty, placeholder, or obsolete bootstrap inputs
keep readiness false. The first owner, role grants, and audit event are created
in one transaction. Once a user exists, bootstrap files are ignored; removing
them does not delete or recreate identity. CSRF and session-signing inputs stay
required. Login cookies are host-only, `HttpOnly` for the session,
`SameSite=Strict`, and `Secure` outside the local deployment environment.

Required when observability is enabled:

- `.secrets/grafana_admin_password`

Required for every A5 application role:

- `.secrets/health_detail_token`

Example for random secrets:

```bash
umask 077
openssl rand -base64 48 > .secrets/postgres_owner_password
openssl rand -base64 48 > .secrets/postgres_migrator_password
openssl rand -base64 48 > .secrets/postgres_runtime_password
openssl rand -base64 48 > .secrets/postgres_recorder_password
openssl rand -base64 48 > .secrets/postgres_backup_password
openssl rand -base64 32 > .secrets/backup_encryption_key
openssl rand -base64 48 > .secrets/postgres_readonly_password
openssl rand -base64 48 > .secrets/grafana_admin_password
openssl rand -base64 48 > .secrets/health_detail_token
sudo chgrp 70 .secrets/postgres_*_password
chmod 640 .secrets/postgres_*_password
sudo chgrp 0 .secrets/grafana_admin_password
chmod 640 .secrets/grafana_admin_password
sudo chgrp 70 .secrets/health_detail_token
chmod 640 .secrets/health_detail_token
sudo chgrp 70 .secrets/bootstrap_owner_email .secrets/bootstrap_owner_password_hash \
  .secrets/csrf_key .secrets/session_signing_key
chmod 640 .secrets/bootstrap_owner_email .secrets/bootstrap_owner_password_hash \
  .secrets/csrf_key .secrets/session_signing_key
```

GID `70` is pinned with `postgres:18.4-alpine` and the A1 application image;
Grafana `12.0.2` is pinned to UID `472` and GID `0`. Recheck these identities before
changing an image. File-backed Compose secrets use bind mounts and cannot remap
UID/GID/mode, so an owner-only `0600` file shared by the PostgreSQL initializer
and non-root application would be unreadable. The reviewed `0640` delivery uses
only the service-specific group and grants each secret to explicit services.

Compose file secrets are mounted files; they are not encrypted secret storage. On a mature server, integrate an external secret manager or protected host provisioning mechanism.

External alert delivery is optional. When enabled, set an HTTPS
`ALERT_WEBHOOK_URL` without userinfo, query, or fragment and set
`ALERT_WEBHOOK_ALLOWED_HOST` to its exact host (including a non-default port).
If the sink needs bearer authentication, mount a narrowly permissioned token
file with a deployment override and set its in-container absolute path as
`ALERT_WEBHOOK_TOKEN_FILE`. The token must never be embedded in the URL or
environment. Redirects are always rejected.

OpenTelemetry tracing is optional and disabled by default. To enable bounded
OTLP/HTTP export, set `OTEL_TRACING_ENABLED=true` and provide a full HTTPS
`OTEL_EXPORTER_OTLP_ENDPOINT` with no userinfo, query, or fragment. Do not put
collector credentials in the endpoint or environment. Export uses a bounded
asynchronous queue: exporter delay or failure may drop spans and emit a
redacted structured error, but cannot block application work. Shutdown gives
the provider at most five seconds to flush.

## 3. Start infrastructure

```bash
docker compose config
docker compose up -d postgres
docker compose ps
```

The PostgreSQL initialization script creates distinct owner, migrator, runtime,
recorder, backup, and read-only roles only on an empty data volume. Later changes belong
in migrations.

The exact migration command is `/app/platform admin migrate`. A4 applies the
embedded checksummed forward-only migrations under an advisory lock after
least-privilege migrator connectivity succeeds. An extra
`up` argument is intentionally rejected so deployment and binary command
surfaces cannot drift.

## 4. Enable application profiles

For local A1 validation, build the reviewed Dockerfile and keep Compose from
pulling a mutable or unrelated image:

```bash
make image
APP_IMAGE=axiom:local APP_PULL_POLICY=never \
  docker compose --env-file .env --profile app up -d --wait
```

For a server, use an image that CI has built, scanned, signed, and published;
set `APP_IMAGE` to its immutable digest where possible.

The `app` profile starts the API, production-public shadow engine, recorder,
and credential-free offline worker together, so the console workflows do not
silently omit their durable consumers. The narrower `record` and `workers`
profiles remain available for independently scaled role deployments.

Typical public shadow stack with observability:

```bash
docker compose --profile app --profile observability up -d
```

The `record` profile runs the A7 `platform recorder` composition. It connects
only to the compiled Binance production-public hosts, synchronizes BTC/USDT and
ETH/USDT, and writes linked wire/canonical Parquet segments under
`MARKET_DATA_HOST_PATH`. Readiness remains false until PostgreSQL is available
and both books are fresh and sequence-healthy. The default five-minute
finalization interval is also the declared raw recorder RPO; lowering it creates
more cumulative manifest revisions and must be capacity-tested. Keep the
recorder on the `exchange_egress` network and do not add proxy or credential
environment variables.

Add the edge only after `APP_DOMAIN`, `ACME_EMAIL`, secure cookies, allowed origins, DNS, firewall, and TLS behavior are correct:

```bash
docker compose --profile app --profile record --profile observability --profile edge up -d
```

## 5. Encrypted PostgreSQL backups

Build or publish the reviewed backup image separately from the scratch runtime
image:

```bash
make backup-image
BACKUP_IMAGE=axiom-backup:local BACKUP_PULL_POLICY=never \
  docker compose --profile backup run --rm backup create
```

The one-shot backup service uses the least-privilege backup role, streams
PostgreSQL custom format directly into framed AES-256-GCM, syncs and atomically
renames the object, and then writes a checksum manifest. Database and encryption
secrets remain file-backed and never enter command arguments. The `backup_data`
volume is independent of `postgres_data`, but a same-host volume is not an
off-host disaster copy. The authenticated manifest records start/completion UTC,
database and schema identity, `pg_dump` version, WAL boundary, encryption format,
object size, and checksum. After a successful backup, the job authenticates and
decrypts the new object through `pg_restore --list`; a structurally invalid
archive is durably quarantined outside the ready inventory. It then authenticates
and fully verifies every completed restore point, safely resumes any interrupted
deletion, and retains the newest 14 generations (or the larger configured
`BACKUP_RETENTION_GENERATIONS` value). Invalid inventory fails pruning closed.
Schedule the reviewed command daily and copy encrypted objects plus manifests to
protected independent off-host storage before release readiness.

Restore only into a clean isolated PostgreSQL database. Set the absolute
manifest path as seen inside the backup container and run:

```bash
BACKUP_RESTORE_MANIFEST=/backups/<name>.manifest.json \
  docker compose --profile restore run --rm restore
```

The restore command verifies the complete artifact authentication once before
starting `pg_restore`, validates the archive with the current `pg_restore`, and
refuses a target containing any non-system schema, relation, routine, or type.
It then decrypts a second verified stream into an atomic
`pg_restore --single-transaction` operation and withholds success unless the
schema version, per-asset journal balance,
nonnegative spot ownership, and active/quarantined reservation projection pass.
Never point this command at the active primary. A successful command is still
not release evidence until journal/projection, manifest/file, replay-hash, role,
RPO, and timed-RTO checks pass on the clean instance.

## 6. Reserved unavailable profile placeholders

The reserved `testnet` and `demo` profile names are documentation-only
placeholders in A1: no Compose service belongs to either profile. The V1A binary
also rejects both modes. `live` is rejected in every V1 release. Later V1C work
requires its own gated implementation after V1A passes; do not pre-provision
exchange credentials for this stack.

## Operational notes

- PostgreSQL, Prometheus, and engine metrics are not publicly published.
- API and Grafana bind to loopback unless deliberately changed.
- Application containers are non-root, read-only, capability-dropped, and resource-bounded.
- The recorder writes to `MARKET_DATA_HOST_PATH`; capacity, retention, and disk alerts must be reviewed before long-running capture.
- A stale exchange should make the engine degraded/unready, not create an endless restart loop.
- Structured logs go to stdout with local rotation. Do not use high-cardinality IDs as Prometheus labels.
- Docker networks do not enforce hostname-level egress policy. The compiled Binance public-only route/host allowlist belongs in code and should be reinforced by a host firewall or egress proxy.

## Troubleshooting

- `CHANGE_ME/axiom:CHANGE_ME` pull failure: build/publish the reviewed image and
  set `APP_IMAGE`; Compose never fabricates a runnable placeholder.
- `secret_file_unsafe_permissions` or `secret_file_unsafe_group`: apply the
  exact GID/mode procedure above and verify the pinned image identities. Never
  respond by making a secret other-readable.
- `/health/live` succeeds but `/health/ready` returns 503: check PostgreSQL
  health, the least-privilege role, secret grant, schema/migration result, and
  timeout settings. Do not redirect the healthcheck to liveness.
- `testnet`, `demo`, or `live` startup rejection: expected V1A behavior; there
  is no override or hidden profile.
- Existing PostgreSQL volume has different role names: preserve it, take a
  verified backup, and use an explicit reviewed migration. Never delete or
  rename the volume merely to match defaults.
