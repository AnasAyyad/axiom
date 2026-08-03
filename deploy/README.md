# Axiom single-server Docker Compose deployment

This deployment is deliberately image-based. The repository contains the A1
platform/health source and pinned production Dockerfile; Compose still consumes
an explicit image reference and never pretends an unpublished image exists.

The base Compose project starts PostgreSQL only. V1A application, recorder,
worker, observability, and edge services are profile-gated. The encrypted
backup service arrives in A4. V1C C4/C5 add two independent authenticated
sandbox engines, two closed egress proxies, and inert-by-default one-shot
canary coordinators. C6 adds the credential-free console/API and a separate
least-privilege observer command; it does not add a Compose service or another
credential owner. No service can target a production-private exchange host.

## 1. Prepare configuration

```bash
cp .env.example .env
mkdir -p .secrets .local/market-data .local/v1c-pr2-canaries
chmod 700 .secrets
# On Linux, ensure bind-mounted writable paths match APP_UID/APP_GID.
sudo chown -R 10001:10001 .local/market-data
sudo chown 10001:70 .local/v1c-pr2-canaries
chmod 750 .local/v1c-pr2-canaries
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

The V1B recorder and B3-B6 strategy and advisory roles may instead select
`deploy/config/platform-shadow-v1b.json`. That immutable graph composes the
compiled Binance and Bybit production-public endpoint sets, three approved
spot instruments per venue, 15m/1h/4h candles, and the complete immutable
`mean-reversion.v1b.1` 1h/4h and `triangular.v1b.1` exact-depth parameter
contracts, B5's closed-cycle concurrent cross-exchange contract, and B6's
reviewed `rebalancing.v1b.1` advisory-only route contract. B4 and B5 remain
simulation-only, while B6 contains no transfer or withdrawal executor. The
graph contains no secret references and does not enable authenticated exchange
behavior. Older V1A and V1B.1 through V1B.4 configuration schemas remain
loadable without reinterpretation. Later V1B roles retain their predecessor
behavior until their sequential phase is implemented.

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
- `.secrets/postgres_binance_engine_password`
- `.secrets/postgres_bybit_engine_password`
- `.secrets/postgres_c6_qualification_password`

The A11 `api` service exposes redacted public liveness/readiness/build data and
uses the independent health-detail token for authenticated component status.
Only that service receives the bootstrap, CSRF, and session-signing files;
the shadow engine, recorder, workers, and observability services do not.

Required for A11 API startup:

- `.secrets/bootstrap_owner_email`
- `.secrets/bootstrap_owner_password_hash`
- `.secrets/csrf_key`
- `.secrets/session_signing_key`
- `.secrets/totp_seed` when the V1C high-risk authorization service is enabled

The TOTP seed is provisioned only to the API and the two inert one-shot canary
coordinators. It never reaches an exchange engine, proxy, browser, recorder,
shadow engine, or ordinary worker. There is no browser enrollment,
secret-return endpoint, recovery-code bypass, or raw seed variable.

V1C exchange credentials are operator-provisioned files:

- `.secrets/binance_testnet_api_key`
- `.secrets/binance_testnet_api_secret`
- `.secrets/bybit_demo_api_key`
- `.secrets/bybit_demo_api_secret`

Only the matching engine receives each pair. The API, public collectors,
recorders, workers, proxies, browser, and canary coordinators receive none.
Raw credential variables and endpoint/proxy overrides are startup errors.

Each manually prepared canary also needs one short-lived, exchange-specific
request file:

- `.secrets/binance_canary_request`
- `.secrets/bybit_canary_request`

The matching coordinator is the only service that receives that file. It
contains the owner email, current plaintext password, current six-digit TOTP
code, operator reason, and exact canary instrument/side/quantity/limit/style.
Create it immediately before `prepare`, use mode `0640` with group `70`, never
pass those values in arguments or environment variables, and remove the file
as soon as prepare succeeds. Canaries are buy-only, quote in USDT, and the
calculated exact notional must be no more than 10 USDT.

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
openssl rand -base64 48 > .secrets/postgres_binance_engine_password
openssl rand -base64 48 > .secrets/postgres_bybit_engine_password
openssl rand -base64 48 > .secrets/postgres_c6_qualification_password
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
On a user-namespaced Docker Desktop/WSL host, the same container GID can appear
as a remapped host GID, and an unprivileged host-side `chgrp` can be rejected.
Provision the short-lived canary request through a locked-down local container
running the application group or through the protected host provisioning
mechanism, then verify container-side readability. Do not weaken mode `0640`.

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
recorder, backup, read-only, Binance-engine, Bybit-engine, and C6 qualification
roles only on an empty data volume. Later changes belong in migrations.
Authenticated engines share no database login, and neither engine can append
or update C6 qualification records.

Before upgrading an existing database to migrations `000021` through `000024`, a
database administrator must provision the separate Binance-engine and
Bybit-engine login roles, the `axiom_c6_qualification` login role, and their
password files. Empty-volume initialization does not run again on an existing
volume, and migration startup fails closed if a required role is absent. Do
not reuse an existing application or authenticated-engine login for the C6
observer.

The `sandbox-foundation` profile starts only the two CONNECT-only proxies:

```bash
APP_IMAGE=axiom:local APP_PULL_POLICY=never \
  docker compose --profile sandbox-foundation up -d
