package triangular

import (
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	"axiom/internal/strategies/arbitrage"
)

func TestEvaluateBothCyclesNativeAndInverseOrientation(t *testing.T) {
	input := profitableInput(t, false)
	candidates, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if len(candidate.Legs) != 3 || candidate.WorstCaseEdge.Compare(candidate.AdditionalSafetyMargin) <= 0 ||
			candidate.ID == "" || len(candidate.Claims) < 6 {
			t.Fatalf("incomplete candidate: %#v", candidate)
		}
	}
	if candidates[0].Cycle != CycleUSDTBTCETHUSDT {
		t.Fatalf("forward cycle was not evaluated: %#v", candidates)
	}

	reverse := reverseProfitableInput(t, false)
	reverseCandidates, err := Evaluate(reverse)
	if err != nil {
		t.Fatal(err)
	}
	if reverseCandidates[0].Cycle != CycleUSDTETHBTCUSDT {
		t.Fatalf("reverse cycle was not evaluated: %#v", reverseCandidates)
	}
	inverse := profitableInput(t, true)
	if _, err = Evaluate(inverse); err != nil {
		t.Fatalf("native BTC-ETH inverse orientation was not supported: %v", err)
	}
	inverseReverse := reverseProfitableInput(t, true)
	if _, err = Evaluate(inverseReverse); err != nil {
		t.Fatalf("reverse cycle with native inverse orientation was not supported: %v", err)
	}
}

func TestEvaluateSizeLadderDynamicClippingAndDepthEconomics(t *testing.T) {
	input := profitableInput(t, false)
	input.AvailableSettlement = balance("31")
	input.StrategyBudget = balance("31")
	input.RecoveryAllowance = balance("1")
	candidates, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	starts := map[string]bool{}
	for _, candidate := range candidates {
		starts[candidate.Start.String()] = true
	}
	if !starts["10"] || !starts["25"] || !starts["30"] || starts["50"] || starts["100"] {
		t.Fatalf("ladder/dynamic clipping mismatch: %#v", starts)
	}

	depthSensitive := profitableInput(t, false)
	depthSensitive.Markets[0] = triangleMarket(t, "binance", "BTC", "USDT",
		[][2]string{{"99", "5"}}, [][2]string{{"100", "0.1"}, {"140", "5"}}, "USDT", "0")
	candidates, err = Evaluate(depthSensitive)
	if err != nil {
		t.Fatal(err)
	}
	profitBySize := map[string]string{}
	for _, candidate := range candidates {
		if candidate.Cycle == CycleUSDTBTCETHUSDT {
			profitBySize[candidate.Start.String()] = candidate.ExpectedNet.String()
		}
	}
	if _, profitableSmall := profitBySize["10"]; !profitableSmall {
		t.Fatalf("small profitable size missing: %#v", profitBySize)
	}
	if _, profitableLarge := profitBySize["50"]; profitableLarge {
		t.Fatalf("depth should eliminate large apparent profit: %#v", profitBySize)
	}
}

func TestEvaluateFailsClosedOnAgeFiltersFeesAndExpiry(t *testing.T) {
	tests := map[string]func(*EvaluationInput){
		"expired": func(input *EvaluationInput) {
			input.DecisionOffsetNanos = input.FirstDetectedOffset + uint64(input.Configuration.CandidateLifetime) + 1
		},
		"stale": func(input *EvaluationInput) {
			input.DecisionOffsetNanos = 1_000_000_000
		},
		"fee": func(input *EvaluationInput) {
			input.FeeBalances[asset("USDT")] = balance("0")
		},
		"budget": func(input *EvaluationInput) {
			input.GlobalReserveFloor = input.AvailableSettlement
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := profitableInput(t, false)
			mutate(&input)
			if _, err := Evaluate(input); err == nil {
				t.Fatal("expected fail-closed rejection")
			}
		})
	}
}

func TestEvaluateIsPermutationIndependent(t *testing.T) {
	input := profitableInput(t, false)
	first, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Markets[0], input.Markets[2] = input.Markets[2], input.Markets[0]
	second, err := Evaluate(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("candidate count changed: %d != %d", len(first), len(second))
	}
	for index := range first {
		if first[index].ID != second[index].ID {
			t.Fatalf("permutation changed identity at %d", index)
		}
	}
}

