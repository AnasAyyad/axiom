# ADR-0014: V1B public multi-exchange recording

- **Status:** Accepted
- **Date:** 2026-07-21
- **Scope:** V1B B1 public adapters and recorder composition

## Context

V1A's consumer contracts are exchange-neutral, but its production recorder
composition, raw/canonical sink, and collector lifecycle are Binance-specific.
B1 must add Bybit without introducing credentials, arbitrary origins, a second
recording architecture, or cross-exchange ordering assumptions that belong to
B2.

## Decision

Ticker, lifecycle, and raw-before-canonical recording facts are owned by the
common public exchange contracts. Binance compatibility types remain aliases so
historical V1A fixtures and manifests are unchanged.

Each exchange receives a separate append-only recorder and dataset manifest
under one recorder process. One process-level ingest-ordinal allocator is shared
across those recorders, preserving an unambiguous local order without pretending
that exchange timestamps are a global clock. B2 may join committed views later;
B1 never rewrites V1A manifests into a combined schema.

Bybit production-public origins and routes are compiled. The public client has
no credential, signer, header injection, private endpoint, order, transfer, or
withdrawal input. Snapshot messages replace the local book at any time; a depth
message with update ID 1 is normalized as a replacement even if the native type
says delta. Other deltas use exact insert/update/delete semantics.

## Consequences

- Recorder composition becomes exchange/instrument driven while the existing
  modular-monolith process topology remains unchanged.
- Binance and Bybit retain exchange-specific synchronization logic behind the
  same consumer-owned contracts.
- B1 datasets remain individually attributable per exchange. Coherent immutable
  cross-market views remain B2 work.
- The recorder process remains public-only and receives no new secret.

## Rejected alternatives

- One broad client with public and future authenticated methods: rejected
  because it weakens the compile-time credential/order boundary.
- Arbitrary configurable Bybit origins: rejected because redirects, DNS
  rebinding, and private-host mistakes must fail before network I/O.
- Treat Bybit's update IDs like Binance's contiguous sequence bridge: rejected
  because the exchanges publish different replacement and sequencing contracts.
- Rewrite V1A manifests into a multi-exchange format: rejected because accepted
  historical hashes are immutable.

## Validation

- Compile-time contract conformance for Binance, Bybit, and emulator.
- Golden normalization for snapshots, deltas, deletion, reset, ticker, trade,
  candle, subscription, heartbeat, and malformed/unknown payloads.
- Endpoint, redirect, DNS, header, source, and binary absence tests.
- Recorder linkage, bounded-queue, restart, and manifest-recovery tests.
- Short production-public validation and isolated continuous 72-hour soak.

## Revisit when

- B2 introduces coherent cross-market view persistence.
- An official Bybit public protocol change invalidates a compiled route or
  snapshot/reset rule.
- Another public adapter proves the shared lifecycle or recording facts are not
  sufficient.
