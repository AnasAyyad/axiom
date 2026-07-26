# B8 local validation evidence

## Status

B8 implementation is complete and targeted qualification is in progress on
2026-07-23. Clean PostgreSQL installation through migration 000020 and exact
000019-to-000020 upgrade have passed. Generated contracts, strict TypeScript,
frontend unit tests, and the desktop/mobile B8 Playwright workflow also pass.

This document is intentionally not final release evidence yet. The cumulative
B4-B8 run, committed-source image, image reproducibility, image-backed Compose
smoke, SPDX SBOM, Trivy gate, final source identity, and evidence commit are
recorded only after those gates complete.

Formal acceptance is not claimed. It remains held by A7/V1A, the deferred B1
and B2 72-hour qualifications, formal predecessor acceptance, and Product,
Security, QA, and SRE approval.

## Implemented authority and safety

- Generic versioned exchange, opportunity, strategy, inventory, rebalancing,
  champion/challenger, replay-fault, and report-export resources are defined in
  OpenAPI and generated for Go and TypeScript.
- A11 Binance, Trend, portfolio, replay, shadow, incident, audit, and command
  aliases remain available.
- REST snapshots expose a global outbox revision before resumable SSE.
- Opportunity detail exposes immutable legs, inventory impact, recovery,
  cost attribution, and evidence timelines.
- Inventory is isolated by exchange, asset, strategy version, experiment,
  portfolio, and virtual owner. The API contract fixes
  `combined_balance=false`.
- Rebalancing remains advisory-only. The API fixes
  `execution_available=false`; the console has no transfer control.
- Replay faults are queued-replay-only, revisioned, idempotent, immutable,
  actor/session bound, deterministic, and simulation-only.
- Report exports are deterministic immutable JSON/CSV research artifacts and
  retain the no-profitability-claim disclaimer.
- B8 introduces no private exchange route, authenticated exchange transport,
  external order, withdrawal, transfer, leverage, lending, staking, or short
  selling.

## Targeted qualification already passed

- Go compilation and targeted storage/API/replay unit tests.
- PostgreSQL 18.4 clean install 000001-000020 and exact 000001-000019 to 000020
  upgrade in `axiom_b8_clean_b8_test` and `axiom_b8_upgrade_b8_test`.
- PostgreSQL qualification executed every generic read projection plus missing
  detail paths, then covered immutable fault schedules, expected revisions,
  idempotent replay, deterministic materialized injection, immutable audit
  emission, idempotency conflict, and simulation-only resources.
- OpenAPI Go and TypeScript contract generation.
- Strict TypeScript compilation and frontend unit tests: 15 tests passed,
  including combined-balance rejection.
- B8 component axe validation for the keyboard-native opportunity detail row.
- Playwright B8 workflow: desktop and Pixel 7 projects passed navigation,
  detail expansion, inventory isolation, advisory review, report export,
  keyboard reachability, and horizontal-overflow checks.

## Pending final qualification

- Run every B4-B8 model, race, fuzz, benchmark, PostgreSQL, independent
  research, API, frontend, security, live-browser, and repository verification
  gate in one uninterrupted invocation.
- Commit qualifying B8 source before producing image evidence.
- Build and inspect the committed-source image with `DIRTY=false`.
- Pass runtime image reproducibility and image-backed Compose smoke through
  migration 000020.
- Generate and retain the SPDX SBOM.
- Run Trivy HIGH/CRITICAL vulnerability, secret, misconfiguration, and license
  scanners with unfixed findings included.
- Record exact source, image, runtime fingerprint, SBOM, and scan identities.

## Explicit holds

- The continuous B1 and B2 72-hour qualifications remain deferred and are not
  claimed by B8.
- A7/V1A and formal B1-B7 predecessor acceptance remain pending.
- Product, Security, QA, and SRE acceptance remains pending.
- Console usability, platform correctness, and research maturity do not prove
  profitability.
