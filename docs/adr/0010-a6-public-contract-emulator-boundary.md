# ADR-0010: A6 public contracts and test-only emulator boundary

- **Status:** Accepted
- **Date:** 2026-07-14
- **Scope:** V1A Phase A6 exchange contracts and conformance emulator

## Context

A6 must define reusable exchange contracts, explicit capabilities, deterministic
fault injection, and typed handling of unsupported behavior. V1A simultaneously
requires every authenticated transport, credential, signer, private route, and
external order method to be absent. A broad exchange client or a dormant broker
interface would violate ADR-0006 even if its implementation returned an error.

## Decision

V1A exposes callable interfaces only for public snapshots, public streams,
instrument metadata, public trade history, public candle history, and capability
reporting. Later account, submission, cancellation, and reconciliation behavior
is represented as closed capability and retry-policy classifications. Requiring
an unavailable feature returns a stable typed capability error; it does not
expose a private-operation transport method.

The A6 conformance emulator is a separate package that binds only to an
ephemeral `127.0.0.1` listener. Its adapter can be constructed only from an
emulator server value, not an arbitrary URL. The emulator supports real local
REST and WebSocket interactions, exact scripted request matching, bounded
payloads, deterministic reconnect generations, and canonical transcript hashes.
Later-release account/order fault facts are inert scripted conformance frames;
they are not exchange requests and create no authenticated implementation.

The platform binary does not import the emulator. Binance A6 code contains only
the public capability descriptor and strict fixture normalization. Production
public endpoint enforcement, connection lifecycle, books, and recording remain
A7 work.

`golang.org/x/net/websocket`, already pinned in the dependency graph, is a direct
dependency solely for the local conformance protocol. The emulator accepts no
authorization or cookie response headers, no non-GET REST step, and no unsafe
path shape.

## Consequences

- A6 proves exchange-neutral public contracts without adding a dormant private
  path.
- Capability displays can explain unavailable functions with stable errors.
- Deterministic emulator faults can validate future adapters without depending
  on an external exchange environment.
- A7 must add a separate hardened production-public transport and cannot reuse
  the emulator origin or constructor.
- Authenticated sandbox interfaces remain deferred to V1C and require a new
  threat model and endpoint boundary.

## Rejected alternatives

- One broad client with public and private methods: creates a forbidden callable
  V1A path.
- Placeholder account or external-order methods that always fail: dormant method
  presence violates the absence gate.
- An emulator base URL in application configuration: could weaken the compiled
  production-public endpoint policy.
- External test environments for conformance: nondeterministic and unable to
  exercise the required fault matrix safely.

## Validation

- Compile-time assertions prove the emulator adapter implements every public
  interface.
- Capability tests exhaustively require public features and reject private,
  order, cancellation, identifier, and reconciliation features with typed
  errors.
- Two complete emulator fault runs must produce the same transcript hash.
- Golden fixtures cover strict normalization, unknown native status retention,
  malformed payloads, and schema changes.
- Source, dependency, symbol, prohibited-capability, and platform-import scans
  prove no signer, credential, private route, callable external-order method, or
  emulator link exists in the V1A binary.

## Revisit when

A7 begins production-public transport work or V1C begins authenticated sandbox
integration. Any authenticated interface requires a new ADR and may not weaken
ADR-0006 for V1A or the program-wide production-order prohibition.
