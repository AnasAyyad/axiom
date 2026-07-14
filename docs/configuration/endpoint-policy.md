# V1A outbound endpoint policy

## Status and purpose

This document defines the code-owned, public-only Binance Spot endpoint policy
for V1A. A7 compiles the narrower routes used by the recorder into the platform
binary. Source and binary checks prove that construction boundary locally; the
72-hour capture and final release-image traffic proof remain separate evidence.

The policy is default-deny. A request is permitted only when its scheme, exact
host, port, method, normalized path, query schema, headers, body, product,
instrument, and redirect behavior all match an entry below. Matching a host
alone is never sufficient.

## Compiled allowlist

The production implementation must express this table as typed constants in the
public Binance adapter. Environment or database configuration may select only a
compiled entry; it cannot add a host, route, method, port, parameter, or stream.

### REST

Exact origin: `https://data-api.binance.vision:443`

| Method | Path | V1A purpose | Allowed query keys |
|---|---|---|---|
| `GET` | `/api/v3/ping` | Connectivity check | none |
| `GET` | `/api/v3/time` | Server-time sampling | none |
| `GET` | `/api/v3/exchangeInfo` | Spot instrument metadata and filters | `symbol`, `symbols`, `symbolStatus`, `showPermissionSets`; values constrained below |
| `GET` | `/api/v3/depth` | Local-book REST snapshot | `symbol`, `limit`, `symbolStatus` |
| `GET` | `/api/v3/trades` | Recent public trades | `symbol`, `limit` |
| `GET` | `/api/v3/aggTrades` | Public aggregate-trade history | `symbol`, `fromId`, `startTime`, `endTime`, `limit` |
| `GET` | `/api/v3/klines` | UTC completed-candle history | `symbol`, `interval`, `startTime`, `endTime`, `limit`, `timeZone` |

Constraints:

- `symbol` values are exactly `BTCUSDT` or `ETHUSDT`; `symbols` is a
  duplicate-free JSON array containing only those values. V1A has no
  all-symbol discovery request.
- `symbolStatus`, when present, is exactly `TRADING`.
- `showPermissionSets`, when present, is `false`.
- `interval` is selected from the compiled intervals required by the approved
  V1A Trend configuration; the baseline is `4h`. Adding an interval requires a
  reviewed policy/code change, not an arbitrary string.
- `timeZone` is omitted or exactly `0`, so candles remain UTC-aligned.
- Numeric limits and time ranges are parsed into bounded typed values. Unknown,
  duplicated, malformed, empty, overflowed, or conflicting parameters fail.
- REST requests have no body and do not carry `X-MBX-APIKEY`, `Authorization`,
  cookies, a `signature`, a `timestamp` used for signing, or any credential.

No other Binance REST origin is a V1A fallback. In particular,
`api.binance.com`, regional/numbered API hosts, testnet/demo hosts, futures and
delivery hosts, margin/SAPI routes, and arbitrary mirrors are denied. A later
availability decision may add another official public origin only through a
reviewed compiled policy change with negative tests.

### WebSocket market streams

Exact origin: `wss://data-stream.binance.vision:443`

Allowed connection paths:

- `/ws/<stream-name>` for one raw stream;
- `/stream?streams=<stream-name-1>/<stream-name-2>/...` for a bounded combined
  stream set; or
- `/stream` followed by a bounded subscription command.

Allowed stream names use lowercase symbols `btcusdt` or `ethusdt` and exactly
one compiled suffix:

- `@depth` or `@depth@100ms` for incremental book updates;
- `@trade` or `@aggTrade` for public trades;
- `@kline_4h` for baseline UTC candle updates.

Only the JSON control methods `SUBSCRIBE`, `UNSUBSCRIBE`, and
`LIST_SUBSCRIPTIONS` are allowed. Parameters must be unique compiled stream
names; request IDs are bounded opaque local identifiers. No method, property,
stream name, listen key, user-data path, or free-form JSON field is passed
through from a user or configuration file.

The WebSocket client treats only a final/closed candle as eligible for strategy
input. Stream messages remain untrusted data and must pass schema, symbol,
sequence, timestamp, size, and generation validation.

