# Axiom implementation status

## Same-exchange triangular as-of coherence — 2026-08-13

**Status:** Implemented and locally validated on
`feature/triangular-asof-coherence`. No deployment, campaign, qualification,
or certification is implied.

This slice replaces corrected-clock interval overlap only for Binance and
Bybit same-exchange triangular shadow evaluation. ETH/BTC book changes trigger
an in-process capture of the latest already-committed BTC/USDT, ETH/USDT, and
ETH/BTC books. Every member must be healthy, in the active generation, ordered
at or before the decision boundary, and no older than the reviewed 100 ms
strategy limit. Missing, stale, post-trigger, gap, reconnect, or excessive
clock-uncertainty evidence fails closed and skips the opportunity. The generic
cross-exchange coherent-view policy remains unchanged.

This remains production-public shadow simulation only. It adds no credentials,
private endpoint, production broker, or real-money order capability. The
five-minute regional timing probes motivate the slice but do not qualify it.
Focused runtime, configuration, triangular-strategy, and bootstrap tests, plus
the repository-wide Go test suite, pass with Go 1.26.5. The separately
authorized long-running campaign remains pending and is required before any
qualification claim.

## Owner-console response and PR corrective slice — 2026-08-10

**Status:** Implemented; hosted rerun pending after PostgreSQL qualification
fixture correction.

This slice aligns the browser's fail-closed response validation with the
published server-approved exchange-sandbox run contract, adds regression
coverage for that response, repairs semantic-name drift in CI Make target
invocations, and updates the vulnerable transitive `nanoid` resolution. It
also refines the shared non-happy-state presentation without changing any
authoritative state, execution path, qualification verdict, or certification
status.

Frontend type checking, full lint, all 44 frontend tests, production build,
the focused Chromium owner workflow, formatting, workflow YAML parsing, and a
live moderate-severity dependency audit pass. The repaired CI targets resolve
in Make dry runs. The local five-project browser matrix passed Chromium
desktop/tablet/mobile and Firefox; WebKit could not launch because this host is
missing its system libraries, which the hosted workflow installs explicitly.
Go-backed contract and backend checks were not rerun because the exact Go
toolchain is not installed in this checkout environment.

The first hosted rerun passed the browser, dependency, secret-scan, operational
readiness, and process-smoke jobs, then exposed two stale PostgreSQL
qualification fixtures. Clean-install fixtures still attempted to grant the
historical `user_roles` authority after the singleton-owner migration, and the
operational-evidence/readiness upgrade fixtures applied only migrations
`000026`/`000027` before asserting semantic names introduced by `000054`. The
local correction creates the singleton `owner_accounts` authority and applies
the complete current migration suffix from each declared upgrade baseline.
Exact Go/PostgreSQL validation remains pending the hosted rerun.

## Complete owner console program

**Status:** In progress — implementation branch only; no formal qualification
or certification is implied.

**Branch:** `product/complete-owner-console` (local, intentionally does not
track `origin/main`)

**Starting SHA:** `a1bb6a89931cb6fcdd89dd842a4d16fe63cd18aa`

This program replaces delivery-stage terminology and the multi-role console
with a semantic, exactly-one-owner product model. It will add a server-validated
unified run catalogue, strategy registry, deterministic guided demonstrations,
and owner-facing workflows for historical, replay, public-data shadow, and
explicitly armed Testnet/Demo sessions. It preserves every existing safety
boundary: production-private orders remain structurally impossible; sandbox
orders remain spot-only, capped, reconciled, and require a short-lived owner
arm; and rebalancing remains advisory only.

Implementation, local validation, review, merge, formal qualification, and
release certification are reported separately. In particular, this program
does not start, replace, or certify B2, C6, D5, or D6 evidence.

### Implemented owner-console slices

- One active owner is enforced by the current authentication path and a
  fail-closed database migration; legacy authorization evidence remains
  historical only. Runtime login, session, and principal records no longer
  carry role, permission, or role-revision claims; the generated authentication
  query surface no longer joins historical authorization tables. A forward-only
  migration rejects every mutation of those historical records and rebinds the
  durable research-promotion check to the singleton owner and its recent
  session. The current resource allowlist has no user projection, compiled
  role-change mutation path, or runtime write/delete grant for historical
  authorization tables; those records remain available only as immutable
  migration/evidence history.
- The server supplies a semantic strategy/run catalogue and a unified run
  history over durable backtest, replay, public-data shadow, and automatic
  exchange-sandbox records. Trend Following, Mean Reversion, Triangular
  Arbitrage, and Cross-Exchange Arbitrage share the credential-free durable
  backtest/replay worker and the armed sandbox catalogue. Trend Following and
  Mean Reversion have installed server-resolved live-shadow workers for exact
  Binance or Bybit public-data instrument selections. Triangular Arbitrage has
  an installed exact three-book public-shadow worker on the selected venue,
  and Cross-Exchange Arbitrage has an installed exact paired Binance/Bybit
  public-shadow worker. All four remain virtual, simulation-only runtimes with
  no production-private client or order route.
