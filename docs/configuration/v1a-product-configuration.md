# V1A product configuration reference

## Effective layers and precedence

V1A keeps four configuration layers separate, in descending authority:

1. Compiled safety policy fixes spot-only products, credential-free V1A modes,
   public endpoint identities, and unavailable capabilities. No file or
   environment value can weaken it.
2. Deployment environment values configure non-financial process wiring in
   `.env.example` and `internal/config.Runtime`.
3. One strict, versioned JSON product graph owns research, portfolio, model,
   asset, instrument, and risk values. `APP_CONFIG_FILE` selects this whole
   graph; it is not an overlay. If unset for a repository-local process, the
   code-owned safe paper default is used.
4. Secret files hold secret material. Product JSON contains references only.

Strategy, portfolio, and risk values have no environment-variable overrides.
`EXECUTION_MODE`, when supplied by deployment, must exactly match the selected
graph and the validated command mode. A mismatch rejects startup.

The image contains [the reviewed shadow graph](../../deploy/config/platform-shadow.json)
at `/etc/axiom/platform.json`. Operators may select another absolute regular
file only when it implements the same strict schema and passes the whole-graph
validator.

## Schema and defaults

The only schema identifier is `axiom.config.v1a.2`. Unknown JSON fields,
trailing documents, zero revisions, and incomplete graphs are rejected.

| Area | Safe default / accepted values | Validation |
| --- | --- | --- |
| `environment` | local paper default; `local`, `test`, or `shadow` | `shadow` environment requires `shadow` mode; unknown and production environments fail. |
| `mode` | `paper`; also `backtest`, `replay`, `shadow` | Exact lower-case closed enum; no aliases. |
| `product` | `spot` | Every other product fails. |
| `safety` | fail closed, initial state `PAUSED`, automatic unpause false | Any deviation fails. |
| `endpoint` | `market-data-only-v1` | Exact REST `https://data-api.binance.vision` and WebSocket `wss://data-stream.binance.vision`; no arbitrary host or URL. |
| `assets` | approved `USDT`, `BTC`, `ETH` | Closed status enum, canonical symbols, no duplicates. |
| `instruments` | `BTC-USDT`, `ETH-USDT` spot | Both assets must be approved; duplicate, self, unknown, or non-spot pairs fail. |
| `portfolio` | `500` USDT virtual starting capital | Settlement asset must be approved; value follows the financial contract below. |
| `models` | `fixed-bps-v1`, `fixed-zero-v1` | Only compiled fee and latency model identifiers are accepted. |
| `trend` | immutable `trend.v1a.1`; completed UTC-aligned `4h` candles | The complete 16-parameter graph below is mandatory; an old or incomplete graph fails closed. |
| `capabilities` | complete ordered list of `*_unsupported` dispositions | A missing, reordered, unknown, or activatable disposition fails. There is no enabling boolean. |
| `secrets` | empty | References follow the secret boundary below. |

## Financial values

Every financial setting is an object containing decimal-string `value`, `unit`,
decimal-string `minimum` and `maximum`, both inclusivity flags, integer `scale`,
and named `rounding`. Scale is at most 18 and the input may not contain more
fractional digits than declared. Allowed rounding names are `down`, `ceiling`,
`floor`, and `half_even`.

| Key | Default | Unit | Range | Inclusivity | Scale | Rounding |
| --- | --- | --- | --- | --- | --- | --- |
| `risk.maximum_asset_allocation` | `0.25` | `decimal_fraction` | `0..1` | both inclusive | 8 | `down` |
| `risk.maximum_order_notional` | `1000` | `USDT` | `0..1000000` | minimum exclusive, maximum inclusive | 8 | `half_even` |
| `risk.maximum_daily_loss` | `100` | `USDT` | `0..1000000` | minimum exclusive, maximum inclusive | 8 | `half_even` |
| `portfolio.starting_capital` | `500` | `USDT` | `0..1000000` | minimum exclusive, maximum inclusive | 8 | `half_even` |

## Trend parameter graph

Every Trend parameter also carries its description, `completed_4h_candle`
cadence, `immutable_per_run` mutability, warm-up, and complete model dependency
list. Executable arithmetic uses 18-decimal intermediate precision with
half-even rounding; the explicit boundary rounding below overrides it where
required.

