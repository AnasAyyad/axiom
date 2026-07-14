# V1A incident response

## Status and principles

This is the V1A response policy and runbook framework. A5 implements durable
in-app alert state, one bounded external delivery sink, and fail-closed
containment for the enumerated critical faults. Later phases still own replay
links and the complete business recovery paths. Safety, evidence integrity,
and containment take priority over availability or completing a research run.

All timelines use UTC and record monotonic elapsed durations. Never put a
secret, token, signature, raw cookie/header, private payload, or unnecessary
personal data in an incident, alert, chat, screenshot, or evidence bundle.

## Severity

| Severity | Criteria | Initial action |
|---|---|---|
| `SEV-0` safety/security | Suspected exchange order path/side effect, production or exchange credential in V1A, secret exposure, active compromise, journal corruption affecting ownership | Lock/stop affected platform immediately, isolate egress/artifact/host, notify owner and security |
| `SEV-1` critical integrity/availability | Unbalanced journal, unresolved reconciliation, lease split brain, critical DB durability loss, missing/double fill, data used after gap/staleness, destructive deletion/backup failure | Pause or lock affected scope, preserve evidence, begin recovery under incident command |
| `SEV-2` degraded | Repeated book rebuild, queue/disk pressure, recorder gap, restore/alert SLO miss, noncritical dependency outage | Pause affected decisions/jobs as policy requires; investigate within operating window |
| `SEV-3` informational | Contained anomaly with no safety, integrity, or service-objective impact | Track, trend, and close with evidence |

When uncertain, declare the higher severity. Downgrade requires recorded facts
and incident-commander approval; it never retroactively erases response time.

## Roles

- **Incident commander:** owns severity, containment, decisions, handoffs, and
  recovery authorization.
- **Technical lead:** diagnoses and executes approved remediation.
- **Security lead:** owns credential, compromise, endpoint, supply-chain, and
  evidence-access decisions.
- **Scribe:** maintains UTC timeline, hypotheses, commands/actions, evidence IDs,
  and decisions without secrets.
- **Service/data owner:** validates domain invariants, restored state, and user
  impact.
- **Communications owner:** sends accurate bounded updates.

One operator may hold multiple roles on a single-owner deployment, but the
roles and decisions remain explicit. Destructive evidence or retention changes
require a second review when available and are never improvised during response.

## Response flow

1. **Detect and declare.** Create a stable incident ID, UTC start, severity,
   affected scope/mode/build/config, detector, and initial symptom. A failed
   external sink does not prevent durable in-app declaration.
2. **Contain.** Apply the most restrictive safe state; stop new decisions/jobs,
   isolate hosts/images/routes or revoke sessions/secrets as appropriate. Do not
   auto-unpause or broaden endpoints to restore service.
3. **Preserve evidence.** Snapshot relevant immutable IDs, logs, metrics,
   manifests, hashes, database transaction boundary, process/build identity,
   lease/fence, config versions, and captured traffic. Use read-only copies and
   chain-of-custody hashes; do not collect secrets.
4. **Assess.** Separate confirmed facts from hypotheses; determine first bad
   event, affected data/decisions, ownership and journal impact, RPO/RTO clock,
   exposure, and whether a safety invariant failed.
5. **Eradicate/remediate.** Fix the narrow cause through reviewed code,
   configuration, credential rotation, dependency replacement, data quarantine,
   or infrastructure repair. Never edit immutable history or silently fill a
   gap/mismatch.
6. **Recover.** Use the normal locked startup path, higher fencing token,
   reconciliation, journal/manifest verification, healthy book rebuild, and
   current configuration. Remain paused after readiness.
7. **Authorize resume.** The incident commander and relevant security/data owner
   verify the recovery checklist. Resume is explicit, authenticated, reasoned,
   and audited; `PAUSED`/`LOCKED` never clear automatically.
8. **Close and learn.** Record impact, root cause, RPO/RTO achieved, evidence,
   corrective actions/owners/dates, remaining risk, and required threat-model,
   runbook, tests, alerts, or specification updates.

## Mandatory containment playbooks

### Suspected exchange-order path or forbidden capability

Treat as `SEV-0` even if no order is known to have left. Stop V1A application
processes, block exchange egress at the host/proxy, quarantine the build and
generated artifacts, preserve captured traffic and configuration, and inspect
source/image symbols, mounts, API/UI contracts, routes, DNS/proxy, and audit.
If any exchange credential could have been exposed, revoke it without displaying
it. Do not restart until a clean build and independent outbound capture prove
the path absent. Escalate any actual production side effect immediately.

