# V1A secret handling

## Status and non-negotiable boundary

This document defines the required V1A secret lifecycle. It is policy for later
implementation and operational evidence; it does not claim that secret loading,
redaction, rotation, or deployment automation already exists.

V1A uses Binance production-public Spot data without credentials. It must not
accept, mount, read, validate, log, or store any Binance, Bybit, testnet, demo,
or production exchange API key, secret, private key, listen key, signature, or
account token. Finding exchange credential material in a V1A process is a
startup-blocking safety incident, not an optional unused setting.

## Classification

### Secret material

- PostgreSQL owner, migrator, runtime, and read-only passwords;
- session-signing and CSRF keys;
- bootstrap administrator password hash (the plaintext password is never a
  deployment input or stored artifact);
- optional TOTP encryption key and user TOTP seeds;
- Grafana administrator password;
- optional alert webhook token, SMTP credential, TLS private key, and backup
  encryption key;
- opaque session tokens, password-reset/recovery tokens, and CSRF tokens while
  in transit or memory; and
- future V1C sandbox credentials, which are forbidden from every V1A process
  and are outside this release’s handling path.

Secret references, key identifiers, rotation timestamps, and redacted hashes
are sensitive metadata even when they cannot authenticate by themselves.

### Sensitive non-secret material

Database dumps, local market recordings, incident bundles, audit exports,
configuration snapshots, user identifiers, source addresses, session metadata,
and logs require access control and retention but must not be used as a place to
hide secret values.

## V1A process access matrix

Access is deny by default. A service receives only what it needs, through an
explicit mount or secret-manager identity.

| Process/role | Permitted secret classes | Explicitly forbidden |
|---|---|---|
| `api` | Runtime database password; session-signing and CSRF keys; bootstrap admin hash during controlled bootstrap; optional TOTP key | Exchange, migration-owner, recorder, Grafana, and backup credentials |
| `engine-shadow` | Runtime database password only | All exchange credentials; auth/session, owner/migrator, Grafana, alert-delivery, and backup secrets |
| `recorder` | Dedicated/runtime database credential only when required by its least-privilege role | All exchange and console credentials; owner/migrator and backup encryption keys |
| `worker` | Runtime database credential and declared output-storage credential only if required | Exchange, owner/migrator, auth/session, and Grafana secrets |
| `migrate` | Migrator database password for the one-shot task | Owner password after initialization, exchange, session, and runtime-only secrets |
| PostgreSQL initialization | Owner and role-provisioning passwords only for initialization | Exchange and application-session secrets |
| Prometheus | Monitoring credential only if a protected scrape requires it | Database role passwords, session keys, exchange and Grafana admin secrets |
| Grafana | Grafana admin secret and least-privilege data-source credential | Exchange, database owner/migrator, and application-session secrets |
| Backup job | Least-privilege database backup credential and backup-encryption key | Exchange and application-session secrets |
| Edge proxy | TLS private key when not managed internally | Database, exchange, session-signing, Grafana, and backup secrets |

V1A Compose and configuration contain no future exchange-secret names, files,
or executable testnet/demo profiles. Later-release concepts remain roadmap
documentation only. Rendered V1A profiles must prove that no exchange secret is
declared, mounted, or referenced by any service.

## Creation and provisioning

- Generate independent high-entropy values with a cryptographically secure
  generator. Never reuse a value across database roles, session, CSRF, TOTP,
  Grafana, webhook, TLS, or backup encryption.
- Passwords are unique. The administrator password is converted with reviewed
  Argon2id parameters by an audited bootstrap tool; only the encoded hash is
  provisioned.
- Committed examples contain non-secret placeholders only. `CHANGE_ME`, sample,
  empty, duplicated, or known default values fail startup when the secret is
  required.
- Secret files live outside source-controlled paths except for ignored local
  delivery directories. The directory is owned by the deployment operator and
  mode `0700`; each file is regular and non-symlink. A single-consumer file is
  owner-matched with mode `0600` or `0400`. File-backed Docker Compose delivery
  may instead use reviewed mode `0640`/`0440` when the group is the exact pinned
  consumer group, every service is granted the file explicitly, no unrelated
  service joins that group, and the application verifies group membership.
  Other-readable, group-writable, or executable secret files are forbidden.
- A mounted secret is read from the declared absolute file path or secret-
  manager handle. Secret values are not passed directly through command-line
  arguments, ordinary environment variables, Compose labels, image layers,
  build arguments, or URLs.
- Production-like deployments prefer protected host provisioning or a real
  secret manager. Compose secret files are delivery mechanics, not encrypted
  secret storage.
- The operator verifies ownership and permissions before startup. The process
  verifies regular-file type, permissions, expected nonempty format, maximum
  size, and lack of placeholder content where the operating system permits.
- Docker Compose cannot remap UID/GID/mode for a secret whose source is a host
  file. The reviewed group-delivery rules are recorded in
  [ADR-0009](../adr/0009-compose-file-secret-groups.md); image UID/GID changes
  require revalidation before deployment.

## In-memory and runtime use

- Read a secret only in the process that consumes it, as late as practical.
- Do not copy secrets into general configuration snapshots, structs exposed to
  formatting, panic values, dependency-injection diagnostics, or health status.
- Public exchange client constructors have no credential parameter. A generic
  header map cannot inject `Authorization`, cookies, `X-MBX-APIKEY`, signatures,
  or other credential headers into public Binance requests.
