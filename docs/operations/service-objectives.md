# V1A service objectives

## Status and certification rule

These are the initial acceptance objectives from
`crypto_bot_v1_codex_spec.md` for the declared reference server and load. They
are release requirements, not measurements. No objective is met until its
instrumentation, workload, observation window, raw result, and acceptance
artifact exist. A passing narrow test cannot certify a broader soak or recovery
objective.

The owner must record CPU, memory, disk type/capacity, network, operating
system, container limits, PostgreSQL settings, configured instruments/streams,
event rate, queue sizes, dataset, build digest, and configuration hash before
performance certification.

## Safety and integrity objectives

These objectives have a zero error budget:

| Objective | V1A target | Required evidence |
|---|---:|---|
| Decision made from a book already beyond its configured age or skew limit | 0 | Eligibility-boundary tests, recorded decision inputs, soak query |
| Order-book delta gap detected without invalidating that book generation | 0 | Emulator/fault tests and gap/rebuild audit |
| Duplicate test/demo order caused by restart or retry | Not applicable in V1A; target remains 0 when introduced in V1C | V1A proves the capability absent; later kill-point/outbound tests |
| Lost or double-posted fill in fault tests | 0 simulated fills | Order reducer, journal property, and kill-point tests |
| Unbalanced committed journal transaction | 0 | Database constraints, property tests, journal rebuild verification |
| Deterministic replay mismatch across ten identical runs | 0 | Ten canonical result hashes plus manifest/build identity |
| Production, testnet, or demo exchange order side effect | 0 | Clean-build inspection and captured V1A outbound traffic |
| Secret/signature emitted to any observable output | 0 | Canary tests across logs, API, metrics, traces, audit, reports, bundles |

Any occurrence fails the relevant phase/release gate. An average, percentile, or
later successful run cannot offset a safety or integrity violation.

## Latency, recovery, and notification objectives

| Objective | Initial target |
|---|---:|
| p99 decode + sequence validation + local-book update at declared load | `<= 10 ms` |
| p99 strategy + allocator + risk evaluation at declared load | `<= 25 ms` |
| p95 gap-to-healthy resynchronization while Binance REST is available | `<= 15 s` |
| Critical in-app alert creation after detection | `<= 5 s` |
| External alert delivery p95 while the configured sink is available | `<= 60 s` |
| Graceful shutdown | `<= 60 s` |
| Shadow restart/recovery readiness RTO | `<= 5 min` |
| Test/demo restart/reconciliation readiness RTO | Not applicable in V1A; `<= 10 min` when introduced in V1C |
| Tested clean restore RTO | `<= 4 h` initially |

All durations use a monotonic clock. Persisted detection, start, transition, and
completion timestamps use UTC. “Ready” means dependencies, configuration,
ownership, recovery, journal, required books, and data quality pass; it does not
mean strategies are automatically active. A stale exchange may make a process
unready/degraded without forcing a liveness restart loop.

The alert-delivery objective excludes only a demonstrably unavailable external
sink. The in-app incident remains mandatory, sink outage is observable, and the
exclusion cannot be used to discard or silently delay alerts.

## Durability objectives

| Data/state | Initial RPO | Initial recovery objective |
|---|---:|---:|
| Critical database state after acknowledged commit | Zero | Shadow recovery ready within 5 minutes when infrastructure is intact |
| Raw recorder stream | At most the configured flush interval, otherwise an explicit immutable dataset gap | Recover/finalize or quarantine partial segments before declaring dataset ready |
| Database disaster from independent backup | Daily cadence, `<= 24 h` initially | Clean restore and verification within 4 hours |
| Retained market-data segments from independent backup | Per the verified copy cadence in `data-lifecycle.md`; missing data becomes an explicit gap | Restore manifests/files and verify hashes within the 4-hour drill target for the declared drill dataset |

“Zero RPO after acknowledged commit” is a transactional runtime invariant, not
a claim that a once-daily disaster backup contains every commit. The backup RPO
is separately `<= 24 h`. A Docker volume is primary storage, not a backup.

## Soak, capacity, and accessibility objectives

