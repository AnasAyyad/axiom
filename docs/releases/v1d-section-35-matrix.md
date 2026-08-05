# Section 35 acceptance matrix

Vocabulary: **Implemented** means code/documentation exists; **Locally
verified** means a named local check ran; **Hosted CI verified** means a retained
candidate run passed; **Formally qualified** means the required duration and
environment verdict exists; **Formally accepted/certified** requires complete
signed cumulative evidence. **Blocked** is not Passed.

| # | Criterion | Code/tests/evidence | Current status | Blockers |
|---|---|---|---|---|
| 1 | Reliable Binance and Bybit public BTC/ETH spot books | public adapters, book synchronizers, recorder tests; A7/B1/B2 records | Implemented; Locally verified on earlier slices | B2 formal 72-hour verdict and current signed acceptance |
| 2 | Raw market data records and replays deterministically | segment writer, manifests, replay/checksum tests; A4/A8 records | Implemented; Locally verified | current exact-candidate evidence and formal acceptance |
| 3 | All six strategy/recommendation families run | A10 and B3-B7 strategy packages and tests | Implemented; Locally verified | V1B cumulative formal acceptance |
| 4 | Strategies support applicable backtest/replay/shadow | lab/runtime tests and D3 browser workflows | Implemented; dirty-worktree fixtures Locally verified | clean exact-candidate integrated workflow evidence and formal acceptance |
| 5 | Binance Testnet and Bybit Demo plumbing validated | C4/C5 closed adapters, emulator, capture, recovery tests | Implemented; Locally verified | C6 72-hour verdict and current V1C owner/security acceptance |
| 6 | Central allocator and risk control every simulated/demo order | allocator/risk/planner/dispatcher contracts and kill-point tests | Implemented; Locally verified | independent safety and reconciliation review |
| 7 | Virtual accounting balances exactly | journal, valuation, balance and invariant tests | Implemented; Locally verified | independent accounting review and current formal verdict |
| 8 | One-leg arbitrage failures recover | arbitrage compensation/reconciliation fault tests | Implemented; Locally verified | current formal reconciliation evidence |
| 9 | Inventory P&L separated from arbitrage P&L | ledger attribution models, reports, accounting tests | Implemented; Locally verified | independent accounting review |
| 10 | React exposes every required screen | D2-D4 route/component/axe and browser suites | Implemented; current dirty-worktree five-project matrices Locally verified | clean exact-candidate and Hosted CI verified result |
| 11 | Safe reconnect, gap, slow storage, restart, and fault behavior | adapter/runtime/recovery/D5 chaos tests | Implemented; Locally verified | D5 formal declared-load evidence |
| 12 | Backtests and replays reproduce | deterministic scheduler, dataset/config/result identities, D3 bundle tests | Implemented; Locally verified | independent determinism-reproducibility review |
| 13 | Production real-money trading impossible | compiled policy, endpoint/signing allowlists, secret/prohibited/binary scans, D6 safety model | Implemented; Locally verified | clean exact-source signed safety manifest and independent review |
| 14 | Documentation and runbooks complete | Section 33 canonical paths and documentation link/boundary checks | Implemented; 167-file link and D6 boundary checks Locally verified | independent operations review and formal acceptance |
| 15 | All quality gates pass | `make verify`, phase gates, image and supply-chain gates | Implemented; available dirty-worktree gates Locally verified | clean exact-candidate integrated/Compose rerun and Hosted CI verified result |
| 16 | Critical requirements have current evidence and no unsafe expired waiver | traceability, cumulative evidence index, D6 expiry/signature verifier | Implemented | current signed A0-D5 prerequisite set; A7 non-qualifying machine verdict must be resolved safely |
| 17 | Seven-day declared-load readiness meets SLOs | default-off D5 runner and manifest | Implemented | **Blocked**: D5 seven-day run not started; not Formally qualified |
| 18 | Restore, market recovery, and clean-server Compose deployment demonstrated | backup/restore tools, D5 recovery checks, deployment docs | Implemented; repository checks available | **Blocked**: current declared-server restore and deployment evidence absent |
| 19 | Kill points never duplicate orders, lose fills, or release reservations unsafely | C3-C6 crash/recovery/reconciliation tests | Implemented; dirty-worktree non-soak tests Locally verified | clean exact-candidate rerun and reconciliation review |
| 20 | Signed capture and clean build prove production-private submission impossible | egress policy, redacted emulator captures, binary/image scans, exact destination model | Implemented; local emulated capture and dirty images verified | **Blocked**: current signed capture bundle and independent clean-build manifest absent |
| 21 | Strategy results expose mode/confidence/valuation/models/sample/uncertainty/maturity | report and D3 lab projections/UI tests | Implemented; Locally verified | current candidate workflow evidence and product review |
| 22 | Platform readiness and strategy viability remain separate | result/report/qualification contracts and UI labels | Implemented; Locally verified | current signed product/cumulative acceptance |

No row is **Formally accepted/certified**. Criteria 17, 18, and 20 have explicit
external/current-evidence blockers; other rows also require exact-source signed
cumulative evidence. Therefore the only honest aggregate state is **Blocked / NOT
CERTIFIED**.
