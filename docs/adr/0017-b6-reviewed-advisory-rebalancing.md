# ADR-0017: Reviewed deterministic advisory rebalancing

- **Status:** Accepted
- **Date:** 2026-07-23
- **Scope:** V1B B6 inventory distribution and advisory route selection

## Context

B5 can identify an inventory imbalance, but restoring an asset between venues
depends on facts that public market books do not establish: venue availability,
exact network and chain identity, deposit and withdrawal status, fees, delay
ranges, operational risk, and human review. Missing or stale compatibility
evidence must not be interpreted as permission. V1 also cannot gain an
authenticated transport merely because it can describe a route.

## Decision

B6 is an in-process deterministic advisory optimizer over an immutable,
versioned asset-at-exchange graph. Every fact carries its logical version,
source, observer, observation and expiry times, exact confidence, explicit
approval, complete cost components, duration range, risk score, warnings,
manual checks, and a canonical provenance hash.

Only the latest logical fact version participates. A selected fact must be
current at the decision time, approved no later than that time, available,
compatible, unambiguous, sufficiently confident, and large enough for the
requested quantity. A transfer fact additionally requires exact equality of
network, source chain, and destination chain plus current deposit and withdrawal
availability. Missing, conflicting, expired, or unapproved evidence fails
closed.

An eligible B5 natural reverse plan is preferred before any transfer route.
Otherwise B6 enumerates bounded simple paths and orders complete candidates by
exact total cost, upper duration, risk, hop count, and canonical path identity.
Every recommendation retains all eight cost components, duration bounds,
warnings, a minimum four-step operator checklist, configuration and fact-set
hashes, selected fact versions and provenance hashes, and one canonical output
hash.

The package exposes data types, validation, hashing, and `Optimize` only. It has
no authenticated exchange adapter, HTTP transport, signer, credential, transfer
client, withdrawal client, execution command, API endpoint, setting, or UI
control. PostgreSQL stores immutable input and output evidence but grants no
external side-effect authority.

## Consequences

- Route recommendations can be reproduced exactly from retained reviewed facts
  and configuration instead of reconstructing mutable venue knowledge.
- Natural inventory-restoring arbitrage is chosen before an otherwise cheaper
  manual transfer when it remains eligible.
- Cost, time, compatibility, and risk uncertainty stay visible rather than
  collapsing into one opaque score.
- Operators receive explicit warnings and checks, but all external action
  remains outside Axiom and is never represented as completed by B6.
- The latest reviewed fact can revoke an older eligible fact; B6 does not fall
  back to superseded evidence.

## Rejected alternatives

- Automatic transfer or withdrawal: rejected because V1 forbids authenticated
  side effects and B6 is advisory-only.
- Treating matching asset tickers as network compatibility: rejected because
  chain and venue semantics can differ.
- Falling back to an older approved fact when the latest is unavailable:
  rejected because it silently ignores revocation or changed conditions.
- Binary floating-point route weights: rejected because exact cost ordering and
  reproducibility are required.
- Unbounded graph search: rejected because qualification must retain explicit
  latency and work ceilings.

## Validation

- Permutation, tie-break, natural-reversal, exact-cost, duration, warning,
  checklist, stale, unapproved, low-confidence, ambiguous, incompatible,
  network-mismatch, tamper, latest-version, fuzz, race, benchmark, and declared
  p99 tests.
- PostgreSQL 18 clean install and exact B5-to-B6 upgrade qualification with
  deferred aggregate checks, restart reload, immutability, provenance
  rejection, and least-privilege role assertions.
- Source, API, UI, configuration, generated binary, image, and prohibited
  capability scans proving that no transfer or withdrawal executor exists.

## Revisit when

- A venue changes network identifiers or publishes a stronger signed
  compatibility fact that the versioned schema must represent.
- The reviewed asset or venue graph expands enough to require a different
  bounded deterministic search strategy.
- V1C proposes authenticated sandbox movement; that requires a separate
  architecture and safety decision and cannot inherit B6 authority.
