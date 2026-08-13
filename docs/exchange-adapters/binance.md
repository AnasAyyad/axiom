# Binance Spot adapter status

## Current A7 boundary

The repository contains a credential-free Binance Spot production-public client,
per-instrument collector, deterministic local books and completed-candle views,
and a raw/canonical Parquet recorder. `platform recorder` composes those pieces
for exactly BTC/USDT and ETH/USDT. The deterministic local emulator remains the
test-only conformance boundary and is not linked into the platform binary.

A7 is accepted under the repository owner's time-bounded, non-safety
availability/resynchronization waiver. Its preserved automated result remains
`qualified:false`; the waiver neither changes the 15-second SLO nor converts a
short public probe into soak evidence. Future exact-source qualification runs
remain the remediation path.

## Combined triangle stream experiment

The adapter exposes an observed-only combined subscription for the three
approved triangle instruments: BTC/USDT, ETH/USDT, and ETH/BTC. It opens one
credential-free Binance WebSocket URL containing all requested stream names,
while every received frame retains its own instrument, exchange event,
connection generation, local receipt time, and monotonic offset.

This is an experiment, not the production collector topology. A combined
connection does not turn separately published Binance instrument updates into
one atomic exchange snapshot. It also creates one shared connection failure
domain. The existing per-instrument snapshot bridge, sequence-gap recovery,
recording, and reconnect lifecycle remain unchanged until comparative evidence
supports a separately reviewed collector change.

Run the public-only regional probe explicitly with:

```bash
AXIOM_BINANCE_COMBINED_TRIANGLE_LIVE=1 \
AXIOM_BINANCE_COMBINED_TRIANGLE_DURATION=30s \
AXIOM_BINANCE_COMBINED_TRIANGLE_REGION=tokyo \
make binance-combined-triangle-live-probe
```

The probe warms the shared Binance clock, waits for depth events from all three
instruments, evaluates on every later depth event using the unchanged 250 ms
age/skew and 100 ms uncertainty policy, checks per-instrument update-sequence
continuity, and emits progress plus one JSON result. It records no orders and
its result is non-qualifying.

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

## Recovery transport and combined health

DNS resolution, TCP connect, TLS negotiation, and WebSocket upgrade share one
five-second setup deadline. Every DNS answer is validated before dialing, no
more than four validated public addresses are tried with bounded IPv4/IPv6
fallback, and losing connections are closed. Typed failures retain only bounded
stage durations, candidate/attempt counts, family, HTTP status, response
timing/size, and valid `Retry-After`; IPs, URLs, headers, payloads, and arbitrary
errors are never evidence fields.

Recorder clients reserve 768 Binance request-weight units for recovery. The
three simultaneous 5,000-level snapshots and clock samples use that recovery
class; unrelated public requests retain the ordinary public class. The
5,000-level snapshot remains required for the internal reserve even though the
published book retains 1,000 levels.

Clock sampling runs asynchronously with at most one request in flight. Clock
failure makes combined book/clock eligibility false without stopping a valid
book or stream, and retries in place using deterministic bounded backoff or a
larger valid `Retry-After`. Reconnect is reserved for stream, book, or
subscription defects. Readiness consumers use the immutable combined health
snapshot, never book health alone.

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

Formal runners synchronously journal each health transition, retry, operation
result, recovery action, and terminal transition while mirroring the bounded
record to the service log. Journal, rolling-status, recorder-flush, capacity,
or terminal-evidence failure terminates qualification. Five-minute status
replacement remains atomic.

At the official end time the runner freezes final combined health before
canceling collectors. Cancellation during setup, write, receive, clock,
snapshot, or backoff is normal termination: it must not invalidate a healthy
book, increment failure counts, or create a false reconnect.
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
