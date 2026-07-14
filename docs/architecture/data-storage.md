# V1A data storage

**Status:** Normative architecture contract; A4 implementation in progress

The A4 working tree now includes checksummed forward-only migrations, exact
journal/reservation conformance models, closed runtime/recorder/reporting role
matrices, reviewed and generated `sqlc` queries, a real Parquet/Zstd level 3
codec, and a crash-safe segment finalization/recovery protocol. Clean
PostgreSQL 18 integration and backup/restore drills remain incomplete and are
not claimed here.

## Storage split

V1A uses two complementary stores:

1. PostgreSQL 18 with `pgx`, reviewed SQL migrations, and `sqlc` for transactional state, metadata, ownership, audit, and durable coordination.
2. Append-only Parquet files with Zstandard compression for high-volume raw and normalized market events.

Raw depth streams do not belong row-by-row in PostgreSQL. PostgreSQL stores segment manifests, hashes, coverage, gaps, compatibility, and quality metadata. A Docker volume is persistence, not a backup.

## PostgreSQL responsibilities

The V1A relational model includes:

- users, password/session state, roles, audit events, incidents, alerts, and acknowledgements;
- immutable configuration, asset/instrument metadata, strategy, risk, accounting, and model versions;
- virtual accounts, portfolios, ownership, balances, positions, exposure, reservations, decisions, and explanations;
- simulated execution plans, orders, attempts/events, fills, fees, recovery attempts, and checkpoints;
- immutable journal transactions/lines and rebuildable projections;
- backtest, replay, paper, and shadow run definitions, lifecycle, results, and reproducibility identity;
- commands, jobs, claims, leases, fencing tokens, inbox identities, outbox events, and consumer cursors;
- dataset segments/manifests, gaps, validation results, and quality incidents.

Schema design uses typed columns, UTC timestamps, foreign keys, constraints, and indexes derived from query patterns. JSON is limited to genuinely variable metadata. Authoritative financial columns use exact decimal representations with explicit scale/rounding semantics; binary floating-point financial columns are prohibited.

## Transaction and immutability rules

Domain logic and database constraints enforce the same critical invariants:

- available and reserved balances cannot be negative;
- reservations cannot exceed owned available cash/inventory and are exclusive under concurrency;
- a simulated sell cannot exceed owned inventory;
- journal transactions balance independently per asset/commodity;
- a journal is sealed by its deferred balance constraint, after which even a
  balanced line append is rejected;
- fill reduction, journal posting, balance/position projection, reservation consumption/release, inbox identity, and outbox insertion commit atomically when they are one logical transition;
- order transitions follow the idempotent reducer;
- stable order/fill/inbox/run identities are unique;
- one active lease exists per protected resource and fencing tokens only increase;
- configuration, strategy, decision, fill, journal, audit, order-event, and used model versions are immutable.

Historical rows are never corrected in place. Reversals, compensating entries, external adjustments, and reconciliation suspense retain causation and incident evidence.

## Journal as source of truth

Backtest, replay, paper, and shadow use an immutable multi-commodity double-entry journal. Each transaction header records mode, run, portfolio, strategy, order/fill, policy/configuration versions, UTC/logical time, ingest ordinal, correlation/causation, and reversal identity. Lines record account, exact asset quantity, debit/credit direction, valuation/cost-basis references, and rounding metadata.

Balances, positions, exposure, and P&L are derived from the journal or transactionally maintained projections that can be discarded, rebuilt, and verified. USDT, BTC, and ETH each balance independently; BTC is never numerically offset against USDT. Fees, spread, slippage, latency deterioration, realized/unrealized P&L, recovery, and dust remain explicit.

## Durable coordination

- Commands and long-running jobs are PostgreSQL records with idempotency keys and explicit lifecycle states.
- Database triggers freeze command/job identity and reject invalid initial,
  claim, renewal, completion, and terminal-state changes.
- Inbox uniqueness makes retries/redelivery safe.
- Engine state changes and their business event commit with an outbox row.
- Consumers checkpoint a monotonic durable revision. `LISTEN/NOTIFY` may reduce latency but cannot replace polling/replay from durable rows.
- Outbox publication is one-way and consumer revisions can only increase.
- Leases enforce one owner and monotonically increasing fencing epochs. Protected writes include the current token.
- Queue pressure in PostgreSQL coordination rejects new entry work; it must not fall back to an unaudited in-memory command.

PostgreSQL roles are separated for migration, runtime, recorder/segment metadata, read-only reporting, and monitoring with least privilege.

