"""Independent B7 statistical and promotion-eligibility validation.

This module deliberately uses only the Python standard library and remains a
cold-path evidence checker. It cannot mutate strategy maturity, authorize an
order, or write accounting state.
"""

from __future__ import annotations

import math
from decimal import Decimal, InvalidOperation
from typing import Any

MULTIPLE_TESTING_METHOD = "benjamini_hochberg_fdr.v1"
SHARPE_ALGORITHM = "bailey_lopez_de_prado_psr_dsr.v1"
REGISTRATION_CONTRACT = "research-preregistration.v1"
VALIDATION_CONTRACT = "multi-strategy-validation.v1"
DISCLAIMER = (
    "Backtest, replay, paper, and shadow results are research evidence only "
    "and are not evidence or a guarantee of production profitability."
)
_EULER_MASCHERONI = 0.5772156649015329


def adjust_multiple_tests(inputs: dict[str, Any]) -> dict[str, Any]:
    """Independently apply the registered Benjamini-Hochberg FDR procedure."""

    if inputs.get("method") != MULTIPLE_TESTING_METHOD:
        raise ValueError("multiple_testing_invalid")
    alpha = _probability(inputs.get("alpha"))
    raw = inputs.get("raw_p_values")
    if not isinstance(raw, list) or not raw:
        raise ValueError("multiple_testing_invalid")
    values = [_probability(value) for value in raw]
    order = sorted(range(len(values)), key=values.__getitem__)
    adjusted = [0.0] * len(values)
    next_value = 1.0
    for rank in range(len(order), 0, -1):
        index = order[rank - 1]
        next_value = min(next_value, values[index] * len(values) / rank)
        adjusted[index] = next_value
    return {
        "method": MULTIPLE_TESTING_METHOD,
        "alpha": _fixed(alpha),
        "raw_p_values": [_fixed(value) for value in values],
        "adjusted_p_values": [_fixed(value) for value in adjusted],
        "rejected": [value <= alpha for value in adjusted],
        "family_size": len(values),
    }


def analyze_sharpe(inputs: dict[str, Any]) -> dict[str, Any]:
    """Independently compute probabilistic and deflated Sharpe evidence."""

    observations = inputs.get("observations")
    trials = inputs.get("independent_trials")
    if (
        not isinstance(observations, int)
        or isinstance(observations, bool)
        or observations < 3
        or not isinstance(trials, int)
        or isinstance(trials, bool)
        or trials < 1
    ):
        raise ValueError("sharpe_input_invalid")
    names = ("observed_sharpe", "benchmark_sharpe", "skewness", "excess_kurtosis")
    parsed = [_statistic(inputs.get(name)) for name in names]
    observed, benchmark, skewness, excess = parsed
    variance = (
        1 - skewness * observed + ((excess + 2) / 4) * observed * observed
    ) / (observations - 1)
    if variance <= 0 or not math.isfinite(variance):
        raise ValueError("sharpe_input_invalid")
    standard_error = math.sqrt(variance)
    deflated_benchmark = benchmark
    if trials > 1:
        first = _inverse_standard_normal(1 - 1 / trials)
        second = _inverse_standard_normal(1 - 1 / (trials * math.e))
        expected_maximum = standard_error * (
            (1 - _EULER_MASCHERONI) * first + _EULER_MASCHERONI * second
        )
        deflated_benchmark = max(deflated_benchmark, expected_maximum)
    probability = _standard_normal_cdf((observed - benchmark) / standard_error)
    deflated = _standard_normal_cdf(
        (observed - deflated_benchmark) / standard_error
    )
    return {
        "observed_sharpe": _decimal_text(inputs.get("observed_sharpe")),
        "benchmark_sharpe": _decimal_text(inputs.get("benchmark_sharpe")),
        "skewness": _decimal_text(inputs.get("skewness")),
        "excess_kurtosis": _decimal_text(inputs.get("excess_kurtosis")),
        "observations": observations,
        "independent_trials": trials,
        "probabilistic_sharpe_probability": _fixed(probability),
        "deflated_benchmark_sharpe": _fixed(deflated_benchmark),
        "deflated_sharpe_probability": _fixed(deflated),
        "algorithm": SHARPE_ALGORITHM,
    }


