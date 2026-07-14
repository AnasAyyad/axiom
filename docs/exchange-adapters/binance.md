# Binance Spot adapter status

## Current A6 boundary

The repository currently contains the credential-free Binance V1A capability
descriptor and strict sanitized-fixture normalization only. The deterministic
local emulator implements the shared public contracts for conformance tests.

Phase A7 owns the production-public REST/WebSocket transport, compiled endpoint
allowlist, server-time synchronization, snapshot/delta book algorithm,
reconnect/resubscribe behavior, raw/canonical recording, manifests, and 72-hour
soak. Those functions must not be inferred from the A6 package layout.

## A6 public capabilities

| Feature | V1A disposition | Constraint |
|---|---|---|
| Public market data | Supported | Public data only |
| Instrument metadata | Supported | Canonical BTC-USDT/ETH-USDT eligibility remains A7 |
| Historical trades | Supported by contract/emulator | Bounded requests |
| Historical candles | Supported by contract/emulator | `4h` |
| Order-book snapshots | Supported by contract/emulator | Bounded depth |
| Incremental depth | Supported by contract/emulator | Real-time or 100 ms cadence |
| Checksums | Unsupported | No constraints |
| Private data | Unsupported | No callable method |
| Orders and order-type variants | Unsupported | No callable method |
| Cancellation and client-generated IDs | Unsupported | No callable method |
| Reconciliation | Unsupported | No callable method |

## Normalization

A6 fixtures cover exchange information, depth snapshots, incremental depth,
public trades, candle history, and candle stream frames. Prices, quantities,
notional filters, and candle values parse directly into exact domain decimals.
Native symbol/status and raw payload hashes are preserved. Unknown status,
schema drift, malformed arrays, invalid decimals, and inconsistent candle
ranges fail closed with typed sanitized errors.

## Safety

No Binance credential field, signer, private route, account client, external
order method, test environment, or arbitrary production URL exists in this A6
boundary. The emulator is loopback-only test infrastructure and is absent from
the platform binary dependency graph.

## References

- [Endpoint policy](../configuration/endpoint-policy.md)
- [Contracts and emulator](contracts-and-emulator.md)
- [ADR-0010](../adr/0010-a6-public-contract-emulator-boundary.md)
- [Real-money lock test plan](../security/real-money-lock-test-plan.md)
