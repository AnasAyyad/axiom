# ADR-0006: V1A prohibits every external order path

- **Status:** Accepted
- **Date:** 2026-07-12
- **Scope:** V1A safety boundary

## Context

V1A is public-data research and simulation. The approved plan is stricter than later V1 releases: it excludes testnet and demo as well as production trading. Configuration-only disabling is insufficient because a hidden or accidentally enabled transport could create external side effects.

## Decision

V1A enables only `backtest`, `replay`, `paper`, and `shadow`. Every broker is a simulator. Shadow reads code-allowlisted Binance public Spot REST/WebSocket routes without credentials and cannot submit, cancel, or query an exchange order.

V1A source, binary, API/OpenAPI, UI, configuration/schema, database enums/constraints, container image, and Compose contain no authenticated exchange constructor, credential key/reference, signer, private/account/order route, external order interface, production broker, testnet/demo service, or `live` mode. Capability descriptions may display such operations as unavailable but expose no callable methods. Unknown modes, credentials, hosts, products, or prohibited keys fail startup before network I/O.

CI and release evidence scan source, generated contracts, symbols/packages, image, Compose, and configuration, and inspect outbound hosts/routes. Spot-only, approved-asset, owned-inventory, allocator, risk, reducer, and journal checks still apply to simulated orders.

## Consequences

- V1A cannot validate authenticated integration; that work waits for gated V1C phases.
- No operator action or environment variable can arm an external order.
- Public market-data ingestion and realistic simulated execution remain available.
- Adding a future sandbox adapter is a deliberate new release capability, not a dormant toggle.

## Rejected alternatives

- A disabled production broker or hidden feature flag: violates the absence requirement.
- Shipping signing/private code but relying on missing credentials: configuration error or compromise could activate it.
- Including testnet/demo early: violates the V1A phase gate and approved plan.
- A generic exchange client that owns both public and order methods: breaks the public-client trust boundary.

## Validation

Prohibited-capability scans, build-symbol/package inspection, schema/API/UI tests, configuration fuzzing, Compose profile rendering, container inspection, and captured outbound requests must independently prove that only Binance public routes are reachable and no external-order method exists.

## Revisit when

Only after V1A passes its full gate and V1C explicitly begins under its credential threat model, sandbox allowlists, reconciliation, fencing, manual arming, caps, and evidence requirements. Real-money production orders remain prohibited throughout V1 and require a separate future specification and approval.