- New recorded-data and installed public-shadow runs are created directly from
  the semantic catalogue. The browser sends only the reviewed strategy,
  version, mode, venue set, instrument, and `latest-qualified-inputs` preset;
  the server resolves immutable configuration, portfolio, dataset, model, and
  generation identities. The former direct-entry backtest, replay, and shadow
  screens now retain existing-job inspection only and hand new work to this
  reviewed path. Comparison controls likewise select from server-listed
  durable records rather than accepting a pasted identifier.
- The New Run page is a progressive workflow: choose purpose, then a
  compatible strategy, then a server-reviewed venue/instrument combination,
  and finally review the execution boundary and server-resolved assumptions
  before starting. Guided proof routes to deterministic demonstrations. An
  exchange-sandbox choice creates only a prepared, non-executable session and
  sends the owner to the existing password/TOTP reauthentication, short-lived
  arm, start, observation, and stop workflow.
- Triangular Arbitrage now has a canonical recorded-decision input that
  rebuilds its exact three recorded book snapshots, publication evidence, and
  exchange filters entirely in process before invoking the existing evaluator.
  JSON round-trip coverage proves it preserves candidate identity and rejects
  missing snapshot identity or a mismatched replay envelope. It now supports
  the complete deterministic in-process saga pipeline and is materialized by
  the credential-free durable backtest/replay worker. It cannot submit
  anything.
- Cross-Exchange Arbitrage now has the corresponding canonical recorded input:
  two executable snapshots with filters and publication evidence, the exact
  coherent-view identity/policy/member vector, venue inventory, fee balances,
  and recovery economics. Replaying the JSON input restores the coherent view
  before the evaluator verifies every reconstructed book member; a changed
  book is rejected rather than treated as equivalent market evidence. It now
  also exhaustively evaluates viable directions, atomically claims the exact
  two-venue balance, fee, liquidity, and recovery resources for one
  deterministic candidate, and passes that allocation through central risk
  using immutable canonical-input policy and observation evidence. Invalid
  evidence, unavailable capacity, and central-risk blocks all fail closed.
  The input-bound claim set derives its capacity from recorded two-venue
  ownership, fee buffers, recovery allowance, and full-depth book sides. The
  credential-free worker reconstructs this input-scoped virtual ownership for
  each immutable event; because no external order can occur between allocation
  and reduction, restart replays the event rather than retaining a partial
  external claim.
  Its public-shadow worker captures one coherent production-public
  Binance/Bybit book pair, initializes separate immutable virtual venue
  accounts under a versioned single-instrument prefunding model, and applies
  only exact both-filled virtual reductions. It persists the two venue balance
  projections, accounting, reconciliation, and multi-leg execution evidence
  atomically; uncertain recovery that lacks exact unwind fills fails closed.
  Before claiming, the allocator recomputes the complete candidate set from
  the canonical input, so an altered intermediate candidate payload cannot
  change economics or resource ownership.
  Central-risk approval materializes a deterministic concurrent virtual execution saga, which the
  shared broker executes only through the existing recorded-input simulator.
  Canonical decision records can carry the exact future venue books, simulated
  response directives, reviewed latency distribution, and recovery policy used
  by that simulator; they also carry independently captured attribution and
  expected-versus-observed reconciliation projections. Missing response or
  reduction evidence fails rather than being invented.
  Its reducer appends the existing independent accounting-attribution facts,
  requires a mode-specific reconciliation comparison, then releases the
  atomic allocation only when reconciliation is clean; a critical mismatch is
  quarantined. An end-to-end shared saga-pipeline test proves those stages run
  in their required order using only canonical market, risk, simulation,
  attribution, and reconciliation evidence. It is available for durable
  credential-free backtest/replay and now also has a fenced automatic sandbox
  path. The Binance coordinator evaluates one coherent Binance/Bybit public
  generation, while each credential-owning engine can claim and submit only
  its own durable account leg. Missing peer facts, risk, inventory, leases, or
  synchronized books produce explicit waiting or blocked evidence.
- The generic multi-leg saga pipeline now provides the same mandatory stage
  order for Triangular and Cross-Exchange runtimes: strategy evaluation,
  atomic allocation, central risk, plan, simulation or sandbox broker, then
  reduction of execution, accounting, and reconciliation evidence. Its cleanup
  behavior releases an allocation on rejected risk/planning and quarantines it
  after uncertain execution/reduction. Triangular and Cross-Exchange adapters
  exercise the complete sequence in end-to-end tests and are installed for
  durable credential-free backtest/replay catalogue choices and by the armed
  sandbox coordinator. It does not enable production-private order flow.
