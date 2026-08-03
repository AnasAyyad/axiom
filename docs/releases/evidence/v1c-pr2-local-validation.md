# V1C PR2 local validation

## Decision

C1-C5 deterministic phase gates, final-candidate PostgreSQL qualification, and
the complete repository `make verify` gate pass across the final functional
candidate. The final private-stream file split was followed by the affected
targeted suites, exact source scanners, and another complete `make verify`.
The Binance Spot Testnet and Bybit Demo canaries are complete with independent
sealed evidence files from the same final dirty executable. The exact
implementation source was then frozen at
`912892161d0b5f6c24fc8c1b035ffdb99847aaa5`; its truthful `DIRTY=false`
runtime image passes reproducibility, minimal and compiled boundary
inspection, image-backed Compose smoke, SPDX, and Trivy. Earlier dirty-image
evidence below is retained diagnostic history only. This file is local
qualification evidence, not owner/security acceptance or profitability
evidence.

## Identity

| Field | Value |
|---|---|
| Main baseline | `fdeb923b61a83d5a5328a2b5c764def3a6393e8d` |
| Branch | `v1c-c4-c5-adapters` |
| Candidate implementation commit | `912892161d0b5f6c24fc8c1b035ffdb99847aaa5` |
| Configuration schema | `axiom.config.v1c.1` |
| Migration | `000023_v1c_private_stream_runtime.sql` |
| Final dirty image | `sha256:e748058ebdf4cd1bd70d660977d34c3dbb7f2912cee7011ae207419163017940` |
| Final executable | `e2837fde470fc8f7b89f180593b422e8ea8cf972ed18888b5f231735e3e170c9` |
| Qualified clean image | `sha256:c8d561191d6967c0f25677b19460c43e137e453de474f256bd1a7fc0450c51a9` |
| Qualified clean executable | `c9c0ab83bba960bd2ef60835f4ae26cf806335423218e6a7a8e9e9eb69fc8308` |
| Evidence completed | Binance `22072e94ccd24ee10094068ca74720479ba2362b374efac961751f23f4fc3473`; Bybit `e39883d4f5e0650b3861b2c3cd753ba1fc832f0536902c9bcbf357568dfa765b` |

## Toolchains

- Go `1.26.5`
- Node `24.18.0`
- pnpm `11.12.0`
- PostgreSQL `18.4`

## Passed gates

- `make preflight`
- `make c1-security-qualify`
- `make c2-auth-qualify`
- `make c3-recovery-qualify`
- `make c4-binance-testnet-qualify`
- `make c5-bybit-demo-qualify`
- Full repository aggregate using
  `make verify VULNDB=file:///tmp/axiom-vulndb.J5OmWl/vulndb-master/data/osv`
- Targeted bootstrap, sandbox, exchange, and V1C PostgreSQL tests
- Independent clean install on `axiom_pr2_clean_v1c_test`
- Independent exact B8 upgrade on `axiom_pr2_upgrade_v1c_test`
- Aggregate clean install on `axiom_pr2_aggregate_clean_v1c_test`
- Aggregate exact B8 upgrade on `axiom_pr2_aggregate_upgrade_v1c_test`
- Final source-policy candidate clean install on
  `axiom_pr2_source_final_clean_v1c_test`
- Final source-policy candidate exact B8 upgrade on
  `axiom_pr2_source_final_upgrade_v1c_test`
- Idempotent private-inbox retry clean install on
  `axiom_inbox_retry_clean_20260729_v1c_test`
- Idempotent private-inbox retry exact B8 upgrade on
  `axiom_inbox_retry_upgrade_20260729_v1c_test`
- Final function/file-policy candidate clean install on
  `axiom_final_policy_clean_20260729_v1c_test`
- Final function/file-policy candidate exact B8 upgrade on
  `axiom_final_policy_upgrade_20260729_v1c_test`
- The exact Binance normal-cancellation regression passed 50 consecutive race
  runs; the complete Binance package passed three race runs before C4 and the
  aggregate repository gate were repeated
- Earlier complete `make v1c-pr2-local-qualify` rerun using
  `axiom_pr2_aggregate_rerun_clean_v1c_test` and
  `axiom_pr2_aggregate_rerun_upgrade_v1c_test`; the later functional adapter
  changes passed C1-C5 and fresh clean/upgrade PostgreSQL gates, and the final
  mechanical split passed affected suites plus the complete `make verify`
- `scripts/check-file-policy.sh`
- `scripts/check-v1c-security-boundary.sh`
- `scripts/check-compose.sh`, including all 1,024 active profile combinations
- `git diff --check`
- `govulncheck` against official `golang/vulndb` commit
  `7f04fb463deb2e42d892e6f9165c6eaca2feb1b6` dated 2026-07-27, archive SHA-256
  `b3549500636ce4da8ac7b996638b4abe83709e27f0324a3b31f233e8fb8f6502`;
  no called vulnerabilities were found

## Exact-source clean image and supply-chain evidence

- Image `axiom:v1c-pr2-clean-20260730t094323z` resolves to
  `sha256:c8d561191d6967c0f25677b19460c43e137e453de474f256bd1a7fc0450c51a9`,
  is 11,218,360 bytes, and embeds version `v1c-pr2-clean`, commit
  `912892161d0b5f6c24fc8c1b035ffdb99847aaa5`, build time
  `2026-07-30T09:43:23Z`, and truthful `DIRTY=false`.
