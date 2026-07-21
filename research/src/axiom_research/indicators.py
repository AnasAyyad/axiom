"""Independent Decimal EMA and ATR checker for Go golden fixtures."""

from decimal import Decimal, ROUND_HALF_EVEN, localcontext
from typing import Iterable, Sequence

SCALE = Decimal("0.000000000000000001")


def _rounded(value: Decimal) -> Decimal:
    return value.quantize(SCALE, rounding=ROUND_HALF_EVEN)


def ema(values: Iterable[str], period: int) -> str:
    points = [Decimal(value) for value in values]
    if period <= 0 or len(points) < period:
        raise ValueError("warm_up")
    with localcontext() as context:
        context.prec = 38
        context.rounding = ROUND_HALF_EVEN
        current = _rounded(sum(points[:period]) / Decimal(period))
        alpha = _rounded(Decimal(2) / Decimal(period + 1))
        inverse = _rounded(Decimal(1) - alpha)
        for point in points[period:]:
            current = _rounded(_rounded(point * alpha) + _rounded(current * inverse))
        return format(current.normalize(), "f")


def atr(candles: Sequence[tuple[str, str, str]], period: int) -> str:
    """Calculate ATR from (high, low, close) tuples."""
    if period <= 0 or len(candles) < period:
        raise ValueError("warm_up")
    with localcontext() as context:
        context.prec = 38
        context.rounding = ROUND_HALF_EVEN
        ranges: list[Decimal] = []
        previous_close: Decimal | None = None
        for high_text, low_text, close_text in candles:
            high, low, close = Decimal(high_text), Decimal(low_text), Decimal(close_text)
            candidates = [high - low]
            if previous_close is not None:
                candidates.extend((abs(high - previous_close), abs(low - previous_close)))
            ranges.append(_rounded(max(candidates)))
            previous_close = close
        current = _rounded(sum(ranges[:period]) / Decimal(period))
        for value in ranges[period:]:
            current = _rounded((_rounded(current * Decimal(period - 1)) + value) / Decimal(period))
        return format(current.normalize(), "f")
