# ADR 0029: Cross-exchange actionable coherence experiment

- Status: Accepted
- Date: 2026-08-13
- Owners: Product owner, Market Data, Strategy Runtime

## Context

Cross-exchange shadow evaluation compares public Binance and Bybit books that
arrive through independent routes and use separate venue-clock estimates. The
strict B2 policy correctly requires corrected receive-time intervals to
overlap. The former shadow capture nevertheless introduced avoidable ambiguity:
collector notifications did not identify the committing venue or book version,
the decision boundary was sampled before both books were captured, and a
monotonic duration was reused as though it were an ingest ordinal.

Regional experiments also need to distinguish strict B2 failure from a bounded
locally actionable as-of pair without silently changing strategy admission.

## Decision

Both public collectors publish a coalesced signal backed by the latest exact
committed-book identity: exchange, instrument, connection generation, book
version, ingest ordinal, receive offset, and publish offset. Cross-exchange
shadow evaluation consumes each monotonic venue generation/version once,
captures the triggering view and the latest already-committed peer view, and
then samples the final shared process-monotonic decision boundary. The capture
uses the maximum real member ingest ordinal. A trigger that no longer matches
the current immutable book fails closed rather than being rebound silently.

The existing `axiom.coherent-view-policy.v1` remains the only cross-exchange
strategy admission policy. Its inclusive limits remain 250 ms book age, 250 ms
receive skew, 100 ms clock uncertainty, corrected-interval overlap, active
generations, no unresolved gaps, and no post-trigger evidence.

Alongside that strict verdict, public diagnostics evaluate the versioned
`axiom.cross-exchange-actionable-view-policy.v1`. It requires exactly one
Binance and one Bybit view for the same Spot instrument, age and receive skew at
or below 150 ms, clock uncertainty at or below 100 ms, active generations, no
gaps, and no post-trigger evidence. It does not require corrected intervals to
overlap. It additionally requires an exchange event timestamp, rejects a
timestamp beyond the corrected receive uncertainty interval, and caps the
conservative corrected source-to-receive delay at 250 ms.

The actionable verdict is diagnostic only. It cannot create a strategy input,
candidate, virtual order, sandbox order, or production order. Strict B2 failure
continues to skip the cross-exchange strategy decision.

The opt-in public probe records every sampled trigger and both verdicts as
NDJSON, plus bounded aggregate counts, rejection reasons, trigger venue, receive
skew percentiles, corrected overlap, member ages/source delays, and final
collector health. It uses no credentials and starts no formal qualification
clock.

## Consequences

Successful strict decisions are now causally bound to the exact venue event and
real ordering evidence. Failed actionable experiments cannot weaken B2. The
dual evidence reveals whether regional failures come from capture races,
staleness, uncertainty, interval separation, future exchange timestamps, or
source delay.

Coalescing intentionally favors the latest committed identity. Intermediate
updates may be omitted under load; omission loses an opportunity but cannot
admit future or mismatched evidence. The 500 ms loop remains only a fallback for
the latest unconsumed commit.

## Rejected alternatives

- Replacing or loosening B2 in place: this would reinterpret accepted formal
  evidence and weaken the current strategy contract.
- Enlarging clock uncertainty until intervals overlap: this makes timing less
  certain while appearing to improve results.
- Selecting an older superseded peer book solely to manufacture overlap: this
  no longer represents the newest information known at the decision boundary.
- Waiting indefinitely for matching venue timestamps: this adds look-ahead and
  hides the actual opportunity lifetime.
- Fetching two books by REST at the trigger: the responses are not atomic and
  introduce a slower second data path.

## Validation

Unit tests must prove exact commit identity, monotonic venue-version
consumption, trigger mismatch rejection, real member ordinals, post-capture
decision time, strict interval rejection, actionable disjoint-interval
acceptance, all age/skew/uncertainty/membership boundaries, deterministic
restoration, source-delay/future-time rejection, and unchanged B2 behavior.

Focused and repository-wide tests, race checks, formatting, JSON/shell checks,
and the prohibited-capability scan must pass. Regional public probes are
diagnostic and must not be described as qualification.

## Revisit when

Revisit only after sufficiently long regional evidence is reviewed together
with opportunity survival, fees, execution latency, partial-fill recovery, and
inventory-restoration behavior. Any proposal to admit actionable-only views
requires a new product-spec decision, a superseding ADR, new configuration,
tests, replay evidence, and formal qualification.
