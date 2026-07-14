# ADR-0001: Go modular monolith with an in-process hot path

- **Status:** Accepted
- **Date:** 2026-07-12
- **Scope:** V1A backend architecture

## Context

Strategy, allocation, risk, simulation, accounting, and recovery require low-latency calls, consistent versions, deterministic ordering, and atomic safety invariants. V1A is a single-server deployment and has no demonstrated need for distributed messaging or microservices.

## Decision

Build one Go modular monolith, packaged as `/app/platform` with `api`, `trader --mode shadow`, `recorder`, `worker`, `admin migrate`, and `healthcheck` subcommands. Business logic lives behind explicit internal module boundaries.

The shadow decision path from market view through strategy, allocator, risk, planner, simulator, and accounting runs in one process with typed calls/events. It has no internal HTTP/RPC, JSON hop, or database query between decision modules. Separate API, recorder, worker, and one-shot migrator processes are allowed at operational boundaries; they coordinate durably through PostgreSQL and datasets.

## Consequences

- Domain boundaries remain explicit without distributed failure modes in the hot path.
- Backtest, replay, paper, and shadow reuse the same authoritative modules.
- Deployment and observability are simpler, and profiling can target real bottlenecks.
- A fault can affect the whole runtime, so bounded concurrency, fencing, checkpoints, and graceful recovery are mandatory.
- Scaling is primarily vertical or by independent offline jobs/recording assignments; this is not HA.

## Rejected alternatives

- Microservices with internal HTTP/RPC: adds serialization, version skew, latency, and partial failures without a V1A requirement.
- Kafka/NATS/Redis coordination: PostgreSQL durability and an in-process bus meet current needs; extra infrastructure is unjustified.
- One undifferentiated package/process: violates explicit domain and operational ownership boundaries.

## Validation

Architecture tests/package review must prevent exchange DTO leakage and forbidden dependencies. Benchmarks must show no frontend/database dependency in candidate evaluation. Replay, race, overload, and shutdown tests must pass with bounded resources.

## Revisit when

A measured bottleneck, availability objective, independent scaling requirement, or failure-isolation need cannot be met without a process boundary. A superseding ADR must preserve determinism, fencing, accounting, and the production-order lock.
