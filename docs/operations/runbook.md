# Operations runbook

## Purpose and safety boundary

Operate the image-based single-server Axiom stack without weakening V1's
spot-only, fail-closed, no-production-order boundary. This runbook never enables
a canary or formal qualification. Those procedures remain separately
default-off in `deploy/README.md` and the phase-specific records.

## Inputs and configuration

Use an immutable application image digest, backup image digest, reviewed
`.env`, file-mounted secrets, absolute data paths, and the versioned platform
configuration selected by `APP_CONFIG_FILE`. Run `make preflight`, `make
compose-validate`, and `docker compose config` before a change. Keep published
ports on loopback unless the reviewed TLS edge profile is active.

## Start and verify

1. Verify disk space, clock synchronization, secret ownership/mode, independent
   backup mount, and exact image/configuration identities.
2. Start PostgreSQL, run the one-shot migration, then start only the required
   profiles. Application roles must become live before ready; readiness must
   include their dependencies.
3. Confirm the persistent `REAL TRADING DISABLED` UI state, authenticated
   detailed health, recorder freshness, current storage-pressure state, queue
   bounds, reconciliation state, and alert delivery.
4. Record source SHA, image digests, Compose render hash, configuration hash,
   migration head, server identity, operator, UTC timestamp, and result.

## Routine operation

Watch readiness, stale/gap/reconnect counters, database latency, queue depth,
recorder lag, storage free bytes, pressure state, backup age, restore-point
count, and alert delivery latency. High pressure rejects new heavy work;
critical pressure also disables new shadow entries and finalizes/quarantines
recorder work. Database journal and audit writes remain enabled.

## Failure and recovery

- On sequence gaps or stale public data, stop affected decisions, rebuild from
  the exchange snapshot protocol, and preserve incident/replay evidence.
- On an ambiguous sandbox submission, never retry blindly. Query by client
  order ID, consume private events/history, reconcile balances/fills, and
  resolve `UNKNOWN` first.
- On journal, reconciliation, authorization, clock, configuration, or identity
  failure, fail closed and create an incident. Do not bypass or edit evidence.
- On disk pressure, follow `data-lifecycle.md`; do not delete held or unverified
  raw segments.
- For data loss or corruption, follow `backup-restore.md` into a clean target.
  Never restore over the active database or recorder tree.

## Shutdown, rollback, and forward fix

Pause new work, allow bounded safe work to drain, stop writers before
PostgreSQL, and retain immutable evidence. Database migrations are
forward-fix-first; restore only from verified backups into a clean target. An
image rollback must remain schema compatible and use an immutable digest. A
failed formal run is terminal and cannot have its clock reset.

## Tests and limitations

Repository validation includes Compose renders, command contracts, process
smoke, restart/recovery, pressure, backup, chaos, security, and image gates.
Single-server Compose is not HA. Local/disposable checks are not declared-server
evidence, and no operational result proves profitability.
