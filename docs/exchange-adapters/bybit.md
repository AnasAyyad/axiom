# Bybit production-public adapter

B1 adds a credential-free Bybit V5 Spot adapter at the compiled endpoint set
`bybit-public-v1`:

- REST: `https://api.bybit.com`
- WebSocket: `wss://stream.bybit.com/v5/public/spot`
- instruments: `BTC-USDT`, `ETH-USDT`, and native `ETH-BTC`
- retained order-book depth: 1,000
- candle intervals: 15 minutes, 1 hour, and 4 hours

The endpoint policy accepts only reviewed public market routes and rejects
authentication headers, arbitrary origins, private paths, and unknown query
shapes. The client exposes metadata, server time, snapshots, public trades,
tickers, candles, public streams, health, and request-budget telemetry only.

The gated `bybit-ethbtc-trigger-live-probe` is a public-data-only,
non-qualifying timing experiment. It opens independent depth streams for
BTC/USDT, ETH/USDT, and ETH/BTC using one shared client clock, evaluates only
when ETH/BTC changes, and compares the latest already-observed views against
50, 100, 150, and 250 millisecond age bounds. It rejects future data, excessive
clock uncertainty, and sequence regression. It does not reconstruct the
production books, change strategy behavior, use credentials, or place orders.

## Book semantics

Every Bybit `snapshot` replaces the local generation atomically. A later
snapshot replaces it again. Deltas insert or update non-zero levels and delete
zero-quantity levels. Native update ID `1` is normalized as a full replacement,
even when the wire envelope says `delta`. Gaps, resets, decoder failures,
subscription acknowledgements, heartbeats, and connection generations are
recorded as explicit evidence.
## Recovery transport and combined health

DNS resolution, TCP connect, TLS negotiation, and WebSocket upgrade share one
five-second setup deadline. Every DNS answer is validated before dialing, no
more than four validated public addresses are tried with bounded IPv4/IPv6
fallback, and losing connections are closed. Subscription and heartbeat writes
have a two-second deadline.

Typed transport failures preserve bounded DNS/TCP/TLS/upgrade/write durations,
candidate and attempt counts, address family, HTTP status, response timing and
size, and valid `Retry-After`. They never retain IPs, URLs, headers, payloads,
or arbitrary error text. Deterministic retry waits for the larger of local
backoff and a valid upstream `Retry-After`.

Initial clock acquisition and book synchronization run concurrently. Periodic
clock sampling remains off the ordered event loop with at most one request in
flight. A clock failure marks combined eligibility false but keeps a valid book
and stream alive, then retries in place. Reconnect is reserved for stream,
book, or subscription defects. Recorder and shadow readiness consume the
immutable combined book/clock health snapshot.

Formal runners synchronously append bounded lifecycle evidence to their
hash-chained journal and mirror it to the service log. Journal, status,
recorder-flush, capacity, or terminal-evidence failure terminates
qualification. Final combined health is frozen at the official end time before

## Recording

Raw frames are appended before decoding. Canonical outcomes link to the raw
ingest ordinal and payload hash. The V1B recorder composes three Binance and
three Bybit instrument collectors, shares one process-local ingest ordinal
source, and writes venue-specific crash-safe manifests under separate roots.
The V1A configuration continues to project to its original two-instrument
Binance recorder.

This adapter cannot accept credentials or call any account, order, withdrawal,
or transfer surface. All B1 activity is public recording or simulation.
