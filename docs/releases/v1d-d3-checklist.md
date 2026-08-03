# V1D D3 checklist

## Product implementation

- [x] Provide guided backtest and replay creation over approved immutable
  configuration, dataset, strategy, research-generation, seed, speed, and
  incident-window identities.
- [x] Preserve durable history, reopen, exact-revision lifecycle controls,
  cancellation, deterministic reproduction, comparison, and audited export.
- [x] Project safe input and run manifests, build/source identity, hashes,
  confidence, result identity, and checkpoint evidence without secrets.
- [x] Provide replay timing, pause/resume/step, ordinal navigation, durable
  checkpoints, incident inputs, and approved deterministic faults.
- [x] Provide bounded shadow history, current decisions/risk actions, simulated
  orders/fills, owned virtual inventory, sealed-ledger P&L attribution,
  public-data health, immutable assumptions, and comparison.
- [x] Separate strategy viability from platform readiness and state that
  historical, replay, shadow, demo, and testnet results do not prove
  profitability.
- [x] Preserve spot-only, owned-inventory, fail-closed, no-production-order,
  redaction, and secret boundaries.

## Local qualification

- [x] Pass generated contract parity and D1/D2/D3 source boundaries.
- [x] Pass D3 API, authorization, lifecycle, replay, backtest, and projection
  tests.
- [x] Pass PostgreSQL 18.4 durable lifecycle, restart, quota, manifest,
  shadow-projection, and export-redaction integration qualification.
- [x] Pass strict typecheck, lint, 25 frontend unit/component tests, and the
  production frontend build.
- [x] Pass the D3 workflow in Chromium desktop/tablet/mobile, Firefox desktop,
  and WebKit desktop, including serious/critical WCAG 2.2 AA and overflow
  checks.
- [x] Pass file policy, documentation, secret, and prohibited-capability
  boundaries.
- [x] Pass the cumulative `make verify` gate, including Go race and fuzz
  checks, all 1,024 Compose profile combinations, and vulnerability analysis.

## Delivery and cumulative decision

- [x] Commit and push `v1d-d3-labs`.
- [ ] Pass hosted CI, including the pinned five-project D3 browser job.
- [ ] Merge the D3 pull request into `main` before starting D4.
- [ ] Preserve separate accepted B2, C6, and D5 evidence for final D6 review.

D3 local qualification does not certify V1D and does not waive an earlier or
later formal gate.
