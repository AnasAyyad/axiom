# V1D D3 local validation

## Candidate identity

| Field | Value |
|---|---|
| Merged D2 baseline | `31699cab207429fda4baa450b3633aa909b87790` |
| Branch | `v1d-d3-labs` |
| Product contract | `crypto_bot_v1_codex_spec.md`, Phase D3 |
| API contract | compatible `/api/v1` OpenAPI and generated Go/TypeScript models |
| PostgreSQL | disposable `postgres:18.4-alpine` database ending `_a11_test` |
| Formal C6 run | separate and not run by D3 |
| Formal D5 run | separate and not implemented by D3 |
| Production-private orders | impossible and not attempted |

## Current decision

**D3 implementation and local API, storage, frontend, and browser qualification
pass. Hosted CI, merge, and cumulative acceptance remain pending.**

## Validation record

The following checks passed on 2026-08-03 with Node 24.18.0, pnpm 11.12.0,
Go 1.26.5, and PostgreSQL 18.4 where applicable:

```text
make d3-contract-qualify GO=/tmp/axiom-go1.26.5/bin/go
make d3-api-qualify GO=/tmp/axiom-go1.26.5/bin/go
make d3-postgres-qualify GO=/tmp/axiom-go1.26.5/bin/go
make d3-frontend-qualify
node scripts/check-v1d-d3-boundary.mjs
GOMAXPROCS=2 make verify GO=/tmp/axiom-go1.26.5/bin/go
```

The cumulative gate passed formatting, generated-contract parity,
documentation and source policies, all Go and frontend tests, the Go race
detector, fuzz smoke, production builds, all 1,024 Compose profile
combinations, security scanners and their negative tests, and vulnerability
analysis. The host was carrying unrelated load, so the final run used two Go
scheduler threads: this retained the concurrent collector coverage while
keeping the immutable 25 ms latency thresholds unchanged. Isolated two-thread
qualification measured 0.872 ms rebalancing p99 and 12.044 ms trend-pipeline
p99.

The PostgreSQL test exercises durable job idempotency/quota/restart behavior,
original input and run-manifest projection, result identity, safe export fields,
absence of raw/private export fields, replay lifecycle, shadow single-session
quota, shadow runtime recovery, bounded shadow history, virtual inventory,
sealed-ledger P&L, and public-only safety constraints.

Frontend qualification contains 25 passing tests, including exact-string run
and shadow comparison, D1 response validation, route precedence, navigation,
state presentation, and accessible shared components.

The D3 browser workflow passed:

```text
chromium-desktop: pass
chromium-tablet: pass
chromium-mobile: pass
firefox-desktop: pass
webkit-desktop: pass in mcr.microsoft.com/playwright:v1.61.1-noble
```

The workflow creates and reopens backtests/replays, verifies original inputs,
registered evidence, replay pause/step/resume and checkpoints, starts a
simulation-only shadow, verifies decisions/risk actions, orders/fills, owned
inventory, sealed-ledger attribution and public-data health, checks responsive
overflow and keyboard focus, runs WCAG 2.2 AA serious/critical axe checks, and
performs a graceful shadow stop.

## Corrected browser finding

WebKit exposed a controlled-form race in which rapid sequential input events
could overwrite a prior field because callbacks captured an older form object.
Every guided field now uses a functional state update. The corrected WebKit
workflow passes; the finding was not waived.

## Tooling and cleanup

The host lacks WebKit's native GTK/GStreamer dependencies. The pinned official
Playwright image supplied them without modifying the host. The disposable
PostgreSQL container was stopped and auto-removed after the passing test. The
shadcn post-build checklist was applied; imports, dependencies, lint,
TypeScript, and executable Playwright validation pass.

## Explicit exclusions

- No D4 scheduled-report or complete incident/alert delivery engine is claimed.
- No D5 reference-server hardening or seven-day readiness soak is claimed.
- No D6 final V1 safety certification is claimed.
- D3 does not replace B2, C6, or D5 evidence and does not prove profitability.
