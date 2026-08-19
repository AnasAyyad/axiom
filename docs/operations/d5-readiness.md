# D5 reference-server readiness runbook

This runbook separates local implementation checks from formal acceptance.
Never label smoke output as a D5 pass.

## Before selecting the server

Implementation and local smoke may run. Do not start the official clock until
the owner records the server identity and approves CPU, memory, disk, route,
TLS, clock, remote-backup, and clean-restore evidence. A route outside the
100 ms clock uncertainty threshold is rejected; do not weaken the manifest.

## Immutable inputs

Build every application and infrastructure image at the same source SHA and
record a registry digest (`name@sha256:...`) for app, backup, PostgreSQL,
Prometheus, Grafana, and Caddy. Record the configuration, server, dataset, and
test-manifest SHA-256 identities. Generate a dedicated Ed25519 seed, store only
its base64 value in a root-readable secret file, and keep it outside Git.

The formal process requires:

- `AXIOM_OPERATIONAL_READINESS_ENABLED=1` and
  `AXIOM_OPERATIONAL_READINESS_MODE=formal`;
- paths for the run file, test manifest, fault schedule, live preflight, live
  sample, terminal fault evidence, and signing-key secret;
- an empty evidence root on durable storage;
- authenticated clean-restore evidence with a nonzero, hash-verified market
  segment inventory recovered outside the active recorder filesystem;
- the exact seven-day duration and one-to-five-minute sample interval.

Copy the fail-closed templates in
`deploy/config/operational-readiness-run.example.json`,
`deploy/config/operational-readiness-preflight.example.json`,
`deploy/config/operational-readiness-sample.example.json`, and
`deploy/config/operational-readiness-fault-evidence.example.json` into the
protected server evidence workspace. They are intentionally invalid or
failing until the approved orchestrator replaces every `CHANGE_ME`, current
time/revision, false preflight result, and failed drill outcome with measured
evidence. Do not edit the checked-in
`operational-readiness-test-manifest-v1.json` or
`operational-readiness-fault-schedule-v1.json` on the server.

Every terminal fault event must carry the exact active `run_id`. Evidence from
another run is rejected even when its scenario, timing, and hash otherwise
match the approved schedule.

The scheduled `shadow_worker_restart_and_checkpoint_recovery` drill is a
graceful worker restart. Its evidence must show the same shadow session, a
higher claim epoch, an increased durable checkpoint count, and a recovered
`PAUSED` state with entries disabled. Recorder crash recovery remains covered by
the separate kill-during-finalize scenario; do not replace the shadow restart
with `SIGKILL` and call the resulting lease expiry a checkpoint recovery.

The later `database_restart_and_outbox_recovery` drill reuses that checkpointed
`PAUSED` session. Database loss locks entries immediately. After the old
30-second lease expires, only a session with disabled entries, checkpoint and
snapshot evidence, and no uncertain order, reservation, or execution-plan state
may be reclaimed. The same session must return `PAUSED` under a higher fencing
epoch; it is never armed automatically. Any running, uncheckpointed, or unsafe
session expires terminally, and the drill fails.

Invoke `make operational-readiness-formal` from the exact clean release build.
The runner refuses a dirty/mismatched build, failed preflight, incomplete
declared load, mutable image tags, an existing run ID, stale sample revisions,
missed drills, threshold breaches, or prohibited capabilities.

Set `market_data_recovery_passed=true` in the live preflight only after the
restore evidence authenticates, `market_data_verified=true`, every ready
segment is present exactly once, and the recovered inventory hash is recorded.

## Non-qualifying preflight check

Before scheduling the seven-day run, set the same exact identity, manifest,
schedule, preflight, sample, and signing-key variables used by the formal
command, then run:

```text
make operational-readiness-preflight-check
```

The command verifies the clean build identity, strict run configuration,
checked-in manifest and schedule hashes, signing-key shape, preflight age and
all hard gates, plus one fresh increasing live sample under the formal D5
thresholds. It prints only stable redacted failure categories. It does not read
fault outcomes, create a run directory, write formal evidence, execute a fault,
or start the seven-day clock. Even a successful report has
`formal_clock_started=false` and `qualified=false`. The fail-closed report
shape is documented in
`deploy/config/operational-readiness-preflight-report.example.json`.

