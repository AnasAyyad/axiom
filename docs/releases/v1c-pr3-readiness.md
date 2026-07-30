# V1C PR3 readiness

## Current decision

**Implementation and non-soak qualification are complete; the formal C6 soak
and acceptance are pending.**

The branch adds the redacted sandbox operations API and React console,
purpose-bound controls, migration `000024`, bounded observability,
deterministic chaos/smoke infrastructure, and the default-off exact 72-hour
observer. It extends the merged C1-C5 state and does not add a parallel order,
risk, reconciliation, authorization, or incident state machine.

The cumulative C1-C6 aggregate, PostgreSQL 18.4 clean and exact B8-upgrade
paths, full repository verification, exact-source reproducible image,
non-root/read-only and compiled-absence checks, image-backed Compose smoke,
SPDX SBOM, and current Trivy gates pass for implementation commit
`b5ac868ec38d9204afc6f9fd4db6673aee10e852`.

The formal C6 run has not started. Therefore V1C is not formally qualified or
accepted. The later decision requires a 72-hour run from one exact source and
artifact identity, sealed terminal evidence review, and explicit
owner/security acceptance. Sandbox results are never profitability evidence.

## Safety decision

- The browser and API never receive exchange keys, secrets, signatures, TOTP
  seed material, or raw private payloads.
- The API has no exchange client. Test/demo entry persists only through the
  existing admission and dispatcher path for the credential-owning engine.
- Only Binance Spot Testnet and Bybit Demo are selectable; production-private
  submission remains absent.
- Entry remains buy-only for the C6 control, default-off, manually armed for 15
  minutes, and bounded by 10 USDT per order, 50 USDT per UTC day, one open
  capacity-bearing order per account, and two globally.
- Cancel, query, and reconciliation remain independently available during
  pause, lock, expiry, or uncertainty.
- Reset incidents increment account epoch and expose external adjustments with
  `pnl_effect=false`.

## Qualification boundary

The short smoke target proves the runner and immutable evidence contract only.
It returns `SMOKE_PASSED`, always leaves `qualified=false`, and cannot satisfy
the formal duration constraint. The manual formal target requires explicit
enablement, a clean source identity, exact build/executable/configuration
hashes, the two current account epochs and credential generations, and exactly
72 hours.

Any duplicate create, lost or double-posted fill, unresolved unknown,
reconciliation mismatch, suspense item, stale account, lease loss, persistence
failure, unsafe recovery/restart, production target, cap violation, alert SLO
miss, memory leak trend, operator abort, incomplete chaos set, or evidence
failure terminates without qualification.

## Evidence

The final non-soak commands and clean artifact identities are recorded in
[PR3 local validation](evidence/v1c-pr3-local-validation.md). Requirement
coverage is recorded in
[PR3 traceability](../requirements/v1c-pr3-traceability.md) and
[PR3 source coverage](../requirements/v1c-pr3-source-coverage.md).
