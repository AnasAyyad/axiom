# V1A runtime lifecycle

**Status:** Normative architecture contract; A3 lifecycle framework implemented

The A3 implementation supplies bounded worker ownership, cancellation,
shutdown timing, fail-closed safety state, and the ordered recovery gate. It
does not yet implement later-phase durable checkpoint, journal, market-data,
or reconciliation handlers.

## Lifecycle invariants

- Every process begins unready and fails closed on invalid or incomplete configuration.
- Every decision-capable component starts `PAUSED`; the shadow engine enters a fenced `LOCKED` recovery gate before it can become ready.
- Readiness never activates a strategy. `PAUSED` and `LOCKED` never auto-unpause.
- New entry intake stops before draining, checkpointing, or lease release.
- A clean owner releases its fencing lease last.
- `SIGTERM`, `SIGINT`, administrative stop, and fatal dependency policy use the same bounded shutdown coordinator.
- Graceful shutdown has one process-wide maximum of 60 seconds.

## Service and risk states

Service lifecycle and risk state are related but not interchangeable:

```text
NEW -> STARTING -> RECOVERING -> READY -> STOPPING -> STOPPED
                     |            |
                     +-> FAILED <-+
```

```text
LOCKED -> PAUSED -> CAUTIOUS/NORMAL
   ^         ^             |
   +---------+-------------+
       failure escalation
```

`READY` means a runtime can safely perform its declared administrative/recording role. It may remain `PAUSED` and reject all entry intents. A failure may keep the process live for diagnosis while readiness is false and the effective risk state is more restrictive.

## Common local preflight

All subcommands perform only bounded, non-side-effectful local work before
opening role resources:

1. install signal/cancellation handling and the 60-second shutdown budget;
2. initialize minimal redacted logging and lifecycle state;
3. load the compiled V1A safety policy;
4. parse the subcommand and reject `live`, `testnet`, `demo`, credential or
   private-endpoint key names, and prohibited capabilities; and
5. perform syntax and local-file checks needed to locate the role's
   least-privilege resources.

This preflight does not validate or activate the full configuration, load a run
snapshot, recover protected state, publish readiness, mutate durable business
state, or make an exchange request. A shadow engine performs the protected
sequence below after acquiring its fence. Roles without a protected execution
lease then validate their complete configuration and dependencies in their
role-specific startup before becoming ready.

Partial startup is not success. If a required dependency or invariant fails, the runtime remains unready, cancels started components in reverse order, and exits nonzero or stays in a documented degraded diagnostic state. It never enables a reduced safety path.

## Role-specific startup

### Shadow engine

```text
PAUSED
-> bounded local preflight
-> connect PostgreSQL
-> acquire lease and fencing token
-> LOCKED
-> validate build/configuration safety manifest
-> load immutable configuration versions
-> recover checkpoint, journal, projections, reservations, orders, inbox/outbox
-> reconcile all protected virtual state
-> recover decision-input recording
-> synchronize and qualify Binance public books/candles
-> verify clock, disk, queues, lease renewal, and risk inputs
-> READY + PAUSED
-> explicit authenticated/audited session activation only
```

Any gate failure leaves the engine locked/unready. Activation reevaluates current health and cannot use a stale startup result.

### API

The API verifies schema, session/auth policy, command persistence, outbox cursor, and current snapshot revision before ready. It accepts no exchange credentials and has no exchange-order client. Mutations are durable commands, not direct engine calls.

### Recorder

The recorder scans partial/orphaned segments, finalizes only provably complete files, quarantines all others with an explicit gap, verifies disk headroom and manifest access, then connects to code-allowlisted Binance public endpoints.

### Worker

The worker verifies job storage, declared output paths, dataset compatibility, and bounded concurrency before claiming work. It reclaims only expired fenced claims and resumes from verified checkpoints.

### Migrator and healthcheck

The migrator acquires migration ownership, applies forward-only migrations, verifies the resulting schema, and exits. The healthcheck performs a bounded probe and does not initialize business modules.