- Compare security tokens with appropriate constant-time primitives where a
  timing distinction is meaningful. Store opaque session tokens only as hashes.
- Errors identify the secret by stable configuration field or file label, never
  by value, prefix, length, raw path contents, or parsed payload.
- Core dumps and interactive debugging are disabled or tightly restricted on
  deployed processes. A memory dump is secret-bearing evidence and follows the
  incident process.
- Child processes do not inherit unrelated file descriptors or secret-bearing
  environment. Containers run non-root with a read-only root filesystem,
  dropped capabilities, `no-new-privileges`, and explicit mounts.

## Output and redaction policy

Secrets must never appear in:

- application, proxy, database, migration, backup, or CI logs;
- API/SSE responses, browser storage, HTML, JavaScript bundles, screenshots, or
  frontend error reports;
- Prometheus metric values/labels, traces, profiler output, or health endpoints;
- audit events, incident descriptions, notifications, webhook bodies, email,
  reports, exports, or support bundles;
- fixtures, golden files, test snapshots, coverage artifacts, build caches,
  SBOMs, image metadata, Compose rendered output, or shell history; or
- raw/normalized market-data segments and Parquet metadata.

Use allowlisted structured log fields and a central redaction layer at every
output boundary. Redaction is defense in depth, not permission to pass secrets
through logging APIs. Headers, cookies, query strings, request bodies, config
objects, and error chains are omitted by default. Do not place secret-derived
values in metric labels. When correlation is required, use a non-reversible,
purpose-scoped identifier that cannot authenticate and cannot be correlated
across unrelated systems.

Tests inject unique canary secrets into each supported input path and scan raw
and encoded observable outputs. The canary suite covers common encodings and
structured error wrapping without recording a real secret.

## Storage, backups, and recovery

- Application data backups exclude the live secret directory and plaintext
  secret-manager export. Database backups are encrypted because hashed sessions,
  password hashes, TOTP material, and sensitive operational data may remain.
- Backup encryption keys are stored separately from backup media and database
  credentials. Losing the data and the key together is treated as a single
  control failure.
- A recovery plan documents how independent replacement keys and credentials
  are generated. It does not depend on extracting current secrets from logs,
  images, a database dump, or application APIs.
- Secret values are never included in reproducibility or incident bundles.
  Bundles include only redacted configuration identities and version hashes.
- Expired backup generations remain protected until verified deletion under the
  data-lifecycle policy. “Deleted from the primary host” is not evidence that a
  secret-bearing backup copy is gone.

## Rotation and revocation

Every secret has an owner, purpose, creation time, rotation procedure, and
revocation test. Rotate immediately on suspected exposure, owner/host/control-
plane compromise, unintended disclosure, or algorithm weakness. Planned
rotation also occurs before expiration and after privileged operator turnover.

Use overlap only when the protocol supports bounded dual-key verification:

1. create a new independent value;
2. provision it only to intended consumers;
3. validate the new path without printing the value;
4. atomically switch consumers or permit a short, measured overlap;
5. revoke the old value;
6. invalidate dependent sessions/connections where necessary;
7. confirm the old value no longer works; and
8. record actor, reason, affected identifier, UTC time, and result without value.

Database-role rotation is coordinated so a failed rollout does not fall back to
an overprivileged role. Session-signing rotation defines whether old sessions
remain valid for a bounded interval; emergency rotation revokes them. CSRF and
TOTP-key changes have explicit user recovery behavior. Backup-key rotation
preserves the ability to restore retained backups or re-encrypts them before the
old key is destroyed.

## Secret exposure response

Anyone who discovers a possible secret exposure must:

1. stop copying, displaying, or testing the value;
2. open a security incident using only a redacted identifier;
3. contain access to the source, logs, artifact, host, or session;
4. revoke and rotate the affected credential and dependent material;
5. invalidate affected sessions and assess lateral access;
6. remove the exposure from the working tree/artifact when authorized without
   rewriting evidence needed for investigation;
7. scan history, caches, releases, backups, observability, and downstream sinks;
8. recommend and complete credential rotation even if the file is later
   deleted; and
9. document a UTC timeline, scope, root cause, and control improvement without
   repeating the secret.

If a tracked repository file contains a secret, work stops, the value is not
shown, and rotation is required. Git deletion alone does not remove history or
copies.

## Required evidence and ownership

| Owner | Responsibility | Required evidence |
|---|---|---|
| Security | Classification, redaction policy, exposure response, review | Threat-model review and canary/redaction results |
| Platform/SRE | Generation guidance, permissions, mounts, container and backup isolation | Permission tests, V1A profile render, restore/rotation drills |
| Application/auth | Hashed password/session handling, CSRF, safe errors | Unit/integration/auth negative tests |
| Database/storage | Least-privilege roles, encrypted backups, sensitive-column handling | Role tests, backup inspection, clean restore |
| QA/release | Secret scans across source, history, image, artifacts, outputs | Scanner reports and clean release evidence |
| Owner/operator | Supply unique values, protect recovery keys, approve rotation and access | Provisioning checklist and dated access review |

V1A is not ready until secret scanning, per-process mount inspection, permission
validation, canary redaction, rotation/revocation, and clean-backup restore
evidence pass. This document alone is not that evidence.
