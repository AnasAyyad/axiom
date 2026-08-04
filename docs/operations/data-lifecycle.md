# V1 data lifecycle

## Status and governing rules

This document defines the V1 classification, UTC retention, deletion, backup,
RPO, and RTO policy. D5 adds current revisioned disk pressure with immutable
observations, automatic unheld generated-artifact expiry, independently mounted
encrypted backup enforcement, authenticated clean-restore evidence, and the
formal readiness runner. Formal RPO/RTO readiness is not claimed until the
approved reference server completes the timed drills and uninterrupted
seven-day qualification.

All persisted timestamps and retention cutoffs use UTC. Durations and timeouts
use a monotonic clock. Retention never deletes a record or segment referenced by
a locked final test, incident, active replay, reproducibility bundle, audit
investigation, backup/restore drill, or explicit legal/owner hold.

## Classification levels

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

## Initial retention schedule

The specification fixes raw-data, backup, and metric defaults. The other
durations below are conservative A0 operating assumptions needed to make
deletion behavior explicit; they must be reviewed before the corresponding
writer is enabled.

| Data class | Primary retention | Deletion/archive rule |
|---|---|---|
| Raw and normalized Parquet market data | 30 UTC days hot initially, subject to measured capacity | Unreferenced segments become eligible after the 30-day cutoff and successful manifest/hash/hold checks; referenced segments remain |
| Dataset manifests, gaps, schema/parser versions, and segment checksums | For the life of every retained/referenced dataset; minimum life of the V1A evidence bundle | Never delete before every referenced segment/run/incident and backup copy expires |
| Journal, fills, decisions, risk evaluations, orders, reservations, configuration versions, audit events | Append-only for the V1A program and all retained evidence; no automatic V1A deletion | Corrections use compensating records; archival/deletion needs a future approved policy and dependency proof |
| Incidents and recovery evidence | Life of the V1A program and all linked replay/evidence | Close status without deleting history; hold linked data |
| Users and authorization roles | While the account or audit dependency exists | Disable rather than erase identity referenced by immutable audit; later privacy policy may pseudonymize nonessential fields |
| Hashed session and CSRF state | Active lifetime plus 30 UTC days after expiry/revocation | Purge token hashes after cutoff if no incident hold; retain non-secret audit facts |
| Structured application/edge/database logs | 30 UTC days online initially | Rotate by size and time; delete expired generations unless incident-held |
| Prometheus time series | 15 UTC days with a configured size cap | Automatic expiry; dashboards/reports retain only approved aggregates |
| Traces/profiles | 7 UTC days when explicitly enabled | Disabled by default where unnecessary; incident hold overrides expiry |
| Generated exports and support bundles | 7 UTC days or earlier explicit owner deletion | Per-export expiry; never include secrets; incident/reproducibility bundle may be promoted to a hold |
| Database backups | 14 daily restore points | Expire oldest verified generation only after a newer restore point and policy checks exist |
| Backed-up finalized Parquet segments | At least through their 30-day source retention and every reference/hold | Independently expire only after primary/reference/hold eligibility and checksum inventory reconciliation |
| Backup/drill logs and verification reports | 30 UTC days; release evidence summaries retained with the release | Redact paths/identifiers and hold failures until incident closure |
| Secret material | Only while active or required to decrypt retained backups | Rotate/revoke and destroy under `secret-handling.md`; excluded from application-data backups |

Retention age is calculated from the authoritative data/event or finalization
timestamp defined by the schema, never file modification time. A UTC-day cutoff
is evaluated only after the day closes; clock rollback cannot make data younger
or reorder deletion work.

## Segment and dataset lifecycle

```text
active .partial
-> flushed and fsynced
-> content/checksum calculated
-> atomically finalized
-> parent directory fsynced where supported
-> manifest committed ready
-> retained/referenced/held
-> deletion candidate
-> deleted and tombstoned
```

Startup discovers partial, orphaned, duplicate, missing-manifest, and corrupt
segments. A provably complete segment may be finalized; otherwise it is
quarantined and an explicit dataset gap/incident is recorded. An incomplete or
unverified segment is never relabelled ready to meet an RPO or soak target.

B2 coherent-view evidence follows the same lifecycle in five-minute atomic
segments. Every successful or rejected sample is retained with bounded facts,
exact input references when available, a checksum, and a hash-chain predecessor.
Finalization verifies every segment and deterministically replays each outcome;
partial writes, checksum or chain corruption, identity drift, and unbounded error
text fail qualification. The independent Binance and Bybit candidate recorders
finalize only after collector shutdown. Their cumulative manifests, raw/canonical
linkage, replay hashes, compatibility metadata, instance/region coverage, gap
declarations, and shared ordinal continuity must all validate before the aggregate
`axiom.multi-exchange-dataset.v1` manifest can be retained as Tier A.

Before production-public recording begins, the owner measures bytes/day for
each stream/depth and documents capacity for the retention window plus at least
30% headroom. The integer-only A4 planner also reserves at least 10 GiB free and
rejects duplicate/invalid samples, overflow, or weakened limits; its output is
evidence only when populated from real recorder measurements and declared server
capacity. Initial segment limits are one hour or 256 MiB, whichever occurs
first, with Parquet, Zstandard level 3. The initial high free-space watermark is
10 GiB and the initial critical watermark is 5 GiB. At high, new backtests,
replays, reports, exports, and shadow sessions are rejected and an alert is
opened. At critical, active shadow entries are disabled and recorder collectors
are canceled before a final flush; the recorder becomes unready while database
journal/audit writes remain available. A fresh normal observation resolves the
alert but never deletes historical pressure evidence.
Missing, bootstrap-only, or older-than-two-minutes current pressure is treated
as unavailable and blocks new heavy work and shadow activation.

