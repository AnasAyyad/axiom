# Advisory inventory rebalancing V1B

## Scope and authority

`rebalancing.v1b.1` is B6's deterministic read-only optimizer for BTC, ETH, and
USDT inventory distribution across Binance and Bybit. It consumes immutable
reviewed facts and emits recommendations. It does not initiate, submit,
execute, or observe an external transfer or withdrawal.

The authoritative package is `internal/rebalancing`. The reviewed
`axiom.config.v1b.5` graph fixes the optimizer, fact schema, cost model, venue
and asset universe, bounded search limits, confidence threshold, total cost,
duration, risk, checklist, provenance, compatibility, and advisory-only
contracts for one run.

## Versioned reviewed facts

Each node is one approved asset at one approved exchange. A trade edge stays on
one exchange and changes assets through an approved Spot instrument. A transfer
edge retains the asset and changes exchanges, but remains descriptive data
only.

Every edge records:

- a stable logical identity, version, immutable fact ID, and canonical hash;
- source, observer, observed time, expiry, confidence, and explicit approval;
- availability, ambiguity, compatibility, deposit, and withdrawal status;
- exact network, source-chain, and destination-chain identifiers;
- exact fee, spread, depth, delay, network-fee, compatibility,
  volatility-risk, and operational-risk cost components;
- minimum and maximum duration, risk score, warnings, and manual checks.

Only the latest logical version is considered. It must be current and approved
at the decision time, meet the 0.80 confidence threshold, cover the quantity,
and remain available, compatible, and unambiguous. A transfer route additionally
requires exact equality of its network and both chain identifiers plus current
deposit and withdrawal availability. Missing or conflicting evidence rejects
the edge.

## Deterministic selection

For an imbalance from one venue to another, B6 first considers eligible B5
natural reverse plans. Such a plan sells owned overweight inventory for USDT on
the source venue and buys the same asset with USDT on the depleted venue. An
eligible natural reverse plan wins before every transfer route.

If no natural reverse is eligible, B6 enumerates simple paths with at most six
hops and 1,024 complete candidates. Candidates above 25 USDT total cost, seven
days upper duration, or risk score one are rejected. Remaining routes are
ordered by:

1. exact total cost;
2. upper duration;
3. risk score;
4. hop count;
5. canonical path identity.

The result retains every selected fact and cost component, both duration
bounds, warnings, at least four operator checks, configuration and fact-set
hashes, and a canonical recommendation hash. Map order, input permutation, and
goroutine scheduling cannot change the selected result.

## Persistence and access

Migration 000018 stores immutable fact sets, versioned route facts,
recommendations, selected fact links, and ordered checklist steps. Deferred
constraints reject incomplete aggregates, stale or unapproved selected facts,
copied-version or provenance mismatches, broken graph continuity, malformed
natural reversals, and cost/duration/risk totals that differ from the selected
facts.

The runtime role may append and read B6 evidence. The reporting role is
read-only. The recorder receives no B6 access because it does not author
reviewed route facts. No table or function represents external execution.

## Limitations

B6 does not prove that a manual action will complete, that venue facts remain
true after the decision time, or that a strategy is viable or profitable. It
does not contain credentials, private endpoints, authenticated transports,
signers, external orders, testnet/demo actions, margin, derivatives, leverage,
short sales, transfers, withdrawals, borrowing, lending, or staking. Formal
acceptance remains subject to predecessor, deferred soak, and
Product/Security/QA/SRE gates.
