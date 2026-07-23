package arbitrage

import (
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

func TestConvertExactBuySellFeesFiltersAndDust(t *testing.T) {
	instrument := testInstrument("BTC", "USDT")
	book := testBook(t, "binance", instrument,
		[][2]string{{"9.9", "2"}},
		[][2]string{{"10", "1"}, {"11", "1"}},
	)
	rules := testRules(instrument, "USDT", "0.01")
	result, err := Convert(Request{
		Source: asset("USDT"), Target: asset("BTC"), Input: quantity("10.1"),
		Book: book, Rules: rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.NetOutput.String() != "1" || result.FeeQuantity.String() != "0.1" ||
		result.SourceDust.String() != "0" || result.VWAP.String() != "10" {
		t.Fatalf("unexpected exact buy: %#v", result)
	}

	rules.Fee.Asset = asset("USDT")
	rules.Metadata.QuantityStep = quantity("0.01")
	sell, err := Convert(Request{
		Source: asset("BTC"), Target: asset("USDT"), Input: quantity("1.004"),
		Book: book, Rules: rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sell.TradeQuantity.String() != "1" || sell.NetOutput.String() != "9.801" ||
		sell.SourceDust.String() != "0.004" || sell.FeeQuantity.String() != "0.099" {
		t.Fatalf("unexpected exact sell: %#v", sell)
	}
}

func TestConvertThirdAssetFeeAndOneUnitBoundaries(t *testing.T) {
	instrument := testInstrument("ETH", "BTC")
	book := testBook(t, "bybit", instrument,
		[][2]string{{"0.049", "10"}},
		[][2]string{{"0.05", "10"}},
	)
	rules := testRules(instrument, "BNB", "0.001")
	rules.Exchange = "bybit"
	rules.Metadata.PriceTick = price("0.001")
	rules.Metadata.QuantityStep = quantity("0.01")
	rules.Metadata.MinimumQuantity = quantity("0.1")
	rules.Metadata.MinimumNotional = notional("0.005")
	rules.Fee.ThirdAssetPriceInQuote = price("0.01")
	result, err := Convert(Request{
		Source: asset("BTC"), Target: asset("ETH"), Input: quantity("0.051"),
		Book: book, Rules: rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TradeQuantity.String() != "1.02" || result.FeeAsset != asset("BNB") ||
		result.FeeQuantity.String() != "0.0051" {
		t.Fatalf("unexpected third-asset fee: %#v", result)
	}

	rules.Metadata.MinimumQuantity = quantity("1.03")
	if _, err = Convert(Request{
		Source: asset("BTC"), Target: asset("ETH"), Input: quantity("0.051"),
		Book: book, Rules: rules,
	}); err == nil {
		t.Fatal("expected one-step-below minimum rejection")
	}
}

func TestConvertRejectsFalseProfitInputs(t *testing.T) {
	instrument := testInstrument("BTC", "USDT")
	book := testBook(t, "binance", instrument,
		[][2]string{{"9.99", "1"}},
		[][2]string{{"10.005", "1"}},
	)
	rules := testRules(instrument, "USDT", "0.001")
	if _, err := Convert(Request{
		Source: asset("USDT"), Target: asset("BTC"), Input: quantity("10"),
		Book: book, Rules: rules,
	}); err == nil {
		t.Fatal("expected off-tick book rejection")
	}
}

func testRules(instrument domain.Instrument, feeAsset, feeRate string) InstrumentRules {
	return InstrumentRules{
		Exchange: "binance",
		Metadata: domain.InstrumentMetadata{
			Instrument: instrument, Version: 7, EffectiveAt: time.Unix(10, 0).UTC(),
			PriceTick: price("0.01"), QuantityStep: quantity("0.001"),
			MinimumQuantity: quantity("0.001"), MinimumNotional: notional("0.001"),
		},
		MaximumQuantity: quantity("1000"),
		Fee: FeeSchedule{
			Version: "fee.v7", Rate: rate(feeRate), Asset: asset(feeAsset),
		},
		Active: true, ObservedAt: time.Unix(10, 0).UTC(),
	}
}

func testBook(t *testing.T, exchange string, instrument domain.Instrument, bids, asks [][2]string) marketdata.BookView {
	t.Helper()
	book, err := marketdata.NewBook(exchange, instrument, 20, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = book.BeginGeneration("connection-a", 1); err != nil {
		t.Fatal(err)
	}
	observation := testObservation(1)
	snapshot := exchangecontracts.BookSnapshot{
		Exchange: exchangecontracts.ExchangeID(exchange), Instrument: instrument, LastSequence: 1,
		ReceivedAt: observation.ReceivedAt, Bids: testLevels(bids), Asks: testLevels(asks),
		RawPayloadHash: "sha256:book",
	}
	if err = book.ReplaceSnapshot(snapshot, observation); err != nil {
		t.Fatal(err)
	}
	return book.View()
}

func testObservation(sequence uint64) marketdata.Observation {
	received := domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: sequence*3 - 2}
	processed := domain.EventTime{UTC: time.Unix(10, 1).UTC(), Sequence: sequence*3 - 1}
	published := domain.EventTime{UTC: time.Unix(10, 2).UTC(), Sequence: sequence * 3}
	return marketdata.Observation{
		ReceivedAt: received, ProcessedAt: processed, PublishedAt: published,
		ConnectionID: "connection-a", ConnectionGeneration: 1, SourceSequence: sequence,
		IngestOrdinal: sequence, ReceivedOffsetNanos: sequence * 100,
		ProcessedOffsetNanos: sequence*100 + 1, PublishedOffsetNanos: sequence*100 + 2,
	}
}

func testLevels(values [][2]string) []exchangecontracts.PriceLevel {
	levels := make([]exchangecontracts.PriceLevel, 0, len(values))
	for _, value := range values {
		levels = append(levels, exchangecontracts.PriceLevel{
			Price: price(value[0]), Quantity: quantity(value[1]),
		})
	}
	return levels
}

func testInstrument(base, quote string) domain.Instrument {
	instrument, err := domain.NewSpotInstrument(asset(base), asset(quote))
	if err != nil {
		panic(err)
	}
	return instrument
}

func asset(value string) domain.AssetSymbol {
	result, err := domain.ParseAssetSymbol(value)
	if err != nil {
		panic(err)
	}
	return result
}

func price(value string) domain.Price {
	result, err := domain.ParsePrice(value)
	if err != nil {
		panic(err)
	}
	return result
}

func quantity(value string) domain.Quantity {
	result, err := domain.ParseQuantity(value)
	if err != nil {
		panic(err)
	}
	return result
}

func notional(value string) domain.Notional {
	result, err := domain.ParseNotional(value)
	if err != nil {
		panic(err)
	}
	return result
}

func rate(value string) domain.Rate {
	result, err := domain.ParseRate(value)
	if err != nil {
		panic(err)
	}
	return result
}