## Market-data segments

Record both the immutable wire envelope/payload needed to reproduce parsing and the normalized canonical event, including connection lifecycle, snapshots, subscription results, clock samples, decoder errors, gaps, and resynchronizations.

The codec-independent `market-wire.v1` schema preserves exchange/event/market
identity, source session and connection generation, the exchange-native source
sequence as text, optional exchange UTC time, local UTC receive time, monotonic
session offset, recorded logical time, exact wire
bytes, and their SHA-256 digest. The linked `market-canonical.v1` schema adds the
event identity, parser and normalization versions, source-payload digest,
canonical replay bytes, and canonical digest. Both use integer UTC nanoseconds,
unsigned ordinals/generations, byte arrays, and fixed 32-byte digests; neither
schema contains a binary floating-point column. Row validators reject incomplete
identity, same-asset markets, invalid epoch values, zero source linkage, and
payload mutation before codec entry.

The concrete codec snapshots validated rows before writing, emits bounded row
groups through `parquet-go`, uses single-worker Zstandard default compression
(level 3), and embeds schema, parser, normalization, compression, and ordered
content-hash metadata. Readers require the exact reviewable physical/logical
schema, verify every column is Zstd-compressed, validate row digests and
compatibility metadata, and recompute the versioned length-framed ordered hash
before exposing records.

Dataset manifests pin `dataset-order.v1` and a non-empty ordering scope. The
compatibility reader preserves and validates the exact `(recorded logical time,
ingest ordinal)` order, rejects a missing/duplicate ordinal even across different
logical times, and does not use exchange time, source sequence, or event ID to
repair malformed input. Its compatibility set is closed independently over
schema, parser, and normalization versions; it also rejects ordered-hash
disagreement and any declared gap that overlaps a recorded ordinal.

Segments are partitioned by exchange, event type, instrument, UTC date, and hour or configured maximum size. The initial target is one hour or 256 MiB, whichever comes first. Each segment records schema/parser/normalization versions, start/end times, first/last sequences and ordinals, record count, ordered content hash/checksum, path, quality/gap state, and compatibility requirements.

Commit protocol:

1. create a unique `.partial` file on the target filesystem;
2. append in canonical record order;
3. flush and `fsync` contents;
4. require the codec writer's canonical ordered-content hash to match the
   immutable segment specification, then calculate the file checksum;
5. atomically rename to an immutable final name on the same filesystem;
6. `fsync` the parent directory where supported;
7. commit the PostgreSQL manifest row as `ready`.

Startup discovers incomplete, orphaned, duplicate, corrupt, and manifest-missing files. It finalizes only a provably complete segment; otherwise it quarantines the file and records an explicit dataset gap. An incomplete segment is never advertised as complete.

Recovery validates each proof against a regular (never symlinked) partial/final
file and the complete immutable manifest. A corrupt or mismatched proof and its
associated file are moved together into a synced quarantine directory; an
existing quarantine name fails closed instead of overwriting prior evidence.
Unproved partials are quarantined separately. Valid proofs remain eligible for
idempotent manifest recommit.

The shadow engine's decision-input recorder and the standalone broad recorder use distinct session directories/manifests even when they share a storage root. Files are immutable after finalization.

## Data classification

