# ADR-0007: V1A exchange-exposure basis

- **Status:** Accepted
- **Date:** 2026-07-12
- **Scope:** V1A portfolio and risk policy

## Context

The V1A plan requires one Trend portfolio with 500.00 virtual USDT assigned to Binance and zero BTC/ETH. The global policy also caps marked exposure to one exchange at 60% of equity. V1A has no exchange account, custody, credential, or counterparty balance, so treating uninvested local virtual USDT as exchange exposure would lock the required initial portfolio permanently.

## Decision

For V1A, exchange exposure is the conservative liquidation value of BTC/ETH inventory assigned to the venue, owned inventory reserved there, committed buy notional, fees, and worst-case nonterminal simulated-order or recovery exposure. Unreserved virtual USDT in Axiom's local journal is excluded because it is reporting-currency capital, not an exchange-held balance.

The numerator is evaluated with exact decimals against current virtual equity. Unknown valuation, reservation, order, or recovery state fails closed. The 60% cap remains active and combines with the 30% single-asset, 50% total volatile-asset, 15% reserve, and 85% reservation limits; the tightest result wins.

## Consequences

- The specified Binance-only 500-USDT initialization is coherent without weakening volatile-position or committed-order limits.
- UI and reports must distinguish local virtual cash from simulated venue exposure.
- The formula cannot be reused for an authenticated account, real custody, or a second exchange without review.

## Rejected alternatives

- Count all local virtual USDT as Binance exposure: prevents the required V1A workflow from ever leaving `PAUSED`.
- Disable the 60% cap: silently omits a normative risk limit.
- Split capital across another exchange: violates the V1A Binance-only plan.

## Validation

A9 boundary and model tests cover zero/partial/full inventory, reservations, nonterminal orders, recovery exposure, unknown marks, and exact values below, at, and above 60%. Initialization must remain 500.00 USDT with zero exposure under this formula.

## Revisit when

Before any second exchange, authenticated sandbox, custody representation, or change to the portfolio initialization is introduced. A superseding ADR must preserve explicit ownership, valuation, and fail-closed behavior.
