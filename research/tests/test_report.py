import unittest

from axiom_research.report import DISCLAIMER, validate_report


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


if __name__ == "__main__":
    unittest.main()
