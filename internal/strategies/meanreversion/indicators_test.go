package meanreversion

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestPopulationZScoreGoldenVectorAndZeroDeviation(t *testing.T) {
	fixture := readIndicatorGolden(t)
	values := make([]domain.Price, len(fixture.ZScore.Values))
	for index, text := range fixture.ZScore.Values {
		values[index], _ = domain.ParsePrice(text)
	}
	mean, deviation, zscore, err := RollingZScore(values, fixture.ZScore.Period)
	if err != nil || mean.String() != fixture.ZScore.Mean || deviation.String() != fixture.ZScore.Deviation ||
		zscore != fixture.ZScore.ZScore {
		t.Fatalf("zscore = %s/%s/%s, %v", mean, deviation, zscore, err)
	}
	constant := make([]domain.Price, 20)
	for index := range constant {
		constant[index], _ = domain.ParsePrice("100")
	}
	if _, _, _, err = RollingZScore(constant, 20); errorCode(err) != ReasonZeroDeviation {
		t.Fatalf("zero deviation error = %v", err)
	}
}

func TestWilderADX14GoldenVector(t *testing.T) {
	fixture := readIndicatorGolden(t)
	candles := make([]exchangecontracts.Candle, len(fixture.ADX.Candles))
	for index, row := range fixture.ADX.Candles {
		candles[index] = indicatorCandle(t, index, row.High, row.Low, row.Close)
	}
	value, err := ADX(candles, fixture.ADX.Period)
	if err != nil || value != fixture.ADX.Expected {
		t.Fatalf("ADX = %s, %v", value, err)
	}
}

func TestEMA200SeedAndExactTenCandleDeclineBoundary(t *testing.T) {
	values := make([]domain.Price, 200)
	for index := range values {
		values[index], _ = domain.ParsePrice("100")
	}
	series, err := EMAValues(values, 200)
	if err != nil || len(series) != 1 || series[0].String() != "100" {
		t.Fatalf("EMA 200 seed = %#v, %v", series, err)
	}
	prior, _ := domain.ParsePrice("100")
	atBoundary, _ := domain.ParsePrice("99.5")
	oneUnitSafe, _ := domain.ParsePrice("99.5000000000000001")
	decline, strong, err := ClassifyEMADecline(prior, atBoundary, "0.005")
	if err != nil || decline != "0.005" || !strong {
		t.Fatalf("decline boundary = %s/%t, %v", decline, strong, err)
	}
	_, strong, err = ClassifyEMADecline(prior, oneUnitSafe, "0.005")
	if err != nil || strong {
		t.Fatalf("one unit below decline boundary classified strong: %v", err)
	}
}

type indicatorGolden struct {
	ZScore struct {
		Values    []string `json:"values"`
		Period    int      `json:"period"`
		Mean      string   `json:"mean"`
		Deviation string   `json:"population_stddev"`
		ZScore    string   `json:"zscore"`
	} `json:"zscore"`
	ADX struct {
		Period  int `json:"period"`
		Candles []struct {
			High, Low, Close string
		} `json:"candles"`
		Expected string `json:"expected"`
	} `json:"adx"`
}

func readIndicatorGolden(t *testing.T) indicatorGolden {
	t.Helper()
	payload, err := os.ReadFile("testdata/indicators_decimal_golden.json")
	var fixture indicatorGolden
	if err != nil || json.Unmarshal(payload, &fixture) != nil {
		t.Fatal(err)
	}
	return fixture
}

func indicatorCandle(t *testing.T, index int, highText, lowText, closeText string) exchangecontracts.Candle {
	t.Helper()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	openTime := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(index) * time.Hour)
	open, _ := domain.ParsePrice(closeText)
	high, _ := domain.ParsePrice(highText)
	low, _ := domain.ParsePrice(lowText)
	closePrice, _ := domain.ParsePrice(closeText)
	volume, _ := domain.ParseQuantity("1")
	return exchangecontracts.Candle{Exchange: "binance", Instrument: instrument, Interval: "1h",
		OpenTime: openTime, CloseTime: openTime.Add(time.Hour), Open: open, High: high, Low: low,
		Close: closePrice, Volume: volume, Closed: true,
		ReceivedAt:     domain.EventTime{UTC: openTime.Add(time.Hour), Sequence: uint64(index + 1)},
		RawPayloadHash: "golden"}
}

func errorCode(err error) string {
	if failure, ok := err.(Error); ok {
		return failure.Code
	}
	return ""
}