def validate_multi_strategy_suite(
    registration: dict[str, Any], suite: dict[str, Any]
) -> list[str]:
    """Validate B7 derived evidence and return independently eligible labels."""

    if registration.get("contract") != REGISTRATION_CONTRACT:
        raise ValueError("registration_contract_invalid")
    if suite.get("contract") != VALIDATION_CONTRACT:
        raise ValueError("validation_contract_invalid")
    if (
        suite.get("research_generation_id")
        != registration.get("research_generation_id")
        or suite.get("strategy_version_id") != registration.get("strategy_version_id")
    ):
        raise ValueError("validation_identity_mismatch")
    expected_multiple = adjust_multiple_tests(suite.get("multiple_testing_input", {}))
    expected_sharpe = analyze_sharpe(suite.get("sharpe_input", {}))
    expected_stability = _neighborhood_stability(suite.get("neighborhood"))
    if suite.get("multiple_testing") != expected_multiple:
        raise ValueError("multiple_testing_evidence_mismatch")
    if suite.get("sharpe") != expected_sharpe:
        raise ValueError("sharpe_evidence_mismatch")
    if suite.get("stability") != expected_stability:
        raise ValueError("stability_evidence_mismatch")
    if suite.get("disclaimer") != DISCLAIMER:
        raise ValueError("research_disclaimer_missing")
    _validate_coverage(suite)
    eligible = _eligible_maturities(registration, suite)
    if suite.get("eligible_maturities") != eligible:
        raise ValueError("eligible_maturities_mismatch")
    return eligible


def _eligible_maturities(
    registration: dict[str, Any], suite: dict[str, Any]
) -> list[str]:
    primary = [source for source in suite["sources"] if source.get("primary") is True]
    qualified = bool(primary) and all(
        source.get("dataset_tier") == "tier_a"
        and source.get("confidence_label") == "formal_tier_a"
        and source.get("mode") in {"backtest", "replay", "shadow"}
        for source in primary
    )
    globally_eligible = (
        suite.get("confidence_label") == "formal_tier_a"
        and suite.get("viability_disposition") == "viable_for_more_research"
        and _whole_number(suite.get("observed_samples"))
        >= _whole_number(registration.get("minimum_samples"))
        and _whole_number(suite.get("observed_trades"))
        >= _whole_number(registration.get("minimum_trades"))
        and suite["stability"]["stable"] is True
        and bool(suite["multiple_testing"]["rejected"])
        and suite["multiple_testing"]["rejected"][0] is True
        and all(item.get("passed") is True for item in suite["criteria"])
        and qualified
        and _decimal(suite["sharpe"]["deflated_sharpe_probability"])
        >= _decimal(registration.get("minimum_deflated_sharpe_probability"))
        and _decimal(suite["confidence"].get("lower")) > 0
    )
    if not globally_eligible:
        return []
    modes = {source["mode"] for source in primary}
    eligible: list[str] = []
    if "backtest" in modes:
        eligible.append("BACKTEST_VALIDATED")
    if {"backtest", "replay"}.issubset(modes):
        eligible.append("REPLAY_VALIDATED")
    if (
        {"backtest", "replay", "shadow"}.issubset(modes)
        and _whole_number(suite.get("observed_shadow_duration"))
        >= _whole_number(registration.get("minimum_shadow_duration"))
    ):
        eligible.append("SHADOW_VALIDATED")
    return eligible


def _validate_coverage(suite: dict[str, Any]) -> None:
    sources = suite.get("sources")
    criteria = suite.get("criteria")
    if not isinstance(sources, list) or not sources:
        raise ValueError("validation_sources_incomplete")
    if not isinstance(criteria, list) or not criteria:
        raise ValueError("validation_criteria_incomplete")
    required_stress = {"fee", "spread", "slippage", "latency", "gap", "missed_fill"}
    required_benchmarks = {"cash", "buy_and_hold", "static_inventory"}
    if not required_stress.issubset(_names(suite.get("stress"))):
        raise ValueError("validation_stress_incomplete")
    if not required_benchmarks.issubset(_names(suite.get("benchmarks"))):
        raise ValueError("validation_benchmarks_incomplete")
    allowed_modes = {"backtest", "replay", "shadow", "paper", "testnet", "demo"}
    allowed_tiers = {"tier_a", "tier_b", "low_confidence", "integration_only"}
    allowed_confidence = {"formal_tier_a", "local_tier_b", "insufficient"}
    identities: set[tuple[str, str]] = set()
    for source in sources:
        identity = (source.get("mode"), source.get("run_id"))
        if (
            identity in identities
            or source.get("mode") not in allowed_modes
            or source.get("dataset_tier") not in allowed_tiers
            or source.get("confidence_label") not in allowed_confidence
            or not _hash(source.get("result_hash"))
        ):
            raise ValueError("validation_source_invalid")
        identities.add(identity)


