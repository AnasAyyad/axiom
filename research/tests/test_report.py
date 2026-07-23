import unittest

from axiom_research.report import DISCLAIMER, validate_mean_reversion_report, validate_report


class ReportPolicyTest(unittest.TestCase):
    def test_accepts_separated_provisional_evidence(self) -> None:
        validate_report(
            {
                "research_generation_id": "generation-1",
                "platform_correctness": "Local deterministic checks passed.",
                "strategy_evidence": "Evidence is provisional and uncertain.",
                "viability_disposition": "undetermined",
                "confidence_label": "local_tier_b",
                "disclaimer": DISCLAIMER,
                "run_references": ["run-1"],
            }
        )

    def test_rejects_profitability_claim(self) -> None:
        with self.assertRaisesRegex(ValueError, "profitability_claim_forbidden"):
            validate_report(
                {
                    "research_generation_id": "generation-1",
                    "platform_correctness": "Local checks passed.",
                    "strategy_evidence": "This strategy will profit.",
                    "viability_disposition": "undetermined",
                    "confidence_label": "local_tier_b",
                    "disclaimer": DISCLAIMER,
                    "run_references": ["run-1"],
                }
            )

    def test_mean_reversion_contract_requires_separate_breakdowns(self) -> None:
        report = {
            "report_contract": "mean-reversion-report.v1",
            "research_generation_id": "generation-b3-1",
            "platform_correctness": "Local deterministic checks passed.",
            "strategy_evidence": "Evidence is provisional and uncertain.",
            "viability_disposition": "undetermined",
            "confidence_label": "local_tier_b",
            "disclaimer": DISCLAIMER,
            "run_references": ["run-b3-1"],
            "breakdowns": {key: ["registered-result"] for key in (
                "asset", "regime", "holding_period", "fast_decline_failure",
                "maximum_adverse_excursion", "trend_filter_comparison", "drawdown"
            )},
            "rejections": {
                "mean_reversion.reject.dangerous_regime": 1,
                "mean_reversion.reject.adx": 1,
                "mean_reversion.reject.market_quality": 1,
                "mean_reversion.failure.fast_decline": 1,
            },
            "walk_forward": [{"test_start": 10, "test_end": 20}],
            "neighborhood": ["base", "entry_low", "entry_high"],
            "capacity": ["10", "75"],
            "stress": [{"name": name} for name in (
                "fee", "spread", "slippage", "latency", "gap", "missed_fill"
            )],
            "benchmarks": [{"name": name} for name in (
                "cash", "buy_and_hold", "static_inventory"
            )],
        }
        validate_mean_reversion_report(report)
        del report["breakdowns"]["fast_decline_failure"]
        with self.assertRaisesRegex(ValueError, "mean_reversion_breakdowns_incomplete"):
            validate_mean_reversion_report(report)


if __name__ == "__main__":
    unittest.main()
