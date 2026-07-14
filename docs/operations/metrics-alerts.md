# A5 metrics, health, and alert contract

## Safety boundary

Prometheus and Grafana are diagnostics, never decision authority. Metric values
may use Prometheus floating-point presentation, but financial state enters the
registry only as fixed microunits or integer parts-per-million. IDs, users,
file paths, URLs, arbitrary error text, and secret-derived values are forbidden
as labels. Every vector is private to the observability package; callers can
use only typed methods that validate configured exchanges, instruments,
strategies, modes, closed states, and reason codes.

`/health/live` proves only that the process can answer. `/health/ready` performs
a redacted PostgreSQL dependency check. `/api/v1/system/health` returns the
bounded component detail only after constant-time verification of the opaque
file-backed `health_detail_token`; the authorizer retains only its SHA-256
digest. All responses are `no-store`, and dependency errors are replaced with
stable reason codes. `/metrics` is served on the separate Compose-internal
metrics listener and is not published on the API host port.

## Optional tracing

Backend tracing is OpenTelemetry-compatible and disabled by default. Disabled
tracing installs a true no-op provider. Enabling it requires an explicit full
HTTPS OTLP/HTTP endpoint; userinfo, query strings, fragments, implicit schemes,
and non-TLS endpoints fail configuration validation. Collector credentials do
not belong in the endpoint or environment.

The SDK uses a bounded asynchronous batch processor (2,048 queued spans, 256
per batch, five-second export interval and timeout). A slow or unavailable
collector may cause spans to be dropped rather than delaying the application
hot path. Exporter failures pass through the same structured redaction boundary
as logs. Each process owns its provider instead of changing global tracing
state, and shutdown gets a bounded five-second flush deadline.

## Metric registry

All histograms use seconds and Prometheus default duration buckets until load
evidence justifies a reviewed bucket ADR. The `service` constant label and
Prometheus `job` target label are bounded by the four deployed roles.

| Metric | Type / unit | Bounded labels | Producer and meaning |
|---|---|---|---|
| `axiom_websocket_messages_total` | counter / messages | exchange, instrument, service | Validated public stream messages |
| `axiom_websocket_events_total` | counter / events | exchange, instrument, reason, service | Decode, gap, and reconnect health events |
| `axiom_order_book_age_seconds` | gauge / seconds | exchange, instrument, service | Age of the active generation |
| `axiom_event_queue_depth` | gauge / events | queue, service | Current bounded queue depth |
| `axiom_event_queue_dropped_total` | counter / events | queue, reason, service | Dropped/coalesced-overflow facts |
| `axiom_strategy_evaluations_total` | counter / evaluations | strategy, mode, service | Completed strategy evaluations |
| `axiom_strategy_candidates` | gauge / candidates | strategy, mode, service | Current candidate count |
| `axiom_strategy_rejections_total` | counter / candidates | strategy, mode, reason, service | Rejected candidates by closed reason |
| `axiom_risk_check_duration_seconds` | histogram / seconds | strategy, mode, service | Risk evaluation duration |
| `axiom_execution_simulation_duration_seconds` | histogram / seconds | mode, service | Credential-free simulator duration |
| `axiom_exchange_rest_duration_seconds` | histogram / seconds | exchange, operation, service | Allowlisted public REST latency |
| `axiom_exchange_rest_failures_total` | counter / failures | exchange, operation, service | Public REST failures |
| `axiom_websocket_lag_seconds` | histogram / seconds | exchange, instrument, service | Exchange-event to receipt lag |
| `axiom_shadow_fills_total` | counter / fills | exchange, instrument, state, service | Simulator fill outcomes only |
| `axiom_reconciliation_mismatches_total` | counter / mismatches | exchange, reason, service | Virtual reconciliation mismatches |
| `axiom_journal_failures_total` | counter / failures | reason, service | Exact-journal validation/write failures |
| `axiom_virtual_pnl_reporting_units` | gauge / reporting units | mode, service | Presentation of fixed microunit virtual P&L |
| `axiom_virtual_drawdown_ratio` | gauge / ratio | mode, service | Presentation of integer-PPM drawdown |
| `axiom_database_operation_duration_seconds` | histogram / seconds | operation, service | Bounded database operation latency |
| `axiom_database_failures_total` | counter / failures | operation, reason, service | Database failures |
| `axiom_alerts_open` | gauge / alerts | severity, reason, service | Durable open in-app alerts |
| `axiom_dependency_ready` | gauge / boolean | dependency, service | PostgreSQL, disk, clock, fencing, book, and queue readiness |
| `axiom_disk_free_bytes` | gauge / bytes | storage, service | Free space by configured storage class |
| `go_*`, `process_*` | runtime/process | collector-defined bounded runtime labels | Go runtime, CPU, resident memory, and file descriptors |

