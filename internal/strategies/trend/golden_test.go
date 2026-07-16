package trend

import (
	"encoding/json"
	"os"
	"testing"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

type indicatorGolden struct {
	Generator string `json:"generator"`
	EMA       struct {
		Period   int      `json:"period"`
		Values   []string `json:"values"`
		Expected string   `json:"expected"`
	} `json:"ema"`
	ATR struct {
		Period  int `json:"period"`
		Candles []struct {
			High  string `json:"high"`
			Low   string `json:"low"`
			Close string `json:"close"`
		} `json:"candles"`
		Expected string `json:"expected"`
	} `json:"atr"`
}

func TestIndependentDecimalIndicatorGolden(t *testing.T) {
	contents, err := os.ReadFile("testdata/indicators_decimal_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture indicatorGolden
	if err = json.Unmarshal(contents, &fixture); err != nil || fixture.Generator != "research/src/axiom_research/indicators.py" {
		t.Fatalf("golden provenance = %#v %v", fixture, err)
	}
	values := makePrices(t, fixture.EMA.Values)
	ema, err := EMA(values, fixture.EMA.Period)
	if err != nil || ema.String() != fixture.EMA.Expected {
		t.Fatalf("golden EMA = %s %v", ema.String(), err)
	}
	candles := make([]candleGoldenInput, len(fixture.ATR.Candles))
	for index, candle := range fixture.ATR.Candles {
		candles[index] = candleGoldenInput{high: candle.High, low: candle.Low, close: candle.Close}
	}
	atr, err := ATR(makeGoldenCandles(t, candles), fixture.ATR.Period)
	if err != nil || atr.String() != fixture.ATR.Expected {
		t.Fatalf("golden ATR = %s %v", atr.String(), err)
	}
}

type candleGoldenInput struct{ high, low, close string }

func makePrices(t *testing.T, source []string) []domain.Price {
	t.Helper()
	values := make([]domain.Price, len(source))
	for index, value := range source {
		values[index] = price(t, value)
	}
	return values
}

func makeGoldenCandles(t *testing.T, source []candleGoldenInput) []exchangecontracts.Candle {
	t.Helper()
	values := make([]exchangecontracts.Candle, len(source))
	for index, value := range source {
		values[index] = indicatorCandle(t, index, value.low, value.high, value.close)
	}
	return values
}
