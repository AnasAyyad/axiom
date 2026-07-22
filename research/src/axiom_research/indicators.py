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


def population_zscore(values: Iterable[str], period: int) -> tuple[str, str, str]:
    """Return mean, population standard deviation, and latest z-score."""
    points = [_rounded(Decimal(value)) for value in values]
    if period <= 1 or len(points) < period:
        raise ValueError("warm_up")
    points = points[-period:]
    with localcontext() as context:
        context.prec = 38
        context.rounding = ROUND_HALF_EVEN
        total = Decimal(0)
        for point in points:
            total = _rounded(total + point)
        mean = _rounded(total / Decimal(period))
        squared = Decimal(0)
        for point in points:
            deviation = _rounded(point - mean)
            squared = _rounded(squared + _rounded(deviation * deviation))
        variance = _rounded(squared / Decimal(period))
        deviation = _rounded(variance.sqrt())
        if deviation == 0:
            raise ValueError("zero_deviation")
        zscore = _rounded(_rounded(points[-1] - mean) / deviation)
        return tuple(format(value.normalize(), "f") for value in (mean, deviation, zscore))


def adx(candles: Sequence[tuple[str, str, str]], period: int) -> str:
    """Calculate Wilder ADX from (high, low, close) tuples."""
    if period <= 1 or len(candles) < period * 2:
        raise ValueError("warm_up")
    with localcontext() as context:
        context.prec = 38
        context.rounding = ROUND_HALF_EVEN
        facts: list[tuple[Decimal, Decimal, Decimal]] = []
        previous: tuple[Decimal, Decimal, Decimal] | None = None
        for high_text, low_text, close_text in candles:
            high, low, close = (_rounded(Decimal(high_text)), _rounded(Decimal(low_text)), _rounded(Decimal(close_text)))
            true_range = _rounded(high - low)
            plus_dm = minus_dm = Decimal(0)
            if previous is not None:
                previous_high, previous_low, previous_close = previous
                true_range = max(true_range, abs(_rounded(high - previous_close)), abs(_rounded(low - previous_close)))
                up = _rounded(high - previous_high)
                down = _rounded(previous_low - low)
                if up > 0 and up > down:
                    plus_dm = up
                if down > 0 and down > up:
                    minus_dm = down
            facts.append((_rounded(true_range), _rounded(plus_dm), _rounded(minus_dm)))
            previous = (high, low, close)

        smoothed_tr = smoothed_plus = smoothed_minus = Decimal(0)
        for true_range, plus_dm, minus_dm in facts[1 : period + 1]:
            smoothed_tr = _rounded(smoothed_tr + true_range)
            smoothed_plus = _rounded(smoothed_plus + plus_dm)
            smoothed_minus = _rounded(smoothed_minus + minus_dm)

        def wilder_sum(current: Decimal, value: Decimal) -> Decimal:
            return _rounded(_rounded(current - _rounded(current / Decimal(period))) + value)

        def dx_value() -> Decimal:
            if smoothed_tr <= 0:
                raise ValueError("zero_deviation")
            plus_di = _rounded(_rounded(smoothed_plus / smoothed_tr) * Decimal(100))
            minus_di = _rounded(_rounded(smoothed_minus / smoothed_tr) * Decimal(100))
            denominator = _rounded(plus_di + minus_di)
            if denominator == 0:
                return Decimal(0)
            return _rounded(_rounded(abs(_rounded(plus_di - minus_di)) / denominator) * Decimal(100))

        dx_values: list[Decimal] = []
        for index in range(period, len(facts)):
            if index > period:
                true_range, plus_dm, minus_dm = facts[index]
                smoothed_tr = wilder_sum(smoothed_tr, true_range)
                smoothed_plus = wilder_sum(smoothed_plus, plus_dm)
                smoothed_minus = wilder_sum(smoothed_minus, minus_dm)
            dx_values.append(dx_value())
        current = _rounded(sum(dx_values[:period]) / Decimal(period))
        for value in dx_values[period:]:
            current = _rounded((_rounded(current * Decimal(period - 1)) + value) / Decimal(period))
        return format(current.normalize(), "f")
