# V1C authenticated sandbox threat model

## Protected assets

- exchange API credentials and signatures;
- TOTP seed, session material, and one-use authorizations;
- exact daily/open-order capacity and reservations;
- private account events, balances, fills, and reconciliation state;
- account identity, epoch, credential generation, and fencing ownership.

## Trust boundaries

The API owns identity and high-risk authorization but receives no exchange
credentials. Each future authenticated engine owns one exchange credential
pair, database role, account lease, internal network, and fixed proxy. Public
collectors, recorder, workers, browser, and observability services receive no
exchange credentials. The proxy receives no credentials and supports CONNECT
only.

## Preventive controls

- There is no `live` mode and no generic signed-request or endpoint API.
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
- Deterministic client IDs, fenced claims, durable inbox/outbox, and idempotent
  reduction bound crash and duplicate risks.

## Residual risk and PR1 limitations

PR1 supplies deterministic authenticated emulators and security contracts. It
does not claim real Binance Testnet or Bybit Demo canaries, complete C4/C5
adapters, C6 UI/API completion, or a 72-hour sandbox result. Those are separate
acceptance gates and require frozen-source evidence.
