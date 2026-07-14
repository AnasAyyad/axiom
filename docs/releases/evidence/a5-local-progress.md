# A5 completion evidence — security and observability

Date: 2026-07-14 (Asia/Amman)
Status: verified and complete

## Implemented boundary

- Structured JSON `log/slog` output redacts sensitive keys, registered secret
  literals, arbitrary values, errors, and messages before bytes are written.
- A private Prometheus registry exposes 23 typed, bounded-cardinality A5
  application metrics plus Go/process collectors. Financial values cross the
  presentation boundary as fixed microunits or integer parts-per-million.
- Public liveness/readiness are separate from authenticated detailed health;
  metrics use a separate unexposed listener. The health authorizer retains only
  a constant-time-compared SHA-256 token digest and every response is redacted.
- PostgreSQL migration `000004_observability_alerts.sql` provides durable alert
  deduplication, occurrence/revision state, acknowledgement audit history,
  delivery attempts, retry timing, and indexes.
- Critical persistence, fencing, disk, clock, queue, book/stale-data,
  reconciliation, and accounting facts lock the safety gate before alert
  persistence. Failed persistence or external delivery cannot undo the lock.
- The optional HTTPS webhook has an exact host allowlist, rejects redirects and
  unsafe URL forms, separates bearer material into a file, and retries durable
  due work in bounded batches.
- Optional OpenTelemetry OTLP/HTTP tracing is a true no-op when disabled. When
  enabled it accepts only an explicit HTTPS endpoint and uses a bounded
  asynchronous queue so exporter failure cannot block application work.
- Prometheus rules, a provisioned Grafana dashboard, documented contracts,
  hardened Compose profiles, and a deliberately bounded support artifact are
  included.

## Functional and integration qualification

The following checks passed on the final A5 tree with Go 1.26.5, Node 24.18.0,
the locked pnpm graph, PostgreSQL 18.4, Docker 29.6.1, Prometheus 3.5.0, and
Grafana 12.0.2:

- complete `make verify`: preflight, format, generated contracts, documentation
  and A0-A5 boundary checks, vet/staticcheck/ESLint/policy checks, all backend
  and frontend tests, full Go race suite, four fuzz smoke targets, frontend and
  backend builds, 128 active Compose profile renders, static security checks,
  and current Go vulnerability review;
- clean-database `TestA5DurableAlertFailClosedIntegration`: all four migrations
  applied from zero and durable deduplication, acknowledgement/audit, failed
  delivery retention, reconstruction, and fail-closed fencing passed;
- `promtool check config` and `promtool check rules`: one rule file and all ten
  rules passed;
- image-backed Compose smoke: `api`, `engine-shadow`, `recorder`, and `worker`
  became healthy; authenticated health passed; all four Prometheus targets were
  `up`; Grafana reported healthy and its API found `Axiom V1A Operations`;
- runtime inspection: application roles were non-root, read-only,
  capability-dropped, `no-new-privileges`, resource-bounded, and limited to
  reviewed writable mounts and secret grants;
- deterministic image rebuild comparison passed with runtime payload
  fingerprint
  `sha256:e417c38b39596f93fbff06f47100eba4823bc38b194d1bbb03e6ca81dc4dfb7f`.

Canary tests cover log attributes/groups/errors/messages, API failures, health
details, metrics labels, tracing exporter errors, webhook authorization/body
separation, support JSON, and repository scanning. Slow/failing trace export was
tested with 4,096 producers and remained below the two-second nonblocking test
ceiling.

## Alert objectives

`TestAlertDeliveryServiceObjectivesAtDeclaredLocalLoad` measured 100 samples of
the critical in-app persistence boundary and sanitized available-HTTPS-sink
pipeline:

| SLI | Measured p95 | Objective | Result |
|---|---:|---:|---|
| Critical in-app creation | 10.629 microseconds | at most 5 seconds | Pass |
| Initial external sink delivery | 75.464 microseconds | at most 60 seconds | Pass |

The transport fixture is deterministic and contains no network scheduling or
remote-sink delay; the objective explicitly applies while the sink is
available. Durable retry behavior and the 30-second claim cadence are covered
separately by service and PostgreSQL tests.

## Supply-chain evidence

The pinned Trivy 0.72.0 image was used with its current database. Final image
scanning for high/critical vulnerabilities, secrets, misconfiguration, and
licenses reported zero findings. Repository secret/misconfiguration scanning
also reported zero high/critical findings. `govulncheck ./...` reported no
known reachable Go vulnerabilities. `go-licenses` 2.0.1 accepted every external
Go dependency under the repository allowlist (Apache-2.0, BSD-3-Clause, or
MIT).

Retained machine-readable artifacts:

| Artifact | SHA-256 | Result |
|---|---|---|
| [Final image SPDX JSON](a5/axiom.spdx.json) | `734800035e529a098d1d1d98a10c109dd6076f777777eb08a16f008a792dbfeb` | 38 packages |
| [Final image Trivy JSON](a5/trivy-image.json) | `1c047c270b01c96b55bd2dc719413eebcc1a4e9de7bedb4fdbf8c8d180e9ad3a` | zero high/critical findings |
| [Repository Trivy JSON](a5/trivy-repository.json) | `ae8556a834dd435465326ff413a9db988cceac8c3b4c5d7311eaee2dd42af7ce` | zero high/critical findings |

The SBOM describes the final reproducible runtime payload. Generated scan JSON
is retained verbatim rather than reformatted so its digest remains reviewable.

## Operations review and boundary

The [A5 operations tabletop](a5-tabletop.md) passed startup/shutdown,
pause/lock recovery, data gap/staleness, reconciliation mismatch, disk pressure,
database/fencing outage, clean backup/restore, and sanitized incident
reproduction walkthroughs. It records its single-operator desk-exercise scope;
multi-role deployment fault drills remain a V1A release-gate activity.

A5 adds no exchange signer, credential, private endpoint, or external-order
capability. V1A remains public-data research/simulation, spot-only, fail closed,
and unable to place real-money production orders. This local WSL2 qualification
does not claim a production deployment, later-phase recovery implementation,
long-duration soak, or profitability.