```

Each proxy has one internal engine network and one independent external
egress network. It accepts port-443 CONNECT for its compiled host set, resolves
each tunnel, rejects any private/link-local/loopback/multicast/mixed answer,
and dials the validated address. Proxies receive no credentials. This profile
alone performs no exchange order operation.

The `sandbox` profile adds distinct Binance Testnet and Bybit Demo engine
processes. Each engine receives only its own credential pair, database role,
lease, internal network, and proxy. It has no direct external network. Startup
enters `LOCKED`, validates the live account/key generation, recovers durable
inbox/outbox state, loads exchange-authoritative balances/history, reconciles,
starts the private stream, and only then reaches `READY_PAUSED`.

An empty `BINANCE_SANDBOX_CONFIG_FILE` or `BYBIT_SANDBOX_CONFIG_FILE` selects
the complete built-in V1C graph with all four integration/submission switches
off. The image also contains two complete, reviewed, single-exchange canary
graphs:

- `/etc/axiom/platform-binance-testnet-v1c.json`
- `/etc/axiom/platform-bybit-demo-v1c.json`

Selecting one is an explicit order-enablement action. Pass the matching
`*_SANDBOX_CONFIG_FILE` path to every engine and coordinator command in the
canary window, or set that exact value in `.env` for the complete window. A
coordinator started with the default-off graph must reject an existing canary
as the wrong configuration. The other exchange remains disabled in the
selected graph. Revert the path to empty and restart the engine after
qualification.

Owner attestation values are non-secret hashes derived out of band:

- Binance account identity: lowercase SHA-256 of `<testnet uid>|SPOT`
- Bybit account identity: lowercase SHA-256 of `<demo user id>|UNIFIED`
- key fingerprint: first 32 lowercase hex characters of SHA-256 of that
  exchange's API key

Set the matching `*_OWNER_ATTESTED=true` only after verifying the account is
the intended virtual Testnet/Demo account and its key has read plus Spot-order
permission. Binance must have no prohibited capability. Bybit must be either
Spot-only or the exact Demo UI-coupled Unified Trading permission bundle in
ADR-0022, with Wallet, Exchange, Earn, transfers, withdrawals, partial or
expanded bundles, and unknown nonempty permissions absent. The engine
independently compares the live authenticated response to the attestation.

Before starting the Bybit engine, use Bybit's Demo Trading account to request
virtual BTC, ETH, and USDT funds. The authoritative Demo wallet response must
contain all three approved assets, even when one currently has a zero balance.
Axiom intentionally has no `/v5/account/demo-apply-money` operation: requesting
or reducing Demo funds remains a manual owner action outside the credentialed
runtime boundary.

### Manually armed PR2 canaries

Build the exact pre-commit candidate with a truthful embedded base commit and
dirty flag. Verification seals both that build identity and the SHA-256 of the
running executable, so the later PR2 commit must contain exactly the qualified
working tree without additional code changes. Make sure the API has already
bootstrapped the owner, the current TOTP seed matches that owner, both
engine/database credential files exist, and the selected engine is healthy in
`READY_PAUSED`. Then create the protected request JSON with exactly these
string fields: `email`, `password`, `totp`, `reason`, `instrument`, `side`,
`quantity`, `limit_price`, and `style`. `side` must be `buy`; `instrument` must
be an approved USDT pair; style must be `LIMIT_GTC`, `LIMIT_IOC`, or
`POST_ONLY`.

Before the first verification, provision the bind-mounted evidence directory
for the image's numeric application identity. It must be a real directory,
must not be world-writable, and must be writable by `10001:70`. On a
user-namespaced Docker Desktop/WSL host, provision it from the container
namespace:

```bash
mkdir -p .local/v1c-pr2-canaries
docker run --rm --user 0:0 \
  --volume "$PWD/.local/v1c-pr2-canaries:/evidence:rw" \
  postgres:18.4-alpine \
  sh -c 'chown 10001:70 /evidence && chmod 0750 /evidence'
