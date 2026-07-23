# B5 local validation evidence

## Status

B5 implementation and targeted PostgreSQL qualification are complete on
2026-07-23. Final cumulative B4+B5 repository verification and committed-source
image qualification are still in progress and are not yet claimed by this
revision.

Formal acceptance is not claimed. It remains held by A7/V1A and B1/B2/B3/B4
formal predecessor acceptance, the explicitly deferred B1/B2 soaks, and
Product, Security, QA, and SRE approval. B5 strategy viability is
`undetermined`; engineering correctness is not evidence or a guarantee of
production profitability.

## Implemented authority and safety

- `cross-exchange.v1b.1` exhaustively evaluates BTC-USDT and ETH-USDT in both
  Binance/Bybit directions from exact two-member B2 coherent views.
- Exact closed-cycle economics separately charge fees, spread/depth, latency,
  recovery, maximum one-leg loss, marginal replacement, natural reversal,
  advisory rebalancing, exchange concentration, and USDT concentration.
- Owned sell-side base inventory, owned buy-side USDT, exact 30/50/70 bands,
  natural reversal, and advisory-only rebalancing are enforced.
- Central risk precedes a fenced atomic claim over both balances, both fees,
  both depth slices, and recovery capacity.
- Deterministic concurrent simulation records all complete, partial, missed,
  negative-before-arrival, and delayed-unknown outcomes. Unknown state is
  verified before at most one risk-authorized retry, protected unwind, or
  quarantine.
- Eleven independently balanced journal categories retain execution, BTC and
  ETH inventory, stablecoin, fees, spread, slippage, latency, recovery,
  restoration, and combined P&L.
- No authenticated exchange, production order, testnet/demo, short sale,
  margin, derivative, leverage, transfer, withdrawal, borrowing, lending, or
  staking capability was introduced.

## Passed targeted qualification

- Exact model, configuration, accounting, execution, portfolio, risk,
  conversion, cross-exchange, and PostgreSQL unit tests passed.
- Go documentation/function-size and repository file-layout policies passed.
- PostgreSQL 18.4 clean install through migration 000017 passed against
  `axiom_b5_clean_b5_test`.
- Exact migration 000001-000016 to 000017 upgrade passed against
  `axiom_b5_upgrade_b5_test`.
- The database qualification proved registered configuration identity, exact
  copied B2 members, immutable candidates, exact seven-resource contention,
  fence rejection, quarantine retention, terminal simulation, advisory-only
  rebalancing, eleven balanced journal links, and reviewed role grants.

## Pending completion evidence

- Final B5 model race, fuzz, benchmark, and declared p99 results.
- One cumulative `b5-local-qualify` invocation covering B4, B5, sqlc,
  PostgreSQL, and repository-wide `verify`.
- Final clean committed-source image identity, SBOM, inspection,
  reproducibility, image-backed Compose smoke, and Trivy HIGH/CRITICAL result.
- Final configuration hash, source commit, `git diff --check`, clean worktree,
  branch push, and pull-request handoff.
