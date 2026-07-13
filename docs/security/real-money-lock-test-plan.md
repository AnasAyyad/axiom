# V1A real-money lock test plan

## Objective

Prove independently that a V1A source tree, generated contract, binary, container image, Compose render, database schema, and browser UI cannot submit or authorize an external order. A passing default configuration is insufficient: negative tests must exercise malicious and accidental configuration.

## Required evidence

1. **Source capability scan** rejects authenticated transports, request signers, account/private routes, credential fields, external order methods, and prohibited products. Descriptive text and explicit typed `unsupported` capability values are allowlisted narrowly.
2. **Mode tests** prove only `backtest`, `replay`, `paper`, and `shadow` parse. `testnet`, `demo`, `live`, unknown, mixed-case, whitespace-padded, and empty values fail before runtime startup.
3. **Endpoint tests** exercise every configurable Binance URL. Only the compiled public HTTPS and WSS hosts and public route set are accepted; userinfo, fragments, IP literals, redirects to other hosts, DNS-derived private targets, and lookalike suffixes fail before network I/O.
4. **Public-client conformance** records all emulator traffic and proves methods are public reads with no credential, signature, timestamp-signing, account, order, transfer, withdrawal, margin, futures, or leverage fields.
5. **Interface and symbol inspection** proves the V1A binary has no callable production, testnet, demo, withdrawal, transfer, or signing implementation. The simulation `Broker` accepts only internal plans and cannot receive a network transport.
6. **Configuration scan** proves committed examples and schemas contain no credential key or endpoint for authenticated exchange use and that absolute safety flags cannot be weakened.
7. **Database migration tests** prove execution-mode constraints exclude all external-order modes and no table stores exchange API credentials or production-order requests.
8. **API contract tests** prove no route can enable external trading, accept credentials, or create an exchange order. All mutation schemas reject unknown fields.
9. **UI and generated-asset scan** proves `REAL TRADING DISABLED` remains visible and no control, hidden route, query parameter, local-storage value, or generated client enables an external-order mode.
10. **Container/Compose inspection** proves no authenticated engine, credential secret, private endpoint, signer symbol, or later-release binary is present. Runtime runs as non-root with a read-only root filesystem.
11. **Outbound-host test** runs the release image behind a recording proxy during the public-data workflow and proves all exchange traffic is public Binance market-data traffic.
12. **Independent clean-build review** repeats the scans from a clean checkout and records tool versions, source commit, image digest, SBOM hash, and reviewer.

## Failure policy

Any unexpected symbol, key, route, host, request field, outbound destination, UI control, mode, or database value is a release-blocking failure. Exceptions require a precise allowlist entry for descriptive `unsupported` metadata or test fixture text; broad keyword exclusions are prohibited.

## Evidence locations

- Unit and negative configuration tests: package-local `*_test.go` files.
- Repository and image scanners: `scripts/check-prohibited-capabilities.sh` and CI artifacts.
- API/UI contract evidence: generated-contract and browser test reports.
- Outbound capture and binary inspection: `artifacts/release/v1a/safety/` (generated, not committed).
- Final reviewer record: `docs/releases/v1a-readiness.md`.

This document defines the test plan. It is not evidence that the tests have passed.