- The sandbox dispatcher and PostgreSQL store now have the durable multi-leg
  execution aggregate used by the automatic runtimes. Triangular
  plans persist three sequential legs, exact per-market eligibility evidence,
  dependencies, and a maximum 250-millisecond candidate lifetime; paired
  cross-exchange plans persist two concurrent legs with separate account and
  venue fences. A later triangular leg's reservation is `WAITING`, not owned
  inventory. Only the same transaction that reduces an authoritative full
  fill and posts its accounting facts may activate the next reservation, and
  only when the exact net output asset and quantity cover that reservation.
  Arm or candidate expiry quarantines every dependent claim for recovery;
  clean reconciliation of a zero-fill predecessor releases and closes unsent
  dependents. Deterministic model tests and the clean-install PostgreSQL
  qualification test cover exact-once progression, expiry, dependency state,
  reservation consumption, and final disposition. The engine now installs the
  synchronized public-input readers, exact saga fact reader, central-risk
  projector, concrete plan builders, executor router, and per-account fenced
  dispatcher path for both strategies. This remains local implementation
  evidence, not an external Testnet/Demo qualification.
- Triangular now supplies the complete concrete saga sequence. It decodes the
  canonical replay input, exhaustively evaluates every eligible exact cycle
  and size, ranks candidates by worst-case then expected return and immutable
  ID, and atomically claims the selected candidate's real shared balance, fee,
  liquidity, and recovery resources. A resource conflict may select the next
  fully claimable candidate; missing capacity creates no partial hold. That
  capacity is initialized from recorded balances, fee buffers, recovery
  allowance, and full-depth book sides rather than from candidate output.
  Its claim set is intentionally input-scoped. The credential-free worker
  reconstructs it for each immutable event and does not persist an external
  order or claim between allocation and reduction.
  Before claiming, the allocator recomputes the complete candidate set from
  the canonical input, so an altered intermediate candidate payload cannot
  change economics or resource ownership.
  exact allocation is then passed to the existing central risk engine using
  immutable canonical-input policy and observation evidence; malformed or
  unavailable evidence fails closed. Central-risk approval materializes a deterministic
  sequential virtual execution saga with validated reservation identity. The
  shared broker executes that plan only through the existing recorded-input
  simulator. Canonical decision records can carry the exact future-book
  timeline and reviewed latency model used by that simulator, rather than
  allowing a newer live book to substitute, plus the immutable
  expected-versus-observed reconciliation projections needed for reduction.
  Its reducer appends the existing exact cycle journal,
  requires a mode-specific reconciliation comparison, then releases the
  allocation only when reconciliation is clean; a critical mismatch is
  quarantined. An end-to-end shared saga-pipeline test proves those stages run
  in their required order using only canonical market, risk, simulation, and
  reconciliation evidence. The credential-free worker materializes this path
  for backtest/replay, while the armed sandbox runtime converts the accepted
  material into the same durable sequential plan. Only the existing
  credential-owning Testnet/Demo dispatcher can submit those capped spot legs.
- Each listed run has one semantic detail route with immutable timeline,
  decision, planned-order, simulated-execution, latest portfolio, risk
  availability, and safe reproducibility-evidence projections. A missing
  snapshot or run-scoped risk record is presented as `not_recorded`; the
  product never substitutes current global state or fabricates evidence.
- Run detail now separates the owner workflow into accessible Overview,
  Timeline, Decisions, Orders & Fills, Portfolio & P&L, Risk, Data & Models,
  and Evidence tabs. Every tab reads the same durable projection family; an
  absent run-scoped record is explained instead of being backfilled from a
  global snapshot.
- The run detail page exposes only lifecycle controls that the current durable
  run can accept: replay pause, resume, and one-event step, plus safe public
  shadow or exchange-sandbox stop. The server supplies the allowed actions in
  every run projection, and each uses the existing revision-checked, audited
  command path; unsupported run types expose no control.
- The protected Data Catalogue lists registered immutable manifests, exchange
  and instrument scope, recorded segment types, coverage, gaps, quality tier,
  and hashes. It accepts no browser upload or raw storage path; a missing
  immutable field is shown as not recorded rather than inferred.
- The persistent safety header independently reports Binance and Bybit
  production-public data state from the durable generic exchange projection.
  An absent or stale recorder fact remains unavailable or attention-required;
  this display does not imply that either exchange participates in an active
  run or that sandbox execution is enabled. It also shows the active critical
  alert count; the shell identifies the single signed-in account as Owner,
  rather than presenting a delivery-era role label.
- Owner pages built on the shared page shell now have a reusable “About this
  page” help control. It opens on hover, keyboard focus, and click/tap, and
  states that the view is a server-authoritative projection; empty, stale, or
  blocked sections must explain the next safe action, and operational evidence
  is not proof of profitability. The shared metric card now uses the same
  control to explain its server-authoritative value and evidence limitation.
- The remaining frontend role helper and its role-based display gates were
  removed. Once the authenticated owner session is ready, normal console
  controls and evidence affordances are not hidden behind client permission
  labels; server-side high-risk reauthentication, revision, and safety checks
  still govern each command.
- The sandbox qualification API now uses semantic public contract names for
  sandbox status, chaos, and service-level objectives. Its retained evidence
  window and endpoint are unchanged.
