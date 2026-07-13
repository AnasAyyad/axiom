# V1A threat model

## Status, scope, and review claim

This is the initial A0 threat model for Axiom V1A. It defines the security
architecture, trust boundaries, required controls, failure behavior, owners,
and evidence needed before release. It does not claim that application code,
tests, or a release image currently exist.

The A0 architecture review concludes that the specified V1A component graph has
no authorized path from a strategy, user, configuration value, or credential to
an exchange order endpoint. That is a design-level proof only. The V1A release
gate remains open until source, generated contracts, configuration, database
constraints, Compose renders, release-image symbols, and captured outbound
traffic independently confirm that the implementation matches the design.

## Security objectives

V1A must preserve these properties in priority order:

1. A real, testnet, or demo exchange order cannot be formed, signed, submitted,
   canceled, queried, or reconciled.
2. Withdrawals, transfers, margin, futures, perpetuals, options, leverage,
   borrowing, lending, staking, and short selling are unavailable.
3. A missing, stale, unknown, contradictory, or unverifiable safety input fails
   closed and blocks new decisions in the affected scope.
4. Strategy decisions cannot bypass allocation, risk, simulation, accounting,
   reconciliation, ownership, or fencing boundaries.
5. Secrets and security tokens never enter source control, browser responses,
   URLs, logs, metrics, traces, audit payloads, fixtures, screenshots, support
   bundles, or market-data recordings.
6. Financial, journal, configuration, audit, and replay state remains exact,
   attributable, deterministic, and tamper-evident enough to investigate.
7. Availability degradation never converts into stale execution, silent loss,
   automatic unpause, or a broader network capability.

## Assets and impact

| Asset | Required property | High-impact failure |
|---|---|---|
| Production-order hard lock | Integrity and non-bypassability | Any external exchange-order path or production credential capability |
| Virtual portfolios, reservations, journal, and risk state | Exact integrity, ownership, durability | Double spend, negative balance, unbalanced journal, hidden exposure |
| Recorded and normalized market data | Provenance, completeness, deterministic order | Decisions from stale/corrupt/future data; irreproducible evidence |
| Configuration and safety manifest | Authenticity, immutability per run, auditability | Endpoint/mode/risk policy weakened by input |
| Operational and authentication secrets | Confidentiality, least privilege, revocability | Account/session/database compromise |
| Sessions and administrative commands | Authentication, authorization, CSRF/replay resistance | Unauthorized activation, unlock, config change, or evidence deletion |
| Audit, incidents, logs, and metrics | Integrity, availability, redaction | Undetected incident, secret exposure, misleading evidence |
| Build, dependencies, and release image | Provenance and reproducibility | Malicious code, signer, endpoint, or hidden capability introduced |
| Availability and bounded resources | Controlled degradation and recovery | Stale decisions, data gaps, lost critical state, denial of service |

Local market recordings, database dumps, secret files, and exported account or
incident data are treated as sensitive even when some source market events were
public.

## Actors and assumptions

Threat actors include an unauthenticated network attacker, an authenticated but
compromised browser session, a malicious or mistaken administrator, a process
with only one service role, a compromised dependency or build runner, a hostile
DNS/proxy/network path, malformed exchange input, and an operator responding
under pressure.

The host kernel, container runtime, PostgreSQL, TLS trust store, and CI control
plane are trusted only within their declared roles and require independent
hardening. Binance public data is authentic only to the extent established by
TLS and still remains untrusted input. Docker networks, DNS, public-source data,
and documentation are not security boundaries by themselves.

## Trust boundaries

1. **Browser to edge/API.** Untrusted HTTP, cookies, Origin, CSRF tokens,
   pagination/filter data, and administrative commands cross into the API.
2. **API to durable command/audit storage.** Authorized intent crosses into
   PostgreSQL. The API cannot call an exchange or engine broker directly.
3. **Configuration and secrets to each process.** Deployment input crosses from
   host-controlled files/environment into typed validation. Compiled safety
   policy is above and cannot be weakened by this boundary.
4. **Public exchange network to Binance adapter.** DNS, TLS, HTTP/WebSocket,
   redirects, frames, schemas, timestamps, and sequences are hostile until
   validated. Egress is restricted by `docs/configuration/endpoint-policy.md`.
5. **Adapter to ordered market-data state.** Parsed exchange events cross into
   per-instrument queues and books only after schema, symbol, generation,
   sequence, time, size, and quality checks.
6. **Strategy to allocator/risk/simulator/journal.** Candidates cross explicit
   in-process interfaces. Strategy output is not an order and grants no balance
   ownership.
