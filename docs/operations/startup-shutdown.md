# V1A startup and shutdown

## Status and invariant

This is the normative lifecycle policy for V1A processes. It is not evidence
that a lifecycle manager, leases, health endpoints, or recovery code currently
exist. Every process starts fail-closed. Readiness never activates strategy
execution; the initial execution state is `PAUSED` and activation is a separate
authorized, audited action.

## V1A process topology

| Process | Responsibility | Exchange credentials/order capability |
|---|---|---|
| `api` | Browser API, authentication, durable administrative command intake, state fan-out | None; cannot call an exchange |
| `engine-shadow` | Single owner of live public-data hot path, strategy, allocator, risk, simulation, and virtual journal | None; public reads and simulated orders only |
| `recorder` | Public market-data recording and segment finalization | None |
| `worker` | Offline backtest, replay, and report jobs | None; recorded inputs read-only except declared outputs |
| `migrate` | One-shot schema migration with a separate role | None |

Binance Testnet, Bybit demo, and live engines are unavailable in V1A. V1A has
no executable deployment profile, secret, or startup target for those modes;
their future release sequence is documentation only.

## Lifecycle states

```text
STARTING -> LOCKED -> RECOVERING -> SYNCING -> READY/PAUSED
READY/PAUSED -> ACTIVE_SHADOW (explicit authorized action only)
any state -> DEGRADED or PAUSED -> LOCKED
any state -> STOPPING -> STOPPED
```

`ACTIVE_SHADOW` authorizes virtual decisions only. It does not add a network or
exchange-order capability. Unknown, contradictory, or non-durable state moves
to the most restrictive applicable state.

## Startup sequence

### Local preflight and protected shadow startup

1. Perform bounded local preflight only: initialize redacted logging and a
   monotonic/UTC clock, load the compiled V1A safety policy, parse the
   subcommand, reject prohibited modes/capability or credential key names, and
   locate the least-privilege resource references. This step does not fully
   validate configuration, load a run snapshot, mutate durable business state,
   or contact Binance.
2. Acquire required PostgreSQL connectivity with the process's least-privilege
   runtime role; do not fall back to owner/migrator credentials.
3. For the shadow engine, acquire the database-backed ownership lease and
   monotonically increasing fencing token, then enter `LOCKED` and remain
   unready.
4. Validate the complete build/configuration safety manifest, compiled
   modes/endpoints, secret-file permissions, storage roots, and prohibited
   keys/capabilities.
5. Load and validate immutable configuration/model/schema versions and record
   their hashes.
6. Verify database schema, durability, and storage compatibility with those
   versions.
7. Recover incomplete jobs, segments, reservations, intents, journal/outbox
   state, projections, and checkpoints applicable to the process.
8. Reconcile virtual simulator, reservation, balance, position, and journal
   state. Mismatch is quarantined and blocks readiness.
9. Establish only the public Binance connections in `endpoint-policy.md`,
   restore subscriptions, build books, and pass sequence, freshness, clock, and
   quality checks where required.
10. Verify critical audit/decision recording, queue capacity, disk headroom,
    database durability, and fencing ownership.
11. Report truthful readiness while execution remains `PAUSED`. Activation is
    separate and cannot be restored implicitly from a prior process lifetime.

No exchange network request occurs before endpoint/configuration safety passes.
No strategy input is eligible before the exact book generation is healthy. A
failed step cancels dependents, records a redacted incident when durable storage
is available, remains unready/locked, and never tries a broader mode, endpoint,
credential, role, or stale snapshot.

## Readiness by process

- `api`: schema compatible, database reachable, auth/session inputs valid,
  durable command/audit writes available. Engine degradation is shown in status
  but does not make the API claim the engine ready.
- `engine-shadow`: valid safety/configuration, current lease/fence, recovered and
  balanced virtual state, critical persistence/audit available, required books
  healthy, queues and clock within limits, embedded decision evidence writable.
- `recorder`: endpoint policy valid, storage writable with headroom, manifest
  database available, partial-file recovery complete, configured streams
  healthy. A declared gap may be operationally visible but never hidden.
- `worker`: schema/config compatible, declared dataset manifests and readers
  compatible, input hashes valid, durable job lease/output path available.
- `migrate`: exclusive reviewed migration operation succeeds or fails as a
  one-shot task; it is not a long-running ready service.

Liveness means the process can report and make progress; readiness means it is
safe to serve its declared function. Stale exchange data makes the engine
degraded/unready without inducing an automatic restart loop. Health details are
authenticated; public liveness exposes no secret or internals.

## Lease ownership and fencing

One active lease exists per protected engine/session resource. Every protected
write carries the current fencing token and rejects a stale token. Renewal uses
bounded deadlines. Failure to renew, database durability loss, ownership
conflict, or a higher observed token immediately stops acceptance of new plans
and locks the engine before further protected writes.

The lease is released only after safe shutdown completes. If state is uncertain
or the database is unavailable, do not assert release; let the lease expire and
require the next owner to acquire a higher token and perform full recovery.

## Graceful shutdown

`SIGTERM` or `SIGINT` starts one idempotent shutdown with a maximum 60-second
budget. A second signal may request forced termination but cannot mark shutdown
successful.

1. Enter `STOPPING`, become unready, and stop new sessions, commands, jobs,
   entries, and strategy evaluations.
2. Stop producers before consumers; cancel outbound public reads and prevent
   reconnect/resubscribe loops.
3. Finish or explicitly reject in-flight decision work. Persist/checkpoint
   durable jobs, virtual orders, plans, reservations, and state transitions;
   quarantine ambiguity rather than guessing.
4. Flush critical journal, inbox/outbox, audit, incident, and metric state.
5. Recorder flushes and fsyncs valid segments, atomically finalizes complete
   segments, and quarantines partial/unverifiable segments with an explicit gap.
6. Release only reservations proven safe after state reduction. Do not release
   anything tied to unknown or recovery-required state.
7. Close streams/listeners and database clients after their dependent work.
8. The engine releases its fencing lease last, only after durable safe state.
9. Exit zero only when the sequence completed; otherwise emit a redacted failure
   and require recovery on next startup.

If the 60-second budget expires, preserve fail-closed state and terminate
without fabricating completion. The next startup performs full recovery.

## Crash recovery

Crash startup never resumes strategy activation automatically. It acquires a
higher fence, discovers partial segments and nonterminal virtual state, verifies
journal/projections and manifests, reduces idempotent events, rebuilds required
books, and remains paused. Duplicate event/order/fill, unsafe reservation
release, silent data gap, and unbalanced journal are release-blocking failures.

## Ownership and required evidence

- Platform/runtime owns signals, deadlines, dependencies, readiness, leases,
  fencing, and process ordering.
- Storage/accounting owns durable checkpoints, journal/projection recovery,
  reservations, and segment recovery.
- Adapter/market-data owns connection, book, generation, and resync readiness.
- SRE owns orchestration, health checks, termination grace, capacity, and drills.
- Security owns mode/endpoint/secret preflight and incident escalation.
- QA owns overlap, lease-loss, kill-point, forced-timeout, partial-file, and
  truthful-readiness tests.

Required evidence includes state-machine tests, overlapping-engine exclusion,
lease-loss tests, process kills after every durable boundary, queue/disk/database
faults, repeated-signal behavior, and successful shutdown within 60 seconds.
Until those pass, this document is policy rather than an operational claim.