- `make image-reproducibility` passed complete runtime
  configuration/root-filesystem comparison with fingerprint
  `sha256:7d193a22bf59b3e6beb076b6ef1d7eeb273408113d968e264df1775d65d3309b`.
- `scripts/inspect-image.sh` passed scratch-shell absence, numeric non-root
  user `10001:70`, fixed `/app/platform` entrypoint, read-only execution, and
  credential-like environment-key checks.
- The extracted runtime executable has SHA-256
  `c9c0ab83bba960bd2ef60835f4ae26cf806335423218e6a7a8e9e9eb69fc8308`.
  It contains the five required Testnet/Demo hosts and the exact build
  identity, contains no production-private Binance or forbidden API-family
  literal, and each embedded canary graph enables submission for exactly one
  exchange.
- `make compose-smoke` passed migration, hardened API/shadow/recorder/worker
  startup, authenticated login/CSRF/logout, production-order disablement,
  Prometheus target discovery, Grafana provisioning, and cleanup using only
  generated temporary secrets.
- The ignored image export is
  `.local/v1c-pr2-image-evidence-clean-9128921-20260730t094323z/axiom-v1c-pr2-clean.tar`,
  SHA-256
  `c07844ff2f83ad861d60fd748274b78e46e183458cea79453128b78f93d00122`.
- The retained ignored SPDX 2.3 SBOM is
  `.local/v1c-pr2-image-evidence-clean-9128921-20260730t094323z/axiom-v1c-pr2-clean.spdx.json`,
  contains 47 packages, and has SHA-256
  `7d416040d7067c286efc521a8f3445c7d99bc7d76bebfaef2ce038fec3e784ce`.
- Trivy 0.72.0 used vulnerability database update
  `2026-07-30T07:43:55.655924266Z`, downloaded at
  `2026-07-30T09:53:16.036734584Z`, to scan the read-only export with no
  Docker socket or network. The offline gate enabled
  `vuln,secret,misconfig,license`, severity `HIGH,CRITICAL`,
  `ignore-unfixed=false`, and `exit-code=1`; it exited zero. The retained
  ignored report is
  `.local/v1c-pr2-image-evidence-clean-9128921-20260730t094323z/trivy-v1c-pr2-clean.json`,
  SHA-256
  `be322413f29c6420b6ce1ef8eba23979ed7ac041ada9ce4c58dcf0990a41b1b8`,
  with zero qualifying findings in every scanner category.

## Final Binance evidence reseal

The first canary-qualified dirty image
`sha256:23117aab2068bd382e72405031ad2e9da32b86eaf380ea940b894e854d7666e8`
and evidence
`e609eb97ab28f781bf17658ed496e746adc4ec43143a99b889ebe1c6b5d91ccd`
are retained as superseded immutable diagnostics. They were not deleted or
overwritten.

After the final source-policy split and cancellation-race correction, both
exchange proxies and engines were replaced with image
`sha256:3d993465d5f01baa1686e1226af80706d94b498de4468d5d50ce40cd631d81be`.
Both engines reached healthy with zero restarts under their reviewed graphs.
The existing terminal Binance canary was then verified into a separate
evidence root. This phase had no exchange credential, no prepare request, and
no create or cancel operation; it issued only query and reconciliation.

The replacement file is owned by `10001:70`, has mode `0440`, and has evidence
ID `c5b4c2c5bc80c371248fd700b41fc5a522420fd6028ae13a86fb910afe56ad02`.
It seals baseline `fdeb923b61a83d5a5328a2b5c764def3a6393e8d`, truthful
`dirty=true`, build time `2026-07-29T16:07:31Z`, executable
`ec2ebc3de71902e8935d565fdeecc1dc7f60c3af33358ea235115d024d46158f`,
one authenticated create, one outbox attempt, zero duplicates,
`qualified=true`, `profitability_evidence=false`, and exactly the ordered
`PLAN_APPROVED`, `QUERY_SUCCEEDED`, `CANCEL_OR_FILL_CONFIRMED`, `RECONCILED`,
and `RESTART_VERIFIED` stages. Final read-only database inspection returned
`1|TERMINAL|FILLED|1|5|0|STOPPED`. No second Binance order was submitted or
canceled.

## Retained earlier image and supply-chain evidence

- The earlier dirty pre-commit image was
  `axiom@sha256:48bfd2fb5574c788cca81d5a9d6c497c62fee0051d509fa20d2c61bb1feadbd8`,
  11,181,189 bytes, with base commit
  `fdeb923b61a83d5a5328a2b5c764def3a6393e8d` and truthful `DIRTY=true`. It
  predates the final adapter hardening and cannot qualify the current source.
- `make image-reproducibility` passed complete runtime
  configuration/root-filesystem comparison with fingerprint
  `sha256:9c909cac9a11b14e7209a0f79f2ea9facd16d2a2c44b3ab9c0df241f3c5d9fdd`.
- `scripts/inspect-image.sh` passed scratch-shell absence, numeric non-root
  user `10001:70`, fixed `/app/platform` entrypoint, read-only execution, and
  credential-like environment-key checks.
