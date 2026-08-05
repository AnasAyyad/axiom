# V1 safety manifest

Status: **UNSIGNED / NOT CERTIFIED**. The machine-readable fail-closed template
is `deploy/config/v1-safety-manifest.example.json`; only an independently
reviewed, exact-source, signed replacement can enter a release candidate.

## Capability assertions

| Assertion | Repository proof surface | Required exact evidence |
|---|---|---|
| Spot-only | compiled execution modes/products, exchange capability models, authenticated serializers | configuration/contract/binary/prohibited-capability scan hashes |
| Owned-inventory-only sells | reservation ledger, portfolio ownership, planner and adapter preconditions | accounting/risk tests and independent accounting review |
| Central allocator and risk for every simulated/demo order | allocator to risk to planner to durable dispatch pipeline; no adapter-side bypass | source/binary identities, kill-point tests, reconciliation review |
| No production-private submission | exact Testnet/Demo REST and WebSocket hosts, fixed routes/methods/fields, closed proxies | redacted signed-request capture, binary symbols, image and egress scans |
| No transfers or withdrawals | absent capabilities/routes/serializers plus negative tests | prohibited-capability source/binary/image results |
| No margin, futures, perpetuals, options, or leverage | spot-only capability and request-category rules | contracts/configuration/request capture and negative tests |
| No borrowing, lending, staking, or short selling | absent domain/execution operations and owned-inventory checks | source/binary/prohibited-capability results |
| No production broker, signer, credential input, or private endpoint | public/private package separation and compile-time destination policy | clean build, dependency/package symbols, environment/Compose and image scans |
| No hidden route, dormant toggle, or environment bypass | strict configuration, closed API/OpenAPI surface, endpoint override rejection | generated contract parity, route tests, configuration and Compose identities |

The only approved signed destinations are:

- Binance REST `testnet.binance.vision`
- Binance WebSocket `ws-api.testnet.binance.vision`
- Bybit REST `api-demo.bybit.com`
- Bybit WebSocket `stream-demo.bybit.com`

Production public-data hosts are credential-free and are not signed
destinations. Captures contain no headers, signatures, credential values, or
private field values.

## Exact identity set

The signed JSON requires exactly these immutable, signature-verified SHA-256
artifacts, all bound to one clean 40-character source SHA:

1. source tree;
2. platform and storage-backup binaries;
3. application and backup images;
4. both SPDX SBOMs;
5. OpenAPI, generated Go, and generated TypeScript contracts;
6. canonical configuration and migration bundles;
7. frontend distribution and rendered Compose configuration;
8. redacted outbound request capture;
9. prohibited-capability, binary-symbol, vulnerability, and license scans.

The manifest also binds reviewer ID/role/independence, review/expiry timestamps,
evidence hash, key fingerprint, and Ed25519 signature. Duplicate names/digests,
mutable or unsigned identities, dirty/wrong source, incomplete assertions,
extra/missing signed destinations, expiry, tampering, and an untrusted reviewer
all fail certification.

No signed current manifest exists in this repository because D6 is uncommitted
and formal prerequisites/reviews remain absent. The repository intentionally
does not include reviewer private keys.
