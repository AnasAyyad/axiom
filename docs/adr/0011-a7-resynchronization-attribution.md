# ADR-0011: A7 resynchronization timing and fault attribution

- **Status:** Accepted
- **Date:** 2026-07-21
- **Scope:** V1A A7 and V1B B1 production-public collectors and qualification evidence

## Context

A7 requires the order book to return from loss of health to `HEALTHY` within a
15-second p95 objective while the public REST snapshot path is available. A
recovery can be delayed by collector logic, local resource pressure, network or
DNS failure, or an objectively observable upstream HTTP response. A duration by
itself cannot identify which boundary caused the delay, and excluding a slow
sample after observing its outcome would make qualification non-reproducible.

Two preserved formal-run failures exposed both concerns. The first completed 72
hours but retained escalating reconnect backoff after successful recovery. The
next candidate reset that backoff correctly but stopped early when a periodic
flush observed a raw record between its raw and canonical recorder calls. The
second failure was a local recorder concurrency defect, not evidence of an
upstream outage.

## Decision

The 15-second p95 objective remains an all-sample qualification gate. A formal
run does not remove, relabel, or forgive a sample based on fault attribution. A
clearly evidenced external incident may justify preserving the failed run and
starting a new candidate run, but it cannot turn the failed artifact into a
pass.

Resynchronization starts at the loss of book health and ends only when a later
generation reaches `HEALTHY`. It includes every unsuccessful generation and
reconnect delay in that cycle. A later independent disconnect begins a new
cycle. Backoff escalates only across consecutive attempts that have not reached
health and resets to attempt one after recovery.

Every lifecycle diagnostic uses bounded fields: cycle, attempt, generation,
phase, stage, fixed reconnect reason and cause, operation, typed failure kind,
HTTP status, bounded retry-after, clock offset and uncertainty, operation and
resynchronization durations, snapshot sequence, buffered depth, and whether
health was reached. Attribution is derived only from those facts:

Both Binance and Bybit retain request, response-header, and response-body
durations plus bounded byte counts and declared content length. They distinguish
a timeout while waiting for headers from a timeout while consuming the body,
an interrupted body, a close failure, an empty success body, and an oversized
body. These facts distinguish an exchange response, network interruption,
contract mismatch, and local collector failure without retaining the body, URL,
remote address, or arbitrary error text.

- explicit HTTP 429/418 or 5xx is `upstream`;
- DNS, timeout, TCP-connect, and network-I/O causes are `network`;
- queue, buffer, sequence, validation, recorder, and local rate-budget causes
  are `internal`;
- planned connection renewal is `scheduled`;
- successful return to health is `recovered`;
- evidence that cannot support a narrower conclusion remains
  `external_unclassified` or `unclassified`.

Recorder pressure is an explicit lifecycle boundary. Each recorder exposes
pending raw and canonical counts, pending and reserved bytes, its hard limit,
the proactive flush threshold, and the session high-water mark. Crossing one
quarter of the hard limit emits an edge-coalesced signal; the runner flushes the
complete raw/canonical prefix immediately and leaves any in-flight suffix
pending. The lower threshold preserves headroom for segment construction and
compression. Capacity-triggered and scheduled flushes have distinct journal
phases and triggers, and either failure fails qualification closed.

Collector completion is monitored independently of the five-minute sampling
and flush timers. An unexpected clean return or any terminal error immediately
marks that instrument stopped, appends a bounded terminal event, atomically
updates rolling status, cancels the sibling collector, and ends the run. Adapter
recorder wrappers preserve the underlying bounded recorder code, stage, class,
cause, and errno. A service therefore cannot remain apparently healthy after
its collector goroutines have exited.

Arbitrary error text, URLs, addresses, response bodies, credentials, and
payloads are never retained. Recent collector diagnostics are memory-bounded
with an explicit dropped count. Immediate structured lifecycle records go to
the service log. Qualification phase events also go to an append-only,
synchronously written SHA-256 hash-chained journal. Rolling status is replaced
atomically, terminal evidence verifies the journal chain, and recorder flush or
status/journal write failure fails the run closed. A7 and B1 use separate output
roots, status files, journals, terminal reports, and service logs.

## Consequences

- The gate continues to measure the user-visible availability of Axiom's public
  market-data boundary, including dependencies it must tolerate.
- A failed run can distinguish a demonstrated local defect from an observed
  upstream or network trigger without changing the pass rule.
- Diagnosis has immediate service-log evidence, five-minute status snapshots,
  durable qualification phase events, and a terminal report tied to the exact
  source commit.
- Rolling and terminal evidence state whether each declared collector is still
  running and report recorder usage and high-water facts.
- Bybit decoder-error canonical records retain only bounded failure kind,
  operation, and fixed cause code while their recorder link identifies the
  exact preserved raw frame. Public Spot `BT` and `RPI` trade classifications
  are accepted as trade facts; unknown fields and malformed envelope, identity,
  sequence, numeric, ticker, book, and candle forms remain fail-closed with
  fixed diagnostic causes.
- The bounded in-memory diagnostic ring can roll over during an extreme event;
  the dropped count makes that visible and the service log remains the detailed
  immediate record.

## Rejected alternatives

- Raise or remove the 15-second objective: weakens the accepted A7 requirement.
- Exclude upstream-attributed samples: makes the primary SLO dependent on
  post-hoc classification and hides observed unavailability.
- Infer Binance fault from duration alone: the evidence does not support that
  conclusion.
- Retain raw errors or response bodies: creates unbounded, potentially
  sensitive qualification data.

## Validation

Deterministic lifecycle tests cover attempt reset and escalation, complete
loss-to-health timing, independent cycles, every reconnect reason, the exact
15-second boundary, bounded diagnostics, cancellation, recorder failure,
scheduled renewal, and high-cycle stress on each exchange. Transport tests
independently force response-body timeout, interruption, empty-body, and
oversize cases and assert bounded timing and byte metadata. Recorder tests cover
in-flight raw/canonical flush interleaving and bounded filesystem causes.
Qualification tests cover atomic status replacement, hash-chain tampering, and
fail-closed flush, status, and journal failures. Targeted race tests and both
public harness smokes must pass before formal runs.

## Revisit when

The authoritative A7 SLO changes, the public endpoint contract provides a
stronger availability signal, or qualification adopts a separately specified
dependency-adjusted SLO in addition to—not in place of—the all-sample gate.
