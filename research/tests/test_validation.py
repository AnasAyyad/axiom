import copy
import json
import unittest
from pathlib import Path

from axiom_research import (
    adjust_multiple_tests,
    analyze_sharpe,
    validate_multi_strategy_suite,
)


class B7ValidationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        repository = Path(__file__).resolve().parents[2]
        cls.golden = json.loads(
            (repository / "research" / "testdata" / "b7_statistics_golden.json").read_text(
                encoding="utf-8"
            )
        )

    def test_statistics_match_the_committed_go_golden(self) -> None:
        self.assertEqual(
            adjust_multiple_tests(self.golden["multiple_testing_input"]),
            self.golden["multiple_testing"],
        )
        self.assertEqual(
            analyze_sharpe(self.golden["sharpe_input"]), self.golden["sharpe"]
        )

    def test_only_formal_primary_evidence_qualifies(self) -> None:
        registration, suite = self._complete_suite()
        self.assertEqual(
            validate_multi_strategy_suite(registration, suite),
            ["BACKTEST_VALIDATED", "REPLAY_VALIDATED", "SHADOW_VALIDATED"],
        )
        for field, value in (("dataset_tier", "low_confidence"), ("mode", "demo")):
            rejected = copy.deepcopy(suite)
            rejected["sources"][0][field] = value
            rejected["eligible_maturities"] = []
            self.assertEqual(
                validate_multi_strategy_suite(registration, rejected), []
            )

    def test_tampered_derived_statistics_are_rejected(self) -> None:
        registration, suite = self._complete_suite()
        suite["multiple_testing"]["adjusted_p_values"][0] = "0.001"
        with self.assertRaisesRegex(ValueError, "multiple_testing_evidence_mismatch"):
            validate_multi_strategy_suite(registration, suite)

    def _complete_suite(self) -> tuple[dict, dict]:
        registration = {
            "contract": "research-preregistration.v1",
            "research_generation_id": "generation-b7-1",
            "strategy_version_id": "trend-v1",
            "minimum_samples": 400,
            "minimum_trades": 100,
            "minimum_shadow_duration": 259_200_000_000_000,
            "minimum_deflated_sharpe_probability": "0.95",
        }
        result = {"net_return": "0.02", "max_drawdown": "0.03", "trades": 120}
        sources = [
            {
                "run_id": f"{mode}-b7",
                "mode": mode,
                "dataset_tier": "tier_a",
                "confidence_label": "formal_tier_a",
                "result_hash": str(index) * 64,
                "primary": True,
            }
            for index, mode in enumerate(("backtest", "replay", "shadow"), 1)
        ]
        suite = {
            "contract": "multi-strategy-validation.v1",
            "research_generation_id": "generation-b7-1",
            "strategy_version_id": "trend-v1",
            "sources": sources,
            "neighborhood": [
                {"name": name, **result} for name in ("base", "low", "high")
            ],
            "stress": [
                {"name": name, **result}
                for name in ("fee", "spread", "slippage", "latency", "gap", "missed_fill")
            ],
            "benchmarks": [
                {"name": name, **result}
                for name in ("cash", "buy_and_hold", "static_inventory")
            ],
            "confidence": {"lower": "0.01"},
            "criteria": [{"id": "registered_thresholds", "passed": True}],
            "confidence_label": "formal_tier_a",
            "viability_disposition": "viable_for_more_research",
            "observed_samples": 400,
            "observed_trades": 120,
            "observed_shadow_duration": 259_200_000_000_000,
            "multiple_testing_input": copy.deepcopy(
                self.golden["multiple_testing_input"]
            ),
            "sharpe_input": copy.deepcopy(self.golden["sharpe_input"]),
            "multiple_testing": copy.deepcopy(self.golden["multiple_testing"]),
            "sharpe": copy.deepcopy(self.golden["sharpe"]),
            "stability": {
                "stable": True,
                "variants": 3,
                "nonnegative": 3,
                "worst_return": "0.02",
                "best_drawdown": "0.03",
                "worst_drawdown": "0.03",
            },
            "eligible_maturities": [
                "BACKTEST_VALIDATED",
                "REPLAY_VALIDATED",
                "SHADOW_VALIDATED",
            ],
            "disclaimer": (
                "Backtest, replay, paper, and shadow results are research evidence "
                "only and are not evidence or a guarantee of production profitability."
            ),
        }
        return registration, suite


if __name__ == "__main__":
    unittest.main()
