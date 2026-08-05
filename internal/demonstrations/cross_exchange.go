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
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/arbitrage"
	"axiom/internal/strategies/crossarb"
)

const CrossExchangeArbitrageID = "cross-exchange-arbitrage-basics"

// RunCrossExchangeArbitrage evaluates a fixed coherent two-venue view through
// the reviewed closed-cycle evaluator. It exposes advisory evidence only: it
// does not claim inventory, plan orders, or invoke a sandbox adapter.
func RunCrossExchangeArbitrage(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("demonstration_context_invalid")
	}
	input, err := guidedCrossExchangeInput()
	if err != nil {
		return Result{}, err
	}
	accepted, err := crossarb.Evaluate(input)
	if err != nil {
		return Result{}, err
	}
	rejectedInput := input
	rejectedInput.Restoration.MarginalInventoryReplacement = mustGuidedMoney("100")
	_, rejectedErr := crossarb.Evaluate(rejectedInput)
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
		ID: CrossExchangeArbitrageID, StrategyID: "cross-exchange-arbitrage",
		StrategyVersion: "cross-exchange-arbitrage@1.0.0", Synthetic: true,
		AdvisoryOnly: true, AdvisoryEvidence: evidence,
		ConfigurationHash: input.ConfigurationHash,
		Accepted:          advisoryEvent("closed_cycle_candidate_recommended"),
		Rejected:          advisoryEvent("restoration_cost_rejected"),
		Metrics:           backtest.Metrics{TotalNetReturn: "not_applicable"},
	}
	hash, err := resultHash(result)
	if err != nil {
		return Result{}, err
	}
	result.ResultHash = hash
	return result, nil
}

func guidedCrossExchangeInput() (crossarb.EvaluationInput, error) {
	base, err := domain.ParseAssetSymbol("BTC")
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	quote, err := domain.ParseAssetSymbol("USDT")
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	instrument, err := domain.NewSpotInstrument(base, quote)
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	binance, err := guidedCrossMarket("binance", instrument, "99", "100", 1)
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	bybit, err := guidedCrossMarket("bybit", instrument, "104", "105", 2)
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	markets := []crossarb.Market{binance, bybit}
	view, err := guidedCoherentView(markets, 200)
	if err != nil {
		return crossarb.EvaluationInput{}, err
	}
	return crossarb.EvaluationInput{
		CoherentView: view, Markets: markets,
		Inventory: []crossarb.VenueInventory{
			guidedCrossInventory("binance", base, "20"),
			guidedCrossInventory("bybit", base, "80"),
		},
		QuoteBudget: mustGuidedBalance("10"),
		FeeBalances: map[string]domain.Balance{
			"binance:USDT": mustGuidedBalance("10"), "bybit:USDT": mustGuidedBalance("10"),
		},
		DecisionOffsetNanos: 200, Configuration: crossarb.DefaultConfiguration(),
		ConfigurationHash: strings.Repeat("d", 64), InstrumentMetadataSetHash: strings.Repeat("e", 64),
		Restoration: crossarb.RestorationEconomics{
			ModelVersion: "closed-inventory-cycle.v1", LatencyModelVersion: "guided-latency-v1",
			RecoveryModelVersion: "guided-recovery-v1", InventoryShadowPriceVersion: "guided-shadow-price-v1",
			ConcentrationModelVersion: "guided-concentration-v1", LatencyDeterioration: mustGuidedMoney("0.005"),
			RecoveryAllowance: mustGuidedMoney("0.005"), MarginalInventoryReplacement: mustGuidedMoney("0.005"),
			NaturalReversalCost: mustGuidedMoney("0.005"), AdvisoryRebalancingCost: mustGuidedMoney("0.005"),
			ExchangeConcentrationPenalty: mustGuidedMoney("0.005"), USDTVenueConcentrationPenalty: mustGuidedMoney("0.005"),
			MaximumOneLegLoss: mustGuidedMoney("0.01"), EstimatedRestorationDelayNanos: 25_000_000,
			NaturalReverseAvailable: true, AdvisoryRebalancingRequired: true,
		},
	}, nil
}

