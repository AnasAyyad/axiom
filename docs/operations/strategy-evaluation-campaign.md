# Strategy evaluation campaign

`balanced_full_v1` is the only owner-startable full evaluation preset. The
browser sends only `{ "preset": "balanced_full_v1" }`; it cannot select a
dataset, configuration, portfolio, model, generation, seed, hash, capital
limit, or exchange credential.

The campaign is spot-only and simulation-only. It uses production-public
market data, has no production-private exchange client, and cannot submit a
real-money order.

## Automated lifecycle

The durable campaign worker advances these stages without owner intervention:

1. official historical candle import;
2. existing-recording audit;
3. fresh campaign-bound recorder rotation;
4. 72 valid hours of simultaneous Binance and Bybit recording;
5. the 14-configuration backtest matrix and deterministic/stress variants;
6. the equivalent replay matrix and variants;
7. per-strategy candidate selection;
8. one combined, centrally allocated shadow campaign for seven valid days; and
9. an immutable final or partial report.

Campaigns use `PENDING`, `RUNNING`, `PAUSED_RECOVERABLE`, `COMPLETED`,
`PARTIAL`, `BLOCKED`, and `CANCELED`. Campaign, stage, member, import, audit,
recorder, shadow, event, decision, fill, ledger, and report state is persisted
in PostgreSQL. Claims and checkpoints make work restart-safe. Recoverable feed
interruptions pause valid-time accounting; elapsed wall time never substitutes
for valid evidence time.

A source-sequence gap invalidates only the affected local book generation and
the observation interval containing it. The collector records the gap, stops
decisions from that book, reconnects, obtains a fresh snapshot, and rebuilds
the book automatically. Qualification remains `PAUSED_RECOVERABLE` until a
subsequent observation proves that all six books, clocks, and persistence are
healthy, then the same campaign resumes its existing valid-time total. Three
consecutive observations without a validated recovery block with
`DATA_CORRUPT`; the gap and recovery evidence are never deleted or rewritten.
A healthy observation after a process restart or observation-window outage is
the new recovery baseline: it does not count valid time, and it resets the
bounded unresolved-attempt counter. Only subsequent unresolved observations
count toward the three-observation block threshold.

Recorder segment finalization also recovers fail-closed. Campaign byte
accounting serializes on the recorder-request row and safely proceeds or waits
through concurrent campaign updates; serialization/deadlock retries replay the complete idempotent
transaction with bounded deterministic jitter. Segment identities include the
revision, first ordinal, and content hash, so a quarantined half-revision can
never collide with fresh data. At startup the recorder verifies the last
complete cumulative manifest and its durable catalogue entry, registers a
proof-backed orphan only as quarantined evidence, and moves every uncommitted
final, proof, partial, or manifest-partial file under the session quarantine
directory. It never overwrites, advertises, or silently deletes those files.
New manifests embed their exact source commit so a crash after the filesystem
manifest but before catalogue registration can be recovered without assigning
the replacement build's identity to older evidence.

Only the owner can start or emergency-cancel a campaign. A member-level
strategy failure preserves the evidence and allows unaffected members to
continue. A shared data, storage, accounting, safety, or persistence failure
blocks the campaign. Every terminal path produces an immutable report that
states completed work, the stable reason code, and the next action.

## Data and storage policy

The immutable evaluation graph records full configured books and 15-minute,
1-hour, and 4-hour candles for `BTC/USDT`, `ETH/USDT`, and `ETH/BTC` from both
Binance and Bybit.

The historical importer retrieves official candles for `BTC/USDT` and
`ETH/USDT` from 2023-08-01 through 2026-07-31. It writes only beneath the
dedicated evaluation-history mount and persists raw/normalized linkage,
checkpoints, hashes, gaps, and source metadata. Candles are never represented
as order-book archives.

Existing recordings are audited but never automatically removed, repaired, or
rewritten. The audit verifies manifest and segment hashes, Parquet checksums,
raw/canonical linkage, event ordinals, allowed exchange/instrument coverage,
time coverage, duplicates, gaps, and clock quality. Ineligible evidence remains
preserved and receives an explicit reason. Existing Binance-only material can
support compatible replay/regression work but cannot replace fresh simultaneous
Binance/Bybit evidence for cross-exchange evaluation.

New campaign recording is independently limited to exactly 200 GiB. The
baseline, current bytes, measured rate, and shadow reserve survive restarts.
The campaign predicts the final segment and buffer before writing it and never
crosses the limit and cleans up afterward. If the qualified recording plus a
conservative seven-day shadow projection cannot fit, it blocks before shadow
with `STORAGE_INSUFFICIENT`. Filesystem pressure remains authoritative and
causes safe segment finalization and fail-closed decision handling.