### Secret or session exposure

Stop further copying; restrict the source/artifact; revoke and rotate the secret
and dependent sessions; search repository history, builds, caches, logs,
telemetry, backups, notifications, and downstream systems using redacted/canary
methods. Deleting the visible file does not remove the need to rotate.

### Book gap, stale/crossed data, or clock anomaly

Pause the affected instrument/exchange, invalidate the entire book generation,
discard unsafe queued work, preserve raw lifecycle/gap evidence, correct clock
health if applicable, obtain a new allowlisted snapshot, bridge buffered events,
and require sequence/freshness/quality health before manual resume. Identify and
quarantine every decision that used ineligible data.

### Journal, reservation, or reconciliation mismatch

Lock the affected scope, reject entries, preserve the journal and projections,
quarantine disputed balances/reservations, and rebuild/compare from immutable
transactions. Never overwrite history or release an ambiguous reservation.
Corrections use explicit compensating entries with incident attribution.

### Database outage or lease/fencing loss

Stop accepting new critical work. On lease uncertainty, lock before further
protected writes and do not claim lease release; let it expire. Restore database
durability, acquire a higher fence, run full recovery/reconciliation, and verify
no competing owner exists before readiness.

### Disk pressure or recorder/storage failure

At high watermark reject new backtest/export jobs and alert. At critical
watermark stop new shadow decisions, preserve critical journal/audit capacity,
finalize complete recorder segments, quarantine partial ones, and declare gaps.
Do not delete held/referenced data or expand a path outside the storage root.

### Dependency, image, or build compromise

Block deployment and isolate affected artifacts/runners, record digests and
provenance, compare a clean independently built image, review SBOM/diffs and
outbound capture, rotate exposed control-plane credentials, and invalidate
signatures/digests as required. Vulnerability suppression cannot waive a
production-order, secret, or integrity finding.

## Recovery checklist

Recovery requires evidence that:

- compiled mode and endpoint policy is unchanged or reviewed and stricter;
- no forbidden credential, signer, broker, route, profile, or process is present;
- configuration/build/database schema identities are known and compatible;
- lease/fencing ownership is exclusive and current;
- journal balances, reservations, projections, jobs, outbox, segments, and
  manifests verify, with ambiguities quarantined;
- required public books, clock, queues, disk, database, and decision recorder
  are healthy;
- affected secrets/sessions are rotated/revoked and old values fail;
- required alerts work or their outage is explicitly contained; and
- a focused regression/fault test and, when relevant, deterministic replay and
  captured outbound test pass.

Readiness is not activation. The platform returns in `PAUSED`; resume is a
separate audited action.

## Evidence, communications, and retention

Evidence bundles contain stable IDs, canonical event/segment/config/build
hashes, redacted logs, bounded metrics, captured-request metadata, decisions,
and command results. Raw evidence stays in access-controlled storage and is
linked rather than pasted. Preserve original timestamps and hashes; record
every transformation. Incident holds override normal deletion under
`data-lifecycle.md`.

Updates state confirmed impact, current containment, next decision/checkpoint,
and owner. Do not speculate about profitability, blame, or compromise. Do not
claim recovery from a green dashboard alone. External/legal notification, if
ever required, is an owner/security decision based on applicable obligations;
this runbook makes no legal determination.

## Objectives, exercises, and ownership

Critical in-app incidents must be created within 5 seconds of detection and an
available external sink must deliver p95 within 60 seconds. Shadow recovery
readiness targets 5 minutes when infrastructure remains intact; verified clean
restore targets 4 hours with daily backup RPO no greater than 24 hours. Raw
recorder RPO is its flush interval or an explicit gap. Safety/integrity failures
have zero error budget and may extend recovery beyond the RTO rather than permit
unsafe resume.

SRE owns alerting, incident tooling, operational containment, and exercises;
security owns `SEV-0`, secrets, endpoint and supply-chain response; service/data
owners validate recovery; product owns impact and release decisions; QA owns
reproduction and regression evidence.

The A5 desk tabletop is recorded in the
[A5 operations tabletop](../releases/evidence/a5-tabletop.md). Before V1A
release, repeat multi-role deployment and fault-injection exercises for
forbidden capability, secret canary, book gap/staleness, database outage, lease
loss, disk full, partial segment, journal mismatch, alert-sink failure, crash
recovery, and clean backup restore. The A5 tabletop proves runbook coverage; it
does not replace those later environment-specific release drills.
