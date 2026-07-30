# V1C endpoint and field matrix

All authenticated destinations use port 443 through the exchange-specific
CONNECT proxy. No caller can supply a URL, method, header map, query map, or
payload.

## Binance Spot Testnet

REST host: `testnet.binance.vision`. The signed private subscription uses
`ws-api.testnet.binance.vision`; `stream.testnet.binance.vision` is permitted
only as the private event-stream destination. The implementation follows the
[official Spot Testnet boundary](https://developers.binance.com/en/docs/products/spot/testnet/general-info).

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v3/account` | startup identity/account |
| GET | `/api/v3/openOrders` | recovery |
| GET | `/api/v3/allOrders` | history/backfill |
| GET | `/api/v3/myTrades` | fills/backfill |
| POST | `/api/v3/order/test` | create validation |
| POST | `/api/v3/order` | create |
| GET | `/api/v3/order` | deterministic client-ID query |
| DELETE | `/api/v3/order` | cancel |

Credential-free Testnet startup requests are limited to `/api/v3/time`,
`/api/v3/exchangeInfo`, `/api/v3/avgPrice`, and `/api/v3/depth`. The signed
WebSocket request is fixed to
`/ws-api/v3/userDataStream.subscribe.signature`. It accepts only normalized
`balanceUpdate`, `outboundAccountPosition`, and `executionReport` data needed
by the durable private inbox. WebSocket requests are JSON text frames, and
the documented initial `subscriptionId=0` is valid.

REST and WebSocket signatures use a conservative timestamp below the bounded
clock estimate. Clock synchronization retries a fixed number of times without
weakening the uncertainty threshold. One reconciliation loads and persists one
authoritative account snapshot, then compares that exact snapshot with durable
state; it does not repeat the 221-weight account/history read. Additional
history pages consume their documented weight from the local reserved budget.

Only `LIMIT` with `GTC`/`IOC` and `LIMIT_MAKER` are accepted. `/sapi`,
derivatives routes, market/FOK/amend/batch/conditional order fields, and any
production-private host are absent.

## Bybit Demo

Private host: `api-demo.bybit.com`; private stream host:
`stream-demo.bybit.com`. `api.bybit.com` and `stream.bybit.com` remain
credential-free public-data destinations only. This split follows the
[official Demo Trading service matrix](https://bybit-exchange.github.io/docs/v5/demo).

| Method | Path | Purpose |
|---|---|---|
| GET | `/v5/user/query-api` | permission inspection |
| GET | `/v5/account/wallet-balance` | unified spot balance |
| POST | `/v5/order/create` | create |
| POST | `/v5/order/cancel` | cancel |
| GET | `/v5/order/realtime` | query |
| GET | `/v5/order/history` | history |
| GET | `/v5/execution/list` | fills |

Authenticated order fields force `category=spot`, `isLeverage=0`, and
`orderFilter=Order`. Only limit GTC, IOC, and PostOnly are accepted.
The private stream authenticates at `/v5/private/auth` and subscribes only to
`order.spot`, `execution.spot`, and `wallet`. WebSocket order entry is absent.
Production-public startup data is limited to `/v5/market/time`,
`/v5/market/instruments-info`, and `/v5/market/orderbook`, and those requests
carry no API key, timestamp, receive window, or signature header.

Startup accepts a stricter Spot-only key or Bybit Demo's exact UI-coupled
Unified Trading permission bundle recorded in ADR-0022. That provider-level
bundle does not add any route to this matrix. Wallet, Exchange, Earn, transfer,
withdrawal, partial/expanded bundles, and unknown nonempty permissions are
rejected. Demo-fund application is also absent and remains a manual owner
action.
