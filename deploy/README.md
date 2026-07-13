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

If you created `.env` or initialized PostgreSQL before the Axiom naming update, leave those existing database and role names alone for now. Branding does not require deleting or recreating a local database. Fresh setups copied from the current `.env.example` use the `axiom` names; an existing database can be renamed later only through a planned migration/backup procedure.

## 2. Create secret files

Use independent random values. Never reuse a database, session, CSRF, Grafana, alert, or backup secret.

Required for PostgreSQL:

- `.secrets/postgres_owner_password`
- `.secrets/postgres_migrator_password`
- `.secrets/postgres_runtime_password`
- `.secrets/postgres_readonly_password`

The A1 `app` profile exposes public health/build information only and mounts no
session, CSRF, bootstrap, or TOTP secret. A11 adds those files only with the
implemented authentication boundary.

Required when observability is enabled:

- `.secrets/grafana_admin_password`

Example for random secrets:

```bash
umask 077
openssl rand -base64 48 > .secrets/postgres_owner_password
openssl rand -base64 48 > .secrets/postgres_migrator_password
openssl rand -base64 48 > .secrets/postgres_runtime_password
openssl rand -base64 48 > .secrets/postgres_readonly_password
openssl rand -base64 48 > .secrets/grafana_admin_password
chgrp 70 .secrets/postgres_*_password
chmod 640 .secrets/postgres_*_password
chgrp 472 .secrets/grafana_admin_password
chmod 640 .secrets/grafana_admin_password
```

GID `70` is pinned with `postgres:18.4-alpine` and the A1 application image;
GID `472` is the pinned Grafana container group. Recheck these identities before
changing an image. File-backed Compose secrets use bind mounts and cannot remap
UID/GID/mode, so an owner-only `0600` file shared by the PostgreSQL initializer
and non-root application would be unreadable. The reviewed `0640` delivery uses
only the service-specific group and grants each secret to explicit services.

Compose file secrets are mounted files; they are not encrypted secret storage. On a mature server, integrate an external secret manager or protected host provisioning mechanism.

## 3. Start infrastructure

```bash
docker compose config
docker compose up -d postgres
docker compose ps
```

The PostgreSQL initialization script creates distinct owner, migrator, runtime, and read-only roles only on an empty data volume. Later changes belong in migrations.

The exact migration command is `/app/platform admin migrate`. In A1 it verifies
least-privilege migrator connectivity and reports zero application migrations;
A4 adds versioned SQL migrations and transactional schema validation. An extra
`up` argument is intentionally rejected so deployment and binary command
surfaces cannot drift.

## 4. Enable application profiles later

After CI builds, scans, signs, and publishes the real application image, set `APP_IMAGE` to an immutable digest where possible.

Typical public shadow stack:

```bash
docker compose --profile app --profile record --profile observability up -d
```

Add the edge only after `APP_DOMAIN`, `ACME_EMAIL`, secure cookies, allowed origins, DNS, firewall, and TLS behavior are correct:

```bash
docker compose --profile app --profile record --profile observability --profile edge up -d
```

The current A0/A1 Compose contract intentionally has no backup service. A4
adds a reviewed encrypted backup image, least-privilege credential delivery,
independent storage, retention, and clean-restore verification. Do not use an
ad-hoc plaintext `pg_dump` as a substitute. A Docker volume is primary storage,
not a backup.

## 5. Reserved unavailable profile placeholders

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
