# V1D D3 readiness

## Current decision

**D3 implementation and local API, PostgreSQL, frontend, and critical-browser
qualification are complete on the feature branch; hosted CI, merge, and
cumulative acceptance are pending.**

Backtest, replay, and shadow laboratories now preserve original input
identities, lifecycle capabilities, safe run manifests, deterministic
comparison, reproduction, audited exports, replay checkpoints/faults, and
public-only shadow decisions, accounting, inventory, P&L attribution, and data
health. The browser sends only execution fields supported by the closed server
contract; advanced assumptions are evidence, not an alternate simulator.

This is not formal V1D acceptance. D4 report/incident/alert operations, D5
hardening and seven-day readiness soak, and D6 safety certification remain
later sequential phases. B2, C6, and D5 retain separate verdicts.

## Safety decision

- No production-private exchange or real-order path was added.
- Shadow remains production-public data plus isolated virtual execution.
- Research controls are permission checked, CSRF protected, idempotent,
  reasoned, and exact-revision guarded by the server.
- Reproduction exports include safe identities and hashes, not raw request
  payloads, private exchange data, credentials, headers, signatures, or logs.
- Financial projections remain decimal strings and ledger attribution uses
  sealed journal entries.
- Lab outcomes explicitly separate platform correctness, strategy evidence,
  viability, maturity, and confidence from profitability claims.

## Browser decision

The D3 critical workflow passes Chromium desktop, Chromium tablet, Chromium
mobile, Firefox desktop, and WebKit desktop. The host lacks WebKit GTK and
GStreamer libraries, so WebKit was executed in the pinned
`mcr.microsoft.com/playwright:v1.61.1-noble` image. That run found and verified
the fix for a cross-browser controlled-form race by requiring functional state
updates for every guided field.

## Evidence

Commands and limitations are recorded in
[D3 local validation](evidence/v1d-d3-local-validation.md). Requirement
coverage is recorded in
[D3 traceability](../requirements/v1d-d3-traceability.md) and
[D3 source coverage](../requirements/v1d-d3-source-coverage.md).