- Deterministic, synthetic Trend Following and Mean Reversion walkthroughs now
  exercise the same strategy, allocation, risk, planning, virtual-execution,
  portfolio, and journal pipelines as their credential-free offline runtimes.
  The protected owner catalogue also includes an Inventory Rebalancing advisory
  walkthrough that uses the reviewed route optimizer to show a natural reversal
  and stale-fact rejection. Triangular Arbitrage and Cross-Exchange Arbitrage
  walkthroughs now use their canonical multi-leg pipelines: exact recorded
  input, atomic allocation, central risk, plan, deterministic virtual fills,
  accounting, and reconciliation, alongside insufficient-fee and
  uneconomic-restoration rejections. They can never create a transfer or an
  external exchange order; their orders and fills are synthetic simulation
  evidence only. The result view exposes every installed scenario in a guided
  all-strategies tour and shows
  canonical evidence. It does not create a durable run, contact an exchange,
  or establish historical-performance or profitability evidence.

Remaining implementation includes richer run-scoped sandbox timeline and
risk/P&L projections, credentialed external Testnet/Demo validation of all
installed automatic runtimes, the remaining historical terminology cleanup,
and full browser acceptance of the final owner workflow. Local deterministic
tests and the clean PostgreSQL qualification are implementation evidence only;
they are not external sandbox qualification, profitability evidence, or a
formal release gate.

