# V1B phase checklist

V1B proceeds B1 through B8. Checked implementation items do not imply formal
verification; live soaks and predecessor gates retain their own checkboxes.
Requirement details are in [V1B traceability](../requirements/v1b-traceability.md).

## Program setup

- [x] Record the clean merged-main baseline identity.
- [x] Pass cumulative `make verify` before V1B changes.
- [x] Reconcile A8-A11 as merged but formally dependent on A7.
- [x] Create stable B01-B08 requirements, coverage, readiness, owners, migrations, limitations, and evidence paths.
- [x] Keep A7 server evidence changes out of the B1 implementation branch.

## B1 — Bybit public adapter and multi-exchange recorder

- [x] Implement server time, metadata, snapshots, trades, tickers, candles, streams, health, budget telemetry, and capabilities.
- [x] Enforce compiled production-public origins and absence of credentials/private/order routes.
- [x] Implement snapshot/delta/delete/update-ID-1 semantics and depth 1000.
- [x] Record raw-before-canonical frames, acknowledgements, heartbeat, generations, resets, gaps, decoder failures, and clock samples.
- [x] Compose Binance and Bybit collectors by exchange/instrument for BTC-USDT, ETH-USDT, and ETH-BTC.
- [x] Accept 15m, 1h, and 4h intervals in the versioned V1B configuration while retaining V1A compatibility.
- [x] Pass B1 model, PostgreSQL 18 clean/upgrade, adapter, emulator, security, image, and cumulative verification gates.
- [x] Complete short production-public validation.
- [ ] Complete and retain the isolated continuous 72-hour B1 soak (explicitly deferred; not run).

## B2 — coherent views and Tier-A datasets

- [x] Confirm the locally verified B1 completion branch is merged into `main` at `91d8bab54216210f2ef54dc20fed716ccf22c831`; formal A7/B1-soak acceptance remains deferred.
- [x] Implement every `AX-V1B-B02-*` requirement and pass model, deterministic replay, Tier A, and PostgreSQL 18 clean/upgrade gates.
- [x] Retain a short real Binance/Bybit Tier A dataset with exact child/replay identities and zero hidden gaps.
- [x] Close the short public coherent-view gate at or below 100 ms clock uncertainty (Southeast Asia passed at 59.569181 ms Binance / 40.927081 ms Bybit).
- [ ] Complete the continuous B2 72-hour qualification (explicitly deferred; not run).

## B3 — mean reversion

- [x] Confirm locally verified B2 completion is merged into `main` at `0c2fce26cae9e171d4e622c080aaf9af5cab018f`; formal predecessor, deferred B2-soak, and approver holds remain.
- [x] Implement and locally qualify every `AX-V1B-B03-*` requirement, including exact indicators, no-look-ahead execution, shared allocation/risk/accounting, PostgreSQL 18 clean/upgrade, deterministic research reports, and the clean image-backed Compose smoke.

## B4 — triangular arbitrage

- [ ] Confirm B3 is verified.
- [ ] Implement and qualify every `AX-V1B-B04-*` requirement.

## B5 — cross-exchange arbitrage

- [ ] Confirm B4 is verified.
- [ ] Implement and qualify every `AX-V1B-B05-*` requirement.

## B6 — advisory inventory and rebalancing

- [ ] Confirm B5 is verified.
- [ ] Implement and qualify every `AX-V1B-B06-*` requirement.
- [ ] Prove no withdrawal or transfer execution surface exists.

## B7 — validation and promotion evidence

- [ ] Confirm B6 is verified.
- [ ] Implement and qualify every `AX-V1B-B07-*` requirement.

## B8 — multi-exchange API and console

- [ ] Confirm B7 is verified.
- [ ] Implement and qualify every `AX-V1B-B08-*` requirement.

## Release decision

- [ ] Confirm V1A and B1-B8 are verified in order.
- [ ] Rerun applicable V1A safety, accounting, replay, recovery, restore, and real-money-lock gates.
- [ ] Record immutable source, configuration, API, image, Compose, dataset, and network identities.
- [ ] Obtain Product, Security, QA, and SRE approval.
