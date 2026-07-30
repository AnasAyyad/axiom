# V1C PR2 readiness

## Current decision

**Implementation and both independent exchange canary qualifications are
complete; exact-source clean-image artifact qualification remains pending.**
C4 Binance Spot Testnet, C5 Bybit Demo, separate engine runtime wiring,
migration `000023`, and the controlled canary/evidence path are implemented.
The C4 and C5 phase gates, clean PostgreSQL 18.4 install, exact B8 upgrade,
security boundary, 1,024-profile Compose isolation matrix, and complete
repository `make verify` and `make v1c-pr2-local-qualify` aggregates pass.
An earlier dirty candidate passed image reproducibility, minimal inspection,
compiled boundary inspection, SPDX, and current-database Trivy gates, but that
artifact predates the final adapter hardening and is retained as diagnostic
history only. Exact-source artifact qualification remains pending the
implementation freeze.

PR2 is not accepted, committed, pushed, or ready to seed PR3. The independent
Binance and Bybit operator-armed canaries are complete on the same final dirty
candidate executable. The required file-backed credentials, database roles,
TOTP input, and owner attestations are provisioned without being recorded in
repository evidence. No sandbox result may be used as profitability evidence.

Live default-off startup validated both exchange identity/key boundaries.
Binance Testnet remained healthy in `READY_PAUSED` across repeated
reconciliation and clock-refresh cycles. After the owner manually funded the
Bybit Demo account, the current candidate accepted the exact ADR-0022 UI
bundle, loaded the approved BTC, ETH, and USDT wallet entries, reconciled
orders and executions, and reached healthy `READY_PAUSED`. Axiom did not add
or call a demo-fund application route.

The first operator canary preparation stopped before authorization input or
order-plan creation. It exposed two pre-order startup defects: the restricted
engine roles lacked SELECT-only access to `sessions` and `users`, which the
outbox authorization query reads, and Binance signed reads had no safe
classification/recovery path for timestamp code `-1021`. The current candidate
grants only SELECT on those two identity tables and permits exactly one clock
re-sync/re-sign retry for read-only Binance requests. Create and cancel
requests still receive no blind retry. Both reviewed canary graphs and both
built-in default-off graphs reached healthy `READY_PAUSED` with zero restarts
on the last built image; that image is now superseded by the bounded
post-response average-price clock validation described below.

