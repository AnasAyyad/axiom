# V1C PR2 C4-C5 checklist

## C4 Binance Spot Testnet

- [x] Close authenticated operations to approved Testnet Spot routes and reject
  `/sapi`, production-private, arbitrary, and prohibited paths before signing.
- [x] Implement filters, clock/book eligibility, account/order/history/fill,
  test-create/create/cancel/query, private stream, reconnect backfill, unknown
  recovery, rate preservation, and coherent reset handling.
- [x] Pass C4 contracts, goldens, request captures, emulator, reducer, reset,
  race, fuzz, and security qualification.
- [x] Pass one manually armed Binance canary of at most 10 USDT and retain its
  sealed evidence identity
  `22072e94ccd24ee10094068ca74720479ba2362b374efac961751f23f4fc3473`.

## C5 Bybit Demo

- [x] Close private activity to Demo REST/private WebSocket and keep production
  Bybit hosts credential-free and public-only.
- [x] Implement key/wallet/order/history/execution/time/instrument operations,
  asynchronous acknowledgement, authoritative private/reconciliation state,
  and only the three approved private topics.
- [x] Pass C5 contracts, permission/signing/serialization, request captures,
  emulator, reducer, reconnect, race, fuzz, and security qualification.
- [x] Pass one manually armed Bybit canary of at most 10 USDT and retain its
  sealed evidence identity
  `e39883d4f5e0650b3861b2c3cd753ba1fc832f0536902c9bcbf357568dfa765b`.

## Runtime and PR decision

- [x] Add separate engine services, credentials, DB roles, leases, networks,
  proxies, startup/recovery, and default-off enablement.
- [x] Prove both restricted engine roles can read only the identity/session
  records required by the authorization query without receiving write access.
- [x] Prove immutable high-risk audit serialization succeeds without granting
  UPDATE or DELETE on its append-only table.
- [x] Keep Bybit canary admission instrument-scoped and retain the 250 ms clock
  uncertainty ceiling with a bounded proxy-sample budget.
- [x] Add the protected full-pipeline, controlled-restart canary runner and
  immutable value-free evidence contract.
- [x] Prove a repeated private-event identity may arrive at a later local
  receive time while every immutable exchange fact must still match.
- [x] Pass clean PostgreSQL 18.4 install and exact B8 upgrade.
- [x] Pass all 1,024 Compose profile renders and secret/network/command
  isolation checks.
- [x] Pass the complete repository `make verify` aggregate, including unit,
  frontend, race, fuzz, build, security, and vulnerability gates.
- [x] Rerun clean PostgreSQL 18.4 install and exact B8 upgrade on the final
  source-policy-refactored candidate.
- [x] Pass final cumulative `v1c-pr2-local-qualify`.
- [x] Pass dirty-candidate image reproducibility, minimal-runtime and compiled
  endpoint/capability inspection, SPDX SBOM, and Trivy HIGH/CRITICAL plus
  secret scan.
- [x] Confirm image-backed Compose startup fails closed on truthful
  `DIRTY=true` build identity.
- [x] Provision all required secret files out of band and verify the live
  owner attestations without recording credential or account payloads.
- [x] Start Binance with the built-in default-off graph and observe stable
  `READY_PAUSED` reconciliation without selecting a canary graph.
- [x] Request virtual BTC, ETH, and USDT in the Bybit Demo account, then prove
  its built-in default-off graph reaches stable `READY_PAUSED`.
- [x] Pass both independent exchange canaries on the same final dirty
  executable; current-source Binance evidence is
  `22072e94ccd24ee10094068ca74720479ba2362b374efac961751f23f4fc3473`
  and Bybit evidence is
  `e39883d4f5e0650b3861b2c3cd753ba1fc832f0536902c9bcbf357568dfa765b`.
- [x] Cut each exchange proxy independently and prove the matching engine
  transitions `READY_PAUSED` → `DEGRADED` → `READY_PAUSED`, completes
  private-stream reconnect/backfill plus fresh reconciliation, and retains
  restart count zero.
- [x] Stop both final-image engines cleanly with exit code zero and no
  eligibility or private-stream startup failure.
- [x] Freeze the exact canary-qualified worktree, rebuild with truthful
  `DIRTY=false`, and repeat image reproducibility, minimal/compiled boundary
  inspection, image-backed Compose smoke, SPDX, and Trivy.
- [x] Commit the final evidence update and push `v1c-c4-c5-adapters`.
- [ ] Record owner and security acceptance.

PR3 must not branch from this work until both canaries pass unless the owner
explicitly authorizes a deferral.