## Deletion protocol

Deletion is a controlled, idempotent job and is deny by default:

1. Freeze one UTC policy time, a retention duration of at least 30 days, and a
   configuration version; the planner derives the cutoff internally.
2. Enumerate candidates from durable metadata; never glob an untrusted path.
3. Revalidate age, classification, dependency graph, locked-test/replay/incident
   references, active leases, legal/owner holds, and backup policy.
4. Write an immutable candidate manifest containing stable IDs, hashes, reason,
   policy version, actor/job, and UTC time, but no secret or unnecessary raw
   content.
5. Mark candidates pending deletion so new references cannot race the job.
6. Recheck invariants and delete through a storage-root-confined operation that
   rejects symlinks, path traversal, mount changes, and unexpected file types.
7. Remove corresponding independent backup copies only when their own retention
   expires; primary deletion never implies backup deletion.
8. Commit a tombstone with outcome and retained hash/provenance, then verify
   capacity and manifest consistency.

Partial failure resumes idempotently. A database outage, ambiguous reference,
checksum mismatch, clock anomaly, or permission/path surprise stops deletion
and opens an incident; it never broadens eligibility. Cryptographic erasure may
destroy a dedicated encryption key when architecture proves no retained object
needs it. The system must not promise physical secure erase on storage that
cannot demonstrate it.

Append-only journal, audit, fill, decision, configuration, and order-event
history is never edited in place. Privacy-driven removal of nonessential user
fields requires an approved pseudonymization/deletion design that preserves
referential and audit integrity.

## Backup policy

### PostgreSQL

- Take an encrypted backup daily and retain 14 daily restore points.
- Write backup output directly to an independently mounted remote filesystem;
  a PostgreSQL/Docker volume is not a backup. The process compares mount
  identities with PostgreSQL, market data, and local staging and fails closed
  when any are the same.
- Use a least-privilege backup identity and a separate encryption key. Do not
  include the live secret directory or plaintext secret-manager export.
- Record start/end UTC, database/schema version, tool version, snapshot/WAL
  boundary, encrypted object identity, size, checksum, and result.
- Treat a command exit code as insufficient. Validate structure/checksum and
  periodically restore a selected generation into a clean isolated instance.

### Market data

- Copy each finalized, manifest-ready segment to independent storage on a
  documented cadence no slower than daily for the initial disaster RPO.
- Back up dataset/segment manifests consistently enough to locate and order
  copied files. Verify content hashes after copy and on restore.
- The database backup does not copy bulk Parquet files. Restore the independently
  backed-up filesystem into a separate clean root, then require the restore
  command to match every ready database segment to exactly one confined file
  and its SHA-256 before it emits success evidence.
- Never back up `.partial` files as ready. Preserve explicit gaps and quarantine
  facts rather than silently filling them from a different dataset.
- A locked final test, incident, or reproducibility bundle pins every required
  segment and compatible reader/schema artifact in primary and backup policy.

### Restore verification

A clean restore is complete only when:

1. expected database roles and least-privilege access are restored or freshly
   provisioned without importing current secrets into the artifact;
2. migrations/schema and configuration identities are compatible;
3. journal transactions balance and projections rebuild to the same balances;
4. the authenticated restore evidence reports `market_data_verified=true`, a
   nonzero verified-segment count, and a sealed inventory hash matching the
   clean restored filesystem; ordered coverage and explicit gaps remain intact;
5. a declared replay produces the expected canonical hash; and
6. readiness remains paused until health and recovery checks pass.

Record a UTC drill timeline, RPO achieved, RTO achieved, data exceptions,
versions, owner, and remediation. Failed restore evidence is retained and opens
an incident.

## RPO and RTO

| Failure scope | RPO | RTO/response target |
|---|---:|---:|
| Crash after acknowledged critical database commit | Zero committed critical state loss | Shadow recovery readiness `<= 5 min` when dependencies are intact |
| Recorder crash | No more than configured flush interval, or an explicit dataset gap | Recover/finalize/quarantine before readiness |
| Database/host disaster using independent backup | `<= 24 h` initially from daily backup | Verified clean restore `<= 4 h` initially |
| Retained Parquet disaster copy | `<= 24 h` initial finalized-segment copy cadence; any loss is explicit | Declared drill dataset restored/verified within the 4-hour restore objective |
| External alert/metric history loss | Not authoritative for journal or replay | Restore only where backed; do not reconstruct critical truth from dashboards |

Runtime zero-RPO commits and daily disaster-backup RPO are different guarantees.
Neither may be used to overstate the other. Raw recorder loss within its allowed
interval still creates a declared gap; it is not silently ignored.

## Access, export, and owner responsibilities

- Storage/accounting owns database integrity, backup consistency, journal
  rebuild, and RPO evidence.
- Market-data/recorder owns segment finalization, manifests, capacity, gaps, and
  Parquet copy verification.
- SRE owns storage permissions, encryption, schedules, watermarks, off-volume
  destinations, restore drills, and deletion jobs.
- Security owns classification, holds, sensitive export, key separation, and
  incident review.
- Research owners declare dataset/run dependencies before retention expiry.
- Product/owner approves retention changes and any future immutable-history
  archival or privacy policy.
- QA independently checks restore hashes, replay identity, deletion near-misses,
  and release evidence.

Access is least privilege and reviewed. Exports are explicit, bounded, redacted,
watermarked with classification and expiry, and audited. A user’s ability to
view data does not automatically grant export, backup, or deletion authority.

Local D5 automation does not itself claim backup, retention, deletion, RPO, or
RTO readiness. Those claims require current authenticated clean-instance drill
and seven-day evidence from the approved exact release and reference server.