| Objective | Initial target |
|---|---:|
| V1A Binance public-data soak | At least 72 continuous hours |
| V1D readiness soak | At least 7 continuous days; not a V1A completion claim |
| Sustained memory | Bounded by configured limit with no positive leak trend after warm-up |
| Raw-data capacity | Configured retention plus at least 30% headroom before recording |
| Small-server free-space warning/decision threshold | At least 10 GiB, increased by measured capacity plan |
| Critical workflow accessibility | WCAG 2.2 AA |

A soak records reconnects, gaps, rebuilds, dropped/coalesced events, queue age,
disk growth, memory after warm-up, clock drift, database latency, alert state,
and every pause/lock. Restarting the observation window after an unexplained
failure does not erase the failure; it creates an incident and new evidence run.

## SLI definitions

- **Book age/skew:** computed from the exact immutable market-view versions used
  by a decision, not a dashboard sample.
- **Hot-path latency:** timer begins at entry to the named stage and ends after
  its output is committed to the next in-process boundary. Queue wait is also
  reported separately and end-to-end.
- **Gap recovery:** starts when the first process can detect the invalid
  sequence and ends when a new generation passes snapshot bridging, freshness,
  sequence, and readiness checks.
- **Alert creation/delivery:** starts when the triggering condition is detected;
  durable in-app creation and external-sink acknowledgement are separate SLIs.
- **Graceful shutdown:** starts on accepted termination signal and ends when the
  process exits after its documented safe-shutdown sequence. A forced exit is a
  failed sample unless the signal itself is a forced kill test.
- **Recovery readiness:** starts when the process/recovery attempt begins and
  ends on truthful readiness. Time spent awaiting mandatory operator input is
  reported, not silently removed.
- **Restore RTO:** starts with the declared restore incident/drill and ends only
  after database integrity, journal balance, manifests, hashes, replay identity,
  access control, and readiness verification pass.
- **RPO:** the UTC interval between the last verified recoverable point and the
  failure cutover, plus any explicitly identified recorder gap.

Percentiles come from histograms with documented buckets over the complete
declared window. Failed, timed-out, retried, and rejected operations are counted
and reported alongside latency; dropping them from the distribution is
forbidden. Cold-start and steady-state results are labelled separately.

## Metrics and cardinality

Metrics must cover connection state, message/decode rate, gaps/rebuilds, book
age, queue depth/age/saturation, decision/risk/simulation latency, journal and
reconciliation failures, process resources, disk, database, alerts, shutdown,
and recovery. Exchange, instrument, strategy family, mode, state, and bounded
reason codes may be labels. Order, decision, client, user, session, file-path,
raw-URL, and arbitrary-error values may not be labels.

Metrics are supporting evidence only. Canonical logs, audit/incident records,
run manifests, database verification, captured traffic, and drill reports are
required where a metric cannot prove causality or exact integrity.

## Failure and escalation

Crossing a safety, freshness, persistence, fencing, disk, queue, or clock limit
immediately applies the most restrictive relevant state and rejects new work.
`PAUSED` and `LOCKED` do not auto-clear. Exhausting a latency or availability
objective creates an operational incident and blocks release certification
until the result is understood, corrected or explicitly handled by a permitted
non-safety waiver. Safety, accounting, deterministic-replay, and real-money-lock
objectives cannot be waived.

## Ownership

| Owner | Responsibilities |
|---|---|
| Product/security | Zero-order/secret objectives, release interpretation, non-waivable gates |
| Market-data/Binance adapter | Decode/book latency, gap detection, resynchronization, freshness evidence |
| Runtime/domain | Strategy/risk latency, deterministic replay, queue and shutdown behavior |
| Storage/accounting | Journal integrity, RPO, segment recovery, backup/restore verification |
| SRE/platform | Reference profile, instrumentation, capacity, alerts, soak and recovery drills |
| API/frontend | Alert visibility, truthful health/mode state, accessibility evidence |
| QA/release | Workload control, raw artifact retention, independent gate decision |

Targets change only through a reviewed ADR/specification update with rationale,
new baselines, traceability, and no weakening of a zero-error safety invariant.
