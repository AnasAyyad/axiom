"""Validate A10 report policy without making strategy decisions."""

from typing import Mapping, Sequence

DISCLAIMER = (
    "Backtest, replay, paper, and shadow results are research evidence only and "
    "are not evidence or a guarantee of production profitability."
)


def validate_report(report: Mapping[str, object]) -> None:
    required = {
        "research_generation_id",
        "platform_correctness",
        "strategy_evidence",
        "viability_disposition",
        "confidence_label",
        "disclaimer",
        "run_references",
    }
    if required.difference(report):
        raise ValueError("report_incomplete")
    if report["disclaimer"] != DISCLAIMER:
        raise ValueError("disclaimer_invalid")
    references = report["run_references"]
    if not isinstance(references, Sequence) or isinstance(references, (str, bytes)) or not references:
        raise ValueError("run_references_invalid")
    text = f"{report['platform_correctness']} {report['strategy_evidence']}".lower()
    if any(phrase in text for phrase in ("guaranteed profit", "production profitable", "will profit")):
        raise ValueError("profitability_claim_forbidden")


def validate_mean_reversion_report(report: Mapping[str, object]) -> None:
    """Validate the separate B3 report contract without weakening Trend."""
    validate_report(report)
    if report.get("report_contract") != "mean-reversion-report.v1":
        raise ValueError("mean_reversion_contract_invalid")
    breakdowns = report.get("breakdowns")
    required = {
        "asset",
        "regime",
        "holding_period",
        "fast_decline_failure",
        "maximum_adverse_excursion",
        "trend_filter_comparison",
        "drawdown",
    }
    if not isinstance(breakdowns, Mapping) or any(not breakdowns.get(key) for key in required):
        raise ValueError("mean_reversion_breakdowns_incomplete")
    rejection_reasons = {
        "mean_reversion.reject.dangerous_regime",
        "mean_reversion.reject.adx",
        "mean_reversion.reject.market_quality",
        "mean_reversion.failure.fast_decline",
    }
    rejections = report.get("rejections")
    if not isinstance(rejections, Mapping) or rejection_reasons.difference(rejections):
        raise ValueError("mean_reversion_failures_incomplete")
    if not isinstance(report.get("walk_forward"), Sequence) or not report["walk_forward"]:
        raise ValueError("mean_reversion_walk_forward_incomplete")
    if not isinstance(report.get("neighborhood"), Sequence) or len(report["neighborhood"]) < 3:
        raise ValueError("mean_reversion_neighborhood_incomplete")
    if not isinstance(report.get("capacity"), Sequence) or len(report["capacity"]) < 2:
        raise ValueError("mean_reversion_capacity_incomplete")
    if not _contains_named(report.get("stress"), {"fee", "spread", "slippage", "latency", "gap", "missed_fill"}):
        raise ValueError("mean_reversion_stress_incomplete")
    if not _contains_named(report.get("benchmarks"), {"cash", "buy_and_hold", "static_inventory"}):
        raise ValueError("mean_reversion_benchmarks_incomplete")


def _contains_named(value: object, required: set[str]) -> bool:
    if not isinstance(value, Sequence) or isinstance(value, (str, bytes)):
        return False
    names = {
        item.get("name")
        for item in value
        if isinstance(item, Mapping) and isinstance(item.get("name"), str)
    }
    return not required.difference(names)
