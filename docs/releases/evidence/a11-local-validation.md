# A11 local implementation evidence

**Status:** API, authentication, durable control plane, SSE, and routed console
implemented. The A11 phase is not locally complete because the durable offline
and live-shadow consumers are not composed in the runtime, and browser/runtime
acceptance could not be executed in this restricted environment.

**Candidate branch:** `a11-research-console`, stacked from A10 commit
`e63d6ed69c5cc549a052ac7127f715e84e17dedc`. This evidence does not merge or
pre-approve A7, A8, A9, A10, A11, or the V1A release gate.

## Implemented slice

- Authoritative OpenAPI contract for the exact 30 A11 method/path operations,
  generated Go boundary models, and generated TypeScript schema types.
- One-time hashed owner bootstrap, reviewed Argon2id profile, opaque hashed
  sessions, independent signed CSRF values, exact Origin checks, authorization,
  rate limiting, expiry, rotation, revocation, and five-session enforcement.
- Forward-only migration `000010`, durable commands, immutable audit linkage,
  optimistic revisions, idempotency conflicts, qualified-reference checks,
  bounded job queues, one-shadow-session quota, and fail-closed recovery gates.
- Cursor-bound deterministic list pagination and versioned resumable SSE with
  durable revisions, retention checks, user stream quotas, heartbeats, bounded
  batches, and snapshot recovery in the browser client.
- Routed React console for login, command center, Binance public health,
  virtual portfolio/journal, risk, Trend evidence, backtest, replay, shadow,
  incidents, and audit. Zod validates every consumed response. ECharts is
  isolated behind a project adapter and lazy-loaded.
- Persistent `REAL TRADING DISABLED` and virtual/shadow labels, light/dark and
  UTC/local controls, explicit non-happy states, responsive layout, accessible
  primitives, and a deterministic Playwright workflow specification.
- Compose mounts API authentication secrets only into the API process. The
  engine and worker receive no exchange credential, signing, or external-order
  capability.

## Current local commands and results

| Command | Result |
| --- | --- |
| `make a11-sqlc` with pinned Go/sqlc | Passed. All reviewed SQL regenerated deterministically and PostgreSQL packages compiled with the destructive DSN disabled. |
| `make a11-contract-qualify` | Passed. Generated contracts were current, all 30 exact operations passed the boundary audit, and API packages passed. |
| `make a11-api-qualify` | Passed. Authentication, authorization, router, health, bootstrap, configuration, and console tests passed. |
| Targeted `go test -race` across A11 API/authentication/bootstrap/configuration/PostgreSQL packages | Passed. |
| `make a11-frontend-qualify` | Passed. Strict TypeScript, ESLint, three Vitest files/seven tests, axe smoke, and the production Vite build passed. The chart bundle remains lazy-loaded. |
| `make a11-security-qualify` | Passed after excluding ignored compiled frontend output from the source scanner. Secret/prohibited-capability negative tests and A6/A7 binary boundaries passed. |
| `go tool staticcheck ./...`, `scripts/check-file-policy.sh`, and `go run scripts/check_go_policy.go` | Passed with writable tool caches after splitting the A11 client and page modules and tightening Go helpers. |
| Repeated OpenAPI/sqlc generation with before/after SHA-256 manifests | Passed with identical generated outputs on the second run. |
| A11 PostgreSQL 18 qualification before the final pagination/SSE refactor | Passed on a clean dedicated `_a11_test` database. The latest pagination/SSE SQL has compiled but requires a fresh destructive database rerun before it can be claimed current. |
| `pnpm --dir web test:e2e` | Not run. Vite preview could not bind `127.0.0.1:4173` in the filesystem/network sandbox, and the required escalation was unavailable after the environment reported its usage limit. The authored Playwright workflow is not counted as passing evidence. |
| `go test ./...` | All packages compiled and all non-listener packages passed. Four existing exchange-emulator tests failed only because the sandbox denied their local listener (`emulator_scenario_rejected:listen`). This is not recorded as a cumulative pass. |

## Remaining implementation and validation gaps

- `platform worker` does not yet consume A11 durable backtest/replay jobs through
  a PostgreSQL `backtest.JobStore` and the production A10 Trend → A9 allocation
  and risk → A8 planner/simulator/reducer pipeline.
- `platform trader --mode shadow` does not yet consume queued shadow sessions
  through the production-public market-view/Trend/allocation/risk/simulation/
  journal/outbox hot path. The API deliberately does not call it in memory.
- Because those consumers are absent, the configured experiment → backtest →
  replay → public-live shadow → report → incident replay workflow is not yet
  operational through the authenticated API and UI.
- Current PostgreSQL 18, Playwright desktop/mobile, full `make verify`, image,
  Compose smoke, and outbound-network inspection must be rerun from a permitted
  environment after the runtime composition exists.

## Safety and formal blockers

- No private exchange credential, authenticated exchange transport, signer,
  production broker, external order endpoint, withdrawal, transfer, margin,
  futures, leverage, shorting, testnet, or demo capability was added.
- A7 still requires formal 72-hour qualification; formal A8/A9/A10 acceptance
  remains dependent on that gate.
- A11 and V1A readiness checkboxes remain unchecked. Incomplete workflow and
  browser acceptance rows remain `In progress`, never `Verified`.