The official Binance Spot documentation also identifies public market-stream
origins such as `stream.binance.com`. V1A deliberately selects the market-data-
only origin above as the narrower boundary. Official references reviewed for
this policy are the Binance Spot REST general information, REST general/market
endpoints, and Spot WebSocket market-stream documentation.

## Transport and redirect rules

The public transport must enforce all of the following before network I/O and
again on every reconnect:

- Parse with a structured URL type; reject user information, fragments,
  non-ASCII host tricks, IP literals, trailing-dot aliases, encoded path
  separators, dot segments, duplicate query keys, and non-canonical ports.
- Compare the lower-cased, IDNA-normalized host by exact equality. Wildcards and
  suffix matching are forbidden.
- Require TLS with certificate and hostname verification. Do not expose a
  disable-verification option.
- Disable HTTP redirects. A `3xx` response is an error; a redirect target is not
  re-evaluated as an implicit allowlist entry.
- Do not honor environment proxy variables in the exchange public transport.
  A future explicit egress proxy must be separately configured, authenticated,
  reviewed, and unable to weaken destination validation.
- Resolve DNS through the operating environment, then reject loopback,
  link-local, multicast, unspecified, private, carrier-grade NAT, documentation,
  and otherwise non-public destination addresses. Revalidate each new
  connection and prevent a validated host from being replaced after checking.
- Bound connection, TLS, response-header, body, idle, and total-request times;
  bound response/message sizes and decompression ratios.
- Log only the policy route identifier and sanitized error class, never raw
  query strings, headers, payloads, resolved URLs, or connection credentials.

DNS and network controls are defense in depth. They do not replace the exact
application-level allowlist.

## Absolute deny rules

V1A rejects before network I/O:

- every method other than allowlisted `GET` and the constrained WebSocket
  subscription control messages;
- every account, user-data, order, cancel, test-order, private-stream,
  listen-key, transfer, withdrawal, wallet, margin, lending, futures,
  perpetual, options, leverage, staking, or signing route;
- API-key, signature, cookie, bearer-token, client-certificate, or exchange
  credential material on a public request;
- Binance Testnet, Binance demo, Bybit demo, or any production-private origin;
- URLs supplied by API/UI users, datasets, exchange payloads, redirects,
  database content, or webhook input;
- a request whose route classification is unknown after a dependency or API
  update.

An allowlist mismatch is a non-retryable safety error. The affected component
remains unready or pauses new decisions, emits a bounded metric and redacted
audit/incident event when durable storage is available, and requires a reviewed
configuration or code correction. It must not try a broader host or route.

## SSRF separation

Exchange destinations are constructors over compiled identifiers, never a
general URL-fetch facility. Webhook, report-import, browser, and emulator URLs
must use separate transports and policies. The production V1A binary must not
let an emulator/test override reach a deployed configuration. Response fields
that resemble URLs are data and are never followed automatically.

## Required verification evidence

The release gate needs independent evidence, not only source review:

1. Table-driven tests accept every exact allowlist entry and reject near misses
   in scheme, host, port, path, encoding, method, query, header, and stream name.
2. Redirect, DNS-rebinding/private-address, proxy, oversized-body, and timeout
   tests fail closed.
3. Credential canaries cannot enter public client constructors, requests, logs,
   errors, metrics, traces, API responses, or support bundles.
4. Emulator tests cover allowed behavior without widening production policy.
5. Captured outbound traffic from the release image matches only this policy.
6. Package/symbol and generated-contract scans find no signer, authenticated
   transport, order/account method, production broker, or arbitrary URL path.
7. A clean-build safety review maps the compiled constants back to this table.

The A7 source, route-negative, DNS/redirect, public-integration, and binary
checks exist. The long soak and final release-image outbound capture remain
mandatory; this document alone is never runtime evidence.

## Ownership and change control

Security owns this policy; the Binance adapter team owns its exact typed
implementation; platform engineering owns transport hardening; QA owns negative
and release-image egress evidence. Any official API change is treated as
untrusted until reviewed. Policy additions require a threat-model update,
official-documentation reference, tests, traceability, and security approval.
Removing a route is always preferred when V1A no longer needs it.
