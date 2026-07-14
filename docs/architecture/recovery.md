# V1A recovery and readiness

**Status:** Normative architecture contract; A3 lifecycle framework implemented

## A3 implementation checkpoint

`internal/runtime.RecoveryGate` enforces the fourteen shadow-startup gates in
the order below, measures them through an injected clock, and remains locked
after administrative readiness. `Lifecycle` owns bounded cancellable workers
and enforces a configured shutdown limit no greater than 60 seconds. A3 tests
use deterministic and local conformance dependencies; A4-A11 must connect each
gate to its durable subsystem and the complete shadow profile must be timed
again before release certification.

## Recovery principle

Recovery proves current state before allowing new entries. It never assumes that a process exit rolled back durable work, silently edits history, releases an uncertain reservation, repairs a journal by mutation, or resumes merely because dependencies respond again.

Every decision-capable runtime starts `PAUSED`. The shadow engine must also acquire its ownership lease and enter `LOCKED` while it restores and reconciles state. `PAUSED` and `LOCKED` never auto-unpause; becoming ready only means an explicit authenticated, authorized, audited activation may be considered.

## Shadow startup recovery

Before the protected sequence, the engine performs the bounded local preflight
defined in [Lifecycle](lifecycle.md#common-local-preflight). That preflight may
reject an impossible subcommand, mode, or prohibited key name early, but it
does not declare the full build/configuration safe, load an immutable run
snapshot, mutate durable business state, or contact Binance.

The shadow engine then executes these protected gates in order:

1. Connect to PostgreSQL with the shadow runtime's least-privilege role and verify only the prerequisites needed to acquire ownership.
2. Acquire the protected shadow resource lease and new fencing token; enter `LOCKED` and unready.
3. Validate the binary/build identity and complete build/configuration safety manifest, including the compiled V1A mode and Binance public endpoint policies. Reject unknown/prohibited modes, hosts, products, keys, credentials, or capabilities.
4. Load and validate the complete immutable configuration graph and all required configuration/model/schema versions.
5. Verify the database schema, required durability, critical storage capacity, and compatibility with those immutable versions.
6. Locate the last verified run checkpoint and durable input cursor.
7. Recover reservations, simulated orders/plans, fills, scheduler/model state, jobs, inbox/outbox cursors, and audit chain.
8. Idempotently replay committed events after the checkpoint. Duplicate facts must have no duplicate effect.
9. Rebuild or verify journal-backed balances, positions, exposures, and P&L; every transaction must balance per asset.
10. Reconcile simulator state against orders, fills, reservations, positions, journal, and projections. Unknown or inconsistent state is quarantined and creates an incident.
11. Recover the decision-input recorder and Parquet segments: finalize only provably complete `.partial` data; otherwise quarantine it and record an explicit gap.
12. Connect to Binance public Spot, rebuild required books from snapshot plus buffered deltas, validate completed candles, clock, freshness, quality, and connection generation.
13. Verify queues, disk headroom, outbox lag, alerting, fencing renewal, and recording evidence.
14. Become `READY` while remaining `PAUSED`. Strategy/session activation is a separate authenticated and audited action.

Any failed gate keeps the affected scope locked/unready. Recovery may continue recording diagnostics and public data when safe, but it cannot accept a new entry intent.

## Process-specific recovery

| Runtime | Recovery requirement |
|---|---|
| API | Verify schema/session stores, command/outbox cursors, snapshot revision, and authentication policy before ready. Resume outbox consumption from durable revision; never infer state from missed notifications. |
| Shadow engine | Perform the complete fenced recovery and reconciliation sequence above. |
| Recorder | Scan `.partial`, orphaned, duplicate, corrupt, and manifest-missing segments. Finalize a segment only when content, sync state, hash, and boundaries are provable; otherwise quarantine and record a dataset gap. |
| Worker | Reclaim only expired fenced job claims. Verify run identity/checkpoint and resume idempotently or fail the run with evidence. |
| Migrator | Acquire migration ownership, apply forward-only migrations, verify version, and exit. Application processes stay unready on an incompatible schema. |

## Readiness contract

Liveness reports that the process loop can respond; it does not assert safe decisions. Readiness is role-specific and returns false with stable reason codes when any required condition is unsafe.

The shadow engine requires:

- current fenced ownership and successful renewal;
- valid build/configuration safety manifests and immutable run identity;
- durable PostgreSQL access, compatible schema, and critical-write capacity;
- balanced journal, verified projections, resolved reconciliation, and no unknown/inconsistent protected state;
- recovered orders/plans/reservations/checkpoints and idempotency cursors;
- healthy Binance public connection, sequence-valid fresh required book/candle views, acceptable clock drift, and hard quality checks;
- healthy decision-input evidence pipeline, bounded queue lag, and sufficient disk;
- effective risk state no less restrictive than the persisted state.

Readiness does not require risk to be `NORMAL`; a healthy but intentionally paused engine may be ready for administration while rejecting entries. API status must expose both readiness and effective risk/activation state so operators cannot mistake one for the other.

A stale exchange makes the engine degraded/unready without forcing an automatic container restart loop. Liveness remains true while the process can diagnose, record, resynchronize, and serve health.

## Failure matrix

| Failure | Required response | Recovery gate |
|---|---|---|
| Book sequence gap/invalid generation | Invalidate generation, pause instrument, discard unsafe queued opportunities, rebuild snapshot/stream | Fresh sequence-valid generation and quality checks |
| Stale data or clock drift | Reject decisions and pause affected scope | Sustained healthy values and explicit resume when state is `PAUSED`/`LOCKED` |
| Queue overload | Reject new work; invalidate market generation or lock critical pipeline; never drop critical facts | Bounded backlog and state-specific reconciliation/resync |
| Lease/fencing loss | Stop new plans, enter `LOCKED`, become unready, reject stale writes | New lease/token and full protected-state recovery |
| PostgreSQL critical-write failure | Do not report transition durable; pause/lock | Durability restored, journal/projections verified, inbox/outbox reconciled |
| Journal/projection mismatch | Preserve journal, quarantine difference, incident, block entries | Rebuild and explain/compensate; never edit history |
| Unknown/inconsistent simulated order | Retain reservation, quarantine exposure, reconcile idempotently | Terminal/recovered state and balanced journal |
| Disk critical watermark | Stop new decisions/jobs, preserve journal/audit, finalize or quarantine segments | Headroom restored and storage/manifests verified |
| Partial/corrupt segment | Never advertise complete; quarantine or deterministically recover | Hash/manifest compatibility and explicit gap status |
| Outbox/SSE interruption | State commit remains valid; consumers resume from durable revision | Cursor replay or fresh REST snapshot after retention expiry |

## Crash consistency

Critical transactions atomically couple the durable order/fill fact, reservation change, journal posting, projection update, inbox identity, and outbox item where they form one business transition. Acknowledged critical commits have zero RPO. Redelivery is expected and idempotent.

Active, cancel-pending, unknown, or recovery-required simulated orders keep their reservations. Cleanup uses compare-and-set state and fencing. Historical journal, fill, order-event, decision, configuration, audit, and manifest facts are immutable; corrections use explicit compensating records.

## Recovery objectives

- Shadow restart to recovery readiness: at most 5 minutes on the declared reference server.
- Acknowledged critical database state RPO: zero.
- Raw recorder RPO: at most the configured flush interval, or an explicit dataset gap that prevents an overstated confidence claim.
- Initial database backup cadence/disaster RPO: daily/at most 24 hours.
- Tested restore RTO: at most 4 hours initially.
- Gap-to-healthy book recovery: p95 at most 15 seconds when Binance public REST is available.

These are acceptance targets, not evidence until measured drills pass.

## Required evidence

Tests inject process termination at every critical persistence boundary, duplicate/out-of-order events, partial fills, corrupt checkpoints, database loss, disk full, lease loss, stale-token writes, book gaps, clock drift, and SSE cursor expiry. Evidence must prove no duplicate simulated order/fill, lost committed journal fact, negative balance, unsafe reservation release, unbalanced journal, stale decision, or automatic unpause.

Backup/restore must rebuild the same balances and replay hashes from a clean instance. The V1A release also requires a crash/restart and fencing drill plus a 72-hour Binance public-data soak.

## Known limitations

V1A recovery concerns virtual execution and public data only. It contains no authenticated exchange account to reconcile. Single-server recovery has downtime and does not provide automatic HA failover.
