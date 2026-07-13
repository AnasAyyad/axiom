# ADR-0005: Resumable Server-Sent Events for browser updates

- **Status:** Accepted
- **Date:** 2026-07-12
- **Scope:** V1A API-to-browser live state

## Context

The administration UI needs one-way health, decision, simulated order/fill, risk, portfolio, alert, and job updates. Live delivery may disconnect or lag and cannot be the only copy of current state.

## Decision

Use versioned Server-Sent Events for browser live updates and versioned REST snapshots as the authoritative recovery source. Each event has a durable event ID, stream/schema version, monotonic stream revision, entity revision, UTC time, and correlation/causation identity.

Client recovery is `fetch snapshot with revision -> subscribe after revision -> apply only monotonic events`. `Last-Event-ID`/resume cursor replays retained durable outbox events. A cursor outside retention or a detected revision gap forces a fresh REST snapshot. Mutations remain authenticated, authorized, CSRF-protected, idempotent REST commands; SSE is never a command channel.

High-rate views may be sampled/coalesced by key. The shadow engine never waits for SSE delivery.

## Consequences

- Browser reconnection uses standard HTTP infrastructure and a simple one-way protocol.
- Durable revisions close the snapshot/stream race and tolerate API restarts.
- SSE needs bounded connection, buffer, retention, heartbeat, authorization, and Origin policies.
- It is unsuitable for binary/high-rate full book distribution; the UI receives projections.

## Rejected alternatives

- WebSocket for V1A UI state: unnecessary bidirectional complexity for the required flow.
- Polling only: higher latency/load and poorer operational timeline experience.
- Ephemeral in-memory pub/sub: loses events and cannot resume after restart.
- SSE as the sole truth: cannot recover a cursor beyond retention.

## Validation

Contract and browser tests cover snapshot/subscribe races, disconnect/resume, duplicates, out-of-order/stale events, authorization/Origin, slow clients, retention expiry, API restart, and fallback snapshot. Load tests prove bounded memory and that UI backpressure never stalls the engine.

## Revisit when

A proven bidirectional or binary/high-rate browser requirement cannot be served by REST commands plus SSE projections. A replacement must preserve durable revisions and snapshot recovery.
