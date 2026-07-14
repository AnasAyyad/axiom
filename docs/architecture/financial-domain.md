# V1A exact financial domain

## Boundary

All authoritative V1A financial values use project-owned types in
`internal/domain`. `Price`, `Quantity`, `Money`, `Rate`, `Percent`, `Fee`,
`Notional`, `Balance`, and `PnL` are distinct Go types. The selected decimal
engine is `cockroachdb/apd`, but its types do not appear in an exported domain
API. The Go source policy rejects such a leak, and the prohibited-capability
scanner rejects binary floating-point declarations in authoritative packages.

Decimal input is fixed-point text matching `-?(0|[1-9][0-9]*)(\.[0-9]+)?`.
Exponent notation, a leading plus, leading zeroes, negative zero, non-finite
values, more than 38 significant digits, and input scale above 18 are rejected.
Non-negative types also reject a negative value. Successful serialization is a
quoted canonical fixed-point string with insignificant trailing zeroes removed.
Database values use the same text form; binary numeric database inputs are
rejected.

## Arithmetic contexts

Exact operations use one immutable context with precision 38, exponent range
`-96..96`, half-even default rounding, and traps for invalid operations,
division failures, overflow, underflow, subnormal, inexact, and rounded results.
An operation that cannot satisfy that contract returns the stable typed error
`arithmetic_rejected` instead of silently losing precision.

Quantization is a separate explicit operation. It accepts scales `0..18` and a
named rounding direction. No caller inherits a third-party default context.

## Risk-sensitive rounding

| Operation | Rule |
| --- | --- |
| Buy quantity | Round down to the exchange quantity step. |
| Sell quantity | Cap at owned virtual inventory, then round down to the step. |
| Buy limit price | Round down to the exchange price tick. |
| Sell limit price | Round up to the exchange price tick. |
| Notional | Multiply exact price and quantity, then quantize half-even to the caller's declared quote scale. |
| Fee | Multiply exact notional and rate, then round toward positive infinity to the declared quote scale. |

These helpers only construct virtual spot values. They do not create an
exchange client, authenticated request, or external-order side effect.

## Identity, reference data, and time

Aggregate identifiers use a closed namespace and canonical `kind:value` text.
The initial asset registry contains exactly approved `USDT`, `BTC`, and `ETH`;
the only registry states are `approved`, `scan_only`, `blocked`, and
`pending_review`. Instruments are canonical base/quote `spot` pairs. Exchange
filter metadata is explicitly versioned and effective at a UTC timestamp.

Every event time combines a UTC wall time with a strictly increasing local
sequence. Live code receives a `SystemClock`; deterministic tests and replay use
a caller-controlled `ReplayClock`. Direct wall-clock calls are confined to the
system-clock adapter.

## Verification

The A2 suite covers canonical parsing, checked arithmetic, overflow and inexact
traps, JSON/text/database round trips, quantity and price rounding boundaries,
the no-oversell property, concurrent clock/snapshot access, fuzz parsing, race
detection, and decimal benchmarks. The recorded commands and results are in
[A2 local validation](../releases/evidence/a2-local-validation.md).