```

If a sealed diagnostic file is later superseded by an exact candidate rebuild,
do not delete, chmod, or overwrite it. Provision a separate evidence root with
the same ownership and mode, pass that root through
`V1C_CANARY_EVIDENCE_ROOT`, and record both identities with the older one
explicitly marked superseded. Re-verification is permitted only for the same
already-terminal, exactly-once canary and remains query/reconciliation-only.

Prepare Binance without putting any factor or order value on the command line:

```bash
BINANCE_SANDBOX_CONFIG_FILE=/etc/axiom/platform-binance-testnet-v1c.json \
  BINANCE_CANARY_REQUEST_SOURCE_FILE=./.secrets/binance_canary_request \
  docker compose --env-file .env --profile sandbox-canary run --rm \
  binance-sandbox-canary \
  sandbox-canary --exchange binance --phase prepare \
  --input-file /run/secrets/binance_canary_request
```

The command logs no password, TOTP, price, quantity, signature, or private
payload. It logs in as the owner, consumes one password/TOTP authorization,
creates the exact 15-minute arm, persists one typed canary through
intent → allocator → central risk → planner → durable dispatcher, then queries,
cancels or observes a fill, and reconciles. Save only the printed `canary_id`.
Remove `.secrets/binance_canary_request`, restart only the credential-owning
engine, and run verification:

The committed Compose default mounts an intentionally invalid, non-secret
placeholder at the request-secret target. Only the prepare command overrides
that source with the protected short-lived file. Verification and abort
therefore cannot receive request contents and do not recreate a missing host
request path as a directory.

If prepare is interrupted only after exactly one authenticated create request
has already reached a terminal `CANCELED` or `FILLED` state, complete its
read-only query and reconciliation evidence without creating or canceling an
order:

```bash
BINANCE_SANDBOX_CONFIG_FILE=/etc/axiom/platform-binance-testnet-v1c.json \
  docker compose --env-file .env --profile sandbox-canary run --rm \
  binance-sandbox-canary \
  sandbox-canary --exchange binance --phase recover \
  --canary-id 'execution_plan:REPLACE_WITH_EXISTING_ID'
```

Recovery refuses rejected, ambiguous, nonterminal, duplicate-attempt,
wrong-configuration, missing-create, duplicate-create, non-prefix evidence,
and already-restarted canaries. It receives no exchange credential or prepare
request and cannot submit an exchange mutation.

```bash
docker compose --env-file .env restart binance-sandbox-engine
BINANCE_SANDBOX_CONFIG_FILE=/etc/axiom/platform-binance-testnet-v1c.json \
  docker compose --env-file .env --profile sandbox-canary run --rm \
  binance-sandbox-canary \
  sandbox-canary --exchange binance --phase verify \
  --canary-id 'execution_plan:REPLACE_WITH_PRINTED_ID' \
  --evidence-dir /var/lib/axiom/canary-evidence
```

Verification requires a newer startup cycle to reach `READY_PAUSED`, issues a
post-restart query and reconciliation, proves the durable outbox attempt count
is one, and proves exactly one authenticated create-request evidence record
exists in the canary interval. It then stops the canary session and creates one
new `0440` evidence file with `qualified=true` and
`profitability_evidence=false`. Existing files are never overwritten.

If a prepared canary cannot qualify after its order has reached one proven
terminal `CANCELED`, `FILLED`, or `REJECTED` state, explicitly revoke its arm
and stop its session:

```bash
BINANCE_SANDBOX_CONFIG_FILE=/etc/axiom/platform-binance-testnet-v1c.json \
  docker compose --env-file .env --profile sandbox-canary run --rm \
  binance-sandbox-canary \
  sandbox-canary --exchange binance --phase abort \
  --canary-id 'execution_plan:REPLACE_WITH_PRINTED_ID'
