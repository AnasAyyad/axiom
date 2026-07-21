# A11 local implementation evidence

**Status:** Local implementation qualification passed. The current A11
evidence/reporting, shadow, SSE, filtering, and integrated-workflow delta
passed clean PostgreSQL 18, real browser, full repository, image-inspection, and
image-backed Compose gates. Formal acceptance remains blocked by A7 and formal
A8/A9/A10 acceptance.

**Candidate branch:** `a11-research-console`, stacked from A10 commit
`e63d6ed69c5cc549a052ac7127f715e84e17dedc`. The primary A11 implementation is
commit `faabd012eb44484c792cb2f44f7f30bba6cb5689`; live-stack fixes are commit
`d5119bfd9d5335b6c469fb99ec59f150d6496f76`; the last fully qualified committed
baseline is `73902b3e11d528d0c204642324253a5c96addf0e`. Commit `6adb93e`
contains its evidence alignment. The qualified delta remains unmerged. This
evidence does not merge or pre-approve A7, A8, A9, A10, A11, or the V1A release
gate.

## Implemented scope

- Authoritative OpenAPI contract for the exact 30 required A11 method/path
  operations, generated Go models, and generated TypeScript schemas.
- One-time hashed owner bootstrap, Argon2id authentication, opaque hashed
  sessions, signed CSRF, exact Origin checks, authorization, durable rate
  limiting, expiry, rotation, revocation, and the five-session limit.
- Forward-only migrations `000010` and `000011`, least-privilege role grants,
  immutable audit linkage, optimistic revisions, idempotent durable commands,
  job quotas, fenced worker leases, crash reclaim, run
  manifests/results/checkpoints, and fail-closed startup recovery.
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

The baseline results remain historical evidence for commit `73902b3`. The
current-delta results below were collected on 20 July 2026 with Go 1.26.5,
Node 24.18.0, pnpm 11.12.0, sqlc 1.31.1, PostgreSQL 18.4, and the pinned
Playwright Chromium. Secret-file contents were never emitted.

| Command                                    | Result                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Exact-source PostgreSQL and setup          | A dedicated `axiom_a11_test` database was recreated before each proof. `TestA11PostgresAuthenticationCommandsAndConsoleQualification` and `TestA11PrepareIntegratedBrowserEnvironment` passed over the container Unix socket using immutable dataset hash `3ba71bd974e09618322676281a680240d9a6b459a864b95292e68cb98ab385a4` and all 3,072 events.                                                                                                                                          |
| Integrated real-browser workflow           | Passed in 1.4 minutes against a clean database and exact-source API, worker, public shadow, and recorder roles. It covered login, backtest, replay pause/one-step/resume, hashes, incident replay, offline/online SSE recovery, live public shadow start, one-session quota, stop, keyboard focus restoration, overflow, and logout. Live session `shadow-af8586eeb558ff324522f4e16d032e32` reached `CANCELED` with no failure code. The `CANCELED` assertion was not weakened.             |
| Live recorder and shadow evidence          | Recorder and shadow used separate fresh roots; API and worker resolved only the immutable r6 replay root. The passing run flushed 34,440 records at revision 1 and 85,578 records at final revision 2, both with `gap_count=0`. All four roles stopped cleanly with exit status zero. A preceding clean run independently disproved the stale-database `shadow_stop_failed` observation by reaching `CANCELED` before exposing a last-tab-stop browser assertion defect.                    |
| Current contract and frontend checks       | `make a11-frontend-qualify` passed strict TypeScript, ESLint, 13 Vitest/React Testing Library/axe tests, and the production Vite build. `make a11-ui-fixture-qualify` passed both Chromium desktop and Pixel 7 projects. The exact 30-operation OpenAPI/generated contract remained current.                                                                                                                                                                                                |
| Current cumulative repository gate         | Full `make verify` passed preflight, formatting, generated contracts, 68-document link validation, traceability, vet/staticcheck/ESLint, source/file policies, all Go and frontend tests, the complete Go race suite, five fuzz targets, embedded production build, all 128 Compose renders, secret/prohibited-capability/binary scans, and live `govulncheck`. `govulncheck` reported zero called vulnerabilities; one required module vulnerability was not reachable from imported code. |
| Current image inspection and Compose smoke | Candidate `axiom:a11-local` was built with version `v1a-a11-candidate`, build time `2026-07-19T00:00:00Z`, `DIRTY=false`, and pre-documentation source-tree digest `a9f9f7f3eadf9be1e5e710380a0d80e1cde2eeffb4e41963d90a6b8aa56b1824`. `scripts/inspect-image.sh` passed the minimal non-root/read-only inspection. `make compose-smoke IMAGE=axiom:a11-local` passed migration plus healthy API, public shadow, recorder, worker, Prometheus, and Grafana startup and cleanup.             |

## Qualification interpretation and remaining formal gates

- The fixture browser workflow proves presentation behavior only. The separate
  unmocked workflow supplies the local API/PostgreSQL/worker/shadow composition
  evidence; neither result is formal A11 acceptance while prerequisite phases
  remain formally open.
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
