# A10 local validation evidence

**Status:** Implemented and locally validated; formal acceptance blocked by A7
and formal A8/A9 acceptance.

**Candidate branch:** `a10-trend-strategy`, stacked from A9 tip
`a86289eb448ade0c53b5eb5089018b79909eae53`. This evidence does not merge or
pre-approve any formal gate.

## Implemented slice

- Strict `axiom.config.v1a.2` graph with all 16 immutable `trend.v1a.1`
  parameters in defaults and the deployment shadow configuration.
- Exact Go EMA/ATR, completed-candle admission, breakout/position/stop/cooldown,
  stressed sizing, explanations, stable reasons, shared strategy adapter, and
  approval-gated one-leg simulation planner.
- Real shared-pipeline qualification through A9 allocation/risk and A8
  post-latency simulation; no signal-close fill or strategy-owned broker call.
- Forward-only migration `000009`, generated sqlc queries, atomic research
  registration, one-time final-test consumption, canonical Trend explanation,
  and immutable report persistence.
- Deterministic chronological/walk-forward, seeded block bootstrap, parameter
  stability, capacity/stress/benchmark/breakdown, uncertainty, and report-policy
  modules.
- Locked Python 3.12.3 offline checker outside the production image. Go remains
  authoritative in every supported mode.

## Local commands and results

| Command | Result |
| --- | --- |
| `make a10-model-qualify GO=.local/toolchains/go/bin/go` | Passed. Real Trend → allocator → risk → planner → simulated broker composition passed. The declared `linux/amd64`, Go 1.26.5, 8-CPU profile measured 200 post-warm-up typed Trend+allocator+risk samples at p99 `3.255296ms`, below 25 ms. |
| `make a10-research-qualify GO=.local/toolchains/go/bin/go` | Passed. Five independent Python unit/golden checks and deterministic Go research/report tests passed. |
| `make a10-sqlc GO=.local/toolchains/go/bin/go SQLC=/home/anas/.local/bin/sqlc` | Passed. sqlc generation is current and the PostgreSQL repositories compile/test with the destructive DSN disabled. |
| `make a10-postgres-qualify ... AXIOM_A10_TEST_DSN=postgres://.../axiom_a10_test?sslmode=disable` | Passed against a fresh `postgres:18.4-alpine` database. All nine forward migrations applied before immutable registration, referential-integrity, mutation, deletion, and one-time final-test-consumption checks. The local credential was ephemeral and is not committed. |
| `go run scripts/check_go_policy.go` | Passed after adding nil-AST handling required by inferred constant declarations under the pinned Go parser. |
| `node scripts/check-a2-config-reference.mjs` | Passed for the schema, original financial graph, and all 16 Trend parameter rows. |
| `node scripts/check-a10-strategy-boundary.mjs` | Passed. Production Trend source has no broker/simulator/storage/network capability and the scratch image does not copy Python research. |
| `make verify` | Passed cumulatively with exact preflight, formatting, contract generation, documentation, lint/static analysis, backend/frontend/race/fuzz tests, build, all 128 Compose renders, security negative tests, binary boundaries, and `govulncheck` with no findings. |
| `make image IMAGE=axiom:a10-local ...` | Passed. Built Linux/amd64 scratch image ID `sha256:9f47b4b4c0f58cb0d4d2792357e025dd4b778aa1eb219dd4b55a2dacf2bb6d02`, running as `10001:70` with `/app/platform` as the entrypoint. |
| `make compose-smoke IMAGE=axiom:a10-local` | Passed. The image-backed application, recorder, worker, PostgreSQL, Prometheus, and Grafana services reached their required healthy/exited states. |
| `docker export axiom-a10-image-inspect \| tar -tf -` | Passed. The complete runtime filesystem contained only the platform binary, shadow configuration, CA certificates, UTC zoneinfo, and container virtual files; no Python, research source, notebook, lockfile, shell, or broker tooling was present. |

## Database qualification contract

`make a10-postgres-qualify` requires an explicitly supplied dedicated database
whose name ends in `_a10_test`. It applies all migrations from an empty
database, registers the complete immutable research graph, rejects duplicate
registration, mutation, deletion, and repeated final-test consumption, and
checks decision/report referential integrity. The final local run passed on a
fresh PostgreSQL 18.4 container. Absence of that destructive DSN does not
convert the env-gated test into a pass on other machines or in CI.

## Research evidence classification

No untouched final-test result was read while producing this implementation
evidence. Ignored A7 recordings remain local and unexported. The current
classification is Tier B/local platform-correctness evidence only. Strategy
viability is undetermined, and no production-profitability claim is made.

## Formal blockers

- A7 still requires its accepted continuous 72-hour public-data qualification.
- Formal A8 and A9 acceptance depends on that A7 gate.
- A10 readiness checkboxes remain unchecked and matrix statuses are
  `Implemented`, never `Verified`.
