import json
import unittest
from pathlib import Path

from axiom_research import adx, atr, ema, population_zscore


class IndicatorGoldenTest(unittest.TestCase):
    def test_ema_simple_mean_seed(self) -> None:
        self.assertEqual(ema(["1", "2", "3", "4", "5"], 3), "4")

    def test_atr_wilder_seed(self) -> None:
        candles = [("10", "8", "9"), ("12", "9", "11"), ("13", "10", "12"), ("14", "11", "13")]
        self.assertEqual(atr(candles, 3), "2.777777777777777778")

    def test_committed_go_golden_was_independently_reproduced(self) -> None:
        repository = Path(__file__).resolve().parents[2]
        fixture_path = repository / "internal" / "strategies" / "trend" / "testdata" / "indicators_decimal_golden.json"
        fixture = json.loads(fixture_path.read_text(encoding="utf-8"))
        ema_fixture = fixture["ema"]
        atr_fixture = fixture["atr"]
        self.assertEqual(ema(ema_fixture["values"], ema_fixture["period"]), ema_fixture["expected"])
        candles = [(item["high"], item["low"], item["close"]) for item in atr_fixture["candles"]]
        self.assertEqual(atr(candles, atr_fixture["period"]), atr_fixture["expected"])

    def test_mean_reversion_go_golden_was_independently_reproduced(self) -> None:
        repository = Path(__file__).resolve().parents[2]
        fixture_path = repository / "internal" / "strategies" / "meanreversion" / "testdata" / "indicators_decimal_golden.json"
        fixture = json.loads(fixture_path.read_text(encoding="utf-8"))
        zscore = fixture["zscore"]
        self.assertEqual(
            population_zscore(zscore["values"], zscore["period"]),
            (zscore["mean"], zscore["population_stddev"], zscore["zscore"]),
        )
        adx_fixture = fixture["adx"]
        candles = [(item["high"], item["low"], item["close"]) for item in adx_fixture["candles"]]
        self.assertEqual(adx(candles, adx_fixture["period"]), adx_fixture["expected"])


if __name__ == "__main__":
    unittest.main()