## Evaluation and capital boundaries

The offline matrix contains four Trend Following, four Mean Reversion, two
Triangular Arbitrage, two Cross-Exchange Arbitrage, and two Inventory
Rebalancing configurations: 14 backtests and 14 replays before deterministic
repeats and focused stress runs. Both complete matrices are restricted to the
first chronological 80 percent. After their results are durable, one
configuration per strategy is written to an immutable candidate lock; only
then do its baseline and 1.5-times-cost jobs open the untouched final 20
percent. A strategy without complete validation evidence is locked `BLOCKED`
without consuming its final window. It evaluates 500, 1,000, 1,500, and 2,000
USDT capacity levels. The ordinary normative profile remains 500 USDT.

The versioned combined profile contains 10,000 virtual USDT: a protected 2,000
USDT reserve and ceilings of 2,000 USDT for each order-capable strategy.
Reservations, fees, inventory, spread, and slippage remain inside each ceiling.
Inventory Rebalancing evaluates the complete virtual portfolio as advisory
evidence and has no order, transfer, or withdrawal executor.

Offline selection is chronological. The selected configuration and validation
evidence hash are locked using the first 80 percent before the final 20 percent
is opened; each final-window use is also registered as immutable consumption
evidence. Identical repeats must reproduce decisions, accounting, and result
hashes. Cost, latency, depth,
partial and missed fills, filter rejection, delayed/gapped data, restart,
unknown result, and persistence failure scenarios remain explicit evidence.

An order-capable candidate enters combined shadow only with correct datasets
and runtime, reconciled accounting, no unsupported sale or negative inventory,
positive final-test net result, no more than 3 percent strategy drawdown,
positive 1.5-times-cost stress, and adequate non-concentrated samples. Verdicts
are `CONTINUE`, `IMPROVE`, `REJECT`, or `BLOCKED`.

All `CONTINUE` members run in one engine-owned shadow campaign with one central
allocator, shared liquidity accounting, and separate ledgers. Production-public
inputs are recorded, orders remain simulated, and unhealthy periods do not
count toward seven valid days. A restart resumes the same durable session.

## Owner API, console, and evidence

The owner API is:

- `POST /api/v1/evaluation-campaigns`
- `GET /api/v1/evaluation-campaigns`
- `GET /api/v1/evaluation-campaigns/{id}`
- `POST /api/v1/evaluation-campaigns/{id}/cancel`
- `GET /api/v1/evaluation-campaigns/{id}/events`
- `GET /api/v1/evaluation-campaigns/{id}/report`
- `POST /api/v1/data-audits`
- `GET /api/v1/data-audits/{id}`

The **Strategy Evaluation** page is the owner workflow. It displays stage and
matrix progress, wall and valid time, ETA, import coverage, feed health,
recorded bytes and reserve, shadow members, order funnel, cost/P&L evidence,
exact reasons and actions, and downloadable JSON/HTML reports. Optional query
failures remain local to their panels and never erase authoritative campaign
state.

Prometheus exposes bounded labels only for campaign stage/state, valid time,
recording bytes and reserve, feed freshness, strategy/mode/state counts, order
funnel, attributed costs/P&L, and stable failure classes. Logs and the durable
timeline carry campaign, stage, strategy, and stable reason context; no secrets
or raw credential material are emitted.

## Deployment boundary

Do not inspect, stop, replace, or deploy over C6 while it is active. After the
owner explicitly confirms C6 is finished:

1. preserve C6 evidence and safely finalize the active recorder segment;
2. take and verify the normal PostgreSQL and recording backup;
3. authenticate GHCR interactively using a user-entered `read:packages` token;
4. pull the exact merged application and backup image digests, never `latest`;
5. set the two-exchange evaluation configuration and dedicated historical-data
   mount, then render and validate Compose;
6. run forward-only migrations;
7. replace application containers while preserving PostgreSQL and
   `/srv/axiom-data`;
8. verify image/build/config identity, migration, both public exchanges, every
   required instrument, recorder finalization, worker polling, API, UI,
   Prometheus, and alerts; and
9. leave the campaign unstarted for the owner.

The console stays private:

```bash
ssh -N -L 18080:127.0.0.1:8080 axiom-server
```

Open `http://127.0.0.1:18080`. Preserving PostgreSQL also preserves the existing
owner identity. Deployment readiness is not 72-hour qualification, and a
healthy start is not seven-day shadow completion.