```

Abort writes no qualification evidence and refuses ambiguous, nonterminal, or
multi-attempt orders. Use the matching Bybit service and exchange value for a
Bybit canary. Do not clear canary sessions through direct database edits.

Repeat the same sequence with
`BYBIT_SANDBOX_CONFIG_FILE=/etc/axiom/platform-bybit-demo-v1c.json`,
`bybit-sandbox-canary`, `bybit_canary_request`, `--exchange bybit`, and
`bybit-sandbox-engine`. Keep the two evidence files independent. A failed,
missing, or unsealed file is not acceptance evidence. Never infer
profitability from either sandbox result.

### Manual C6 72-hour qualification

Do not start this observer until the PR3 implementation commit is clean, all
non-soak gates pass, both engines are healthy on the exact candidate, and the
owner/security reviewers approve the run window. The observer is not a
Compose service, owns no exchange credential, and cannot create, query, cancel,
or reconcile an order. The matching engines remain the only
credential-owning processes.

For an existing database, provision the dedicated
`POSTGRES_C6_QUALIFICATION_USER` before applying migration `000024`. Its
password file is separate from the API and both engine roles. The migrator
grants the observer only redacted operational reads, immutable qualification
appends, and the constrained terminal run transition.

Use a new run ID and a new absent absolute terminal path. Record the exact
clean commit, build hash, running executable SHA-256, image digest, and
immutable configuration SHA-256. Set `AXIOM_C6_SOURCE_DIRTY=false`; formal
validation rejects a dirty source, a missing image identity, either missing
approved account environment, a duration other than 259,200 seconds, or a
sample interval outside 15 seconds through 5 minutes.

The observer is a standalone committed-source binary, not `go run`. Build it
and the deterministic controller into a new retained qualification directory,
then record both SHA-256 values before launch:

```bash
install -d -m 0750 /srv/axiom-data/qualification/REPLACE_WITH_RUN_ID/bin
AXIOM_C6_OBSERVER_BIN=/srv/axiom-data/qualification/REPLACE_WITH_RUN_ID/bin/c6-soak \
  make c6-observer-build
AXIOM_C6_CHAOS_BIN=/srv/axiom-data/qualification/REPLACE_WITH_RUN_ID/bin/c6-chaos \
  make c6-chaos-build
sha256sum /srv/axiom-data/qualification/REPLACE_WITH_RUN_ID/bin/c6-soak \
  /srv/axiom-data/qualification/REPLACE_WITH_RUN_ID/bin/c6-chaos
```

The base deployment intentionally publishes no PostgreSQL port. Apply
`deploy/c6-qualification.compose.yml` only for the approved run window. It
binds PostgreSQL to loopback `127.0.0.1:55432` by default and does not expose
it publicly:

```bash
APP_IMAGE=REPLACE_WITH_EXACT_IMAGE docker compose --env-file .env \
  -f docker-compose.yml -f deploy/c6-qualification.compose.yml \
  --profile app --profile sandbox up -d --wait