The sandbox domain now has a closed strategy-session lifecycle contract for
automatic spot strategies. It requires a live owner arm that covers every
account, rejects expired or revoked arms, preserves an unconditional stop path,
and rejects Inventory Rebalancing because it remains advisory-only. The durable
creator atomically resolves only fresh, reconciled, leased, correctly
environment-scoped account epochs, then writes the armable parent and immutable
strategy-account topology together; single-venue and paired Binance/Bybit
topologies are covered by the dedicated PostgreSQL qualification path. The
owner-facing start/stop commands are persisted separately and never contact an
exchange. Each credential-owning engine blocks a running strategy session when
its exact arm expires or is revoked, without interrupting cancellation,
reconciliation, or risk-reducing recovery. The owner can prepare a
server-resolved strategy session from reviewed semantic choices; the API binds
the active immutable configuration and derives its strategy-set identity, so
the browser supplies no database IDs, configuration hash, or model identity.
Preparing a session cannot arm an account or create an order. The authenticated
engine now installs the automatic Trend Following, Mean Reversion, Triangular,
and Cross-Exchange router only when all four compiled
integration/submission switches are enabled.
The engine-side scheduler exposes only the current
engine lease holder's running, unrevoked-arm session snapshot for one exact
account epoch, reloads the exact immutable configuration before evaluation,
and accepts only bounded semantic waiting/blocked reasons. Every scheduler
outcome has an immutable, session-scoped PostgreSQL timeline record whose write
rechecks the engine fence, arm, and revisions; it has no adapter or submission
authority. It evaluates only after a successful periodic reconciliation and a
synchronous instrument-eligibility refresh; ordinary one-second dispatch ticks
cannot create a new decision. A durable finalized-candle trigger restored from
the immutable decision journal prevents repeated Trend 4-hour or Mean Reversion
1-hour-plus-4-hour evaluation after a restart. A one-leg plan builder also requires both a fresh account
snapshot and strategy-session-owned base inventory derived only from immutable
strategy-plan fills; an unrelated exchange-account balance cannot authorize an
exit. Fresh allocation, risk, arm, inventory, and dispatcher admission remain
mandatory before any future entry.
The shared automatic-strategy bridge now composes strategy evaluation,
exclusive allocation, central risk, execution planning, and atomic durable
sandbox-plan approval before the existing outbox dispatcher can submit a
request. Its one-leg builder binds the exact arm, account epoch, instrument
eligibility, entry-safety facts, reservation, and approval hashes. Separate
saga builders bind every account snapshot and market-eligibility member for
reviewed sequential Triangular and paired concurrent Cross-Exchange plans.
All planners form only deterministic Testnet/Demo identities; they retain no
adapter or submission authority.
Deterministic full-path tests now prove an armed Trend evaluation on the
Binance Testnet shape and an armed Mean Reversion evaluation on the Bybit Demo
shape wait for the first durable risk baseline, then pass through the real
strategy, allocator, central-risk, planner, IOC plan, and fixed 10/50/1/2
approval boundary. Separate fenced-dispatch tests claim those automatic
strategy submissions once and send them through the authenticated in-memory
Binance and Bybit emulators, retaining only redacted fixed-host request-shape
captures. They prove Testnet/Demo Spot IOC routing, including Bybit's explicit
unleveraged Spot fields, without credentials or network I/O. The same fenced
dispatcher tests cancel the acknowledged order after the arm lifetime has
elapsed and recover the authoritative canceled state, while a second outbox
claim cannot duplicate the entry. Trend and Mean
Reversion entry sizing now both reserve entry fees inside every supplied
notional ceiling, preventing a strategy from accepting an almost-10-dollar
candidate that the shared allocator must reject after adding its fee.
Multi-leg sessions use a separate router, synchronized-book cache, saga fact
reader, and exact risk/plan factory rather than weakening their atomic
reservation and recovery requirements. A credential-free one-leg readiness
executor now validates the selected immutable Trend or Mean Reversion rule
graph and requires the full 1,000 finalized candles for its required
timeframe(s), fresh bounded public book, and immutable metadata. It remains a
credential-free readiness fallback: short candle history, stale books, partial
candles, or substituted metadata result in `waiting_for_public_market_data`.
Multi-leg waits name the missing coordinator, facts, synchronized books, risk,
capital, or pipeline stage. The installed
decision executor supplies the durable session-owned position and sizing
projections required for a one-leg evaluation. It receives only credential-free
public market data and durable projections; it cannot call an exchange adapter
or submit an order directly.
For an automatic one-leg plan, the shared pipeline now retains the exact
canonical strategy input and accepted Trend or Mean Reversion decision in
addition to the opaque allocator candidate. The plan approval hash commits to
both evidence hashes, and PostgreSQL persists those unchanged bytes in the
same serializable plan-approval transaction. The immutable strategy-decision
journal additionally admits complete no-signal evaluations under the current
engine fence, so later projection can reproduce trailing stops, cooldowns, and
holding duration rather than infer them from account balances. Accepted
decisions are linked to their plan in the same transaction; no journal row can
be patched later to add a plan reference. The journal reader fails closed if
it encounters an older accepted-plan record without its corresponding journal
entry. Only a durable automatic strategy session may require these records;
older manual strategy plans remain compatible. A pure position projector now
replays the immutable journal only when each stored input matches the prior
derived state and each stored decision exactly reproduces from the configured
Trend or Mean Reversion evaluator. It carries actual partial-fill quantity,
weighted entry price, trailing stops, holding duration, and cooldowns forward;
an entry while a position remains open, an oversell, or any journal mismatch
fails closed. A pure input builder now combines that projected state only with
fresh credential-free public input and explicit, immutable-configuration-bound
account/policy facts; it rejects stale account snapshots, malformed or crossed
books, oversized order ceilings, and substituted configuration facts. The
builder is installed in the authenticated engine's automatic one-leg worker.
A concrete sizing reader now derives those facts only from the latest immutable
account snapshot, a durable risk-facts projection bound to that same snapshot,
the current full admission, the session decision journal, the bound V1C
configuration, and the exact scheduler fence. It does not manufacture a risk
policy identity, policy hash, or reserve from configuration; missing, stale,
cross-account, or snapshot-mismatched risk facts leave the session waiting.
The reader now loads the complete immutable policy limit set and also requires
the latest durable global risk posture to be `NORMAL`; a merely normal-looking
policy cannot bypass a paused or locked engine.
It resolves the current matching approval version for both the base and quote
assets from durable screening records; a missing, unapproved, or divergent
version leaves the session waiting rather than substituting an engine startup
cycle. It cannot fall back to account-wide inferred inventory or a stale
snapshot. A narrow admission adapter similarly exposes only the store's evaluated admission result
to the executor, not its switch inputs or any exchange capability.
A complete strategy-risk-observation contract now separately binds drawdown,
loss, exposure, reserve, spread, timing, quality, and every health breaker to
that same account snapshot and public instrument. It intentionally has no
"healthy by default" representation: a durable source must provide every
value before the central risk engine can be composed. An immutable Postgres
writer and exact-session reader now persist and reload the complete input under
the current engine fence, arm, snapshot, market, policy, and strategy revision.
The installed authoritative projector derives exact account/strategy value,
loss, exposure, reserve, committed notional, slippage, and accounting state from
the immutable journal and fresh account, reconciliation, storage, engine, and
market evidence. Its first observation records a durable baseline and waits;
the next exact valuation may create the complete central-risk observation.
A one-leg decision executor now composes the immutable configuration, fresh
public input, current admission, projected position, explicit sizing facts,
session-owned sell inventory, and the shared allocation/risk/plan pipeline.
It receives only the scheduler's account/epoch/owner/fence lease, never an
exchange adapter. The admitted one-leg pipeline constructor additionally binds
the exact admission, snapshot, and strategy-owned inventory to the shared
allocator, risk, planner, and durable-plan store; it accepts no broker or
credential-bearing dependency. Its dependency source receives the exact public
market snapshot and sizing facts that produced the candidate, together with the
admission and session-owned inventory; it cannot substitute later liquidity or
infer account capacity. The executor performs the final fact-bound composition
and applies the compiled 10 USDT order, 50 USDT daily, one-per-account, and
two-global caps.
The shared allocator now also accepts an explicit runtime-owned limit contract,
and its account-balance projection accepts only the exact free balances supplied
by an already-validated snapshot; it cannot silently turn externally reserved
funds into strategy capacity. A concrete dependency factory now builds the
snapshot-owned portfolio, approved asset registry, conservative displayed-depth
claim, allocator, immutable risk input provider, central-risk adapter, and
strategy planner before evaluation. It subtracts already-reserved settlement
capital, limits base inventory to the exact session-owned amount, and refuses a
non-`NORMAL` durable risk engine. Each evaluation reloads the current global
risk posture from PostgreSQL. Automatic escalations serialize against the same
revision used by owner commands, append immutable transition evidence, and emit
a bounded sanitized alert; a stale competing engine cannot overwrite a newer
posture.
If a plan is not approved, it appends the complete decision
under that fence; if a plan is approved, the existing plan transaction remains
the sole owner of its journal row. Approved plans enter the existing fenced
outbox; only that engine's existing dispatcher owns adapter submission.
A session-owned decision with a durable plan can now be read only after its
single outbox leg is terminal and its latest immutable cumulative fill snapshot
is valid for that exact journal entry. Pending, unknown, recovery, foreign,
or mismatched plan evidence fails closed. Partial fills retain their exact
quantity and weighted actual price; they are never replaced with a requested
limit, rounded to a full fill, or inferred from an account-wide balance.
Before any future runtime construction, the engine reloads the exact canonical
configuration recorded by the prepared session and proves its hash, child and
parent revisions, arm revision, and matching Testnet/Demo mode. A one-leg sell
will additionally bind the fresh authoritative account-snapshot hash that
proved the base inventory was available; decoding is rejected when the arm is
no longer current. Strategy-plan approval also verifies that
the referenced snapshot is an immutable record for the exact account epoch and
persists that reference with the durable plan, rather than trusting only a
hash supplied by in-process planning.
A one-leg sell also requires a fresh authenticated account snapshot showing
sufficient owned base inventory; stale, missing, or insufficient inventory
fails closed.
The Binance Testnet public-data prerequisite is now a separate credential-free
client with compiled Testnet REST and WebSocket hosts; it cannot reach account
or order routes. The Testnet engine uses it for startup eligibility and the
installed one-leg strategy worker, while the credential-bearing client remains
responsible only for its account, clock, and order boundary.
Bybit Demo follows the same split: the Demo client verifies its account clock,
while the separate credential-free Bybit public client supplies every startup
book used for eligibility. A malformed or crossed public book fails closed;
the Demo private boundary is not used as a substitute for public market data.
The engine persists a separate fresh, fenced readiness observation for each
supported instrument. Session and canary admission select that exact record,
so BTCUSDT readiness cannot make an ETHUSDT session appear eligible.
The clean-install database qualification covers both fence rejection and the
immediate disappearance of that work after arm revocation. Sandbox Operations
now projects each durable session by strategy, Testnet/Demo venue, recorded
instrument, state, and a
plain-English waiting or blocking reason. For a running session, that reason
comes from the latest immutable scheduler evaluation for each selected account,
so paired Binance/Bybit sessions do not hide one venue's actual wait. It never
guesses an instrument for pre-existing rows that did not record one.
The same clean PostgreSQL 18 qualification now covers migrations through
`000046`, atomic fill accounting and projections, baseline/evaluated risk
valuations, durable central-risk restore/escalation, multi-leg decision
evidence, sequential reservation progression, and exact three-market
Triangular accounting. The latter transfers USDT cost basis through BTC and ETH
and fails central risk closed while a cross-asset lot or unresolved fee remains.
This is local integration evidence only; it is not external Testnet/Demo
evidence, formal C6 qualification, release certification, or profitability
evidence.

