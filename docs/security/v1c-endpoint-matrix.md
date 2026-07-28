# V1C endpoint and field matrix

All authenticated destinations use port 443 through the exchange-specific
CONNECT proxy. No caller can supply a URL, method, header map, query map, or
payload.

## Binance Spot Testnet

Host: `testnet.binance.vision`. Private stream proxy destinations may also use
`ws-api.testnet.binance.vision` and `stream.testnet.binance.vision`.

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

Only `LIMIT` with `GTC`/`IOC` and `LIMIT_MAKER` are accepted. `/sapi`,
derivatives routes, market/FOK/amend/batch/conditional order fields, and any
production-private host are absent.

## Bybit Demo

Private host: `api-demo.bybit.com`; private stream host:
`stream-demo.bybit.com`. `api.bybit.com` and `stream.bybit.com` remain
credential-free public-data destinations only.

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
