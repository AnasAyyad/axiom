# API contracts

`api/openapi.yaml` is the published OpenAPI 3.1 source and authoritative HTTP
shape. It documents the complete A11 authenticated research workflow: session,
system/Binance health, virtual portfolio and journal, risk, Trend decisions,
backtest, replay controls, public-live shadow, incidents, audit, and resumable
SSE. The A1 version/build endpoints remain available in addition to the exact
30 required A11 method/path operations.

Run `make contracts` to generate:

- `internal/api/generated/types.gen.go` with pinned `oapi-codegen`; and
- `web/src/api/generated/schema.ts` with the project-owned deterministic
  schema generator.

`make contracts-check` regenerates into a temporary directory and fails on any
drift. Generated files are never hand-edited. REST snapshots remain
authoritative. The browser fetches a REST snapshot and its revision before
subscribing to resumable SSE, ignores duplicate/stale revisions, and refreshes
the snapshot when a cursor gap or retention expiry is reported.

All durable mutations require the authenticated session, exact allowed Origin,
CSRF token, and `Idempotency-Key`. Risk and replay state changes additionally
carry an expected revision. Decimal financial values and large revisions are
strings; timestamps are RFC 3339 UTC values. Stable failures use `code`,
`message`, and `correlation_id` without raw internal details.

The routed console workflow is `/login` followed by Command Center, Binance,
virtual portfolio, Risk, Trend, Backtest, Replay, public-live Shadow, Incident,
and Audit views. Every screen remains simulation-only and the persistent shell
shows `REAL TRADING DISABLED`.
