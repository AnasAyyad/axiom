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

- `AXIOM_D5_READINESS_ENABLED=1` and `AXIOM_D5_MODE=formal`;
- paths for the run file, test manifest, fault schedule, live preflight, live
  sample, terminal fault evidence, and signing-key secret;
- an empty evidence root on durable storage;
- authenticated clean-restore evidence with a nonzero, hash-verified market
  segment inventory recovered outside the active recorder filesystem;
- the exact seven-day duration and one-to-five-minute sample interval.

Copy the fail-closed templates in `deploy/config/d5-run.example.json`,
`d5-preflight.example.json`, `d5-sample.example.json`, and
`d5-fault-evidence.example.json` into the protected server evidence workspace.
They are intentionally invalid or failing until the approved orchestrator
replaces every `CHANGE_ME`, current time/revision, false preflight result, and
failed drill outcome with measured evidence. Do not edit the checked-in test
manifest or fault schedule on the server.

Every terminal fault event must carry the exact active `run_id`. Evidence from
another run is rejected even when its scenario, timing, and hash otherwise
match the approved schedule.

Invoke `make d5-readiness` from the exact clean release build. The runner
refuses a dirty/mismatched build, failed preflight, incomplete declared load,
mutable image tags, an existing run ID, stale sample revisions, missed drills,
threshold breaches, or prohibited capabilities.

Set `market_data_recovery_passed=true` in the live preflight only after the
restore evidence authenticates, `market_data_verified=true`, every ready
segment is present exactly once, and the recovered inventory hash is recorded.

## During the run

The approved sampler atomically replaces the bounded live-sample JSON before
each interval. The runner independently appends and fsyncs that observation to
`samples.jsonl`; revision regression or staleness is terminal. Execute only the
checked-in fault schedule. Do not restart, resume, reset the clock, waive a
failure, or edit thresholds.

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