- The extracted runtime binary has SHA-256
  `c5041a01beac5f0e28f9a302d45dc746fd79533d2ef22890b3dbabd617ecc444`.
  It contains all five required Testnet/Demo hosts, no production-private or
  forbidden API family, no external asset-movement capability, and exactly
  one enabled exchange in each embedded canary graph.
- Retained ignored SPDX 2.3 SBOM:
  `.local/v1c-pr2-image-evidence-precommit/axiom-v1c-pr2-candidate.spdx.json`,
  47 packages, SHA-256
  `1f5e6a1270e53945a8153d5ca4956f21be253490943728acd388367877ea6343`.
- Trivy 0.72.0 used vulnerability database update
  `2026-07-28T07:38:05.767176531Z` to scan the read-only image export without
  a Docker socket or network. The offline gate enabled
  `vuln,secret,misconfig,license`, severity `HIGH,CRITICAL`,
  `ignore-unfixed=false`, and `exit-code=1`; it exited zero. The retained
  ignored JSON is
  `.local/v1c-pr2-image-evidence-precommit/trivy-v1c-pr2-candidate.json`,
  SHA-256
  `7907041594097bf37c77fe5a9703d46604761b2ea4d155b70f4b638355cb1c38`,
  with zero qualifying findings in every scanner category.

## Pending gates

- Formal owner and security acceptance

## Live pre-canary unblock

The owner provisioned the seven required file-backed prerequisites and matching
non-secret attestations on 2026-07-28. No credential, TOTP, password, signature,
account identifier, or private payload was printed or added to this evidence.

- Binance Testnet live startup validated the key/account identity, loaded and
  reconciled account state, opened the signed private stream, and remained
  healthy in `READY_PAUSED` with the built-in submission-off graph across
  repeated 30-second reconciliation and clock-refresh cycles.
- An earlier Binance-only current-source review binary
  `8c75cbe596103ab3364135b0415f6745923cc06f5686d7401bb665724516d254`
  repeated that default-off proof for five minutes with zero restarts, then the
  engine was stopped.
- Live response compatibility now accepts Binance's documented
  `subscriptionId=0`, sends WebSocket JSON as text frames, uses bounded clock
  retries and conservative signed timestamps, and accounts for the 221-weight
  first-page account snapshot without reading it twice per reconciliation.
- Bybit Demo live key inspection accepts only ADR-0022's exact UI-coupled
  Unified Trading bundle. On 2026-07-29, after the owner manually funded the
  isolated Demo account, the authoritative wallet returned all approved
  assets. The candidate accepted Bybit's current Unified-wallet response,
  including the deprecated omitted `free` field and documented `colRes`
  metadata, while continuing to reject borrow, interest, order/position
  margin, unrealized-PnL, hedging, and unknown collateral metadata.
- The funded Bybit engine loaded wallet, order, and execution history,
  reconciled the account, and reached healthy `READY_PAUSED` with submission
  still disabled. Axiom has no demo-fund application route and did not move or
  request funds.
- The final funded default-off review used image
  `sha256:9c11a3d65a98c00c472ce2dbdb73146551d40e968792c3f358b472c6d5d43c78`
  and executable
  `63794ac67099b144a3979464e71af73466631b338ea911188677b2131e0b06b8`.
  Binance remained healthy for five minutes and Bybit for six minutes with
  restart count zero on both containers. The hold crossed repeated 30-second
  clock refreshes, reconciliation windows, and Bybit 20-second private-stream
  heartbeats.
- The post-startup-fix pre-canary review used image
  `sha256:828f13670874ab08089dc5dc2b9630022cb96afe0e45a2c3254d0a741db2d2f8`
  and executable
  `b981a88b819f004f79ce662006fe5f5e93d217422d87d132044d587fff5eba49`.
  Its migrator applied SELECT-only `sessions` and `users` grants to both
  restricted engine roles. Binance and Bybit each reached healthy
  `READY_PAUSED` with zero restarts under the reviewed single-exchange canary
  graph and again under the built-in default-off graph. No canary
  authorization file or engine command existed during these startup checks.
- The final account-lock-fixed pre-canary review used image
  `sha256:4ef20cb4d6abef68f2b29d705119e40beb11ab6a762ae7b8b3f3dedca6358ecb`
  and executable
  `fc97966f8391fed9fa6492922e729426826800a294d1579eab5c34c5bfd4f495`.
  Its immutable audit chain uses the existing transaction-scoped advisory lock
  without requesting UPDATE privilege, Bybit validates the BTCUSDT book named
  by canary admission without coupling it to unrelated ETHBTC inactivity, and
  its unchanged 250 ms clock-uncertainty ceiling has a bounded 12-sample
  warmup budget. Arm creation also normalizes UTC timestamps to PostgreSQL
  microsecond precision before both insert and return, while the plan-reference
  equality gate remains strict. The focused unit regression and a clean-install
  PostgreSQL 18 integration qualification passed with a deliberately
  sub-microsecond arm. Plan-reference validation locks only the mutable
  exchange-account row and reads immutable session membership without
  requesting its intentionally absent UPDATE privilege. A SQL-shape regression
  and clean-install PostgreSQL 18 integration test executed the exact query
  under restricted role `axiom_app` with SELECT-only membership access.
  Binance and Bybit canary graphs both reached healthy `READY_PAUSED` with zero
  restarts. The hold crossed two 30-second clock refresh intervals with zero
  restarts. Both engines and egress proxies ran the exact image and the engines
  were then restored to its built-in default-off graphs, healthy with zero
  restarts.
