# ADR-0025: D5 current pressure, remote backup, and terminal readiness evidence

## Status

Accepted for V1D D5 implementation. Formal acceptance remains pending the
approved reference server and exact seven-day run.

## Decision

Storage pressure has a mutable, revisioned current row and separate immutable
observations. History is never deleted, but only the current row controls
work. This avoids both unsafe history deletion and the former permanent block
caused by treating any past disk event as current pressure.

High pressure rejects new heavy work. Critical pressure additionally disables
shadow entries and makes the recorder cancel collectors and perform a final
flush. The database remains available for journal, command, incident, alert,
and audit writes.

Backup creation and restore run only when the destination is a separate
non-root mount with a filesystem identity different from PostgreSQL, market
data, and local staging. The backup is encrypted and authenticated; a
successful clean restore emits authenticated no-replace evidence.
Bulk market data retains its independent filesystem backup policy. The clean
restore binds the PostgreSQL ready-segment catalogue to exactly one confined
restored file per segment, verifies every checksum, and seals the deterministic
inventory identity into that evidence. The active recorder tree is not accepted
as a formal restore target.

The D5 runner performs preflight before starting its clock, creates a unique
run directory, fsyncs a hash chain of samples, and signs its terminal verdict.
Formal runs are exactly seven continuous days. Smoke runs cannot qualify.
A separate preflight-check path evaluates the exact configuration, preflight,
and one fresh live sample without creating a run directory or starting a clock;
its report is always non-qualifying.

## Consequences

- A newly migrated deployment starts at critical pressure until the recorder
  publishes a measured observation.
- A normal local directory cannot masquerade as remote backup storage.
- The reference server choice and formal clock remain external gate inputs.
- D5 owns no exchange adapter and cannot submit an order.
- B2 and C6 evidence remain independent cumulative release prerequisites.
