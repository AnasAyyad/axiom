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