def _neighborhood_stability(variants: Any) -> dict[str, Any]:
    if not isinstance(variants, list) or len(variants) < 3:
        raise ValueError("parameter_neighborhood_incomplete")
    returns = [_decimal(item.get("net_return")) for item in variants]
    drawdowns = [_decimal(item.get("max_drawdown")) for item in variants]
    nonnegative = sum(value >= 0 for value in returns)
    return {
        "stable": nonnegative * 4 >= len(variants) * 3,
        "variants": len(variants),
        "nonnegative": nonnegative,
        "worst_return": _decimal_text(min(returns)),
        "best_drawdown": _decimal_text(min(drawdowns)),
        "worst_drawdown": _decimal_text(max(drawdowns)),
    }


def _names(values: Any) -> set[str]:
    if not isinstance(values, list):
        return set()
    return {item.get("name") for item in values if isinstance(item, dict)}


def _probability(value: Any) -> float:
    decimal = _decimal(value)
    if decimal < 0 or decimal > 1:
        raise ValueError("multiple_testing_invalid")
    return float(decimal)


def _statistic(value: Any) -> float:
    parsed = float(_decimal(value))
    if not math.isfinite(parsed) or abs(parsed) > 100:
        raise ValueError("sharpe_input_invalid")
    return parsed


def _decimal(value: Any) -> Decimal:
    if isinstance(value, bool) or value is None:
        raise ValueError("statistic_invalid")
    try:
        parsed = Decimal(str(value))
    except (InvalidOperation, ValueError):
        raise ValueError("statistic_invalid") from None
    if not parsed.is_finite():
        raise ValueError("statistic_invalid")
    return parsed


def _whole_number(value: Any) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        raise ValueError("count_invalid")
    return value


def _decimal_text(value: Any) -> str:
    parsed = _decimal(value)
    text = format(parsed, "f")
    if "." in text:
        text = text.rstrip("0").rstrip(".")
    return text or "0"


def _fixed(value: float) -> str:
    bounded = min(1.0, max(0.0, value)) if 0 <= value <= 1 else value
    return f"{bounded:.12f}".rstrip("0").rstrip(".")


def _hash(value: Any) -> bool:
    return (
        isinstance(value, str)
        and len(value) == 64
        and all(character in "0123456789abcdef" for character in value)
    )


def _standard_normal_cdf(value: float) -> float:
    return 0.5 * (1 + math.erf(value / math.sqrt(2)))


def _inverse_standard_normal(probability: float) -> float:
    low, high = 0.02425, 1 - 0.02425
    a = (
        -39.69683028665376,
        220.9460984245205,
        -275.9285104469687,
        138.357751867269,
        -30.66479806614716,
        2.506628277459239,
    )
    b = (
        -54.47609879822406,
        161.5858368580409,
        -155.6989798598866,
        66.80131188771972,
        -13.28068155288572,
    )
    c = (
        -0.007784894002430293,
        -0.3223964580411365,
        -2.400758277161838,
        -2.549732539343734,
        4.374664141464968,
        2.938163982698783,
    )
    d = (
        0.007784695709041462,
        0.3224671290700398,
        2.445134137142996,
        3.754408661907416,
    )
    if probability < low:
        q = math.sqrt(-2 * math.log(probability))
        return (((((c[0] * q + c[1]) * q + c[2]) * q + c[3]) * q + c[4]) * q + c[5]) / (
            (((d[0] * q + d[1]) * q + d[2]) * q + d[3]) * q + 1
        )
    if probability <= high:
        q = probability - 0.5
        r = q * q
        return (((((a[0] * r + a[1]) * r + a[2]) * r + a[3]) * r + a[4]) * r + a[5]) * q / (
            ((((b[0] * r + b[1]) * r + b[2]) * r + b[3]) * r + b[4]) * r + 1
        )
    q = math.sqrt(-2 * math.log(1 - probability))
    return -(((((c[0] * q + c[1]) * q + c[2]) * q + c[3]) * q + c[4]) * q + c[5]) / (
        (((d[0] * q + d[1]) * q + d[2]) * q + d[3]) * q + 1
    )