## V1D D6 repository certification

The `v1d-d6-certification` worktree adds cumulative exact-identity evidence,
independent-review, Section 35, safety-manifest, and signed no-replace release
verdict enforcement. Missing, expired, failed, dirty, wrong-SHA, mutable,
unsigned, tampered, duplicate, or high-severity-open evidence fails closed. The
final command remains default-off.

Repository implementation and all available dirty-worktree non-soak gates are
locally verified. A clean exact-SHA integrated workflow and hosted CI remain
pending because the D6 content is not committed. Final V1 certification is
also blocked by B2 and C6 72-hour verdicts, D5's seven-day verdict, earlier open
owner/security/cumulative acceptances, a declared-server restore, current
exact-candidate artifacts, all seven independent reviews, and signed formal
Section 35 evidence. State: **NOT CERTIFIED**.

## V1D D5 operational hardening

D5 implementation is on `main` at
`93dc3edf74ead553af75a589cd50eeb4735f2db5`. Storage-pressure fail-closed
automation, lifecycle/holds, independent encrypted backup and recovery proof,
hardened deployment, deterministic faults, and a default-off seven-day runner
exist. The formal seven-day run was not started. The D6 worktree repairs the
backup-image supply-chain failure from hosted run `30904448621`; new hosted D6
evidence is still pending.

## V1D D3-D4 application completion

D3 merged through PR #35 and supplies functional backtest, replay, and shadow
labs with immutable inputs, reproduction/comparison, controls, redacted export,
and responsive browser coverage. D4 merged through PR #36 and supplies reports,
incidents, alerts, tamper-evident audit review, evidence holds, and operational
bundles. Both are implemented; current cumulative formal acceptance remains
separate.

