# Exchange contracts and deterministic emulator

## Scope

Phase A6 establishes the exchange-neutral public-data boundary and local
conformance harness. It does not implement a production-public Binance client,
an order book, recording, or any authenticated exchange operation.

## Public contracts

The consumer-facing interfaces under `internal/exchanges/contracts` are narrow:

- `MarketDataSource` loads a bounded snapshot or opens a normalized public
  stream.
- `InstrumentCatalog` loads canonical, versioned public metadata.
- `HistoricalReader` loads bounded public trades and completed candles.
- `CapabilitySource` returns an immutable environment/version-aware descriptor.

Every financial value uses Axiom exact domain types. Exchange-native DTOs remain
inside the Binance normalization boundary. Raw payload hashes and native IDs or
statuses are retained as audit facts.

There is no callable account, signer, private transport, or external-order
interface in V1A. Account, order-type, cancellation, client-ID, and
reconciliation support appear only as descriptive capability features fixed to
`unsupported`. `Descriptor.Require` returns `capability_unsupported` for them.

## Capability identity

A descriptor records exchange, environment, account mode, version, UTC
observation time, support disposition, and sorted constraints. Unknown,
duplicate, out-of-order, or constrained-while-unsupported entries fail closed.

The Binance V1A descriptor supports public metadata, snapshots, incremental
depth, trades, and the configured four-hour candle interval. Checksums and all
private or order-related features are explicitly unsupported.

## Error and retry policy

Stable sanitized error kinds cover capability, rate limit, transient outage,
timestamp, filter, insufficient funds, maintenance, validation, ambiguous
state, and cancellation. Errors contain an operation class and optional retry
delay only; raw payloads, URLs, headers, and native response text are excluded.

Only bounded public reads can retry. Backoff is exponential, capped by policy,
honors a larger official retry delay, and adds deterministic keyed jitter. A
context cancellation stops before the next attempt. Abstract submission
ambiguity classifies as `reconcile`; it never classifies as a blind retry.

The shared weighted rate budget uses integer units and deterministic logical
time. Public reads cannot consume the configured recovery reserve. Recovery
work may use the reserve, time regression fails closed, and a request heavier
than total capacity is rejected rather than waiting forever.

## Emulator protocol

The emulator uses a validated immutable `Scenario` containing exact GET request
steps and WebSocket connection generations. It binds to an ephemeral IPv4
loopback listener and exposes no deployed configuration input. Every request,
response, connection, frame, and close receives a monotonic transcript ordinal
and payload hash. The ordered transcript has a canonical SHA-256 result.

The retained conformance matrix covers:

- snapshots and streams;
- disconnect and reconnect generations;
- gaps, duplicates, regressions, malformed frames, and stale timestamps;
- slow responses, throttling, and official retry delay;
- filter and schema changes;
- asynchronous acknowledgement, partial and late fill facts;
- ambiguous state, account reset, and reconciliation snapshots.

The latter facts are inert local frames for future reducer/reconciliation tests.
They do not submit anything to an exchange and are not linked into the platform
binary.

## Fixture and normalization rules

Sanitized official-style fixtures live under `testdata/exchanges/binance` with
an expected normalized golden file. Decoders reject unknown JSON fields and
malformed exact decimals. Unknown native instrument status is retained in the
result while a typed validation error fails the event closed. Raw fixture hashes
make the native evidence reviewable without adding secrets.

## Failure behavior

- Script mismatch returns a local conflict and does not advance the scenario.
- Oversized payloads, unsafe headers, non-GET steps, path traversal, negative
  delays, and ambiguous scripts are rejected before the server starts.
- Stream disconnect is a typed transient error; caller cancellation is a typed
  canceled error.
- Normalization failure returns no eligible market event.
- Unsupported capability checks are stable typed errors, never silent no-ops.

## Validation

Run the narrow gate with:

```text
go test ./internal/exchanges/...
go test -race ./internal/exchanges/...
node scripts/check-a6-exchange-boundary.mjs
scripts/check-prohibited-capabilities.sh
```

The emulator tests require permission to bind a local loopback port. Full A6
completion also requires the cumulative repository verification command and a
clean platform dependency/symbol inspection.

## Known limitations

- Production-public endpoint allowlisting, DNS/redirect hardening, reconnect
  policy, local-book synchronization, recorder integration, and the 72-hour soak
  belong to A7.
- The emulator approximates protocol behavior; it is deterministic conformance
  infrastructure, not evidence of exchange availability or strategy viability.
- V1A has no authenticated integration and no external order side effect.