- The final canary-contract-fixed pre-canary review used image
  `sha256:d4fef2335949ec8b977ee65810faeb89654542fc6366706e6077dd1bba9a95c5`
  and executable
  `50ea29717f636120ee949cc3fce1638fd71af84023c631dc48a6ee3081482cc6`.
  Pre-network account/book failures now reduce as deterministic local
  rejections rather than ambiguous creates. Prepare requires exactly one
  authenticated create evidence row and accepts cancel-or-fill only after the
  durable outbox is terminal with `CANCELED` or `FILLED`. PostgreSQL may close
  an unsent `UNKNOWN` attempt only when the matching configuration has no
  authenticated create evidence, no fill exists, and a clean reconciliation
  is persisted under the active fence. Adapter, bootstrap, storage, source
  policy, exchange-boundary, and security-boundary checks passed. A fresh
  clean-install PostgreSQL 18 qualification proved the unsent recovery path.
  The prior attempt then closed as `FAILED` with a terminal `REJECTED` outbox
  and released reservation. Both reviewed canary graphs held healthy with zero
  restarts across two 30-second intervals on the exact image, and both engines
  were restored healthy with zero restarts to built-in default-off graphs.
- The terminal-abort and request-mount-fixed pre-canary review uses image
  `sha256:5098ab95220d48b9d497146c096d6edb6eae57edee15485c07498fbd2991a7a4`
  and executable
  `d147c12b2aee4b8264078c6bc0e5b5d5c80305ed2773c82b294f22aea2aba36b`.
  Its explicit abort operation accepts only one-attempt terminal canaries,
  writes no qualification evidence, and cannot clear unknown, nonterminal, or
  duplicate-attempt state. The prior stale terminal session was stopped
  without a direct database edit. The default Compose request mount is an
  intentionally invalid non-secret placeholder; only prepare explicitly
  supplies the protected short-lived request file.
- The pre-response synchronized-average-price candidate used image
  `sha256:b12796ecbc7d2a16b0a487ca878b2d8e21b127c6595631e7debd5728d7627115`
  and executable
  `96a1c47117943ad458b29cd59dc8b699afded89b08cf9daf35985311f9f807e2`.
  It proved that the average-price timestamp and dynamic percent-price filter
  can share the eligible bounded Binance clock upper limit, but live sampling
  then showed that a bound captured before the `avgPrice` request could expire
  during the request. It is superseded by post-response bounded clock
  validation.
- The post-response bounded-clock candidate uses image
  `sha256:e5a8083a9f1205f6da3636a9c348befb3e208aafd8e2f05bee3721e412cdb638`
  and executable
  `4b9061862489c9a2075d6c425694be2513bfa29fee46104b22b47352b1225245`.
  Its complete C4 qualification, fuzz coverage, compiled V1C security scan,
  affected suites, source boundaries, file policy, and documentation gates
  pass. Both exchange engine/proxy graphs then held the exact image for
  35 seconds with healthy engines and zero restarts. Read-only database
  inspection showed zero active sandbox sessions and current Binance and Bybit
  `READY_PAUSED` observations with eligible BTCUSDT books, healthy private
  streams, clean reconciliation, healthy evidence, active leases, and
  completed startup evidence. It is superseded by the verifier-role and
  recovery correction after the real canary exposed the omitted read grant.

Early Binance prepare attempts stopped before order-plan creation. The first
stopped during engine startup; the next exposed user-namespaced host group remapping;
the next reached high-risk authorization but rolled back before issuing a
grant because of the immutable-audit lookup defect; a later attempt passed
reauthentication and created the audited arm but rolled back when its returned
nanosecond timestamps did not equal PostgreSQL's stored microsecond timestamps.
The latest attempt reached plan-reference validation and failed closed because
immutable session membership was included in a row-lock clause that its
SELECT-only runtime role could not execute. Each failed run durably stopped its
session and revoked its arm. Aggregate checks after the latest attempt found
zero plans, zero outbox rows, and zero authenticated Binance
`POST /api/v3/order` evidence in its interval. No exchange order was submitted,
and no sealed canary evidence was created.

The following run created a plan and the old helper printed success, but it did
not qualify as a canary. The outbox recorded one local attempt while durable
authenticated evidence contained zero Binance create requests. A pre-network
read failure had been conservatively marked `UNKNOWN`; the helper then accepted
query, ambiguous cancel, and reconciliation command completion without proving
terminal cancel-or-fill. The strengthened candidate rejected that evidence,
closed the unsent attempt after clean reconciliation, released its virtual
reservation, and removed the stale local identity pointer. At that point, no
exchange order had been submitted, no sealed canary evidence existed, PR2 was
not committed or pushed, and PR3 had not been created.

