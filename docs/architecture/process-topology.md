# V1A process topology

- **Status:** Normative architecture contract; A3 in-process boundary implemented
- **Scope:** Single-server V1A deployment

The A3 runtime establishes the bounded in-process event and pipeline contracts.
Process-specific engines and durable PostgreSQL coordination remain assigned to
their later implementation phases; this document does not claim those services
are operational yet.

## Packaging

V1A is packaged as one `/app/platform` binary with explicit subcommands. Business logic remains behind focused backend module boundaries; subcommands only select and compose a runtime.

| Logical role | Command | Instances | Exchange access | Credentials | Primary responsibility |
|---|---|---:|---|---|---|
| API | `platform api` | 1 | None | Application auth secrets only | REST, authentication, administrative command intake, SSE fan-out, embedded React assets |
| Shadow engine | `platform trader --mode shadow` | 1 active owner per protected shadow resource | Binance public Spot REST/WebSocket only | None | Market hot path, Trend, allocator, risk, simulation, virtual ledger, decision-input recording |
| Recorder | `platform recorder` | 1 per declared recording assignment | Binance public Spot REST/WebSocket only | None | Broad raw and normalized recording, segment finalization, manifests |
| Worker | `platform worker` | Bounded/scalable | None | None | Offline backtest, replay, and report jobs against approved read-only datasets |
| Migrator | `platform admin migrate` | One-shot | None | PostgreSQL migration role | Forward-only schema migration |
| Probe | `platform healthcheck` | Ephemeral | None | None | Container-local liveness/readiness probe |

`engine-binance-testnet`, `engine-bybit-demo`, authenticated account readers, and signed transports belong to later V1C scope and are absent from the V1A source, binary, configuration, image, and Compose profiles. A production engine is prohibited in every V1 release.

## Deployment graph

```text
Browser
  |
  v
api --------------------------> PostgreSQL
 |                                 ^  ^
 | SSE from durable revisions      |  |
 +<--------- outbox consumer ------+  |
                                      |
shadow engine ------------------------+
  | public REST/WS        | Parquet decision evidence
  v                       v
Binance public Spot     dataset storage
  ^                       ^
  | public REST/WS        | broad Parquet recording
recorder -----------------+

worker <---- PostgreSQL jobs/checkpoints ----> read-only datasets
migrate -----------------------------> PostgreSQL
```

PostgreSQL and dataset volumes are not public. The browser reaches only the API through the deployment's edge. The API cannot call an exchange order endpoint and cannot invoke engine behavior through an unrecorded direct call.

## Communication rules

- Strategy, allocator, risk, execution, simulation, and accounting calls inside the shadow engine are in-process typed calls/events.
- Administrative mutations are durable command records with authentication, authorization, CSRF protection, an idempotency key, actor, reason, and audit identity.
- A consumer claims a command through the inbox/command protocol and records an idempotent result. Process restarts may redeliver; they must not duplicate the effect.
- Engine-to-API business events commit with their state change to a durable outbox. The API consumes monotonic revisions and can resume after loss.
- `LISTEN/NOTIFY` is an optional wake-up hint only. Consumers always query durable rows from their last committed cursor.
- High-rate UI views are sampled or coalesced projections. The engine never blocks on browser delivery.
- Workers claim durable jobs with bounded concurrency and leases. Datasets are read-only except for declared output and checkpoint locations.

## Shadow ownership and fencing

Exactly one shadow engine may own each protected resource, including a live shadow session/portfolio and its virtual ledger. Before recovery or decisions, it acquires a PostgreSQL lease that returns a monotonically increasing fencing token. Every protected command, checkpoint, reservation/order mutation, and lease renewal carries that token or is guarded by the same database ownership condition.

An overlapping instance with an older or missing token cannot write protected state. Renewal failure or loss of database durability immediately prevents new plans, moves the engine to `LOCKED`, makes it unready, and begins safe shutdown/recovery behavior. Lease release is last during clean shutdown and is conditional on the current owner/token.

The standalone recorder may have its own recording-assignment lease. Recorder data is not an exact substitute for the shadow engine's embedded decision-input recorder: different subscriptions and receive ordering can differ. If decision evidence cannot be preserved, the shadow engine pauses new decisions.

## Startup order

All processes begin unready. Decision-capable components begin `PAUSED`. The
shadow engine may first perform the bounded, non-side-effectful local preflight
defined in [Lifecycle](lifecycle.md#common-local-preflight); that preflight is
not full build/configuration validation. It then follows this protected gate:

```text
connect to PostgreSQL with the least-privilege runtime role
-> acquire fenced shadow ownership
-> enter LOCKED
-> validate build/configuration safety manifest
-> load immutable configuration versions
-> recover checkpoints, virtual orders, reservations, and journal projections
-> reconcile simulator and journal state
-> synchronize required Binance public market data
-> validate decision-input recording, clock, queues, disk, and risk inputs
-> READY but still PAUSED
-> explicit authenticated and audited strategy/session activation
```

Readiness means the process can safely serve its declared role; it does not mean strategy entries are enabled. There is no automatic transition from `PAUSED` or `LOCKED` to active entry evaluation.

## Shutdown order

Every runtime uses a shared lifecycle manager and a maximum 60-second graceful-shutdown budget. The shadow engine stops entries and command intake, becomes unready, checkpoints and drains critical state, resolves or quarantines nonterminal simulated orders, safely releases reservations, flushes journal/outbox/audit/decision evidence, and releases its lease last. The recorder finalizes or quarantines active segments. Workers checkpoint or return jobs safely. See [Lifecycle](lifecycle.md).

## Readiness and health

Liveness answers whether the process loop can respond. Readiness is role-specific and fails when required PostgreSQL state, ownership/fencing, disk, recorder evidence, book freshness/sequence, clock, configuration, reconciliation, or queue health is unsafe. A stale exchange makes the shadow engine degraded/unready; it must not cause an automatic restart loop that repeatedly discards diagnostic state.

## Required tests and metrics

- Overlapping shadow engines cannot own or mutate the same resource.
- Lease expiry, renewal failure, database loss, process kill, and stale-token writes fail closed.
- Durable command and outbox redelivery is idempotent.
- No V1A process accepts credentials or reaches private/order routes.
- Queue depth/age, lease state/renewal, fencing rejection, command lag, outbox lag, readiness reason, recovery duration, and shutdown duration use bounded-cardinality metrics.

## Known limitation

This is deliberate single-server topology, not high availability. A database or host outage causes downtime; ownership fencing prevents split brain but does not provide automatic failover.