Later prepare attempts remained pre-order and exposed two more fail-closed
integration defects. The immutable high-risk audit lookup used a redundant
`FOR UPDATE` after already taking its transaction-scoped advisory lock, which
required an intentionally absent mutation privilege. The lookup now relies on
the advisory lock and retains only SELECT/INSERT access. Bybit's
instrument-scoped BTCUSDT eligibility observation also validated an unrelated
low-volume ETHBTC book, and its 250 ms clock-uncertainty gate had only three
proxy samples. Eligibility now validates the same BTCUSDT book named by canary
admission, and the unchanged 250 ms ceiling uses a bounded 12-sample budget.
The latest Binance prepare then passed password/TOTP reauthentication and
created the audited arm, but the plan-reference check compared the
nanosecond-resolution returned arm with PostgreSQL's microsecond-resolution
stored timestamps. Arm creation now normalizes both timestamps at the
persistence boundary before insert and return; strict reference equality is
unchanged. A focused unit regression and clean-install PostgreSQL 18
qualification pass with a deliberately sub-microsecond input.
The following prepare reached plan-reference validation but the query also
requested a row lock on immutable session membership. The runtime role
intentionally has SELECT-only access to that table, so PostgreSQL rejected the
query and the adapter mapped it to `v1c_account_epoch_rejected`. The query now
locks only the mutable exchange-account row and reads immutable membership
without a mutation-capable lock. A SQL-shape regression and a clean-install
PostgreSQL 18 integration test running under the restricted `axiom_app` role
prove the exact query succeeds while membership UPDATE remains absent.
The next helper run reached a durable plan and printed `prepare succeeded`, but
immutable request evidence proved that no authenticated create route was ever
built or sent. A transient preflight read failed before network I/O, the
dispatcher conservatively marked the local attempt `UNKNOWN`, and the canary
harness incorrectly treated durable ambiguous cancel/reconcile commands as
cancel-or-fill confirmation. The shared adapters now reduce pre-network read
failures as deterministic local rejection, prepare requires exactly one
authenticated create evidence row, and cancel-or-fill evidence requires a
terminal `CANCELED` or `FILLED` state. An `UNKNOWN` attempt with no matching
create evidence can release its virtual reservation only after clean
reconciliation. The affected suites and a clean-install PostgreSQL 18
qualification pass. The old attempt was closed as a zero-fill rejection, and
no exchange create request occurred.
A fresh prepare against that strengthened candidate then failed closed with
`sandbox_canary_account_not_ready`. The prior invalid canary's order and
reservation were terminal, but its successfully prepared sandbox session was
still `ARMED`; the old command surface had no safe operator cleanup phase
because verification was the only normal session finalizer. The coordinator
now has an explicit `abort` phase that accepts only a one-attempt terminal
`CANCELED`, `FILLED`, or `REJECTED` canary, revokes its arm, stops its session,
and writes no qualification evidence. It rejects unknown, nonterminal, and
multi-attempt states. The failed fresh prepare produced no authenticated order
create evidence. Compose now resolves unused verify/abort request mounts to an
intentionally invalid non-secret placeholder; only prepare explicitly supplies
the protected short-lived request file, so control phases cannot receive its
contents or recreate a missing host request path as a directory.
The next exact-candidate prepare created one local plan but failed closed at
the strengthened create-evidence gate. The plan, outbox, reservation, and
session ended `FAILED`, terminal `REJECTED`, released, and `STOPPED`, with zero
authenticated Binance create requests. The durable rejection hash identified
`book_unavailable`: Testnet's `avgPrice.closeTime` was intermittently ahead of
raw host UTC, while the decoder rejected any future value before using the
already eligible Binance clock estimate. Average-price decoding and dynamic
filter validation now occur after the response against the conservative upper
bound of the synchronized clock (`offset + uncertainty`). If the response
timestamp leads that bound, the adapter may consume only its existing bounded
`/api/v3/time` sample budget to prove that the clock has caught up. The
`avgPrice` response is not fetched again, order mutations are not retried, and
values still beyond the proven bound fail closed without a free-form grace
period or create bypass. Focused regressions and the complete C4 Binance
qualification pass.
The following attempt then created exactly one authenticated Binance Testnet
order. It reached terminal `FILLED`; the plan completed, the reservation was
consumed, and the canary session stopped. The helper nevertheless reported
`sandbox_canary_create_evidence_invalid` because its restricted `axiom_app`
role lacked SELECT on the immutable authenticated-request evidence table. The
single create row was present, but the permission error was intentionally
collapsed into the closed verifier error. The runtime role now receives SELECT
only on that table; INSERT, UPDATE, and DELETE remain absent. A clean-install
PostgreSQL 18 integration test executes the exact count query under
`axiom_app`, and the exact B8 upgrade, C4, C5, race, fuzz, and compiled security
gates pass.
The coordinator also has a recovery-only phase for this post-create
interruption. It accepts only one terminal `CANCELED` or `FILLED` attempt,
exactly one authenticated create row, and a valid prepare-evidence prefix. It
can issue only query and reconciliation commands, receives no exchange
credential or prepare request, and has no create or cancel operation. The
existing filled canary was recovered and verified without another prepare.

The first recovery invocation selected the default-off graph instead of the
original reviewed Binance graph and correctly failed its configuration
identity check. The runbook now passes the exact graph to every coordinator
phase. Under the correct graph, the read-only Binance query normalized
successfully but persistence rejected its already-reduced event identity
because the retry had a later local `received_at`. Arrival time is not an
immutable exchange fact. Duplicate verification now retains the first receive
time while still requiring exact source hashes, canonical payload, event kind,
order/client identity, and exchange occurrence time. A conflicting immutable
fact remains rejected. Fresh PostgreSQL 18 and exact B8-upgrade regressions,
C4, C5, race, fuzz, and security gates pass.

Controlled-restart verification then reached evidence sealing but found the
Docker-created bind directory owned by the container namespace root and not
writable by `10001:70`. Only that dedicated directory was changed to owner
`10001:70` and mode `0750`; the verifier atomically created one `0440` file.
The sealed evidence validates `qualified=true`,
`profitability_evidence=false`, one outbox attempt, zero duplicates, exactly
one authenticated create request, all five required stages, the final
executable hash, and truthful dirty build identity. The durable order remains
terminal `FILLED`, its inbox has zero unreduced rows, and no second order was
created or canceled.