The classification names, examples, and minimum handling below are identical to
the governing [V1A data lifecycle policy](../operations/data-lifecycle.md#classification-levels).

| Level | Meaning | Examples | Minimum handling |
|---|---|---|---|
| Public | Intentionally publishable | Public documentation and approved release notes | Integrity review; no secret or internal path disclosure |
| Internal | Non-public operational/research material | Aggregated metrics, non-sensitive build metadata, capacity plans | Authenticated access; bounded retention/export |
| Confidential | Sensitive business, user, or system data | Local public-market recordings, normalized datasets, config snapshots, logs, reports, source addresses | Least privilege, encrypted transport/backups, audited export/deletion |
| Restricted | Security- or integrity-critical data | Secret files, password/session hashes, TOTP material, database dumps, journal, audit, incidents, backup keys | Strict role separation, encryption, no broad export, explicit rotation/hold/deletion controls |

Public exchange input does not make the local recording Public. Recordings can
expose collection times, gaps, research scope, system behavior, and decision
evidence, so they are Confidential. Future test/demo private payloads are
Restricted and outside V1A.

Prohibited capability is not a classification level. V1A rejects exchange API
keys, signatures, private account/order payloads, testnet/demo credentials, and
production credentials at configuration and startup boundaries; it does not
accept or store them.

## Retention, backup, and deletion

Initial defaults, pending measured capacity evidence:

- Parquet with Zstandard level 3 and 30 days of hot raw-data retention;
- at least 30% measured storage headroom;
- daily PostgreSQL backup, 14 daily restore points, and independent encrypted off-host copy before final readiness;
- Prometheus retention of 15 days with a size cap;
- warning/decision threshold of at least 10 GiB free on a small server, raised by capacity planning.

The A4 capacity planner accepts one measured finalized-byte sample for each
exact exchange/event-type/instrument/depth stream. It rounds each observed rate
up to bytes/day using integer arithmetic, sums 30 or more hot days, adds at least
30% headroom and at least 10 GiB reserved free space, and reports whether the
declared provisioned bytes are sufficient. Duplicate streams, sub-second or
zero samples, arithmetic overflow, a segment older than one hour, a segment
larger than 256 MiB, or a weakened policy fail closed. The planner is implemented
and tested; actual recorder measurements and an owner-approved server capacity
remain evidence gaps.

Retention never deletes data referenced by an active run/replay, locked final test, incident, reproducibility/evidence bundle, audit/legal hold, or unresolved recovery case. Deletion operates only on finalized, expired, unreferenced data through an auditable policy; it never removes a file while leaving a manifest that claims availability. Secret/session deletion follows security policy without erasing immutable audit evidence.

The segment planner derives its own cutoff from a UTC policy time and refuses a
hot-retention duration below 30 days; callers cannot supply a pre-weakened
cutoff. Before eligibility it validates the complete Parquet/Zstd manifest,
ordered-content linkage, file identity/size/checksum metadata, and that
finalization follows segment coverage. Malformed metadata fails the entire plan
closed rather than being treated as deletable garbage.

PostgreSQL restore-point pruning has a hard floor of 14 generations. It first
authenticates every manifest and fully checks/decrypts every completed artifact;
one invalid inventory item prevents all pruning. It marks an old manifest with a
durable deletion tombstone before removing its artifact, resumes authenticated
tombstones after interruption, and never counts `.partial` files as restore
points. This local retention control does not establish daily scheduling,
off-host durability, or restore readiness by itself.

A newly encrypted database object is not advertised as a restore point until a
second authenticated read passes `pg_restore --list`. The manifest records both
`pg_dump` and `pg_restore` versions. A structurally invalid archive is renamed to
authenticated `.invalid` evidence after the ready manifest is durably removed;
it is never counted by retention. This structure check is still not a substitute
for the required clean-instance restore drill.

Restore rejects a target with any non-system schema, relation, routine, or type,
not merely a populated `public` table list. `pg_restore --single-transaction`
applies the complete archive atomically and implies exit-on-error behavior. The
command then verifies the recorded schema version, exact per-asset journal
balance, nonnegative virtual balances/spot positions, and equality between the
reserved projection and active/quarantined reservation ownership. It does not
print success when any check fails. Role reprovisioning, manifest-to-file
inventory, and replay-hash comparison remain part of the external clean drill.

At high disk pressure, reject new backtest/export jobs and alert. At critical pressure, stop new shadow decisions, finalize or quarantine active segments, preserve critical journal/audit writes, and enter `PAUSED` or `LOCKED`.

## Recovery objectives

| Data | RPO | RTO/response |
|---|---|---|
| Acknowledged critical PostgreSQL commit | Zero | Shadow recovery readiness at most 5 minutes on the reference server |
| Raw recorder active segment | Configured flush interval, or explicit gap | Recover/finalize/quarantine during startup |
| PostgreSQL disaster backup | Initially at most 24 hours with daily cadence | Tested clean restore at most 4 hours |
| Dataset manifest + files | No silent loss; missing data becomes explicit gap | Restore must reproduce the same supported replay hashes |

These are targets until drills provide evidence.

## Required evidence and metrics

Migration verification, PostgreSQL integration tests, journal property tests, concurrent reservation tests, inbox/outbox redelivery, lease fencing, kill-point segment tests, corrupt-file recovery, projection rebuild, disk-pressure tests, backup/restore, and replay-hash comparison are mandatory. Metrics cover database durability/latency, pool state, journal failures, inbox/outbox/job lag, lease state, segment flush/sync/finalization, gaps/corruption, disk headroom, backup age, and restore results with bounded labels.

## Known limitations

V1A storage is single-server oriented and does not provide multi-region durability or HA. Bulk public data may have an RPO up to its flush interval, but any loss must be explicit and must lower or invalidate research confidence.