func guidedCrossMarket(exchange string, instrument domain.Instrument, bid, ask string, ordinal uint64) (crossarb.Market, error) {
	book, err := marketdata.NewBook(exchange, instrument, 20, 20, nil)
	if err != nil {
		return crossarb.Market{}, err
	}
	if err = book.BeginGeneration("guided-"+exchange, 1); err != nil {
		return crossarb.Market{}, err
	}
	observation := guidedCrossObservation(exchange, ordinal)
	snapshot, err := guidedSnapshot(exchange, instrument, [][2]string{{bid, "100"}}, [][2]string{{ask, "100"}}, observation)
	if err != nil {
		return crossarb.Market{}, err
	}
	if err = book.ReplaceSnapshot(snapshot, observation); err != nil {
		return crossarb.Market{}, err
	}
	priceTick, priceErr := domain.ParsePrice("0.01")
	quantityStep, quantityErr := domain.ParseQuantity("0.000001")
	minimumNotional, notionalErr := domain.ParseNotional("0.01")
	maximumQuantity, maximumErr := domain.ParseQuantity("1000")
	fee, feeErr := domain.ParseRate("0.001")
	if priceErr != nil || quantityErr != nil || notionalErr != nil || maximumErr != nil || feeErr != nil {
		return crossarb.Market{}, fmt.Errorf("demonstration_input_invalid")
	}
	return crossarb.Market{Book: book.View(), Rules: arbitrage.InstrumentRules{
		Exchange: exchange, Metadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1,
			EffectiveAt: time.Unix(9, 0).UTC(), PriceTick: priceTick, QuantityStep: quantityStep,
			MinimumQuantity: quantityStep, MinimumNotional: minimumNotional},
		MaximumQuantity: maximumQuantity,
		Fee:             arbitrage.FeeSchedule{Version: "guided-fee-v1", Rate: fee, Asset: mustGuidedAsset("USDT")},
		Active:          true, ObservedAt: time.Unix(10, 0).UTC(),
	}}, nil
}

func guidedCoherentView(markets []crossarb.Market, decision uint64) (runtimecore.CoherentView, error) {
	views := runtimecore.NewMarketViews()
	keys := make([]runtimecore.MarketKey, 0, len(markets))
	for _, market := range markets {
		key := runtimecore.MarketKey{Exchange: market.Book.Exchange(), Instrument: market.Book.Instrument()}
		if err := views.ActivateGeneration(key, market.Book.Generation()); err != nil {
			return runtimecore.CoherentView{}, err
		}
		input, err := marketdata.CoherentInput(market.Book, exchangecontracts.ClockHealth{
			ObservedAt: time.Unix(9, 0).UTC(), Uncertainty: time.Millisecond, Eligible: true,
		}, "guided-collector-"+market.Book.Exchange(), "guided-region")
		if err != nil {
			return runtimecore.CoherentView{}, err
		}
		if _, err = views.Publish(input); err != nil {
			return runtimecore.CoherentView{}, err
		}
		keys = append(keys, key)
	}
	return views.CoherentAsOf(keys, runtimecore.AsOfTrigger{MonotonicNanos: decision, IngestOrdinal: 100,
		UTC: time.Unix(10, 0).UTC()}, runtimecore.CoherentPolicy{Version: "axiom.coherent-view-policy.v1",
		MaximumBookAge: 250 * time.Millisecond, MaximumInterBookSkew: 250 * time.Millisecond,
		MaximumClockUncertainty: 100 * time.Millisecond})
}

func guidedCrossObservation(exchange string, ordinal uint64) marketdata.Observation {
	return marketdata.Observation{
		ReceivedAt:   domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: ordinal*3 - 2},
		ProcessedAt:  domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: ordinal*3 - 1},
		PublishedAt:  domain.EventTime{UTC: time.Unix(10, 0).UTC(), Sequence: ordinal * 3},
		ConnectionID: "guided-" + exchange, ConnectionGeneration: 1, SourceSequence: ordinal, IngestOrdinal: ordinal,
		ReceivedOffsetNanos: 100 + ordinal, ProcessedOffsetNanos: 110 + ordinal, PublishedOffsetNanos: 120 + ordinal,
	}
}

func guidedCrossInventory(exchange string, base domain.AssetSymbol, owned string) crossarb.VenueInventory {
	return crossarb.VenueInventory{Owner: "guided-owner", Exchange: exchange, BaseAsset: base,
		OwnedBase: mustGuidedBalance(owned), TotalEligibleBase: mustGuidedBalance("100"),
		OwnedUSDT: mustGuidedBalance("100"), TotalEligibleUSDT: mustGuidedBalance("200"), Revision: 1}
}

func mustGuidedMoney(value string) domain.Money {
	result, err := domain.ParseMoney(value)
	if err != nil {
		panic(err)
	}
	return result
}
