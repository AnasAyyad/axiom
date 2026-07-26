# B8 multi-exchange console API

`api/openapi.yaml` is authoritative. B8 extends the authenticated A11 console
without removing its Binance, Trend, portfolio, replay, shadow, incident, or
audit aliases.

## Generic read resources

| Method and path | Projection |
|---|---|
| `GET /api/v1/exchanges` | Public-only exchange health, capabilities, instruments, and quality |
| `GET /api/v1/opportunities` | Triangular and cross-exchange simulation candidates |
| `GET /api/v1/opportunities/{id}` | Legs, inventory impact, recovery, costs, and evidence timeline |
| `GET /api/v1/strategies` | Strategy versions, supported modes, maturity, and research role |
| `GET /api/v1/inventory` | Isolated virtual inventory dimensions; never a combined balance |
| `GET /api/v1/rebalancing/recommendations` | Advisory-only recommendation summaries |
| `GET /api/v1/rebalancing/recommendations/{id}` | Reviewed route facts and manual checklist |
| `GET /api/v1/research/champion-challenger` | Immutable B7 comparison reports |
| `GET /api/v1/replays/{id}/faults` | Immutable deterministic replay-fault schedule |

List resources use signed cursors and `page_size` from 1 through 200. Inventory
accepts exact `exchange`, `asset`, `strategy`, and `portfolio` filters.
Opportunities accept `kind=triangular` or `kind=cross_exchange`. Each new list
returns both its page revision and the global durable `snapshot_revision`.

Quality objects make `tier`, `confidence`, `freshness`, `source`,
`observed_at`, provenance completeness, and warnings explicit. Decimal values
and revisions remain strings so neither JavaScript nor Go binary floating-point
can alter financial or ordinal values.

## Simulation-only commands

`POST /api/v1/replays/{id}/faults` requires authenticated `commands.write`,
same-origin and CSRF checks, and `Idempotency-Key`. The body carries a supported
fault, event ordinal, delay, expected schedule revision, optional repeatability,
and an audit reason. Latency requires a positive nanosecond delay; every other
fault requires zero delay. The replay must still be `QUEUED`.

`POST /api/v1/reports/{id}/exports` has the same command boundary and accepts
`json` or `csv`. The response contains the deterministic content, content type,
and payload hash. The immutable command and export are retained in PostgreSQL.

Both resources declare `simulation_only: true`. They cannot submit an external
order, contact a private exchange route, move an asset, or change research
maturity.

## Snapshot and SSE sequence

1. Fetch authoritative REST snapshots.
2. Retain the maximum global snapshot/outbox revision.
3. Subscribe to `GET /api/v1/stream?after_revision={revision}`.
4. Accept only `axiom.stream.v1` events with strictly increasing revisions.
5. Refresh REST snapshots on a gap or invalid envelope.
6. If the cursor expired from bounded retention, fetch fresh snapshots and open
   SSE without the expired cursor.

B8 adds `opportunity`, `strategy`, `inventory`, `rebalancing`, and `research`
stream names. The existing A11 streams and resume behavior remain compatible.

## Hard safety responses

- Inventory always returns `combined_balance: false`.
- Rebalancing always returns `execution_available: false`.
- Opportunity, replay-fault, and report-export resources are simulation-only.
- The application shell continuously displays `REAL TRADING DISABLED`.
- There is no transfer, withdrawal, private exchange, or production-order
  method in the B8 API.