The first sealed file was then superseded because the cumulative source gate
required mechanical function/file-policy splits and the all-package race gate
exposed a cancellation timing defect in the Binance public collector. Normal
context cancellation can no longer enter the pause transition after the
context is already canceled. The exact regression passed 50 consecutive race
runs, the complete Binance package passed three race runs, C4 passed again,
and the complete repository `make verify` passed. C1-C5 and fresh PostgreSQL
18.4 clean-install and exact-upgrade gates also pass on this final worktree.

The replacement dirty candidate is image
`sha256:3d993465d5f01baa1686e1226af80706d94b498de4468d5d50ce40cd631d81be`
with executable
`ec2ebc3de71902e8935d565fdeecc1dc7f60c3af33358ea235115d024d46158f`.
Both exchange proxies and engines run that exact image; both engines are
healthy with zero restarts under their reviewed graphs. The already-terminal
Binance canary was verified again through the credential-free query and
reconciliation path into a new evidence root. The prior `0440` file was not
deleted or overwritten. Replacement evidence
`c5b4c2c5bc80c371248fd700b41fc5a522420fd6028ae13a86fb910afe56ad02`
is `0440`, owned by `10001:70`, and contains the exact final build and
executable identity, one create, one outbox attempt, zero duplicates, and all
five ordered stages. Final read-only database inspection remains
`1|TERMINAL|FILLED|1|5|0|STOPPED`; no second Binance order was submitted or
canceled.

The Bybit canary created exactly one Demo Spot order and later recovered it as
terminal `FILLED`. Live REST inspection exposed current envelope-only
`category`, omitted execution `isLeverage`, and additional documented
order/execution response fields; the adapter now binds and validates those
facts without weakening `category=spot`, `isLeverage=0`, route, or field
allowlists. The final recovery then exposed a second observation with the same
canonical terminal event but a different response-envelope hash. The immutable
inbox correctly retained both observations, but the reducer attempted a
forbidden no-op mutation of the terminal outbox. Exact canonical replays are
now marked reduced without reapplying aggregate, reservation, fill, or plan
side effects; different canonical facts still enter the full fail-closed
reducer. A clean PostgreSQL 18.4 qualification proves the duplicate terminal
observation is retained and reduced while the order stays `FILLED` with one
persisted fill.

The final shared dirty candidate is image
`sha256:e748058ebdf4cd1bd70d660977d34c3dbb7f2912cee7011ae207419163017940`
with executable
`e2837fde470fc8f7b89f180593b422e8ea8cf972ed18888b5f231735e3e170c9`.
Complete `make verify`, all 1,024 Compose renders, and fresh PostgreSQL 18.4
clean-install and exact B8-upgrade qualifications pass on that source. Bybit
recovery completed with zero unreduced inbox rows and no pending or claimed
engine command. Its sealed `0440` evidence
`e39883d4f5e0650b3861b2c3cd753ba1fc832f0536902c9bcbf357568dfa765b`
validates one authenticated create, one outbox attempt, zero duplicates, all
five stages, `qualified=true`, and `profitability_evidence=false`. Because the
replay correction is adapter-neutral, the already-terminal Binance canary was
also re-verified without create or cancel capability on the same executable.
Its new sealed `0440` evidence is
`22072e94ccd24ee10094068ca74720479ba2362b374efac961751f23f4fc3473`.

Live proxy cuts then proved both credential-owning engines fail closed without
an automatic restart loop. Each engine independently transitioned
`READY_PAUSED` → `DEGRADED`, failed its readiness probe while remaining
running at restart count zero, reconnected and backfilled its private stream,
completed a fresh authoritative reconciliation, and returned to
`READY_PAUSED` on the same container. Both engines then stopped with exit code
zero and only `service_stopped` lifecycle events. Final database inspection
showed both canaries `TERMINAL/FILLED` at attempt one, both sessions `STOPPED`,
zero active commands, zero unreduced inbox rows, and exactly one create route
record per exchange.

## Candidate identity

