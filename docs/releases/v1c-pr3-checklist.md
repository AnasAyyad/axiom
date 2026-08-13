# V1C PR3 C6 checklist

## Sandbox API and console

- [x] Expose redacted Binance Spot Testnet and Bybit Demo account, engine,
  arm, cap, order, fill, reconciliation, reset, and qualification projections.
- [x] Add versioned OpenAPI operations and regenerate Go and TypeScript
  contracts.
- [x] Keep the API credential-free and persist submissions through the existing
  intent, allocator, risk, planner, and durable dispatcher boundary.
- [x] Require session authorization, exact RBAC, Origin/CSRF, idempotency,
  revisions, reason, and tamper-evident audit evidence for mutations.
- [x] Require password/TOTP reauthentication and a purpose-, session-, source-,
  and reason-bound one-use grant for arm and unlock.
- [x] Keep cancel, query, reconciliation, and safe recovery available while new
  entry is blocked.
- [x] Display persistent test/demo environment labels and
  `REAL TRADING DISABLED`, with loading, empty, stale, degraded, forbidden,
  error, and recovery states.
- [x] Keep passwords and TOTP values local to the active form and clear them
  immediately after authorization.

## Storage, observability, and qualification

- [x] Add forward-only migration `000024` for immutable qualification evidence,
  redacted order-integrity projection, and immutable engine recovery events.
- [x] Keep the C6 observer on a dedicated least-privilege PostgreSQL role and
  keep engine evidence access append-only.
- [x] Add bounded metrics, alerts, and Grafana panels for order/fill integrity,
  unknowns, reconciliation, arms, caps, resets, engine health, recovery, alert
  latency, soak state, and memory trend.
- [x] Add the exact 72-hour, default-off formal observer with create-once
  `0440` terminal evidence and `profitability_evidence=false`.
- [x] Ensure smoke mode can never set `qualified=true` or fabricate a formal
  duration.
- [x] Cover the complete deterministic chaos scenario set without exchange
  network access.

## Non-soak qualification

- [x] Pass contracts, backend, frontend, accessibility, fixture/integrated
  Playwright, race, fuzz, and deterministic C6 smoke gates.
- [x] Pass PostgreSQL 18.4 clean install and exact B8 upgrade.
- [x] Pass all 1,024 Compose profile renders and the closed security boundary.
- [x] Pass complete `make verify` and `make v1c-pr3-local-qualify`.
- [x] Freeze a clean implementation commit and pass image reproducibility,
  non-root/read-only inspection, compiled-absence scan, image-backed Compose
  smoke, SPDX SBOM, and current Trivy vulnerability/secret/configuration scans.
- [x] Commit evidence documentation and push `v1c-c6-console-soak`.

## Deferred formal decision

- [x] Run one later exact-source 72-hour C6 qualification.
- [x] Review the sealed terminal evidence.
- [ ] Record V1C owner and security acceptance.

The formal target is intentionally manual and must not be invoked by any
aggregate above. No new authenticated exchange order is part of PR3
implementation qualification.