## Readiness transitions

Readiness becomes false before rejecting/draining new work on shutdown and immediately when a required invariant becomes unsafe. Stable reason codes include configuration, schema, database durability, lease/fence, recovery, reconciliation, market sequence/freshness, clock, queue, recording evidence, disk, and critical journal failures.

A stale Binance connection makes the shadow engine degraded/unready but does not deliberately fail liveness. This avoids an automatic restart loop while allowing resynchronization, diagnostics, recording, and incident access.

## Graceful shutdown sequence

On the first stop signal, the lifecycle manager starts one monotonic deadline of at most 60 seconds and performs dependency-aware shutdown:

1. Mark readiness false and record the shutdown cause/deadline without secret data.
2. Close administrative/session/job intake and reject new entry intents.
3. Persist the effective pause/lock transition and cancel noncritical producers.
4. Stop market-event publication into strategy evaluation; retain only bounded state-resolution/reconciliation work.
5. Stop producers before draining their consumers. Prioritize commands already accepted, order/fill reducers, reservation/journal transactions, audit facts, and checkpoints over coalescible UI/metric updates.
6. Resolve or quarantine nonterminal simulated orders/plans; never blindly release an uncertain reservation.
7. Flush critical PostgreSQL transactions, inbox/outbox cursors, audit events, and the last verified run checkpoint.
8. Flush and finalize or quarantine decision-input/recorder segments using the crash-safe segment protocol.
9. Stop background renewers and workers after their protected work is safe.
10. Release each lease with an owner-and-current-token condition, last among protected operations.
11. Close PostgreSQL, HTTP/SSE, files, metrics, and other resources; report shutdown duration/result and exit.

The API stops accepting mutations before draining in-flight bounded requests and SSE connections. SSE delivery is not critical state and must not delay the engine or exceed the shutdown budget. Workers checkpoint and return/expire claims safely; the recorder prefers an explicit quarantined partial segment to an invalid final segment.

## Deadline and repeated signals

At the 60-second deadline, the process must terminate rather than hang indefinitely. It must not claim a clean shutdown or release a lease unless protected state reached its safe boundary. The owner leaves an unclean lease to expire, exits nonzero, and relies on a higher fencing token plus full startup recovery to reject stale work.

Where critical persistence is still available, the runtime records an unclean-shutdown/incident fact and last verified checkpoint before the deadline. If persistence is unavailable, absence of a clean marker and lease expiry force the next startup through full locked recovery. A repeated termination signal may request immediate exit, but it cannot cause an unfenced write or false clean marker.

## Failure-triggered lifecycle

- Lease/fence or critical database durability loss: immediately lock, stop entries, make unready, preserve uncertain reservations, and shut down/recover.
- Book gap/staleness: pause only the affected scope, invalidate the generation, remain live, and resynchronize.
- Journal/reconciliation failure: lock or pause the affected protected scope and require explicit recovery.
- Critical disk pressure: reject new jobs/decisions, preserve critical journal/audit state, finalize or quarantine segments.
- Queue overload: apply event-class policy; never silently lose critical state or execute stale work.

## Metrics, logs, and tests

Expose lifecycle state, readiness reason, startup/recovery duration, shutdown phase/duration, forced/unclean exits, in-flight work, queue drain, checkpoint/segment flush, lease release/expiry, and goroutine count with bounded labels. Logs/audit events carry instance, run, correlation, cause, and stable event code without credentials or raw secrets.

Tests cover every startup gate, cancellation propagation, component start/stop ordering, first/repeated signal, dependency failure, stuck noncritical consumer, lease loss during drain, database loss, segment flush failure, unclean deadline, goroutine leaks, and completion at or below 60 seconds.

## Known limitation

The maximum shutdown budget can force an unclean exit when a dependency remains unavailable. Safety comes from stopping intake, refusing false commits/releases, fencing the old owner, and mandatory locked recovery—not from pretending every shutdown can fully drain.