This checker validates measured inputs; it does not invent them. The
credential-free `operational-readiness-observer` reads PostgreSQL through the
read-only role, private health endpoints, fixed-cardinality process/latency
metrics, and a separately produced drill-observation file. It atomically
replaces only the rolling live-sample file and binds every sample to hashes of
all three source aggregates. Missing, stale, malformed, or decreasing source
data fails closed. Never hand-edit a passing value or derive one from the
checked-in fail-closed examples.

The drill orchestrator must write
`drill-observation.json` from measured replay, alert, shutdown, recovery, RPO,
disk-pressure, and declared-load results. The checked-in example is deliberately
failing. The `operational-readiness` profile selects the complete private
rehearsal stack. Start it with:

```text
docker compose --profile operational-readiness up -d
```

The observer has no exchange credentials, no owner credentials, and no write
grant in PostgreSQL. It may write only the bind-mounted rolling sample path.

### Vienna rehearsal profile

Vienna may be approved only as a non-qualifying rehearsal server. Set
`reference_server_approved=true` only in the protected Vienna rehearsal input,
then run:

```text
make operational-readiness-preflight-vienna-rehearsal
```

This profile records final-host-only checks as warnings instead of failing the
rehearsal: route-clock uncertainty, public TLS, independent remote backup,
backup freshness, clean restore/RTO, and market-data recovery. These checks are
deferred to the final Japan host; they are not accepted or silently marked as
passing on Vienna.

Immutable image identity, non-root execution, resource limits, disk capacity,
schema upgrade and forward-fix drills, SBOM/security scanning, production-order
impossibility, and every live-sample requirement remain strict. The report always
has `profile=vienna_rehearsal`, `formal_clock_started=false`, and
`qualified=false`; it is not approval for the Japan reference server and it
cannot start or qualify D5. The normal preflight command and formal runner still
reject every deferred final-host check.

## During the run

The approved observer atomically replaces the bounded live-sample JSON before
each interval. The runner independently appends and fsyncs that observation to
`samples.jsonl`. A transient source-readiness interruption may delay one sample:
the runner retries the same acquisition every two seconds for at most two
minutes while the seven-day clock continues. Recovery does not reset or extend
the clock. A non-retryable source failure, malformed evidence, revision
regression, two-minute acquisition timeout, stale accepted sample, failed or
late drill, or any safety/integrity threshold remains terminal.

The runner must not infer that an unavailable sample means the application is
down. The observer records the exact fixed-cardinality source, stage, role,
reason, retryability, attempt number, consecutive failure count, duration, and
last-success timestamp in `observer-status.json` and the append-only,
hash-chained `observer-lifecycle.jsonl`. Raw errors, URLs, payloads, database
text, and credentials are forbidden. `controller-lifecycle.jsonl` separately
records startup, immutable input binding, strict preflight, runner start, drill
start/pass/failure, terminal verdict, observer detachment, and evidence sealing.
Together with `samples.jsonl`, these files provide start-to-terminal causality
without relying on a generic container-health guess.

Execute only the checked-in fault schedule. Do not restart, resume, reset the clock,
waive a failure, or edit thresholds. A drill command failure immediately
writes terminal failed evidence; the runner does not wait until day seven to
discover it. After a terminal verdict, the cleanup guard restores the shared
observer to the normal readiness path, stops remaining timers, and writes
`evidence-manifest.sha256`. A run workspace must not receive further observer
writes after that seal.

At high disk pressure, verify new labs, reports, and exports are rejected while
journal/audit writes continue. At critical pressure, verify recorder final
flush or quarantine, recorder unready state, disabled shadow entries, and
continued journal/audit writes.

## Verdict and failure handling

`verdict.json` is created once and contains the evidence hash, signing-key
fingerprint, and signature. `PASSED` with `qualified=true` is possible only for
the full formal duration. `SMOKE_PASSED` always has `qualified=false`.

A failed or interrupted run remains failed. Open an incident, preserve the run
directory under an evidence hold, fix through a reviewed PR, build new exact
artifacts, repeat preflight, and start a new run ID. D5 never replaces B2 or C6.
The failed `d5-formal-65901b54-20260818t065654z` run remains disqualified and
must not be resumed, edited, or relabelled by this recovery policy.

Memory qualification is per required service, not an aggregate first/last RSS
comparison. After a one-hour warm-up, compare each service's heap-allocation
low-watermark in the first one-hour window with its low-watermark in the final
one-hour window. A service fails only when the final baseline rises by both
more than five percent and more than 8 MiB. Missing service/window samples and
hard aggregate or per-service memory-limit breaches still fail closed.
