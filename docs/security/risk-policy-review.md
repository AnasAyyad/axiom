# V1A initial risk-policy review

## Review record

| Field | Value |
|---|---|
| Status | A0 design review complete; A9 implemented and locally validated; formal acceptance blocked by A7 and formal A8 acceptance |
| Review date | 2026-07-12 UTC |
| Reviewer | Codex technical review under the user-approved specification and V1A plan; independent release review remains required |
| Scope | Initial V1A central-risk policy for backtest, replay, paper, and public-data shadow modes |
| Normative sources | [Product specification §§15 and 18](../../crypto_bot_v1_codex_spec.md#15-control-e-cash-and-market-risk-regime), [configuration policy §30](../../crypto_bot_v1_codex_spec.md#30-configuration-and-environment-contracts), and the approved V1A implementation plan A9/A10 |
| Product owner | Anas Abu-Sulik |
| Implementation owner | Portfolio and Risk Engineering (A9) |
| Approval basis | Normative user-supplied limits plus the safest explicit assumptions recorded here and in ADR-0007 |
| Re-review trigger | Before A9 policy activation, after any policy/schema change or incident, and before any cap is loosened |

The design policy now has local A9 implementation and model evidence in the
[A9 local validation record](../releases/evidence/a9-local-validation.md). That
record includes the updated PostgreSQL gate and final image-backed Compose smoke.
It is not formal release acceptance; the owner-authorized speculative candidate remains unmerged
while A7 and formal A8 acceptance are pending. The values below are conservative
safety starting caps, not evidence that a strategy is viable or profitable.

## Policy invariants

- Every candidate passes the allocator and then the central risk engine. No
  strategy, simulator, replay path, recovery path, API, or administrator can
  bypass either boundary.
- Financial values, rates, percentages, and comparisons use exact decimal or
  fixed-point types. In the source policy, `1.00%` means one percent, not a
  multiplier of one.
- An `ENTRY` is eligible only when every applicable scope and input passes. A
  successful narrower check cannot override a more restrictive state or limit.
- Missing, invalid, stale, inconsistent, non-finite, or overflowed inputs reject
  the candidate and cause the applicable pause, lock, quarantine, incident, and
  alert behavior. No missing value becomes zero, one, healthy, or unlimited.
- Limits may be tightened by a versioned policy. Loosening requires a versioned
  policy, authenticated owner confirmation, an immutable audit record, and new
  validation evidence. Environment values may only tighten policy; a conflict
  or attempted loosening fails startup.
- V1A has no external broker, testnet/demo mode, credential, signing path, or
  exchange-order side effect. Dormant later-release caps do not make those
  capabilities available.

## Initial global limits

The following table preserves every default in specification §18.1. “Pass
boundary” defines the initial A0 comparison assumption so boundary tests are
unambiguous; it may be changed only through the approval process above.

| Control | Initial default | Pass boundary | V1A applicability |
|---|---:|---|---|
| Initial startup state | `PAUSED` | No entry while paused | Active |
| Automatic unpause | Disabled | No automatic transition from `PAUSED` or `LOCKED` | Active |
| Maximum account drawdown | 5.00% of equity | Current value `< 5.00%`; `>= 5.00%` trips the breaker, and a candidate is rejected if its stress projection would exceed the cap | Active |
| Maximum UTC-day or rolling-24h loss | 1.00% of equity, whichever measurement is stricter | Stricter current value `< 1.00%`; `>= 1.00%` trips the breaker, and a candidate is rejected if its stress projection would exceed the cap | Active |
| Maximum strategy drawdown/loss | 3.00% of strategy equity | Current value `< 3.00%`; `>= 3.00%` trips that strategy's breaker, and a candidate is rejected if its stress projection would exceed the cap | Active |
| Maximum one volatile-asset exposure | 30.00% of equity | Projected marked exposure `<= 30.00%` | Active for BTC and ETH |
| Maximum combined BTC+ETH exposure | 50.00% of equity | Projected marked exposure `<= 50.00%` | Active |
| Maximum marked exposure to one exchange | 60.00% of equity | Projected marked risk exposure `<= 60.00%` under the V1A formula below | Active |
| Minimum global reserve | 15.00% of equity | Projected reserve `>= 15.00%` | Active |
| Maximum reserved capital | 85.00% of equity | Projected reserved capital `<= 85.00%` | Active |
| Maximum open orders | 8 virtual; 1 test/demo | Resulting virtual count `<= 8` | Virtual cap active; test/demo cap dormant and cannot enable those modes |
| Maximum spread | 100 bps unless a strategy limit is stricter | Observed executable spread `<= 100 bps` | Active |
| Maximum simulated slippage | 50 bps unless a strategy limit is stricter | Modeled/observed slippage `<= 50 bps` | Active |
| Arbitrage additional safety margin | 15 bps | Required edge includes at least 15 bps beyond estimated costs | Dormant until an arbitrage release; retained so V1A cannot silently weaken it |
| Cross-exchange maximum book age | 250 ms | Each coherent input book age `< 250 ms` | Dormant until cross-exchange research; not a substitute for the V1A Trend freshness policy |
| Maximum event-queue lag | 250 ms | Oldest applicable event age `<= 250 ms` | Active |
| Maximum local clock-drift estimate | 100 ms | Absolute drift estimate `<= 100 ms` | Active |
| Minimum quality score after hard checks | 90/100 | Score `>= 90` after every hard eligibility check passes | Active |
| Maximum individual test/demo order | 10.00 USDT | Exact notional `<= 10.00 USDT` | Dormant; testnet/demo are unavailable in V1A |
| Maximum test/demo daily submitted notional | 50.00 USDT | Exact UTC-day submitted notional `<= 50.00 USDT` | Dormant; testnet/demo are unavailable in V1A |

The maximum unresolved reconciliation or suspense amount for continued entries
is **0 in every asset**. Any unknown, inconsistent, or unbalanced amount blocks
entries and is quarantined until resolved with immutable evidence.

## V1A Trend and portfolio limits

The approved V1A implementation plan narrows the first research slice to one
Trend-only virtual portfolio initialized with exactly **500.00 USDT on Binance
and zero BTC/ETH**. Its strategy checks add:

| Control | Initial value | Enforcement |
|---|---:|---|
| Risk budget per approved Trend trade | 0.50% of Trend virtual-portfolio equity | Exact stressed-loss sizing before global caps; the tighter resulting quantity wins |
| Open Trend positions | 1 per asset per portfolio | A second position is rejected; no averaging down or adverse-move size increase |
| Initial protective-exit distance | 2.5 ATR(14) | Invalid, nonpositive, stale, or uncomputable stop distance rejects sizing |
| Trailing-exit distance | 3 ATR(14) from highest favorable completed close | Exit management only; never expands entry authority |
| Protective-loss cooldown | 3 completed 4-hour strategy candles | New Trend entry for the affected scope is rejected during cooldown |

The source leaves several V1A values open. The safest coherent implementation
baseline is explicit: maximum Trend notional is **150.00 USDT** (also bounded by
the 30.00% single-asset cap and 15.00% reserve); candidate age and marketable-
limit validity are each **5 seconds**; the arrival-time book must be no older
than **250 ms**; a completed-candle signal must be evaluated within **5 seconds**
of its validated final publication; and maximum USDT concentration is **100%**
because the required initial portfolio is entirely virtual USDT. Every value is
an exact inclusive maximum except age/validity, which expire at equality. A
missing or invalid value rejects the entry. USD reporting and depeg enforcement
stay disabled until an independent versioned source is approved; missing or
stale USD reference data fails USD reporting closed and never invents a one-
dollar mark.

For V1A, “marked exposure to one exchange” means the conservative marked value
of BTC/ETH inventory, reservations, and worst-case open-order exposure assigned
to that venue. It excludes uninvested virtual USDT because V1A has no exchange
account, custody, credential, or counterparty balance; the 500-USDT
initialization is a local journal fact. This stated A0 assumption reconciles
the Binance-only research slice with the 60.00% cap without weakening volatile
exposure controls. It must be re-reviewed before an authenticated sandbox or a
second exchange exists. ADR-0007 records the formula and change boundary.

## Hierarchy and effective state

Risk is evaluated across these persisted scopes:

```text
platform/global
-> exchange account or virtual account
-> exchange
-> strategy
-> portfolio
-> asset
-> instrument
```

The hierarchy is not a child-override mechanism. The effective state is the
most restrictive applicable state, and the tightest applicable numeric limit
wins. Every evaluation records all contributing scopes, observations, policy
and configuration versions, decision time, and stable reason codes. A broad
pause propagates downward; a narrow pause affects only its scope. A restrictive
state does not silently prohibit an explicitly policy-approved, risk-reducing
cancel, exit, or recovery action.

Risk results use only these actions: approve, reject, pause strategy, pause
instrument, pause exchange, lock engine, or quarantine state. State transitions
and actions emit immutable audit events and bounded-cardinality alerts/metrics.

## State and intent-action matrix

| Effective state | `ENTRY` | `EXIT` | `CANCEL` | `RECOVERY` | Reconciliation |
|---|---|---|---|---|---|
| `NORMAL` | Allowed after allocator and all normal checks | Allowed | Allowed | Allowed after recovery checks | Required on schedule |
| `CAUTIOUS` | Reduced size and stricter edge; all other checks still apply | Allowed | Allowed | Allowed after recovery checks | Increased cadence |
| `PAUSED` | Rejected | Allowed only when risk-reducing | Allowed | Allowed only when bounded and risk-reducing | Required |
| `LOCKED` | Rejected | Explicit locked-state policy approval only | Allowed | Explicit locked-state policy approval only; otherwise manual quarantine | Mandatory |

In V1A these intents affect only simulated orders and virtual inventory.
`CANCEL`, `EXIT`, and `RECOVERY` permissions cannot create a new exposure,
increase quantity, switch product/mode, or reach an external exchange endpoint.

## Escalation, hysteresis, startup, and manual recovery

Escalation to a more restrictive state is immediate. `CAUTIOUS` may return
automatically to `NORMAL` only after every applicable signal has remained
healthy continuously for the initial **5-minute** hysteresis interval. A new
failure resets the interval. `PAUSED` and `LOCKED` never auto-unpause.

Every decision-capable process begins `PAUSED`. The shadow engine acquires its
fencing lease, enters `LOCKED`, validates build/configuration safety, restores
durable state, reconciles its journal/orders/reservations/projections, qualifies
fresh required public market data, and becomes `READY + PAUSED`. Readiness is
not activation. A separate authenticated, authorized, recently reauthenticated,
and audited action is required before any strategy session can enter `NORMAL`
or `CAUTIOUS`.

Manual recovery from `PAUSED` or `LOCKED` requires, in order:

1. Resolve or quarantine the triggering condition without editing immutable
   history or releasing uncertain reservations.
2. Prove current reconciliation, balanced journal/projections, healthy critical
   persistence and fencing, fresh sequence-valid required market data, bounded
   queue lag, acceptable clock drift, and no unresolved unknown order/state.
3. Reevaluate every applicable scope and limit from a single current immutable
   configuration snapshot; stale recovery evidence is rejected.
4. Require an authorized operator with recent reauthentication to state the
   reason and intended target scope/state.
5. Persist the command, before/after states, evidence references, actor/session,
   policy versions, result, and immutable audit event.
6. Move a recovered `LOCKED` engine to `PAUSED`, not directly to active entry.
   Strategy/session activation remains a separate command and fresh evaluation.

If any prerequisite is missing, stale, invalid, or cannot be durably recorded,
the recovery command fails closed and the current restrictive state remains.

## Ownership and approval basis

| Responsibility | Accountable role |
|---|---|
| Approve future changes to the initial business risk appetite | Product owner |
| Own formulas, policy schema/version, hierarchy, reason codes, and enforcement | Portfolio and Risk Engineering |
| Own exact balances, reservations, exposure, loss, journal, and reconciliation measurements | Portfolio, Accounting, and Reconciliation owners |
| Own book age, hard quality checks, spread/depth inputs, queue lag, and clock evidence | Market Data, Runtime, and SRE owners |
| Own simulated slippage, order-count, recovery, and intent-state enforcement | Execution owner |
| Review fail-closed behavior, manual recovery authorization, and audit integrity | Security owner |
| Verify every threshold boundary, hierarchy combination, hysteresis timer, restart, missing/stale input, and recovery path | QA |

Acceptance as the implementation baseline is based only on fidelity to the
normative source and conservative fail-closed assumptions. It is not based on
historical profitability or external execution. Runtime activation still
starts `PAUSED` and requires the authenticated manual workflow plus all A9
boundary, model, configuration-hash, and audit evidence. Any changed or loosened
value requires named Product, Risk, and Security approval with UTC timestamp and
rationale.

## Required verification evidence

- Boundary tests immediately below, exactly at, and immediately above every
  maximum/minimum, including combined limits and exact decimal rounding.
- Model tests for every hierarchy/state/intent combination, escalation,
  five-minute hysteresis reset, and prohibition of automatic pause/lock recovery.
- Startup, restart, lease loss, reconciliation mismatch, stale/missing input,
  persistence failure, queue lag, clock drift, and quarantine tests.
- Call-path tests proving allocator/risk enforcement for every simulated order,
  exit, cancel, and recovery action, plus audit/alert assertions.
- An approval record and immutable policy/configuration hash linked from the
  [V1A readiness evidence index](../releases/v1a-readiness.md#a9).
