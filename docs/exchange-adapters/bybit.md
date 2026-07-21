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

## Book semantics

Every Bybit `snapshot` replaces the local generation atomically. A later
snapshot replaces it again. Deltas insert or update non-zero levels and delete
zero-quantity levels. Native update ID `1` is normalized as a full replacement,
even when the wire envelope says `delta`. Gaps, resets, decoder failures,
subscription acknowledgements, heartbeats, and connection generations are
recorded as explicit evidence.

## Recording

Raw frames are appended before decoding. Canonical outcomes link to the raw
ingest ordinal and payload hash. The V1B recorder composes three Binance and
three Bybit instrument collectors, shares one process-local ingest ordinal
source, and writes venue-specific crash-safe manifests under separate roots.
The V1A configuration continues to project to its original two-instrument
Binance recorder.

This adapter cannot accept credentials or call any account, order, withdrawal,
or transfer surface. All B1 activity is public recording or simulation.