## V1D D2 React command center

D2 merged through PR #34. It implements the six-group role-aware product
navigation, persistent safety header, Decisions & Orders and restricted System
Events, Strategy Center, Qualification Center, approved Run Lab, scoped risk
controls, and the required operational resource screens over the merged D1
control-plane API. High-risk actions retain exact-revision password/TOTP
reauthorization, and the UI adds no arbitrary command or production-private
execution surface.

Strict TypeScript, lint, unit/axe, production build, generated-contract parity,
D2 boundary, file policy, secret, prohibited-capability, and five-project
browser gates are implemented. Formal earlier-gate acceptance and C6's separate
72-hour qualification remain pending. See
`docs/releases/v1d-d2-readiness.md`.

## V1D D1 control plane

D1 is merged into `main` at merge commit `4cf0b14`. It provides the compatible
OpenAPI contracts, durable commands, read projections, reason catalogue,
exports, qualifications, roles, and permission-filtered stream extensions
consumed by D2. D1 merge is implementation state, not cumulative V1D safety
acceptance.

## V1C PR3 C6 console and non-soak qualification

The `v1c-c6-console-soak` branch implements the C6 redacted sandbox
operations API and React console, purpose-bound administrative controls,
bounded observability, deterministic chaos coverage, and the default-off
72-hour qualification runner/evidence contract. The implementation extends the
existing V1C account, arm, allocator/risk/planner/dispatcher, order reducer,
reconciliation, reset-incident, and audit state. It does not add an API-side
exchange client or a parallel order state machine.

Focused API, authentication, storage, React/axe, desktop/mobile C6 Playwright,
and the complete unmocked A11 browser workflow pass. The cumulative C1-C6
non-soak aggregate, PostgreSQL 18.4 clean-install and exact B8-upgrade gates,
all 1,024 Compose renders, security boundaries, and complete `make verify`
pass. The exact-source reproducible image, non-root/read-only and compiled
absence checks, image-backed Compose smoke, SPDX SBOM, and current Trivy gates
also pass for implementation commit
`b5ac868ec38d9204afc6f9fd4db6673aee10e852`. Implementation and non-soak
qualification are complete. The formal 72-hour C6 soak and owner/security
acceptance are explicitly pending and are not run as part of PR3
implementation qualification. No additional authenticated exchange order was
placed or required for this branch.

## V1C PR2 C4-C5 adapters and engines

The `v1c-c4-c5-adapters` branch extends the merged C1-C3 foundation with
complete Binance Spot Testnet and Bybit Demo authenticated adapters, separate
credential-owning engines, migration `000023`, and a controlled full-pipeline
canary/restart evidence path.

C4/C5 deterministic gates, clean PostgreSQL 18.4 install, exact B8 upgrade,
the closed security boundary, all 1,024 Compose profile renders, the complete
PR2 aggregate, and exact-source clean-image artifact gates pass. Both required
operator-armed canaries passed on the same final dirty executable. The
matching live proxy-cut tests also proved fail-closed `DEGRADED` recovery,
private-stream backfill, fresh reconciliation, zero engine restarts, and clean
exit-zero shutdown. PR2 is merged into `main` at
`8902b3b794ed344a131822b34fa8bb81cedaa35e`; formal owner/security acceptance
remains pending. See
`docs/releases/v1c-pr2-readiness.md`.

## V1C PR1 C1-C3 foundation

The `v1c-c1-c3-foundation` branch adds the default-off
`axiom.config.v1c.1` policy, neutral sandbox contracts, closed authenticated
signers and proxies, TOTP/one-use authorization and rotation foundations,
durable sandbox dispatch/recovery models, and migrations `000021`/`000022`.

C1-C3 and the aggregate PR1 local qualification passed and merged. C4/C5 are
tracked above. C6 and the V1C 72-hour soak are not implemented or claimed here.
See `docs/releases/v1c-pr1-readiness.md`.

This tracker records implemented behavior and verified evidence. A phase is marked complete only after every acceptance criterion in the authoritative specification and the V1A implementation plan has current evidence.

