# V1 known limitations register

## Product and market model

- Only Binance and Bybit are implemented; only USDT, BTC, and ETH are approved
  by default.
- Real-money production trading, withdrawals, transfers, margin, futures,
  perpetuals, options, leverage, borrowing, lending, staking, and short selling
  are intentionally unavailable.
- Binance Testnet and Bybit Demo behavior and liquidity do not represent
  production liquidity and do not prove profitability.
- Maker queue, fill, latency, market-impact, hidden-liquidity, and public-book
  simulations are approximations. Spot cross-exchange arbitrage retains
  inventory price risk.
- USDT is not risk-free USD; USD display and depeg controls depend on a separate
  configured reference.

## Deployment and operations

- Single-server Compose is not highly available; fencing prevents dual
  ownership but does not prevent downtime.
- Off-host backup requires an operator-provided independent mounted filesystem;
  a Compose volume is not accepted as remote evidence.
- The declared reference server, current clean restore evidence, B2/C6 72-hour
  verdicts, and D5 seven-day verdict are absent for the current candidate.
- D5 formal faults and service objectives have not been demonstrated for this
  candidate. Short deterministic smoke runs are non-formal.

## Evidence and release

- Earlier local, hosted, canary, waiver, and formal records may be bound to older
  SHAs. D6 rejects them until an independent current verdict explicitly binds
  their acceptable evidence to the candidate.
- The A7 machine result recorded `qualified:false`; its owner waiver cannot
  waive safety, accounting, determinism, or production-order lockout and cannot
  silently become a D6 pass.
- A8-A11 and multiple V1B/V1C owner/security/cumulative acceptances remain open.
- Independent D6 reviews, the exact signed safety manifest, and the final signed
  release verdict do not exist. V1 is **NOT CERTIFIED**.
