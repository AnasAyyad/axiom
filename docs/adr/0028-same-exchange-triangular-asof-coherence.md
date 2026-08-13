# ADR 0028: Same-exchange triangular as-of coherence

- Status: Accepted
- Date: 2026-08-13
- Owners: Product owner, Market Data, Strategy Runtime

## Context

The existing coherent-view reader was designed for cross-exchange comparisons.
It requires corrected UTC uncertainty intervals from independent venue clocks
to overlap. Reusing that condition for three instruments collected from one
exchange rejects otherwise safe same-process views and does not express the
triangular strategy's actual decision boundary.

## Decision

Triangular public-shadow evaluation is triggered once per newly committed
ETH/BTC book version. The runtime immediately captures the latest already
committed BTC/USDT, ETH/USDT, and ETH/BTC books from the selected exchange. It
does not issue a REST request, wait for another leg, or retry the same ETH/BTC
version after a later fast-leg update.

The versioned `axiom.same-exchange-triangular-view-policy.v1` policy requires
exactly three members from one exchange, one shared exchange-client clock
estimate, active connection generations, no unresolved gaps, no post-trigger
receive or ingest evidence, clock uncertainty at or below 100 ms, and book age
and inter-book receive skew at or below the immutable run's reviewed book-age
limit. The reviewed default is 100 ms and the hard configuration ceiling
remains 250 ms. Any failed condition skips the opportunity.

Corrected UTC interval overlap is not applied to this same-exchange view. The
existing `axiom.coherent-view-policy.v1` cross-exchange behavior, including its
250 ms age/skew limits and corrected-interval overlap requirement, is unchanged.
Candidate lifetime remains a separate 250 ms strategy constraint.

This decision changes public-data simulation admission only. It adds no
authenticated endpoint or external order capability; V1 production-private
orders remain structurally unavailable.

## Consequences

Same-exchange triangular evaluation follows the causal update stream and no
longer fails because three members sharing one client clock have disjoint
per-instrument corrected intervals. A quiet or stale ETH/BTC book naturally
reduces evaluation frequency. Conservative skipping can miss an opportunity,
but cannot create one from future, stale, gapped, or cross-exchange evidence.

Recorded coherent evidence carries a distinct policy version so restoration
can apply the same membership and skew rules without weakening legacy or
cross-exchange records.

## Rejected alternatives

- Loosening the generic corrected-interval rule: this would weaken the
  cross-exchange safety contract.
- Waiting until all books produce a matching timestamp: this introduces an
  arbitrary look-ahead window and can select evidence unavailable at the
  original trigger.
- Fetching all books through one REST call: neither supported exchange public
  stream supplies an atomic three-order-book snapshot, and REST would add a
  second, slower data path.
- Retrying one ETH/BTC version after BTC/USDT or ETH/USDT changes: that changes
  the trigger semantics and can repeatedly test one slow-leg observation.

## Validation

Runtime tests must prove inclusive 100 ms boundaries, shared-clock acceptance,
same-exchange membership, gap/generation/post-trigger/staleness/uncertainty
rejection, deterministic restoration, and unchanged cross-exchange interval
rejection. Bootstrap tests must prove one evaluation attempt per ETH/BTC book
version and use of the immutable reviewed strategy limit.

Regional probes and local tests are diagnostic only. The separately authorized
long-running campaign remains required before any qualification claim.

## Revisit when

Revisit if the trigger instrument changes, the exchange supplies a documented
atomic three-book stream, the reviewed freshness ceiling changes, shared-client
clock semantics change, or campaign evidence shows persistent unsafe skipping.
