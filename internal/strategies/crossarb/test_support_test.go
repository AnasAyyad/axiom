package crossarb

import (
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/arbitrage"
)

func evaluationFixture(t testing.TB, base string, reverse bool) EvaluationInput {
	t.Helper()
	instrument := testInstrument(base, "USDT")
	binanceBid, binanceAsk := "99", "100"
	bybitBid, bybitAsk := "104", "105"
	binanceBase, bybitBase := "20", "80"
	if reverse {
		binanceBid, binanceAsk = "104", "105"
		bybitBid, bybitAsk = "99", "100"
		binanceBase, bybitBase = "80", "20"
	}
	binance := testMarket(t, "binance", instrument, binanceBid, binanceAsk, 1)
	bybit := testMarket(t, "bybit", instrument, bybitBid, bybitAsk, 2)
	view := coherentFixture(t, []Market{binance, bybit}, 200)
	return EvaluationInput{
		CoherentView: view, Markets: []Market{binance, bybit},
		Inventory: []VenueInventory{
			testInventory("binance", asset(base), binanceBase),
			testInventory("bybit", asset(base), bybitBase),
		},
		QuoteBudget: balance("10"),
		FeeBalances: map[string]domain.Balance{
			"binance:USDT": balance("10"), "bybit:USDT": balance("10"),
		},
		DecisionOffsetNanos: 200, Configuration: DefaultConfiguration(),
		ConfigurationHash: "config-b5", InstrumentMetadataSetHash: "metadata-b5",
		Restoration: testRestoration(),
	}
}

func testMarket(
	t testing.TB,
	exchange string,
	instrument domain.Instrument,
	bid, ask string,
	ordinal uint64,
) Market {
	t.Helper()
	book, err := marketdata.NewBook(exchange, instrument, 20, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = book.BeginGeneration("connection-"+exchange, 1); err != nil {
		t.Fatal(err)
	}
	observation := testObservation(exchange, ordinal)
	snapshot := exchangecontracts.BookSnapshot{
		Exchange: exchangecontracts.ExchangeID(exchange), Instrument: instrument, LastSequence: ordinal,
		ReceivedAt:     observation.ReceivedAt,
		Bids:           []exchangecontracts.PriceLevel{{Price: price(bid), Quantity: quantity("100")}},
		Asks:           []exchangecontracts.PriceLevel{{Price: price(ask), Quantity: quantity("100")}},
		RawPayloadHash: "sha256:" + exchange,
	}
	if err = book.ReplaceSnapshot(snapshot, observation); err != nil {
		t.Fatal(err)
	}
	rules := arbitrage.InstrumentRules{
		Exchange: exchange,
		Metadata: domain.InstrumentMetadata{
			Instrument: instrument, Version: 7, EffectiveAt: time.Unix(9, 0).UTC(),
			PriceTick: price("0.01"), QuantityStep: quantity("0.000001"),
			MinimumQuantity: quantity("0.000001"), MinimumNotional: notional("0.01"),
		},
		MaximumQuantity: quantity("1000"),
		Fee: arbitrage.FeeSchedule{
			Version: "fee.v1", Rate: rate("0.001"), Asset: asset("USDT"),
		},
		Active: true, ObservedAt: time.Unix(10, 0).UTC(),
	}
	return Market{Book: book.View(), Rules: rules}
}

func coherentFixture(t testing.TB, markets []Market, decision uint64) runtimecore.CoherentView {
	t.Helper()
	views := runtimecore.NewMarketViews()
	keys := make([]runtimecore.MarketKey, 0, len(markets))
	for _, market := range markets {
		key := runtimecore.MarketKey{
			Exchange: market.Book.Exchange(), Instrument: market.Book.Instrument(),
		}
		if err := views.ActivateGeneration(key, market.Book.Generation()); err != nil {
			t.Fatal(err)
		}
		input, err := marketdata.CoherentInput(
			market.Book,
			exchangecontracts.ClockHealth{
				ObservedAt: time.Unix(9, 0).UTC(), Offset: 0,
				Uncertainty: time.Millisecond, Eligible: true,
			},
			"collector-"+market.Book.Exchange(), "test-region",
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = views.Publish(input); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	view, err := views.CoherentAsOf(keys, runtimecore.AsOfTrigger{
		MonotonicNanos: decision, IngestOrdinal: 100,
		UTC: time.Unix(10, 0).UTC(),
	}, runtimecore.InitialB2CoherentPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func testObservation(exchange string, ordinal uint64) marketdata.Observation {
	received := domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: ordinal*3 - 2}
	processed := domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: ordinal*3 - 1}
	published := domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: ordinal * 3}
	return marketdata.Observation{
		ReceivedAt: received, ProcessedAt: processed, PublishedAt: published,
		ConnectionID: "connection-" + exchange, ConnectionGeneration: 1,
		SourceSequence: ordinal, IngestOrdinal: ordinal,
		ReceivedOffsetNanos: 100 + ordinal, ProcessedOffsetNanos: 110 + ordinal,
		PublishedOffsetNanos: 120 + ordinal,
	}
}

func testInventory(exchange string, baseAsset domain.AssetSymbol, owned string) VenueInventory {
	return VenueInventory{
		Owner: "portfolio-b5", Exchange: exchange, BaseAsset: baseAsset,
		OwnedBase: balance(owned), TotalEligibleBase: balance("100"),
		OwnedUSDT: balance("100"), TotalEligibleUSDT: balance("200"), Revision: 7,
	}
}

func testRestoration() RestorationEconomics {
	return RestorationEconomics{
		ModelVersion:        "closed-inventory-cycle.v1",
		LatencyModelVersion: "latency-b5.v1", RecoveryModelVersion: "recovery-b5.v1",
		InventoryShadowPriceVersion: "shadow-price-b5.v1",
		ConcentrationModelVersion:   "concentration-b5.v1",
		LatencyDeterioration:        money("0.005"), RecoveryAllowance: money("0.005"),
		MarginalInventoryReplacement: money("0.005"), NaturalReversalCost: money("0.005"),
		AdvisoryRebalancingCost: money("0.005"), ExchangeConcentrationPenalty: money("0.005"),
		USDTVenueConcentrationPenalty: money("0.005"), MaximumOneLegLoss: money("0.01"),
		EstimatedRestorationDelayNanos: 25_000_000, NaturalReverseAvailable: true,
		AdvisoryRebalancingRequired: true,
	}
}

func testInstrument(base, quote string) domain.Instrument {
	result, err := domain.NewSpotInstrument(asset(base), asset(quote))
	if err != nil {
		panic(err)
	}
	return result
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