7. **Processes to PostgreSQL.** API, engine, recorder, worker, migrator,
   monitoring, and reporting use separate least-privilege roles. Durable
   commands, outbox events, leases, and fencing tokens cross this boundary.
8. **Processes to writable files/backups.** Parquet segments, manifests, logs,
   exports, and backups cross into separately permissioned storage with hashes,
   atomic finalization, retention, and restore validation.
9. **Application to observability/notification sinks.** Bounded, redacted
   structured events leave the process. Arbitrary URLs and payload reflection
   are forbidden.
10. **Source/dependency inputs to release image.** Third-party code, generated
    artifacts, builders, registries, and signatures cross the software-supply-
    chain boundary.

## Threat analysis and required controls

| Threat | Attack or failure path | Required preventive/detective controls | Fail-closed response and evidence |
|---|---|---|---|
| Secret disclosure | Secret committed, overbroad mount, raw error/header logging, browser return, dump or backup leakage | V1A rejects exchange credentials; file/secret-manager delivery for operational secrets; per-process mounts; `0600` files; structured redaction; secret scanning and canaries | Reject startup on forbidden/unsafe secret input; revoke/rotate on exposure; scanning plus canary tests across logs, APIs, metrics, traces, audit, exports, and support bundles |
| Malicious configuration | Add `live`, alternate host, credential, unsafe product, looser cap, hidden flag, or partial reload | Versioned typed schema; compiled modes/endpoints/capabilities; immutable run snapshot; complete-graph validation; risky-change authorization/audit | Reject entire startup/reload before readiness; preserve previous snapshot; negative schema and fuzz evidence |
| Network redirection | DNS rebinding, malicious resolver/proxy, redirect, host-confusion, TLS downgrade, alternate port or encoded path | Exact scheme/host/port/path allowlist; TLS verification; no redirects; public-IP checks; proxy isolation; reconnect revalidation; bounded responses | No fallback; mark adapter unready and pause affected decisions; captured egress, redirect/DNS/proxy tests |
| SSRF | User, config, payload, webhook, import, or report supplies a URL that exchange transport follows | Compiled destination constructors; separate webhook/emulator transports; no response URL following; URL normalization; private-address and redirect rejection | Reject before network I/O; audit stable reason without raw URL; SSRF corpus and negative outbound tests |
| Dependency/build compromise | Library, generator, action, image, registry, or build worker introduces signer/order route or exfiltration | Minimal pinned dependencies; checksums/lockfiles; review; SBOM; vulnerability/license/image scans; reproducible clean build; signed provenance/digests; package/symbol and endpoint scans | Block merge/release; quarantine artifact; compare clean rebuild; provenance and scan evidence |
| Log/telemetry exposure | Credentials, session tokens, cookies, signatures, raw payloads, URLs, PII, or high-cardinality IDs emitted | Allowlisted structured fields; centralized redaction at every sink; bounded labels; no raw headers/query strings; restricted access and retention | Drop/redact unsafe event rather than emit; alert on canary; redaction tests and access review |
| Session theft | Cookie theft, fixation, replay, CSRF, cross-origin SSE, stale privilege, brute force | Argon2id password hash; random opaque session tokens stored only as hashes; `Secure`, `HttpOnly`, host-only, `SameSite=Strict`; CSRF and Origin validation; rotation, expiry, revocation, login rate limits; recent reauthentication for high risk | Revoke session(s), block command, audit incident; auth/CSRF/origin/replay/rate-limit tests |
| Privilege escalation | API actor, service role, SQL injection, or worker acts as admin/migrator/engine | Deny-by-default authorization; separate database/service roles; parameterized SQL; authenticated idempotent commands; no API-to-broker shortcut; container non-root/capability drop | Deny and audit; lock affected component on role ambiguity; authorization, SQL, role, filesystem, and container tests |
| Accidental production capability | Generic exchange SDK, signer, order interface, Compose profile, generated API/UI, credential name, or alias creates a path | No production broker/signer/order interface; public client cannot accept credentials; V1A testnet/demo rejected; compiled public routes; prohibited-capability scans; persistent disabled banner | Build/release stops immediately; incident opened; clean-build symbol/config/contract/image and outbound-traffic proof |
| Malformed/stale exchange input | Oversized/malformed frame, unknown enum, gap, duplicate, reorder, crossed book, stale time, schema drift | Bounded decode; raw-before-normalized evidence; strict validation; ordered writer; generation/sequence checks; immutable views; hard freshness/quality gates | Invalidate book generation, pause affected decisions, resynchronize, record gap/incident; emulator/fuzz/soak evidence |
| Queue/resource exhaustion | Event flood, slow storage, disk full, DB outage, unbounded labels/exports | Bounded queues and quotas; event-class overflow policy; disk watermarks; reserved critical capacity; resource limits; cardinality rules | Reject new jobs/decisions; never drop critical journal/risk/command state; finalize/quarantine recorder gap; load/chaos evidence |
| Ownership/fencing failure | Two engines, expired lease, stale writer, reservation race, restart overlap | One active lease per protected resource; monotonically increasing fencing token; exclusive reservation; atomic journal transitions | Stop accepting plans before/at lease loss, lock and recover; concurrency, split-brain, kill-point evidence |
| Data/evidence tampering | Edit journal/audit/config/run, replace segment, delete referenced data, manipulate clock | Append-only/immutable records; hashes/manifests; UTC plus monotonic ordering; database constraints; audit sealing; holds and backup verification | Quarantine mismatch, pause affected scope, never silently repair; integrity, restore, and deterministic replay tests |

