# V1C authenticated sandbox threat model

## Protected assets

- exchange API credentials and signatures;
- TOTP seed, session material, and one-use authorizations;
- exact daily/open-order capacity and reservations;
- private account events, balances, fills, and reconciliation state;
- account identity, epoch, credential generation, and fencing ownership.

## Trust boundaries

The API owns identity and high-risk authorization but receives no exchange
credentials. Each authenticated engine owns one exchange credential pair,
database role, account lease, internal network, and fixed proxy. Public
collectors, recorder, workers, browser, and observability services receive no
exchange credentials. The proxy receives no credentials and supports CONNECT
only.

The two inert one-shot canary coordinators use the ordinary runtime database
role and internal `core` network. They receive no exchange credential and have
no external network. Each receives only its own short-lived protected request
file plus the API's CSRF/TOTP inputs. The request file is a sensitive manual
input, must be removed after prepare, and is never persisted in evidence.

The C6 browser and API are a separate credential-free control boundary. The
browser receives only generated redacted projections. The API may authenticate,
authorize, validate, audit, and persist durable commands, but has no exchange
client or exchange credential mount. The default-off C6 observer reads only
redacted operational facts through its dedicated database role and cannot
submit or cancel an exchange order.

## Preventive controls

- There is no `live` mode and no generic signed-request or endpoint API.
- Bybit Demo key admission accepts either a Spot-only key or the exact
  provider-UI Unified Trading bundle recorded in ADR-0022. Any partial,
  expanded, fund-management, transfer, withdrawal, or unknown nonempty
  permission set fails startup.
- Signed requests use compile-time host, route, method, field, product, and
  style policies. Redirects, production-private hosts, arbitrary ports, and
  endpoint/proxy overrides are rejected.
- Proxies resolve every tunnel, reject any non-public answer, and dial the
  validated IP directly.
- Evidence commits before network I/O and stores only the redacted request
  shape and hash.
- TOTP counters advance transactionally. Authorizations are hashed,
  session/purpose-bound, one-use, and expire after two minutes.
- Entries require an active 15-minute arm and every safety gate. Cancel and
  reconciliation remain available when entry is blocked.
- Daily notional is reserved in the plan transaction and cannot decrease.
  `UNKNOWN` and every nonterminal state keep capacity and reservations.
- Binance reconciliation accounts for the provider's IP-based endpoint
  weights, reuses one validated authoritative snapshot per cycle, and applies
  bounded clock retries plus conservative signed timestamps. It never retries
  an order-changing request through this read-only recovery path.
- Deterministic client IDs, fenced claims, durable inbox/outbox, and idempotent
  reduction bound crash and duplicate risks.
- Engines publish only the latest public eligibility and health booleans to the
  coordinator. Admission rejects observations older than 250 ms.
- The coordinator can request only query, cancel, and reconciliation through
  a fenced durable command table. Order creation has no command-table route and
  can enter an engine only through a capped, armed dispatcher outbox.
- Canary lifecycle evidence is hash-only and database-immutable. Post-restart
  export is create-once, atomically sealed `0440`, excludes price/quantity and
  private payloads, and is never profitability evidence.
- C6 high-risk HTTP controls retain the A11 session, exact permission, Origin,
  CSRF, idempotency, revision, and reason boundaries. Arm and unlock also
  consume a short-lived password/TOTP authorization bound to purpose, session,
  request source, and reason.
- The C6 qualification role can read only bounded/redacted observations and
  append qualification evidence. Engine runtime recovery evidence is
  append-only; qualification tables are unavailable to engine roles.
- Formal C6 qualification is exact-source, exactly 72 hours, and default-off.
  Smoke mode is database- and code-constrained to remain non-qualified.

## Residual risk and PR3 limitations

C4/C5 supply complete deterministic adapters, runtime wiring, and a canary
harness. Both required no-more-than-10-USDT canaries passed independently and
their sealed evidence records are retained. That closes the PR3 entry
prerequisite, but does not itself record formal V1C owner/security acceptance.
Local or emulator success still does not prove future exchange availability,
operator factors, or a later formal run.

The accepted Bybit UI bundle grants non-Spot capabilities at the provider key
layer even though Axiom cannot execute them. Theft and direct use of that key
outside Axiom could affect the isolated Demo account. This defense-in-depth
reduction is owner-approved for Demo only; production hosts and real funds
remain outside Axiom's credentialed boundary.

Bybit Demo funding is an external owner prerequisite. The engine has no
demo-fund application, transfer, or withdrawal route and fails closed when the
approved virtual asset set is absent.

C6 UI/API, observability, deterministic chaos, and formal-run infrastructure
are implemented on PR3. The actual 72-hour result is not claimed: it must use a
later frozen clean candidate, complete with no closed failure, produce sealed
terminal evidence, and receive explicit review. No PR3 non-soak aggregate
starts that run.
