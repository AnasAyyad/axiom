# Axiom V1A system overview

- **Status:** Normative A0 architecture contract
- **Scope:** V1A only
- **Authority:** `crypto_bot_v1_codex_spec.md` and the approved V1A implementation plan

## Purpose

Axiom V1A is a production-quality research and shadow-trading platform. It consumes Binance Spot public market data, records and replays that data, evaluates the Trend strategy, allocates virtual capital, applies central risk policy, simulates execution, and maintains an exact virtual journal.

This document defines the target architecture. It does not claim that application services already exist or are runnable. Implementation and acceptance evidence are delivered by phases A1 through A11.

## Absolute safety boundary

V1A has no external order side effects. It must not contain or accept:

- authenticated exchange credentials or a signing transport;
- private account, order, cancel, transfer, withdrawal, margin, futures, leverage, borrowing, lending, staking, or short-selling operations;
- Binance Testnet, Bybit Demo, or production-order processes;
- an `execution_mode=live` value, production broker, hidden switch, placeholder interface, endpoint, route, profile, or UI control that could enable external orders.

The only enabled modes are `backtest`, `replay`, `paper`, and `shadow`. All brokers in V1A are simulators. `shadow` reads live production **public** data but submits nothing to Binance. V1A permits spot instruments and owned virtual inventory only; a simulated sell may never exceed the portfolio's owned quantity.

## System context

```text
Operator browser
      |
      | HTTPS/REST + resumable SSE
      v
     API <------ PostgreSQL ------> worker
      ^              ^                |
      | durable      | journal,       | read-only datasets
      | outbox       | commands,      v
      |              | leases      Parquet datasets
      |              ^                ^
      |              |                |
shadow engine <------+-------> decision-input recorder
      ^                               ^
      | Binance public REST/WS        |
      +-------------------------------+

standalone recorder <--- Binance public REST/WS ---> Parquet datasets
```

Only the shadow engine and recorder may make outbound exchange connections, and only to code-owned Binance public Spot hosts and routes. The API, worker, database, and browser have no exchange credentials. PostgreSQL and dataset storage are private deployment resources, not browser-facing services.

## Architectural boundaries

The backend is a modular monolith with explicit domain boundaries:

- exchange adapters decode and normalize exchange-specific public data;
- market data owns sequence validation, books, completed candles, quality, and immutable versioned views;
- strategies return opportunities or desired position changes only;
- the allocator owns virtual-capital and inventory reservations;
- the risk engine is the only authority that approves, rejects, pauses, or locks;
- execution planning and simulation turn approved intents into virtual orders and fills;
- accounting owns the immutable, balanced, multi-commodity journal;
- reconciliation compares virtual projections, reservations, orders, fills, and journal state;
- recording owns raw and normalized append-only data and reproducibility manifests;
- API and reporting expose state but never calculate authoritative finance or risk results.

The versioned [asset registry](../configuration/asset-registry.md) is an
independent eligibility boundary. Only owner-supplied `approved` assets may be
used by simulation; unknown, scan-only, blocked, or pending-review assets fail
closed for entries and fills.

A strategy cannot call a broker, modify a balance, post a journal entry, or bypass allocation and risk. Exchange-specific models stop at the adapter boundary. Financial domain APIs expose project-owned decimal types, never a third-party decimal type or binary floating point.

## Authoritative data and control flows

The shadow decision path is in-process:

```text
public event -> validated market view -> Trend -> allocator -> risk
             -> simulated plan/order/fill -> virtual journal
```

There are no internal HTTP/RPC calls, JSON hops, or database round trips between strategy, allocator, risk, and execution modules. Critical transitions are persisted at explicit transaction boundaries before being reported as durable. Analytics, reports, browser delivery, and notifications remain outside candidate evaluation.

Administrative mutations enter a durable, idempotent PostgreSQL command/inbox path. The API must not use an unaudited in-memory shortcut to an engine. Engine business events commit to the durable outbox; PostgreSQL `LISTEN/NOTIFY` may wake consumers but is never the source of truth.

PostgreSQL is authoritative for transactional state and metadata. Parquet with Zstandard compression stores high-volume raw and normalized market events. Backtest, replay, paper, and shadow use the same strategy, allocator, risk, execution, and accounting packages.

## Safety and failure posture

Axiom fails closed. Every decision-capable runtime starts paused; the shadow engine enters a fenced locked recovery gate. New entries remain disabled until configuration, ownership, persistence, reconciliation, required market data, clock, and recording evidence are healthy. Becoming ready does not automatically activate a strategy.

Stale or gapped data, queue overload, lease loss, database durability loss, journal failure, reconciliation mismatch, critical disk pressure, or unknown/inconsistent state pauses or locks the affected scope. `PAUSED` and `LOCKED` never auto-unpause. Recovery and activation require explicit authorized, audited action after current checks pass.

## Interfaces and outputs

External outputs are versioned REST resources, resumable SSE events, Prometheus metrics, redacted structured logs, audit records, reports, PostgreSQL backups, and verifiable dataset manifests. Every displayed balance, order, fill, position, and result must be labelled virtual, backtest, replay, paper, or shadow.

## Required evidence

Later phases must prove this architecture with prohibited-capability scans, outbound-host inspection, adapter contract tests, deterministic replay hashes, race and overload tests, lease-fencing drills, journal and reservation properties, crash/restart tests, backup/restore drills, and the 72-hour Binance public-data soak.

## Known limitations

- V1A supports Binance public Spot data for BTC-USDT and ETH-USDT initially; it has no authenticated exchange integration.
- Single-server Compose is not high availability. Fencing prevents simultaneous owners but does not eliminate downtime.
- Simulated fills based on public data cannot prove production fill, hidden liquidity, queue position, market impact, or production latency.
- Platform readiness and strategy viability are separate; neither shadow nor replay results demonstrate profitability.

## Related documents

- [Process topology](process-topology.md)
- [Hot path](hot-path.md)
- [Concurrency and ownership](concurrency.md)
- [Deterministic replay](deterministic-replay.md)
- [Recovery](recovery.md)
- [Data storage](data-storage.md)
- [Lifecycle](lifecycle.md)