The next strengthened-candidate retry failed before session creation with
`sandbox_canary_account_not_ready`. Read-only aggregate inspection proved the
failed interval contained authenticated account/reconciliation reads and zero
Binance `POST /api/v3/order` evidence. The blocker was the earlier invalid
canary's still-`ARMED` sandbox session: its unsent order and reservation had
been safely closed, but verification had never finalized that session. The
command surface now includes an explicit abort operation that revokes the arm
and stops the session only for one-attempt terminal `CANCELED`, `FILLED`, or
`REJECTED` canaries. It writes no qualification evidence and rejects
ambiguous, nonterminal, and multi-attempt states. The parser, coordinator,
storage, source-policy, documentation, and security-boundary checks pass.
Compose defaults the unused request mount to an intentionally invalid,
non-secret placeholder, while prepare explicitly overrides that source with
the protected short-lived file. Verify/abort cannot receive request contents
or recreate a missing request path as a host directory.

The next exact-candidate prepare created one plan and then correctly failed the
strengthened create-evidence gate. Read-only evidence showed one local attempt,
a `FAILED` plan, terminal `REJECTED` outbox, released reservation, stopped
session, only `PLAN_APPROVED`, and zero authenticated Binance
`POST /api/v3/order` records. Matching the durable native hash against the
compiled rejection allowlist identified `book_unavailable`. Live public
samples showed `avgPrice.closeTime` intermittently ahead of raw host UTC, while
the decoder rejected any positive skew even though the engine already held an
eligible bounded Binance clock estimate. Average-price decoding and dynamic
filter validation now occur after the response against the conservative
synchronized upper bound `local UTC + offset + uncertainty`. If the response
timestamp still leads that bound, the adapter consumes only its existing
bounded `/api/v3/time` sample budget to prove that the clock caught up. It
does not refetch `avgPrice` or retry any order mutation. A timestamp still
beyond the proven bound is rejected without an arbitrary tolerance or create
bypass. Focused tests cover existing-bound acceptance, bounded clock catch-up,
budget exhaustion, and exactly one `avgPrice` fetch; the complete C4
qualification passes.

The next attempt created exactly one authenticated Binance Testnet order. The
durable outbox reached one attempt and terminal `FILLED`; the plan completed,
the reservation was consumed, and the canary session stopped. Only
`PLAN_APPROVED` was recorded because the canary process then failed at its
create-evidence check. Read-only inspection found one authenticated
`POST /api/v3/order` and one `POST /api/v3/order/test` in the interval. The
failure was not missing evidence: restricted role `axiom_app` had no SELECT
grant on `v1c_authenticated_request_evidence`, so the exact count query returned
permission denied and the verifier collapsed that error to
`sandbox_canary_create_evidence_invalid`.

The runtime grant now adds only SELECT for that immutable table. INSERT,
UPDATE, and DELETE remain denied. Unit tests cover the complete post-create
coordinator read/write surface, and a clean-install PostgreSQL 18 integration
test executes the exact create-count query under `axiom_app`. The exact B8
upgrade, complete C4 and C5 gates, race tests, fuzz tests, and compiled security
boundary pass. A recovery-only phase can complete the existing terminal
canary's query, cancel-or-fill, and reconciliation evidence without creating
or canceling an order. It refuses rejected, ambiguous, nonterminal,
duplicate-attempt, wrong-configuration, missing-create, duplicate-create,
non-prefix-evidence, and already-restarted cases. No second prepare is allowed.
The verifier-role/recovery candidate used image
`sha256:f0ac7a7721accb384e940b04fb94cfa28488d1fef071ed05974644179612ea0e`
and executable
`0f0461e7dac3d9299a0ab931489d580142fab42867011d614761d5de2fefdcd0`.
It was superseded after the real recovery proved that an idempotent event
identity can legitimately have a later local receive time.

The final Binance-qualified candidate uses image
`sha256:23117aab2068bd382e72405031ad2e9da32b86eaf380ea940b894e854d7666e8`
and executable
`0377285efbc6bb90e70f89f6985199147019cf6fb011acf3b65328ffc945d829`.
Recovery and controlled-restart verification completed the existing filled
canary without create or cancel capability. Final read-only inspection proved
one outbox attempt, terminal `FILLED`, exactly one authenticated
`POST /api/v3/order`, five ordered evidence stages, and zero unreduced inbox
rows. The only evidence file is owned by `10001:70`, sealed mode `0440`, and
validates `qualified=true`, `profitability_evidence=false`, zero duplicate
submissions, truthful dirty build identity, and the executable hash above. Its
evidence identity is
`e609eb97ab28f781bf17658ed496e746adc4ec43143a99b889ebe1c6b5d91ccd`.

## Final shared-image canary qualification

The Bybit canary submitted exactly one Demo Spot create and reached terminal
`FILLED`. Current live responses required explicit validation for envelope
`category`, omitted execution `isLeverage`, and newly present documented
order/execution fields. Those adapter-boundary changes retained the closed
Demo REST host/route policy, Spot-only category, non-leveraged request schema,
and prohibited-capability rejection.

Recovery later received an exact canonical `FILLED` replay whose raw response
envelope hash differed. The immutable inbox stored both observations, but the
second reduction attempted to update the already-terminal outbox and was
rejected by its mutation guard. The final persistence change recognizes only
an already-reduced event with equal canonical JSON for the same account,
epoch, and order. It marks that new observation reduced without replaying
aggregate or accounting side effects. Any changed canonical state, fill, fee,
quantity, identity, or occurrence fact still follows the normal fail-closed
reducer.

