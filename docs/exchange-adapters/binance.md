# Binance Spot adapter status

## Current A7 boundary

The repository contains a credential-free Binance Spot production-public client,
per-instrument collector, deterministic local books and completed-candle views,
and a raw/canonical Parquet recorder. `platform recorder` composes those pieces
for exactly BTC/USDT and ETH/USDT. The deterministic local emulator remains the
test-only conformance boundary and is not linked into the platform binary.

The A7 implementation is not phase-complete until the continuous 72-hour public
qualification and final candidate inspections pass. A successful public probe
or short harness smoke is not a soak result.

## Public capabilities

| Feature | V1A disposition | Constraint |
|---|---|---|
| Public market data | Supported | Public data only |
| Instrument metadata | Supported | BTC/USDT and ETH/USDT only |
| Historical trades | Supported | Bounded recent public requests |
| Historical candles | Supported | UTC `4h` only |
| Order-book snapshots | Supported | Compiled depths through 5,000 levels |
| Incremental depth | Supported | 100 ms combined public stream |
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

Current production-public metadata and stream shapes are also covered by an
opt-in network probe. Upstream fields remain strictly allowlisted: an unknown
field or route fails closed and requires a reviewed code change.

## Book and connection lifecycle

Each instrument has one ordered writer. A generation opens the depth, trade,
and 4-hour candle streams, samples public server time, buffers depth deltas,
loads a 5,000-level snapshot, discards obsolete updates, applies the bridging
update, and only then publishes a healthy immutable view. The view retains the
best configured 1,000 levels while a bounded internal reserve reduces depth
loss after deletions.

Sequence gaps, conflicting duplicates, crossed or empty books, stale data,
clock uncertainty, malformed frames, overload, and connection failures make the
generation ineligible. Reconnect uses deterministic capped backoff, restores
all subscriptions, resynchronizes from a new snapshot, and renews a connection
before 24 hours.

Exchange time, local receipt, processing, and publication time are separate.
Every view also carries connection ID, generation, source sequence, ingest
ordinal, version, and monotonic freshness offsets.

## Recording and qualification

Wire bytes are appended before decoding. A successfully appended raw record is
always completed with a canonical or decoder outcome even across shutdown.
Lifecycle, subscription, snapshot, rebuild, gap, clock, trade, candle, and depth
facts share the same linkage. Five-minute bounded segments use Parquet/Zstd,
atomic finalization, cumulative checksum manifests, explicit source gaps, and a
bounded-memory replay verifier.

The formal qualification runs both approved instruments and all three streams
for at least 72 continuous hours. It records latency histograms, reconnects,
gaps, rebuilds, book eligibility, forced-GC heap samples, manifest identity, and
the canonical replay checksum. Its declared pending-recorder ceiling is 512 MiB;
the process container retains a separate 2 GiB hard limit.

## Safety

No Binance credential field, signer, private route, account client, external
order method, test environment, or arbitrary production URL exists in the A7
boundary. The two exact public hosts are compiled in code. Redirects, proxies,
private DNS results, credential-bearing headers, duplicate queries, unapproved
symbols, and non-public stream names are rejected before use.

## References

- [Endpoint policy](../configuration/endpoint-policy.md)
- [Contracts and emulator](contracts-and-emulator.md)
- [ADR-0010](../adr/0010-a6-public-contract-emulator-boundary.md)
- [Real-money lock test plan](../security/real-money-lock-test-plan.md)