| ID | Default | Unit | Range | Inclusivity | Scale | Rounding | Warm-up | Model dependencies |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `trend.ema_confirmation_period` | `50` | `completed_candles` | `1..10000` | both inclusive | 0 | `half_even` | `50_candles` | `candle_model` |
| `trend.ema_regime_period` | `200` | `completed_candles` | `2..10000` | both inclusive | 0 | `half_even` | `200_candles` | `candle_model` |
| `trend.breakout_lookback` | `20` | `completed_candles` | `1..1000` | both inclusive | 0 | `half_even` | `20_candles` | `candle_model` |
| `trend.atr_period` | `14` | `completed_candles` | `1..1000` | both inclusive | 0 | `half_even` | `15_candles` | `candle_model` |
| `trend.initial_stop_atr_multiplier` | `2.5` | `decimal_multiplier` | `0..100` | minimum exclusive, maximum inclusive | 18 | `half_even` | `200_candles` | `candle_model`, `fill_model` |
| `trend.trailing_stop_atr_multiplier` | `3` | `decimal_multiplier` | `0..100` | minimum exclusive, maximum inclusive | 18 | `half_even` | `200_candles` | `candle_model`, `fill_model` |
| `trend.protective_loss_cooldown` | `3` | `completed_candles` | `0..1000` | both inclusive | 0 | `half_even` | `0_candles` | `position_model` |
| `trend.trade_risk_budget` | `0.005` | `decimal_fraction` | `0..0.01` | minimum exclusive, maximum inclusive | 18 | `down` | `200_candles` | `fee_model`, `latency_model`, `gap_model` |
| `trend.maximum_notional` | `150` | `USDT` | `0..150` | minimum exclusive, maximum inclusive | 18 | `down` | `200_candles` | `instrument_metadata`, `portfolio_policy` |
| `trend.maximum_simulated_slippage` | `0.005` | `decimal_fraction` | `0..0.005` | both inclusive | 18 | `down` | `200_candles` | `slippage_model` |
| `trend.candidate_lifetime` | `5` | `seconds` | `0..5` | minimum exclusive, maximum inclusive | 0 | `down` | `200_candles` | `latency_model` |
| `trend.marketable_limit_validity` | `5` | `seconds` | `0..5` | minimum exclusive, maximum inclusive | 0 | `down` | `200_candles` | `fill_model`, `latency_model` |
| `trend.arrival_book_max_age` | `250` | `milliseconds` | `0..250` | minimum exclusive, maximum inclusive | 0 | `down` | `200_candles` | `market_view_model` |
| `trend.signal_evaluation_window` | `5` | `seconds` | `0..5` | minimum exclusive, maximum inclusive | 0 | `down` | `200_candles` | `candle_model` |
| `trend.candle_finalization_delay` | `2` | `seconds` | `0..60` | both inclusive | 0 | `down` | `200_candles` | `candle_model` |
| `trend.maximum_positions` | `1` | `count` | `1..1` | both inclusive | 0 | `down` | `200_candles` | `position_model` |

Percentages are decimal fractions: `0.01` is one percent and `1` is one hundred
percent. Whole-percent input such as `25` is therefore outside the allocation
range and fails closed.

## Startup and secret boundary

Configuration loading occurs after command/environment rejection and before a
database pool, listener, worker, or outbound client is opened. `APP_CONFIG_FILE`
must be absolute, present, regular, non-symlink, and at most 1 MiB.

A secret reference has a stable non-placeholder name, an absolute file path,
and a `required` declaration. A present file must be regular, non-symlink,
narrowly permissioned, bounded, non-empty, and free of placeholder content.
Missing required files fail with a redacted stable error. Secret contents are
never included in the snapshot, hash, history, log, or error.

## Snapshots, hashes, and reloads

Validation produces a defensive deep copy and SHA-256 hash over canonical JSON.
Snapshot IDs derive from that hash. Acceptance records UTC/monotonic time,
source (`default`, `file`, `environment`, or `admin`), actor, revision, and a
deterministically ordered top-level change list. Readers receive defensive
copies through an atomic snapshot pointer.

Reload always validates the complete graph; partial reload does not exist.
Mode, environment, product, endpoint, safety posture, secret references,
capability dispositions, and starting portfolio require restart. An instrument
may be disabled but not added during reload. An approved asset may be restricted
immediately; restoration to approved is rejected. Risk maxima may tighten but
not loosen. Approved fee or latency model changes swap atomically. Existing
runs/orders retain their recorded snapshot identity; durable storage of the
history arrives with A4.

## Stable rejection classes

Errors expose a code and field only. Principal classes include
`invalid_configuration`, `unsafe_configuration`, `prohibited_environment`,
`prohibited_mode`, `prohibited_product`, `prohibited_capability`,
`endpoint_rejected`, `prohibited_instrument`, `unapproved_asset`,
`invalid_financial_value`, `invalid_unit`, `financial_value_out_of_range`,
`model_rejected`, `required_secret_missing`, `secret_reference_rejected`,
`restart_required`, `reload_rejected`, `risk_loosening_rejected`, and
`stale_configuration`.