A fresh PostgreSQL 18.4 clean-install regression proves the terminal replay is
retained and reduced, the outbox remains `TERMINAL/FILLED`, and the fill count
remains one. The complete repository `make verify` then passed, including the
1,024-profile Compose matrix and vulnerability scan. Fresh clean-install and
exact B8-to-V1C PostgreSQL qualifications also passed on the final source.

The shared candidate image is
`sha256:e748058ebdf4cd1bd70d660977d34c3dbb7f2912cee7011ae207419163017940`
and its executable is
`e2837fde470fc8f7b89f180593b422e8ea8cf972ed18888b5f231735e3e170c9`.
Bybit recovered with zero unreduced inbox rows, no pending or claimed engine
commands, one outbox attempt, and no second create. Controlled-restart
verification sealed evidence
`e39883d4f5e0650b3861b2c3cd753ba1fc832f0536902c9bcbf357568dfa765b`.
The file SHA-256 is
`b803d5c42221a5b2c6e4a804171a6fc3ec43a1f974a3fdc8db850207f14c9f1d`.

Because the terminal-replay correction is shared persistence code, the
already-filled Binance canary was re-verified on the same executable without
another create or cancel. Its new sealed evidence is
`22072e94ccd24ee10094068ca74720479ba2362b374efac961751f23f4fc3473`
with file SHA-256
`5a8e7428ee57fd559b09cc9436d75b46d8e0927a652db269f13eb1f8157cd974`.
Both files are owned by `10001:70`, mode `0440`, and validate
`qualified=true`, `profitability_evidence=false`, one authenticated create,
one outbox attempt, zero duplicates, all five stages, and the exact executable
above.

After sealing, each proxy was stopped independently. The matching engine
immediately persisted `DEGRADED`, failed readiness, and stayed running with
restart count zero. Restarting the proxy completed private-stream reconnect
and deterministic backfill, fresh eligibility and authoritative
reconciliation, then restored `READY_PAUSED` on the same container. Both
engines subsequently stopped with exit code zero and a `service_stopped`
event. No order-changing command ran during failover qualification.

## Superseded runtime-lifecycle candidates

The terminal-replay image
`sha256:44ddb43116e4c1ede699afffc0d49dc3dfc1b1e2e205b147f20ee1e79b5012a2`
and executable
`2df755a0f7a201ec9df02aeec8dc6cfd1cbcffa71a53451ac275bc4dca8d0f27`
remain retained diagnostic history. Its Bybit evidence
`2a3ee436aff1e1687b2059cc1e230a4c660d2621ebc85c80cd194e3500a8fe85`
and Binance evidence
`8c156a8fb37a2416a89750b42fcbd0e4efe9c03483b1ace09038f63777f9989a`
were not overwritten.

The eligibility-only lifecycle image
`sha256:095e7f9e738714712c70e8a4637c7d9fceb48e7f5dc9d46e3c04d6db76895489`
and executable
`4628864f27f0ed4237ea9a49f1a852899fb5d342a4383c6d01915383e7edf136`
also remain superseded diagnostic history. Its Bybit evidence
`2333eb9194e3ea9973d58b45d71d050c583a5f62c84632ba931c95c0eb25c989`
and Binance evidence
`136c8fdf57f93adc16a77235ce2848e2b31845e6e3713a0806ae6ae3dfa031d9`
were sealed in separate roots and were not modified.

## Retained failures

1. The first aggregate PostgreSQL attempt used database names without the
   mandatory `_v1c_test` suffix. The database guard rejected both before
   migration.
2. The correctly named aggregate attempt inside the filesystem sandbox reached
   PostgreSQL but localhost socket access was denied. The exact command was
   rerun with explicit access to the disposable local PostgreSQL container and
   both database gates passed.
3. The first cumulative format check found Prettier drift in
   `scripts/check-compose-command-contract.mjs`; it was formatted without
   changing the checked contract.
4. The first final `make verify` attempt inside the filesystem sandbox could
   not bind the emulator's loopback listener. The aggregate was rerun outside
   that restriction and passed.
5. The legacy V1A prohibited-capability and A6 binary scanners initially
   rejected the reviewed V1C Testnet/Demo boundary. Their policy was narrowed
   to exact V1C files and production-private/forbidden destinations remained
   denied; negative scanner tests and the stricter V1C binary/source scan pass.
6. The newly funded Bybit Unified wallet exposed current response behavior not
   present in the empty wallet: omitted deprecated `free` and documented
   `colRes` metadata. The normalizer now derives available balance exactly from
   wallet balance minus locked balance, accepts only documented `colRes`
   values as non-executable metadata, and retains fail-closed checks for borrow,
   interest, margin, unrealized-PnL, hedging, duplicates, and arithmetic
   mismatch.
7. Multi-cycle live review exposed Bybit clock code `10002`, absent private
   heartbeat, and authenticated REST idle-tunnel failures. The client now
   performs one clock refresh/re-sign retry, sends and strictly consumes the
   documented 20-second private heartbeat, expires idle REST connections before
   the proxy, and uses bounded retries only for idempotent snapshot reads.
   Order mutations received no generic transport retry.
6. Direct `govulncheck` access to `vuln.go.dev` was network-restricted. The
   official `golang/vulndb` snapshot identified above was downloaded and
   scanned through the supported local file database interface.
