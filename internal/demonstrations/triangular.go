package demonstrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/triangular"
)

const TriangularArbitrageID = "triangular-arbitrage-basics"

// RunTriangularArbitrage evaluates a fixed, profitable three-conversion view
// and a separately rejected zero-fee-capacity view. It is read-only evidence:
// no candidate is claimed, planned, or submitted.
func RunTriangularArbitrage(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("demonstration_context_invalid")
	}
	input, err := guidedTriangularInput()
	if err != nil {
		return Result{}, err
	}
	accepted, err := triangular.Evaluate(input)
	if err != nil {
		return Result{}, err
	}
	rejectedInput := input
	rejectedInput.FeeBalances = map[domain.AssetSymbol]domain.Balance{
		mustGuidedAsset("USDT"): mustGuidedBalance("0"),
		mustGuidedAsset("BTC"):  mustGuidedBalance("10"),
		mustGuidedAsset("ETH"):  mustGuidedBalance("10"),
	}
	_, rejectedErr := triangular.Evaluate(rejectedInput)
	if rejectedErr == nil {
		return Result{}, fmt.Errorf("demonstration_rejection_incomplete")
	}
	evidence, err := json.Marshal(map[string]any{
		"candidates":      accepted,
		"rejected_reason": rejectedErr.Error(),
	})
	if err != nil {
		return Result{}, err
	}
	result := Result{
		ID: TriangularArbitrageID, StrategyID: "triangular-arbitrage",
		StrategyVersion: "triangular-arbitrage@1.0.0", Synthetic: true,
		AdvisoryOnly: true, AdvisoryEvidence: evidence,
		ConfigurationHash: input.ConfigurationHash,
		Accepted:          advisoryEvent("candidate_recommended"),
		Rejected:          advisoryEvent("fee_capacity_rejected"),
		Metrics:           backtest.Metrics{TotalNetReturn: "not_applicable"},
	}
	hash, err := resultHash(result)
	if err != nil {
		return Result{}, err
	}
	result.ResultHash = hash
	return result, nil
}

func guidedTriangularInput() (triangular.EvaluationInput, error) {
	markets := make([]triangular.Market, 0, 3)
	for _, definition := range []struct {
		base, quote string
		bids, asks  [][2]string
		feeAsset    string
	}{
		{"BTC", "USDT", [][2]string{{"99", "5"}}, [][2]string{{"100", "5"}}, "USDT"},
		{"ETH", "USDT", [][2]string{{"60", "10"}}, [][2]string{{"61", "10"}}, "USDT"},
		{"ETH", "BTC", [][2]string{{"0.54", "10"}}, [][2]string{{"0.55", "10"}}, "BTC"},
	} {
		market, err := guidedTriangularMarket(definition.base, definition.quote, definition.bids, definition.asks, definition.feeAsset)
		if err != nil {
			return triangular.EvaluationInput{}, err
		}
		markets = append(markets, market)
	}
	return triangular.EvaluationInput{
		Exchange: "binance", Markets: markets, DecisionOffsetNanos: 1_000,
		FirstDetectedOffset: 900, AvailableSettlement: mustGuidedBalance("101"),
		StrategyBudget: mustGuidedBalance("101"), GlobalReserveFloor: mustGuidedBalance("0"),
		RecoveryAllowance: mustGuidedBalance("1"),
		FeeBalances: map[domain.AssetSymbol]domain.Balance{
			mustGuidedAsset("USDT"): mustGuidedBalance("10"),
			mustGuidedAsset("BTC"):  mustGuidedBalance("10"),
			mustGuidedAsset("ETH"):  mustGuidedBalance("10"),
		},
		Configuration:     triangular.DefaultConfiguration(),
		ConfigurationHash: strings.Repeat("c", 64), InstrumentMetadataID: "guided-metadata-v1",
	}, nil
}