| Field | Value |
|---|---|
| Refreshed merged-main baseline | `fdeb923b61a83d5a5328a2b5c764def3a6393e8d` |
| Branch | `v1c-c4-c5-adapters` |
| Candidate commit | pending; exact pre-commit worktree |
| Configuration schema | `axiom.config.v1c.1`, integrations/submission default off |
| Migrations | merged `000021`/`000022` plus PR2 `000023` |
| C4 deterministic gate | passed |
| C5 deterministic gate | passed |
| Repository `make verify` | passed |
| Final candidate PostgreSQL gate | passed |
| Aggregate `v1c-pr2-local-qualify` | passed before final mechanical source split; final C1-C5, fresh clean/upgrade PostgreSQL, affected race stress, and complete `make verify` passed afterward |
| Dirty-candidate image/SBOM/Trivy gates | retained earlier candidate only; exact-source rerun pending freeze |
| Prior funded default-off hold image | `sha256:9c11a3d65a98c00c472ce2dbdb73146551d40e968792c3f358b472c6d5d43c78` |
| Strengthened-contract review image | `sha256:d4fef2335949ec8b977ee65810faeb89654542fc6366706e6077dd1bba9a95c5`; superseded by explicit abort safeguard |
| Strengthened-contract executable | `50ea29717f636120ee949cc3fce1638fd71af84023c631dc48a6ee3081482cc6` |
| Terminal-abort review image | `sha256:5098ab95220d48b9d497146c096d6edb6eae57edee15485c07498fbd2991a7a4`; superseded by synchronized average-price validation |
| Terminal-abort executable | `d147c12b2aee4b8264078c6bc0e5b5d5c80305ed2773c82b294f22aea2aba36b` |
| Pre-response-clock review image | `sha256:b12796ecbc7d2a16b0a487ca878b2d8e21b127c6595631e7debd5728d7627115`; superseded by bounded post-response clock validation |
| Pre-response-clock executable | `96a1c47117943ad458b29cd59dc8b699afded89b08cf9daf35985311f9f807e2` |
| Post-response-clock review image | `sha256:e5a8083a9f1205f6da3636a9c348befb3e208aafd8e2f05bee3721e412cdb638`; superseded by verifier-role and recovery correction |
| Post-response-clock executable | `4b9061862489c9a2075d6c425694be2513bfa29fee46104b22b47352b1225245` |
| Verifier-role review image | `sha256:f0ac7a7721accb384e940b04fb94cfa28488d1fef071ed05974644179612ea0e`; superseded by idempotent inbox recovery |
| Verifier-role executable | `0f0461e7dac3d9299a0ab931489d580142fab42867011d614761d5de2fefdcd0` |
| Superseded first sealed-evidence image | `sha256:23117aab2068bd382e72405031ad2e9da32b86eaf380ea940b894e854d7666e8`; retained without overwrite |
| Superseded first sealed evidence | `e609eb97ab28f781bf17658ed496e746adc4ec43143a99b889ebe1c6b5d91ccd` |
| Superseded Binance-only qualified image | `sha256:3d993465d5f01baa1686e1226af80706d94b498de4468d5d50ce40cd631d81be` |
| Superseded terminal-replay shared image | `sha256:44ddb43116e4c1ede699afffc0d49dc3dfc1b1e2e205b147f20ee1e79b5012a2`; exposed runtime lifecycle failure |
| Superseded eligibility-only lifecycle image | `sha256:095e7f9e738714712c70e8a4637c7d9fceb48e7f5dc9d46e3c04d6db76895489`; exposed independent private-stream supervisor exit |
| Current shared canary-qualified image | `sha256:e748058ebdf4cd1bd70d660977d34c3dbb7f2912cee7011ae207419163017940` |
| Current shared canary-qualified executable | `e2837fde470fc8f7b89f180593b422e8ea8cf972ed18888b5f231735e3e170c9` |
| Dirty-image Compose admission | rejected as required: `a11_startup_recovery_build_invalid` |
| Clean-image artifact qualification | pending exact-source implementation freeze |
| Binance default-off live proof | healthy `READY_PAUSED`, five-minute hold, zero restarts |
| Bybit default-off live proof | healthy `READY_PAUSED`, six-minute hold, zero restarts |
| Current Binance graph startup | exact shared image reached healthy `READY_PAUSED`; live proxy cut degraded and recovered without restart; clean exit zero |
| Current Bybit graph startup | exact shared image reached healthy `READY_PAUSED`; live proxy cut degraded and recovered without restart; clean exit zero |
| Binance canary | passed on shared executable; one authenticated create, terminal `FILLED`, five stages, sealed evidence `22072e94ccd24ee10094068ca74720479ba2362b374efac961751f23f4fc3473` |
| Bybit canary | passed on shared executable; one authenticated create, terminal `FILLED`, five stages, sealed evidence `e39883d4f5e0650b3861b2c3cd753ba1fc832f0536902c9bcbf357568dfa765b` |
| PR2 push | not performed |

Any subsequent code, configuration, migration, or evidence-contract change
invalidates candidate-local results and requires the affected gates to repeat.
Detailed results and retained failures are in
[`evidence/v1c-pr2-local-validation.md`](evidence/v1c-pr2-local-validation.md).