7. Docker Desktop stopped after the earlier PostgreSQL qualification. The WSL
   daemon socket did not become available immediately after restart. Docker
   later recovered; the final PostgreSQL, aggregate, and artifact gates then
   ran successfully.
8. The first final aggregate run had two isolated five-second frontend test
   timeouts under host load. The targeted frontend suite immediately passed,
   and a fresh full aggregate rerun with new clean/upgrade databases passed
   end to end.
9. The first image build exposed that the Docker build stage invoked the V1C
   canary-configuration generator without copying its source. The exact source
   file is now copied into the build stage; rebuild and reproducibility pass.
10. Image-backed Compose smoke correctly rejected the truthful dirty image
    with `a11_startup_recovery_build_invalid`. The API, worker, recorder,
    migration, PostgreSQL, and observability services otherwise started. No
    provenance flag was falsified; a clean smoke remains required after the
    exact candidate is committed.
11. The first offline SPDX command mounted Trivy's analysis cache read-only.
    Trivy refused to initialize its cache. The rerun kept the image export,
    network, and Docker socket isolated while allowing only the local analysis
    cache to be writable; SPDX generation and the full scanner gate passed.
12. The first live Binance default-off run restarted after repeated recovery
    cycles. Diagnosis showed that recovery fetched the same 221-weight account
    snapshot twice and that clock refresh treated transient uncertainty as
    terminal. Recovery now persists and reconciles one authoritative snapshot,
    reserves documented endpoint weight, retries clock samples within a fixed
    bound, and signs with a conservative timestamp. The live engine then
    remained in `READY_PAUSED` for five minutes without an order or restart.
13. A C4 aggregate attempt retained one unrelated collector-cancellation
    failure. The targeted race-enabled test passed 20 consecutive runs and a
    fresh complete C4 qualification passed.
14. The final aggregate initially rejected oversized functions and then the
    combined Binance private-stream source file. Helpers were split without
    weakening policy, the new authenticated file was added to every exact
    exchange/security allowlist, and the complete repository `make verify`
    subsequently passed.
15. The first operator Binance prepare attempt exposed a restricted-role grant
    gap. The outbox authorization query joins `sessions` and `users`, but the
    two engine principals could not read either table. Both now receive only
    SELECT on those identity records; write privileges remain absent. The
    current image migrator applied the closed matrix, and the canary graph
    reached healthy `READY_PAUSED` with zero restarts.
16. Immediate Binance engine handoff also exposed a transient signed-account
    rejection. A separate read-only signed account check confirmed the key
    remained valid, Spot-only, and trade-enabled. The client now identifies
    Binance timestamp code `-1021`, invalidates the clock, and re-signs exactly
    once for a read-only request. Order-changing routes remain non-retried.
    Numeric status/code diagnostics omit the response message and payload.
17. Docker Desktop/WSL rejected host-side `chgrp 70` for the short-lived
    Binance canary request. The local helper now provisions that one file
    through a networkless, capability-dropped container running the pinned
    application group, verifies container-side mode `0640`, and removes the
    request after use. The committed runbook documents the remapped-GID case;
    no file mode was weakened.
18. The next Binance prepare reached high-risk authorization after successful
    password login and TOTP validation, then PostgreSQL rejected
    `SELECT ... FOR UPDATE` on the immutable high-risk audit table. The
    transaction had already acquired the dedicated advisory lock, so the row
    lock was redundant. It was removed; concurrent-chain serialization remains
    covered, the actual runtime role can execute the advisory-lock/read path,
    and UPDATE/DELETE remain absent.
19. Exact-image Bybit canary-graph holds exposed two independent eligibility
    mismatches before any order. The BTCUSDT observation also required a fresh
    ETHBTC book; a live response was valid but 2.071 seconds old. The
    instrument-scoped observation now validates only its declared BTCUSDT
    book. Separately, the 30-second clock refresh could exhaust three proxy
    samples above the unchanged 250 ms uncertainty ceiling. The bounded budget
    is now 12 samples; tests prove a late safe sample succeeds and an entirely
    unsafe budget still fails closed.
20. A later Binance prepare passed password/TOTP reauthentication and
    persisted the audited 15-minute arm, then failed closed with
    `v1c_arm_not_active` before plan creation. Go returned the original
    nanosecond-resolution arm while PostgreSQL stored timestamps at microsecond
    precision, so the strict immutable-reference comparison correctly rejected
    the mismatch. `CreateSandboxArm` now normalizes both timestamps after
    validating the command and before insert, audit, and return. Unit and
    clean-install PostgreSQL 18 regressions prove a sub-microsecond input
    returns the exact stored timestamps without changing the arm lifetime or
    weakening strict equality.
21. The following Binance prepare reached plan-reference validation, then
    failed closed with `v1c_account_epoch_rejected` before plan creation.
    The reference query requested `FOR SHARE` on both the mutable
    exchange-account row and immutable session membership. PostgreSQL requires
    mutation privilege for that membership row lock, while the runtime role
    intentionally receives only SELECT. The query now retains `FOR SHARE` on
    the mutable account only. A SQL-shape regression verifies the lock target
    and runtime grant boundary, and a clean-install PostgreSQL 18 integration
    test executes the exact query as restricted role `axiom_app` after
    explicitly withholding membership UPDATE. The failed run left zero plans,
    zero outbox rows, and zero authenticated Binance order submissions.