## Fail-closed control hierarchy

The effective state is the most restrictive applicable platform, exchange,
instrument, strategy, portfolio, data-quality, persistence, and ownership state.
On uncertainty the system rejects new `ENTRY` intents and stops new decisions in
the affected scope. Cancellation or explicitly policy-approved risk-reducing
virtual recovery may continue; it cannot widen privileges. `PAUSED` and
`LOCKED` never auto-unpause. Recovery requires current health, durable state,
resolved gaps/mismatches, explicit authorization where applicable, a reason,
and an audit record.

Security monitoring must itself fail safely: inability to persist a critical
journal, ownership, risk, command, or audit fact blocks the dependent action.
Failure to send an external notification does not erase the in-app incident.

## A0 exchange-order path safety review

### Authorized V1A graph

```text
public Binance bytes
-> public-only adapter
-> validated market view
-> strategy candidate
-> allocator
-> risk engine
-> simulation planner/broker
-> virtual journal
```

The only exchange egress node is the public adapter, whose policy permits
credential-free `GET` requests and public market-data subscriptions. The broker
node is a simulator and has no exchange transport. API administrative commands
enter a durable command table; the API has neither exchange credentials nor an
exchange transport. Recorder and worker processes also receive no credentials.

### Design-path proof obligations

An external exchange order would require all of: an order-capable interface and
implementation, a serializer, a signer/credential, an authenticated order
route, a transport that can reach it, a mode that selects it, and a deployment
that supplies it. V1A policy requires every one of those elements to be absent
or rejected independently:

- order-capable exchange and production broker interfaces are absent;
- V1A accepts no exchange credentials and public constructors cannot receive
  them;
- the compiled endpoint policy contains no authenticated/account/order route;
- `testnet`, `demo`, and `live` are rejected by schema and persistence policy;
- API/UI contain no external-order or real-trading control;
- V1A processes mount no exchange secret; and
- safety failures stop before outbound network I/O.

Therefore there is no authorized edge connecting the simulation broker to an
exchange transport in the A0 architecture. This review does **not** prove the
future implementation conforms. Required conformance evidence is:

1. source and generated-contract search for broker, signer, order, account,
   credential, transfer, withdrawal, margin, leverage, and production routes;
2. compiler/package/symbol inspection of a clean V1A release build;
3. configuration, database, API, UI, and Compose negative tests;
4. credential-canary tests and per-process mount inspection;
5. captured outbound traffic during full workflow and fault tests; and
6. independent security review of the built image and evidence bundle.

Any contradictory or missing evidence keeps the gate failed; absence cannot be
inferred from a narrow test or documentation review.

## Ownership and review cadence

- Product owns scope and the absolute real-money prohibition.
- Security owns this model, endpoint/mode review, auth, secrets, and incident
  criteria.
- Platform/SRE own host, container, network, availability, backup, and response
  controls.
- Adapter owners own parsing, sequencing, capability, and transport boundaries.
- Domain/storage owners own allocation, risk, fencing, journal, and integrity.
- QA owns adversarial, negative, clean-build, egress, and release evidence.

Review this model at every phase gate and whenever a trust boundary, dependency,
endpoint, mode, process, secret, role, deployment profile, alert sink, or data
class changes. V1C requires a separate extension before any authenticated
sandbox implementation. Any production-order proposal requires a new approved
specification and threat model outside V1; it cannot be an amendment that
silently weakens this one.

## Residual limitations

Public TLS-protected data may still be wrong, delayed, incomplete, or
manipulated by an upstream. A single-server Compose deployment is not highly
available. Public order books cannot prove hypothetical fills or profitability.
Supply-chain scanning cannot prove absence of all malicious behavior. These
limitations are handled through validation, deterministic evidence, restricted
claims, defense in depth, and fail-closed operation; they do not justify adding
private exchange access or real orders.
