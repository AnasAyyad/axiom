# ADR 0022: Bybit Demo Unified Trading key bundle

Status: accepted for V1C PR2.

## Context

Bybit Demo's API Transaction UI exposes Spot trading under one parent Unified
Trading selection. The live read-write Demo key created through that UI reports
this exact nonempty permission profile:

- `Spot=[SpotTrade]`
- `ContractTrade=[Order,Position]`
- `Options=[OptionsTrade]`
- `Derivatives=[DerivativesTrade]`

The same response reports empty Wallet and Exchange permissions. Axiom
previously required the key itself to contain only `SpotTrade`, which rejects
the provider-issued UI bundle even though Axiom has no non-Spot operation or
route.

## Decision

Bybit Demo startup accepts either:

1. a read-write UTA key with exactly `Spot=[SpotTrade]` and no other nonempty
   permission; or
2. a read-write UTA key with the exact four-category bundle above.

Partial bundles, additional values in those categories, and every other
nonempty permission category fail startup. In particular, Wallet, Exchange,
Earn, transfer, withdrawal, fiat, asset-management, and unknown permissions
remain rejected.

This exception changes only key admission. Axiom's authenticated Bybit surface
remains a closed enum of Demo REST/private-stream operations. Orders still
force `category=spot`, `isLeverage=0`, and `orderFilter=Order`; contract,
position, options, derivatives, margin, transfer, withdrawal, and generic
signed-request routes remain absent and fail before signing.

## Consequences

The Demo key has broader provider-level permissions than Axiom can exercise.
If stolen and used outside Axiom, it could submit non-Spot orders in the
isolated Demo account. It cannot access production funds because startup,
transport, proxy, host, and attestation policy bind Axiom to the independent
Demo account and `api-demo.bybit.com`.

The exact bundle is intentionally compiled and test-covered. A Bybit
permission change fails closed until this ADR, the specification, threat model,
tests, and owner/security acceptance are reviewed again.
