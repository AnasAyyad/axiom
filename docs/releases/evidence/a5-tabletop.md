# A5 operations tabletop

Date: 2026-07-14 (Asia/Amman)
Scope: local single-operator desk tabletop for `AX-V1A-A05-OPS-002`
Result: passed

## Method and pass criteria

The exercise walked each required incident from detection through containment,
evidence preservation, locked recovery, verification, and explicit resume. The
single operator held the incident-commander, technical-lead, security, scribe,
and data-owner roles separately in the walkthrough. A scenario passed only if
the documented response remained fail closed, named an evidence source, linked
to a recovery procedure, and never inferred readiness or activation from a
green dashboard.

This was a structured desk exercise backed by the A3/A4 recovery tests and A5
fault, alert, health, and container tests. It was not represented as a
multi-person production drill or long-running deployment exercise.

## Scenario record

| Injection | Required response and recovery decision | Runbook evidence | Result |
|---|---|---|---|
| Startup prerequisite fails; shutdown is interrupted | Remain `LOCKED` and unready, cancel dependants, preserve ambiguity, then use the complete locked startup and recovery sequence; never restore activation implicitly | [Startup sequence](../../operations/startup-shutdown.md#startup-sequence), [graceful shutdown](../../operations/startup-shutdown.md#graceful-shutdown), and [crash recovery](../../operations/startup-shutdown.md#crash-recovery) | Pass |
| Pause/lock recovery after a critical alert | Stop new decisions, retain the durable alert and fence, verify all recovery invariants, return in `PAUSED`, and require an explicit audited resume | [Response flow](../../operations/incident-response.md#response-flow) and [recovery checklist](../../operations/incident-response.md#recovery-checklist) | Pass |
| Book gap, stale/crossed data, or unsafe clock | Invalidate the complete book generation, discard unsafe queued work, quarantine affected decisions, rebuild from an allowlisted snapshot, and require sequence/freshness health before manual resume | [Book and clock playbook](../../operations/incident-response.md#book-gap-stalecrossed-data-or-clock-anomaly) | Pass |
| Journal, reservation, or reconciliation mismatch | Lock the affected scope, preserve immutable history, quarantine ambiguous ownership, rebuild projections, and use only explicit incident-linked compensating entries | [Journal and reconciliation playbook](../../operations/incident-response.md#journal-reservation-or-reconciliation-mismatch) | Pass |
| Disk pressure or partial recorder segment | Reject new noncritical jobs at the high watermark; at critical pressure stop decisions, preserve journal/audit capacity, finalize complete segments, quarantine partial segments, and declare gaps | [Disk/storage playbook](../../operations/incident-response.md#disk-pressure-or-recorderstorage-failure) and [segment lifecycle](../../operations/data-lifecycle.md#segment-and-dataset-lifecycle) | Pass |
| Database outage or lease/fencing uncertainty | Lock before protected work, do not claim lease release, restore durability, acquire a higher fence, perform full reconciliation, and prove exclusive ownership before readiness | [Database/fencing playbook](../../operations/incident-response.md#database-outage-or-leasefencing-loss) | Pass |
| Backup loss or primary recovery request | Refuse in-place restore, use a clean isolated database, authenticate/decrypt and validate the archive, verify schema/journal/ownership/manifests, and retain the primary until recovery acceptance | [Backup policy](../../operations/data-lifecycle.md#backup-policy), [restore verification](../../operations/data-lifecycle.md#restore-verification), and [deployment restore procedure](../../../deploy/README.md#5-encrypted-postgresql-backups) | Pass |
| Incident must be reproduced without leaking sensitive data | Record stable IDs, build/config/schema hashes, monotonic/UTC timing, bounded metrics, immutable input hashes, and sanitized support data; keep raw evidence access-controlled and use a focused regression or deterministic replay when available | [Evidence and retention](../../operations/incident-response.md#evidence-communications-and-retention) and [response flow](../../operations/incident-response.md#response-flow) | Pass |

## Cross-checks and decisions

- Critical A5 fault tests assert the safety lock occurs before alert persistence;
  persistence or external delivery failure cannot undo containment.
- The measured 100-sample local alert test recorded in
  [A5 completion evidence](a5-local-progress.md) satisfies the declared in-app
  and available-sink objectives with substantial margin.
- Clean PostgreSQL migration/alert qualification and the prior A4 clean restore
  exercise provide executable evidence for durable alert and restore paths.
- Prometheus/Grafana are supporting detection only. Durable in-process alert
  state and the safety gate remain authoritative.
- No scenario authorizes real exchange orders, exchange credentials, private
  endpoints, or automatic unpause.

## Remaining release work

Repeat these exercises with named independent participants against the actual
deployment, including fault injection and alert routing, before the V1A release
gate. Later A6-A11 components must add their own recovery evidence as they
become real; this A5 artifact does not claim those unimplemented paths operate.