| Phase | Status                                                               | Current slice                                                                                                                           | Evidence                                                                                                                                                          |
| ----- | -------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A0    | Complete                                                             | Scope traceability, safety architecture, threat model, topology, lifecycle, and readiness policy                                        | `docs/releases/evidence/a0-review.md`                                                                                                                             |
| A1    | Complete                                                             | Repository, toolchain, application skeleton, Compose, and CI                                                                            | Local validation, owner-verified hosted CI/supply-chain evidence, and clean-machine setup/governance walkthrough pass                                             |
| A2    | Complete                                                             | Fixed-point finance, canonical domain types, and immutable fail-closed configuration                                                    | `docs/releases/evidence/a2-local-validation.md`; external integration remains owner-managed                                                                       |
| A3    | Complete                                                             | Deterministic runtime, bounded concurrency, and fencing                                                                                 | `docs/releases/evidence/a3-local-validation.md`; PostgreSQL durability remains A4 work                                                                            |
| A4    | Complete                                                             | PostgreSQL, journal, generated repositories, Parquet/Zstd, and recovery                                                                 | `docs/releases/evidence/a4-local-progress.md`; clean PG18 and timed restore qualification passed                                                                  |
| A5    | Complete                                                             | Redacted logs/traces, bounded metrics, authenticated health, durable alerts, rules, and dashboards                                      | `docs/releases/evidence/a5-local-progress.md`; Docker, scans, alert SLO, and tabletop qualification passed                                                        |
| A6    | Complete                                                             | Public exchange contracts, capability boundary, deterministic controls, emulator, and fixtures                                          | `docs/releases/evidence/a6-local-validation.md`; cumulative verification and binary absence gate passed                                                           |
| A7    | Accepted — owner waiver                                              | Binance public adapter, synchronized books, operational recorder, and completed 72-hour qualification                                   | `docs/releases/evidence/a7-owner-waiver-2026-07-26.md`; terminal result remains `qualified:false`; waiver is availability/resynchronization-only and time-bounded |
| A8    | Implemented and locally validated — own formal acceptance pending    | Backtesting, replay, simulation, durable orders, persistence, and local dataset qualification                                           | `docs/releases/evidence/a8-local-validation.md`; implementation is merged into `main`                                                                             |
| A9    | Implemented and locally validated — formal A8/A9 acceptance pending  | Portfolio allocation, risk, reconciliation, and recovery                                                                                | `docs/releases/evidence/a9-local-validation.md`; implementation is merged into `main`                                                                             |
| A10   | Implemented and locally validated — formal A8-A10 acceptance pending | Trend strategy, exact sizing/exits, shared simulated pipeline, immutable research governance, and reporting                             | `docs/releases/evidence/a10-local-validation.md`; implementation is merged into `main`                                                                            |
| A11   | Implemented and locally qualified — formal A8-A11 acceptance pending | Versioned API/authentication, durable worker/replay controls, production-public shadow runtime, resumable SSE, and routed React console | `docs/releases/evidence/a11-local-validation.md`; implementation is merged into `main`, and clean PostgreSQL/browser/verify/image/Compose gates passed            |

## Absolute V1A boundary

V1A application roles remain public-data research and simulation software
only. They receive no exchange credentials and expose no external order side
effects. The repository now also contains separately gated, default-off V1C
Testnet/Demo foundations. Production `live` execution and every prohibited
product or fund-management capability remain rejected.

## Current limitations

- The A1-A6 foundations and A7 production-public collector/recorder
  implementation exist. A7 is accepted under the repository owner's documented,
  time-bounded non-safety availability/resynchronization waiver; the completed
  machine result remains `qualified:false`. This acceptance does not pre-check
  or formally accept A8-A11.
- Immutable-candidate local A1 validation is recorded in
  `docs/releases/evidence/a1-local-validation.md`. Owner-verified hosted CI and
  retained supply-chain evidence for commit
  `5ce09c3611e05a8fa5d0f1afc4706e17698b2d90` are recorded in
  `docs/releases/evidence/a1-hosted-ci.md`; the completed setup/governance
  walkthrough is recorded in
  `docs/releases/evidence/a1-clean-machine-walkthrough.md`.
- A8 has local implementation evidence but its own formal acceptance remains pending.
- A9 has local implementation evidence but formal A8/A9 acceptance remains pending.
- A10 has local implementation evidence but is not formally complete because
  formal A8/A9/A10 acceptance remains pending. No final-test strategy result
  was consumed for implementation evidence.
- A11 has current local implementation evidence for clean PostgreSQL 18 setup,
  desktop/mobile fixtures, the unmocked browser workflow, full cumulative
  verification, live vulnerability lookup, exact-identity image inspection,
  and image-backed Compose smoke. This closes the local implementation gate,
  not formal phase acceptance; A11 remains blocked by formal A8/A9/A10
  acceptance.
- The clean backup/restore drill and A8-A11 formal acceptances remain open release evidence.
- V1C PR2 C4/C5 deterministic, database, aggregate, and exact-source clean
  artifact gates pass locally. Both operator-armed exchange canaries passed on
  the same final dirty executable, and both engines passed live proxy-cut
  degradation/recovery without restarting. The PR2 branch is committed and
  merged into `main`; formal owner/security acceptance remains open. PR3 C6
  implementation is tracked above, while its 72-hour qualification remains
  pending.
- V1C PR3 C6 implementation and non-soak qualification pass on exact
  implementation commit
  `b5ac868ec38d9204afc6f9fd4db6673aee10e852`, including the clean artifact
  record. The formal 72-hour soak, sealed evidence review, and owner/security
  acceptance remain open.

V1B planning and implementation status is tracked separately in
[V1B implementation status](releases/v1b-implementation-status.md). V1B work
does not change the open V1A evidence gates above.
