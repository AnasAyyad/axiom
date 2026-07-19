# A11 local implementation evidence

**Status:** Implemented and locally validated. Formal acceptance remains blocked
by A7 and formal A8/A9/A10 acceptance.

**Candidate branch:** `a11-research-console`, stacked from A10 commit
`e63d6ed69c5cc549a052ac7127f715e84e17dedc`. The primary A11 implementation is
commit `faabd012eb44484c792cb2f44f7f30bba6cb5689`; live-stack fixes are commit
`d5119bfd9d5335b6c469fb99ec59f150d6496f76`; final race and qualification
alignment is commit `73902b3e11d528d0c204642324253a5c96addf0e`. This evidence
does not merge or pre-approve A7, A8, A9, A10, A11, or the V1A release gate.

## Implemented scope

- Authoritative OpenAPI contract for the exact 30 required A11 method/path
  operations, generated Go models, and generated TypeScript schemas.
- One-time hashed owner bootstrap, Argon2id authentication, opaque hashed
  sessions, signed CSRF, exact Origin checks, authorization, durable rate
  limiting, expiry, rotation, revocation, and the five-session limit.
- Forward-only migration `000010`, least-privilege role grants, immutable audit
  linkage, optimistic revisions, idempotent durable commands, job quotas,
  fenced worker leases, crash reclaim, run manifests/results/checkpoints, and
  fail-closed startup recovery.
- Production worker composition for durable backtest and replay jobs through
  the A10 Trend, A9 allocation/risk, and A8 simulation pipeline. Replay pause,
  resume, and single-step use durable controls and immutable checkpoints.
- Production-public shadow composition with Binance collectors, exact decision
  inputs, Trend evaluation, allocation/risk checks, simulation-only plans,
  fills, virtual journal/projections, outbox events, graceful stop, and durable
  evidence. Startup and risk recovery remain paused until all gates pass.
- Recorder metadata bootstrap, cumulative public/decision-input datasets, and
  lossless contiguous-prefix flushing that retains an in-flight raw/canonical
  suffix without dropping or reordering it.
- Cursor-bound pagination and versioned resumable SSE with durable revisions,
  retention checks, stream quotas, heartbeats, bounded batches, and browser
  snapshot recovery.
- Routed React console for login, command center, Binance, virtual portfolio,
  risk, Trend, backtest, replay, public-live shadow, incidents, and audit. It
  uses generated contracts, Zod boundaries, accessible project primitives,
  responsive layouts, and an ECharts adapter.
- Persistent `REAL TRADING DISABLED` and virtual/replay/backtest/shadow labels.
  API and worker processes have no exchange egress or exchange secrets; the
  engine and recorder have only production-public Binance egress and no signing
  or external-order capability.

## Current local commands and results

| Command | Result |
| --- | --- |
| `make a11-sqlc` with pinned Go/sqlc | Passed; reviewed SQL regenerated and PostgreSQL packages compiled with the destructive DSN disabled. |
| `make a11-postgres-qualify` against a clean PostgreSQL 18 `_a11_test` database | Passed; authentication, recovery, immutable commands/audit, pagination, risk recovery, quotas, worker completion/failure/crash reclaim, shadow safety, resumable streams, and privilege/session rotation were exercised. |
| `make a11-contract-qualify` | Passed; generated contracts were current and the exact 30-operation boundary audit passed. |
| `make a11-api-qualify` | Passed; authentication, API, bootstrap, health/readiness, configuration, and console packages passed. |
| `make a11-frontend-qualify` | Passed; strict TypeScript, ESLint, Vitest/axe, and production Vite build passed. |
| `make a11-e2e-qualify` | Passed in Chromium desktop and Pixel 7 viewports (`2 passed`); login, routed virtual workflows, durable controls, SSE recovery, shadow stop, and logout were exercised in the deterministic browser environment. |
| `make a11-security-qualify` | Passed; A11 ownership, secret/prohibited-capability scans, negative scanner tests, and A6/A7 binary boundaries passed. |
| `make verify` with the pinned toolchains | Passed from the final aligned source: formatting, generated contracts, documentation, vet/staticcheck/ESLint, source policies, all Go/Vitest tests, full Go race suite, five fuzz targets, embedded production builds, all 128 Compose profile renders, security scans, and `govulncheck` passed. `govulncheck` reported zero called vulnerabilities. |
| Clean A11 image build plus `scripts/inspect-image.sh axiom:a11-local` | Passed; embedded frontend, non-root user, read-only runtime posture, and minimal/prohibited binary boundaries passed. |
| `make compose-smoke IMAGE=axiom:a11-local` from clean commit `73902b3` | Passed on PostgreSQL 18 with API, shadow engine, recorder, worker, Prometheus, and Grafana. All four app roles became healthy; authenticated API login/status/logout, hardened containers, scrapes, and dashboard provisioning passed. |

## Qualification interpretation and remaining formal gates

- The browser workflow is deterministic acceptance using contract-shaped
  fixtures; PostgreSQL and image-backed Compose separately exercise the real
  persistence, role, recovery, recorder, and runtime composition boundaries.
- A clean live shadow session still requires current production-public health,
  valid metadata, at least 200 completed four-hour candles, disk headroom, and
  completed locked recovery. The system fails closed while any prerequisite is
  absent.
- Strategy evidence remains Tier B/local and viability remains undetermined.
  No profitability or production-readiness claim is made.
- A7 still requires formal continuous 72-hour qualification. Formal A8, A9,
  and A10 acceptance remains prerequisite to formal A11 acceptance.
- No private exchange credential, authenticated exchange transport, signer,
  production broker, withdrawal, transfer, margin, futures, leverage, shorting,
  testnet, demo, or risk-bypass capability was added.
- A11 and V1A readiness checkboxes remain unchecked. Traceability rows are
  `Implemented`, never `Verified`, until prerequisite and formal evidence gates
  close.