func BenchmarkTriangularEvaluator(b *testing.B) {
	input := profitableInput(b, false)
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if _, err := Evaluate(input); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzTriangularExactCycles(f *testing.F) {
	f.Add("50")
	f.Add("100")
	f.Fuzz(func(t *testing.T, value string) {
		parsed, err := domain.ParseBalance(value)
		if err != nil {
			return
		}
		input := profitableInput(t, false)
		input.AvailableSettlement = parsed
		_, _ = Evaluate(input)
	})
}

type testingT interface {
	Helper()
	Fatal(...any)
}

func profitableInput(t testingT, inverse bool) EvaluationInput {
	t.Helper()
	crossBase, crossQuote := "ETH", "BTC"
	if inverse {
		crossBase, crossQuote = "BTC", "ETH"
	}
	markets := []Market{
		triangleMarket(t, "binance", "BTC", "USDT",
			[][2]string{{"99", "5"}}, [][2]string{{"100", "5"}}, "USDT", "0.0001"),
		triangleMarket(t, "binance", "ETH", "USDT",
			[][2]string{{"60", "10"}}, [][2]string{{"61", "10"}}, "USDT", "0.0001"),
	}
	if inverse {
		markets = append(markets, triangleMarket(t, "binance", crossBase, crossQuote,
			[][2]string{{"1.81", "10"}}, [][2]string{{"1.82", "10"}}, "ETH", "0.0001"))
	} else {
		markets = append(markets, triangleMarket(t, "binance", crossBase, crossQuote,
			[][2]string{{"0.54", "10"}}, [][2]string{{"0.55", "10"}}, "BTC", "0.0001"))
	}
	configuration := DefaultConfiguration()
	return EvaluationInput{
		Exchange: "binance", Markets: markets, DecisionOffsetNanos: 1_000,
		FirstDetectedOffset: 900, AvailableSettlement: balance("101"),
		StrategyBudget: balance("101"), GlobalReserveFloor: balance("0"),
		RecoveryAllowance: balance("1"),
		FeeBalances: map[domain.AssetSymbol]domain.Balance{
			asset("USDT"): balance("10"), asset("BTC"): balance("10"), asset("ETH"): balance("10"),
		},
		Configuration: configuration, ConfigurationHash: "config-sha256",
		InstrumentMetadataID: "metadata-v7",
	}
}

func reverseProfitableInput(t testingT, inverse bool) EvaluationInput {
	input := profitableInput(t, inverse)
	input.Markets[1] = triangleMarket(t, "binance", "ETH", "USDT",
		[][2]string{{"49", "10"}}, [][2]string{{"50", "10"}}, "USDT", "0.0001")
	if inverse {
		input.Markets[2] = triangleMarket(t, "binance", "BTC", "ETH",
			[][2]string{{"1.64", "10"}}, [][2]string{{"1.65", "10"}}, "ETH", "0.0001")
	} else {
		input.Markets[2] = triangleMarket(t, "binance", "ETH", "BTC",
			[][2]string{{"0.60", "10"}}, [][2]string{{"0.61", "10"}}, "BTC", "0.0001")
	}
	return input
}

func triangleMarket(
	t testingT,
	exchange, base, quote string,
	bids, asks [][2]string,
	feeAsset, feeRate string,
) Market {
	t.Helper()
	instrument, _ := domain.NewSpotInstrument(asset(base), asset(quote))
	book, err := marketdata.NewBook(exchange, instrument, 20, 20, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = book.BeginGeneration("connection-a", 1); err != nil {
		t.Fatal(err)
	}
	observation := triangleObservation(1)
	snapshot := exchangecontracts.BookSnapshot{
		Exchange: exchangecontracts.ExchangeID(exchange), Instrument: instrument, LastSequence: 1,
		ReceivedAt: observation.ReceivedAt, Bids: triangleLevels(bids), Asks: triangleLevels(asks),
		RawPayloadHash: "sha256:triangle",
	}
	if err = book.ReplaceSnapshot(snapshot, observation); err != nil {
		t.Fatal(err)
	}
	rules := arbitrage.InstrumentRules{
		Exchange: exchange,
		Metadata: domain.InstrumentMetadata{
			Instrument: instrument, Version: 7, EffectiveAt: time.Unix(10, 0).UTC(),
			PriceTick: price("0.01"), QuantityStep: quantity("0.0001"),
			MinimumQuantity: quantity("0.0001"), MinimumNotional: notional("0.001"),
		},
		MaximumQuantity: quantity("10000"),
		Fee: arbitrage.FeeSchedule{
			Version: "fee-v7", Rate: rate(feeRate), Asset: asset(feeAsset),
		},
		Active: true, ObservedAt: time.Unix(10, 0).UTC(),
	}
	return Market{Book: book.View(), Rules: rules}
}

func triangleObservation(sequence uint64) marketdata.Observation {
	return marketdata.Observation{
		ReceivedAt:   domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: 1},
		ProcessedAt:  domain.EventTime{UTC: time.Unix(10, 1).UTC(), Sequence: 2},
		PublishedAt:  domain.EventTime{UTC: time.Unix(10, 2).UTC(), Sequence: 3},
		ConnectionID: "connection-a", ConnectionGeneration: 1, SourceSequence: sequence,
		IngestOrdinal: sequence, ReceivedOffsetNanos: 100, ProcessedOffsetNanos: 101,
		PublishedOffsetNanos: 102,
	}
}

func triangleLevels(values [][2]string) []exchangecontracts.PriceLevel {
	levels := make([]exchangecontracts.PriceLevel, 0, len(values))
	for _, value := range values {
		levels = append(levels, exchangecontracts.PriceLevel{
			Price: price(value[0]), Quantity: quantity(value[1]),
		})
	}
	return levels
}

func asset(value string) domain.AssetSymbol {
	result, err := domain.ParseAssetSymbol(value)
	if err != nil {
		panic(err)
	}
	return result
}

func balance(value string) domain.Balance {
	result, err := domain.ParseBalance(value)
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