Allowed queue values are `market`, `persistence`, `strategy`, `alerts`, and
`jobs`. Dependency values are `postgres`, `disk`, `clock`, `fencing`, `books`,
and `queues`. Storage values are `market_data`, `postgres`, `backups`, and
`prometheus`. The code-owned reason vocabulary is tested in
`internal/observability/metrics_test.go`.

## Core alert rules

| Rule | Threshold | Severity / fail-closed behavior | Runbook |
|---|---|---|---|
| `AxiomServiceUnavailable` | scrape target down for 1 minute | critical; process recovery remains paused | [database/lease](incident-response.md#database-outage-or-leasefencing-loss) |
| `AxiomDatabaseUnavailable` | PostgreSQL unready for 30 seconds | critical; lock on critical persistence failure | [database/lease](incident-response.md#database-outage-or-leasefencing-loss) |
| `AxiomFencingLeaseLost` | fencing health false for 15 seconds | critical; synchronous `LOCKED` | [database/lease](incident-response.md#database-outage-or-leasefencing-loss) |
| `AxiomClockUnsafe` | clock health false for 15 seconds | critical; synchronous `LOCKED` | [book/clock](incident-response.md#book-gap-stalecrossed-data-or-clock-anomaly) |
| `AxiomBooksUnhealthy` | required book health false for 15 seconds | critical; synchronous `LOCKED` | [book/clock](incident-response.md#book-gap-stalecrossed-data-or-clock-anomaly) |
| `AxiomQueueDrops` | any drop in 5 minutes | critical; synchronous `LOCKED` | [response flow](incident-response.md#response-flow) |
| `AxiomDiskLow` | free bytes below 10 GiB for 5 minutes | warning; reject new jobs | [disk](incident-response.md#disk-pressure-or-recorderstorage-failure) |
| `AxiomDiskCritical` | free bytes below 2 GiB for 1 minute | critical; finalize/quarantine and lock | [disk](incident-response.md#disk-pressure-or-recorderstorage-failure) |
| `AxiomReconciliationMismatch` | any mismatch in 5 minutes | critical; synchronous `LOCKED` | [journal/reconciliation](incident-response.md#journal-reservation-or-reconciliation-mismatch) |
| `AxiomJournalFailure` | any failure in 5 minutes | critical; synchronous `LOCKED` | [journal/reconciliation](incident-response.md#journal-reservation-or-reconciliation-mismatch) |

Prometheus rule evaluation is supporting detection. The in-process alert
service is authoritative for containment: every critical persistence, fencing,
disk, clock, queue, book/stale-data, reconciliation, or accounting fault locks
the safety gate before durable alert insertion. If insertion itself fails, the
gate remains locked. Alerts deduplicate durably, retain occurrence/revision
counts, support transactional acknowledgement history, and retain external
delivery attempt state. The HTTPS webhook sink rejects credentials in URLs,
queries, fragments, non-allowlisted hosts, and all redirects; its JSON schema
contains only bounded codes and operational identities. A configured process
claims at most 25 due deliveries every 30 seconds; failed attempts retain a
stable reason code and a next-attempt timestamp, keeping the initial p95 target
within 60 seconds when the sink is available.