22. The next Binance helper run printed `prepare succeeded`, but durable
    evidence proved it was not a real canary: the outbox had one local attempt
    and zero authenticated `POST /api/v3/order` records. A transient preflight
    read failed before network I/O and was conservatively marked `UNKNOWN`.
    The old helper accepted successful durable query/cancel/reconcile commands
    without proving that cancel-or-fill became terminal. Pre-network read
    failures now reduce as local rejections, prepare requires exactly one
    create evidence row, and `CANCEL_OR_FILL_CONFIRMED` requires a terminal
    `CANCELED` or `FILLED` state. Clean reconciliation may close an unsent
    `UNKNOWN` only when create evidence is absent for the matching
    configuration and no fill exists. The old attempt closed as terminal
    `REJECTED`, its reservation was released, and no exchange create request
    occurred.
23. The next fresh prepare failed with
    `sandbox_canary_account_not_ready` before creating a session. The earlier
    invalid canary's order was terminal `REJECTED` and its reservation was
    released, but its successfully prepared sandbox session remained `ARMED`
    because no qualifying verification had finalized it. The prior CLI exposed
    no safe cleanup operation. The explicit `abort` phase now accepts only a
    one-attempt terminal `CANCELED`, `FILLED`, or `REJECTED` canary, revokes
    its arm, stops the session, writes no qualification evidence, and refuses
    unknown, nonterminal, or multi-attempt states. Aggregate request evidence
    for the failed retry contained zero Binance create requests. The unused
    control-phase request mount now resolves to an intentionally invalid
    non-secret placeholder; only prepare overrides it with the short-lived
    protected file.
24. The following exact-candidate prepare passed session creation, arm, risk,
    planning, and dispatch admission but failed closed before create evidence.
    Its durable rejection hash matched `book_unavailable`; the order was
    terminal `REJECTED`, the reservation was released, the session was stopped,
    and zero Binance create requests existed. Testnet
    `avgPrice.closeTime` could be slightly ahead of raw host UTC, but the
    decoder used a zero-tolerance host comparison instead of the already
    eligible bounded exchange clock. The decoder and dynamic filter gate now
    validate after the response against `local UTC + offset + uncertainty`.
    When necessary, only the existing bounded `/api/v3/time` sample budget may
    advance that proof. The adapter never refetches `avgPrice` and never
    retries an order mutation; a value beyond the final bound remains rejected
    without an arbitrary grace period or create bypass.
25. The next candidate created exactly one authenticated Binance Testnet order,
    which reached terminal `FILLED`; its plan completed, reservation was
    consumed, and session stopped. The helper then reported
    `sandbox_canary_create_evidence_invalid` because `axiom_app` lacked SELECT
    on the immutable authenticated-request evidence table. The row existed,
    but the exact count query returned permission denied. The role now receives
    SELECT only; INSERT, UPDATE, and DELETE remain absent. Clean-install
    PostgreSQL 18 executes the exact query under that restricted role. A
    closed recovery phase completes only read/query/reconciliation evidence for
    one terminal `CANCELED` or `FILLED` attempt with exactly one create row and
    a valid evidence prefix; it cannot submit or cancel an order.
26. The first recovery command omitted the explicit Binance canary-graph
    selection and therefore loaded the built-in default-off graph. The
    coordinator correctly rejected the configuration mismatch before queuing
    an engine command. Every documented prepare, recover, verify, and abort
    command now supplies the exact exchange-specific graph.
27. Recovery under the correct graph queried and normalized the already-filled
    order, then failed while replaying its already-reduced canonical event. The
    inbox identity verifier compared `received_at`, even though a safe REST
    retry necessarily has a later local arrival time. It now retains the first
    arrival time and compares every immutable source hash, canonical payload,
    order/client identity, event kind, and exchange occurrence time. A
    deliberately conflicting source hash still fails closed. Fresh PostgreSQL
    18 clean-install and exact B8-upgrade regressions pass.
28. Controlled-restart verification completed its database checks but could
    not create the local evidence file because Docker had auto-created the bind
    directory as container-namespace root. Only
    `.local/v1c-pr2-canaries` was provisioned as `10001:70` mode `0750`.
    Verification then atomically sealed one `0440` Binance evidence file; no
    existing file was overwritten.
29. Post-seal shutdown of the first shared terminal-replay image exposed that
    a canceled or transient one-second eligibility poll terminated the engine
    and allowed Compose to restart it. Eligibility loss now persists
    `DEGRADED`, removes readiness, pauses dispatch/recovery, and remains live
    until health returns. Context cancellation no longer becomes an
    eligibility failure.
30. A real Bybit proxy cut against that first lifecycle correction exposed an
    independent private-stream supervisor exit and restart loop. Private
    sources now expose an explicit reconnect operation that closes stale
    sockets, authenticates/subscribes, and completes deterministic REST
    backfill before reporting healthy. Runtime eligibility, private-stream,
    dispatch/recovery, and reconciliation faults all degrade fail closed.
    Recovery requires every boundary plus a fresh reconciliation. Live Bybit
    and Binance proxy cuts on the final image proved the same container
    recovered with restart count zero; controlled shutdown then exited zero.

No evidence file contains credentials, signatures, TOTP/session material,
private payloads, prices, quantities, or raw account exports.