func guidedTriangularMarket(base, quote string, bids, asks [][2]string, feeAsset string) (triangular.Market, error) {
	baseAsset, err := domain.ParseAssetSymbol(base)
	if err != nil {
		return triangular.Market{}, err
	}
	quoteAsset, err := domain.ParseAssetSymbol(quote)
	if err != nil {
		return triangular.Market{}, err
	}
	instrument, err := domain.NewSpotInstrument(baseAsset, quoteAsset)
	if err != nil {
		return triangular.Market{}, err
	}
	book, err := marketdata.NewBook("binance", instrument, 20, 20, nil)
	if err != nil {
		return triangular.Market{}, err
	}
	if err = book.BeginGeneration("guided-triangular", 1); err != nil {
		return triangular.Market{}, err
	}
	observation := marketdata.Observation{
		ReceivedAt:   domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: 1},
		ProcessedAt:  domain.EventTime{UTC: time.Unix(10, 1).UTC(), Sequence: 2},
		PublishedAt:  domain.EventTime{UTC: time.Unix(10, 2).UTC(), Sequence: 3},
		ConnectionID: "guided-triangular", ConnectionGeneration: 1, SourceSequence: 1,
		IngestOrdinal: 1, ReceivedOffsetNanos: 100, ProcessedOffsetNanos: 101, PublishedOffsetNanos: 102,
	}
	snapshot, err := guidedSnapshot(instrument, bids, asks, observation)
	if err != nil {
		return triangular.Market{}, err
	}
	if err = book.ReplaceSnapshot(snapshot, observation); err != nil {
		return triangular.Market{}, err
	}
	fee, err := domain.ParseRate("0.0001")
	if err != nil {
		return triangular.Market{}, err
	}
	feeSymbol, err := domain.ParseAssetSymbol(feeAsset)
	if err != nil {
		return triangular.Market{}, err
	}
	priceTick, err := domain.ParsePrice("0.01")
	if err != nil {
		return triangular.Market{}, err
	}
	quantityStep, err := domain.ParseQuantity("0.0001")
	if err != nil {
		return triangular.Market{}, err
	}
	minimumNotional, err := domain.ParseNotional("0.001")
	if err != nil {
		return triangular.Market{}, err
	}
	maximumQuantity, err := domain.ParseQuantity("10000")
	if err != nil {
		return triangular.Market{}, err
	}
	return triangular.Market{Book: book.View(), Rules: arbitrage.InstrumentRules{
		Exchange: "binance", Metadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1,
			EffectiveAt: time.Unix(9, 0).UTC(), PriceTick: priceTick, QuantityStep: quantityStep,
			MinimumQuantity: quantityStep, MinimumNotional: minimumNotional},
		MaximumQuantity: maximumQuantity, Fee: arbitrage.FeeSchedule{Version: "guided-fee-v1", Rate: fee, Asset: feeSymbol},
		Active: true, ObservedAt: time.Unix(10, 0).UTC(),
	}}, nil
}

func guidedSnapshot(instrument domain.Instrument, bids, asks [][2]string, observed marketdata.Observation) (exchangecontracts.BookSnapshot, error) {
	levels := func(values [][2]string) ([]exchangecontracts.PriceLevel, error) {
		result := make([]exchangecontracts.PriceLevel, 0, len(values))
		for _, value := range values {
			price, priceErr := domain.ParsePrice(value[0])
			quantity, quantityErr := domain.ParseQuantity(value[1])
			if priceErr != nil || quantityErr != nil {
				return nil, fmt.Errorf("demonstration_book_invalid")
			}
			result = append(result, exchangecontracts.PriceLevel{Price: price, Quantity: quantity})
		}
		return result, nil
	}
	parsedBids, err := levels(bids)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, err
	}
	parsedAsks, err := levels(asks)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, err
	}
	return exchangecontracts.BookSnapshot{Exchange: "binance", Instrument: instrument, LastSequence: 1,
		ReceivedAt: observed.ReceivedAt, Bids: parsedBids, Asks: parsedAsks, RawPayloadHash: "sha256:guided-triangular"}, nil
}

func mustGuidedAsset(value string) domain.AssetSymbol {
	result, err := domain.ParseAssetSymbol(value)
	if err != nil {
		panic(err)
	}
	return result
}

func mustGuidedBalance(value string) domain.Balance {
	result, err := domain.ParseBalance(value)
	if err != nil {
		panic(err)
	}
	return result
}