```

Run the observer under a no-restart supervisor so any exit is terminal. The
following is the exact process environment; the supervisor definition must
preserve these values without copying secret contents:

```bash
DB_HOST=127.0.0.1 \
DB_PORT=55432 \
DB_USER=axiom_c6_qualification \
DB_PASSWORD_FILE="$PWD/.secrets/postgres_c6_qualification_password" \
AXIOM_C6_SOAK_ENABLED=1 \
AXIOM_C6_SOAK_MODE=formal \
AXIOM_C6_RUN_ID=REPLACE_WITH_NEW_RUN_ID \
AXIOM_C6_COMMIT_SHA=REPLACE_WITH_40_HEX \
AXIOM_C6_BUILD_HASH=REPLACE_WITH_64_HEX \
AXIOM_C6_EXECUTABLE_HASH=REPLACE_WITH_64_HEX \
AXIOM_C6_IMAGE_HASH=sha256:REPLACE_WITH_64_HEX \
AXIOM_C6_CONFIGURATION_HASH=REPLACE_WITH_64_HEX \
AXIOM_C6_SOURCE_DIRTY=false \
AXIOM_C6_EVIDENCE_PATH=/absolute/new/c6-terminal.json \
AXIOM_C6_OBSERVER_BIN=/absolute/retained/bin/c6-soak \
make c6-soak
```

The approved deterministic chaos controller must append exactly one run-bound
result for every closed C6 scenario after the run starts. Missing, duplicate,
unknown, or failed scenario evidence makes the terminal verdict fail closed.
After the run row is confirmed `RUNNING`, invoke the retained controller once
from the same exact clean checkout:

```bash
DB_HOST=127.0.0.1 \
DB_PORT=55432 \
DB_USER=axiom_c6_qualification \
DB_PASSWORD_FILE="$PWD/.secrets/postgres_c6_qualification_password" \
AXIOM_C6_CHAOS_ENABLED=1 \
AXIOM_C6_CHAOS_MODE=formal \
AXIOM_C6_RUN_ID=REPLACE_WITH_RUN_ID \
AXIOM_C6_COMMIT_SHA=REPLACE_WITH_40_HEX \
AXIOM_C6_SOURCE_ROOT="$PWD" \
AXIOM_C6_CHAOS_BIN=/absolute/retained/bin/c6-chaos \
AXIOM_C6_CHAOS_EXECUTABLE_HASH=REPLACE_WITH_64_HEX \
make c6-chaos-record
```

The controller verifies its own hash, the exact clean Git commit, and the
active run identity. It runs `make c6-chaos-qualify` with a strict child
environment that contains no database or exchange credentials, hashes the
transcript, and appends the complete fourteen-scenario result atomically.
The runner also fails on duplicate create, lost/double-posted fill, unresolved
unknown, mismatch/suspense, stale account, lease loss, persistence failure,
unsafe recovery/restart, production target, cap breach, alert latency, or
memory-leak evidence.

The terminal file is create-once, mode `0440`, and directory/file-synced.
Never delete or overwrite it. A smoke result always remains non-qualified. A
passed formal file still requires evidence review and explicit V1C
owner/security acceptance, and always carries
`profitability_evidence=false`.

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
and every configured book is fresh and sequence-healthy and every instrument's
exchange-clock sample is valid. The default five-minute
finalization interval is also the declared raw recorder RPO; lowering it creates
more cumulative manifest revisions and must be capacity-tested. Keep the
recorder on the `exchange_egress` network and do not add proxy or credential
environment variables.

Add the edge only after `APP_DOMAIN`, `ACME_EMAIL`, secure cookies, allowed origins, DNS, firewall, and TLS behavior are correct:

```bash
docker compose --profile app --profile record --profile observability --profile edge up -d
```

### Formal qualification services

Formal A7, B1, and future B2 qualifications use separate, new empty output
roots and separate service logs. Each runner must use an exact full committed
source identity, a public-only environment, `Restart=no`, and a 73-hour test
timeout. Their artifacts, status files, hash-chained journals, terminal evidence,
and service logs must never be shared.

A formal service is considered started only after all of the following are
observed:

- the unit is active with `NRestarts=0`, a valid PID, and expected start time;
- the log names the exact 72-hour test and committed source;
- the atomically replaced rolling status parses with the current schema;
- lifecycle journal entries exist and their bounded facts mirror to the log; and
- segment and cumulative-manifest data are arriving in the dedicated root.

DNS, TCP, TLS, and WebSocket upgrade share a five-second setup deadline and
Bybit subscription/heartbeat writes use two seconds. Clock-only degradation is
retried in place while stream/book processing continues; recorder and shadow
readiness use combined book/clock health. The official end freezes terminal
health before cancellation. Normal cancellation must not invalidate a healthy
book or create a reconnect, while journal, rolling-status, recorder-flush,
capacity, or terminal-evidence failure remains fail-closed.

The B2 service contract additionally requires one dedicated empty B2 root and
log, exact `AXIOM_B2_SOURCE_COMMIT`, bounded `AXIOM_B2_COLLECTOR_REGION`, and
`AXIOM_B2_SOAK=1`. It runs `TestB2Continuous72HourPublicSoak` with a 73-hour
timeout and `Restart=no`. This change documents that future contract only: no B2
unit is installed or started, and the 20-second smoke cannot promote B2.

Do not modify old qualification directories, terminal JSON, journals, logs, or
the services supporting an active or historical run.

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

## 6. Unavailable production trading

The `testnet` and `demo` execution modes are reachable only through the two
closed `sandbox-engine` commands and their compiled endpoint policies. There
is no production-private engine, profile, credential name, endpoint editor, or
order route. `live` remains rejected in every V1 release.

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
