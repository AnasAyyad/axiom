# ADR-0019: Generic multi-exchange console and simulation commands

- **Status:** Accepted
- **Date:** 2026-07-23
- **Scope:** V1B/B8 API, frontend, replay faults, report exports, and SSE

## Context

B8 must expose the B1-B7 multi-exchange and research evidence through one
versioned operator console without weakening the V1 real-money lock. The A11
Binance, Trend, portfolio, replay, shadow, incident, and audit routes are
already published aliases and must remain compatible. REST snapshots are
authoritative, while live updates use the resumable SSE decision in ADR-0005.

Inventory, rebalancing, and replay faults need especially explicit boundaries:
inventory cannot be netted across owners, rebalancing cannot acquire execution
authority, and a browser-selected replay fault must be deterministic evidence
rather than an arbitrary runtime mutation.

## Decision

The OpenAPI 3.1 contract owns generic exchange, opportunity, strategy,
inventory, rebalancing, champion/challenger, replay-fault, and report-export
resources under `/api/v1`. Existing A11 routes remain available as aliases.
Generated Go and strict TypeScript types derive from that contract.

Every paginated B8 snapshot includes the global durable outbox
`snapshot_revision`. The browser completes its REST snapshots before opening
SSE after that revision, ignores non-monotonic events, refreshes snapshots on a
gap, and restarts without a cursor after bounded retention expiry.

Inventory rows retain exchange, asset, strategy version, experiment, portfolio,
and virtual ownership dimensions. `combined_balance` is contractually `false`.
Rebalancing resources are immutable reviewed evidence;
`execution_available` is contractually `false`, and the UI renders route facts
plus a manual checklist without a transfer or withdrawal control.

Replay faults are accepted only for a `QUEUED` replay, with an expected schedule
revision, idempotency key, authenticated actor/session, reason, unique event
ordinal, and a supported deterministic fault kind. Migration 000020 stores the
immutable schedule. Job materialization wraps the verified dataset source with
the existing deterministic `replay.FaultSource`; injection writes immutable
audit and outbox evidence. No fault path receives an exchange client.

Champion/challenger exports are deterministic JSON or CSV renderings of an
immutable B7 report. The command and rendered payload hash are persisted.
Exports remain simulation-only research evidence and are not profitability
claims.

## Consequences

- Operators get one exchange-neutral workflow while retained A11 links and API
  consumers continue to work.
- Quality, confidence, freshness, provenance, recovery, and cost attribution
  are visible instead of inferred by the browser.
- REST queries perform read-time projections over immutable B4-B7 evidence;
  the outbox revision supplies a common reconciliation boundary.
- Fault scheduling is intentionally unavailable once a worker claims a replay.
  A changed schedule requires a new queued replay or a valid new revision.
- The runtime gains no authenticated exchange transport, private endpoint,
  external order, transfer, withdrawal, leverage, or short-selling capability.

## Rejected alternatives

- A combined cross-exchange balance: rejected because it erases ownership and
  inventory-risk dimensions.
- Browser-side joins over exchange-specific endpoints: rejected because they
  cannot provide one authoritative revision or server-validated provenance.
- Executable rebalancing buttons: rejected because B6 and V1 explicitly prohibit
  external asset movement.
- Mutable faults on a running replay: rejected because they race worker claims
  and break reproducibility.
- WebSocket live updates: rejected because the console is server-to-browser and
  the retained, resumable SSE boundary already satisfies that direction.

## Validation

- OpenAPI generation and drift checks for Go and TypeScript.
- API boundary, cursor, idempotency, validation, and SSE tests.
- PostgreSQL 18 clean install through 000020 and exact 000019-to-000020 upgrade.
- Replay fault determinism, immutable audit/outbox evidence, and race tests.
- Strict TypeScript, lint, component tests, axe accessibility checks, production
  build, and Playwright desktop/mobile navigation and overflow checks.
- Source and binary prohibited-capability scans.
- Cumulative B4-B8 qualification, committed-source image reproducibility,
  image-backed Compose smoke, SPDX SBOM, and HIGH/CRITICAL scan.

## Revisit when

- V2 explicitly authorizes a separately reviewed authenticated exchange or
  external asset-movement boundary.
- Snapshot projections require a dedicated materialized read model to meet a
  measured SLO.
- SSE retention, event volume, or client count exceeds the bounded A11 design.
