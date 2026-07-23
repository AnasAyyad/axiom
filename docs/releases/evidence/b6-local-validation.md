# B6 local validation evidence

## Status

B6 implementation is complete and targeted model and PostgreSQL qualification
have passed. The cumulative B4+B5+B6 repository gate, committed-source image,
supply-chain scans, and image-backed Compose smoke remain pending at this
implementation checkpoint and will be recorded here after they run.

Formal acceptance is not claimed. It remains held by A7/V1A and B1/B2/B3/B4/B5
formal predecessor acceptance, the explicitly deferred B1/B2 72-hour soaks,
and Product, Security, QA, and SRE approval. Engineering correctness is not
evidence or a guarantee of production profitability.

## Implemented authority and safety

- `rebalancing.v1b.1` deterministically optimizes immutable versioned reviewed
  route facts with exact provenance, cost, duration, risk, warning, and manual
  checklist evidence.
- Eligible B5 natural reverse arbitrage is preferred before a reviewed transfer
  route.
- Stale, unapproved, low-confidence, unavailable, ambiguous, incompatible, or
  network/chain-mismatched facts fail closed.
- Migration 000018 stores immutable fact sets and recommendations, validates
  selected evidence at deferred commit, and closes runtime, recorder, and
  read-only grants.
- No transfer or withdrawal execution transport, interface, endpoint, UI
  control, setting, credential, or compiled capability was introduced.

## Targeted qualification completed

- Optimizer and configuration unit tests, deterministic permutations, exact
  costs, natural reversal, fail-closed negative cases, p99, benchmark, and fuzz
  coverage pass.
- PostgreSQL 18 clean install through migration 000018 and exact migrations
  000001-000017 to 000018 upgrade pass against the dedicated
  `axiom_b6_clean_b6_test` and `axiom_b6_upgrade_b6_test` databases.
- Database qualification proves registered configuration identity, immutable
  reload, rejection of unapproved selected facts, exact aggregate evidence,
  and the closed role matrix.

## Pending checkpoint evidence

- Implementation and qualification commit identities.
- Final p99 and benchmark measurements from the cumulative gate.
- Reviewed configuration file hash.
- Cumulative `make b6-local-qualify` output.
- Clean committed-source image identity, reproducibility fingerprint, SPDX SBOM
  identity, Trivy result, and image-backed Compose smoke.
