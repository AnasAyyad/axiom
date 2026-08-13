package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/rebalancing"
	"axiom/internal/replay"
	"axiom/internal/strategies/inventoryrebalancing"
)

type evaluationInventoryRoute struct {
	instrument                domain.Instrument
	source, destination       rebalancing.Node
	sourceBid, destinationAsk domain.Price
	quantity                  domain.Balance
	now                       time.Time
}

// processInventory evaluates the complete virtual BTC/ETH/USDT portfolio over
// a deterministic four-sample cycle (both base assets and both venue
// directions). Route facts come from the same recorded public books. It can
// recommend a manual route, but the advisory pipeline cannot create an order,
// transfer, reservation, fill, or journal mutation.
func (processor *evaluationMarketProcessor) processInventory(ctx context.Context,
	event replay.Event) (backtest.EventResult, error) {
	bucket := event.LogicalTime / uint64(time.Minute)
	if bucket == 0 || bucket == processor.lastSample {
		return processor.observationResult(event, "inventory_sample_waiting"), nil
	}
	input, ok, err := processor.inventoryInput(event)
	if err != nil {
		return backtest.EventResult{}, err
	}
	if !ok {
		return processor.observationResult(event, "inventory_coherent_books_waiting"), nil
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return backtest.EventResult{}, fmt.Errorf("evaluation_inventory_input_invalid")
	}
	result, err := processor.delegate.Process(ctx, replay.Event{Ordinal: event.Ordinal,
		LogicalTime: event.LogicalTime, Canonical: payload})
	if err != nil {
		return backtest.EventResult{}, err
	}
	processor.lastSample = bucket
	processor.snapshotEvidence++
	var decision struct {
		Outcome string `json:"outcome"`
	}
	if json.Unmarshal(result.Decision, &decision) != nil || decision.Outcome == "" {
		return backtest.EventResult{}, fmt.Errorf("evaluation_inventory_decision_invalid")
	}
	condition := "no_eligible_route"
	if decision.Outcome == "recommended" {
		processor.routeEvidence++
		condition = "rebalancing_route_recommended"
	}
	processor.inventoryConditions[condition]++
	return result, nil
}

func (processor *evaluationMarketProcessor) inventoryInput(event replay.Event) (inventoryrebalancing.Input,
	bool, error) {
	route, ok, err := processor.inventoryRoute(event)
	if err != nil || !ok {
		return inventoryrebalancing.Input{}, ok, err
	}
	usdt, _ := domain.ParseAssetSymbol("USDT")
	sourceUSDT := rebalancing.Node{Exchange: route.source.Exchange, Asset: usdt}
	destinationUSDT := rebalancing.Node{Exchange: route.destination.Exchange, Asset: usdt}
	sell, err := processor.inventoryTradeFact("sell", route.source, sourceUSDT, route.instrument.Symbol(),
		route.sourceBid, route.quantity, route.now)
	if err != nil {
		return inventoryrebalancing.Input{}, false, err
	}
	buy, err := processor.inventoryTradeFact("buy", destinationUSDT, route.destination, route.instrument.Symbol(),
		route.destinationAsk, route.quantity, route.now)
	if err != nil {
		return inventoryrebalancing.Input{}, false, err
	}
	return processor.inventoryRouteRequest(event, route, []rebalancing.Edge{sell, buy}), true, nil
}

func (processor *evaluationMarketProcessor) inventoryRoute(event replay.Event) (evaluationInventoryRoute, bool, error) {
	instrument, binance, bybit, ok, err := processor.inventoryBooks(event)
	if err != nil || !ok {
		return evaluationInventoryRoute{}, ok, err
	}
	binanceBids, binanceAsks, binanceOK := binance.levels()
	bybitBids, bybitAsks, bybitOK := bybit.levels()
	if !binanceOK || !bybitOK {
		return evaluationInventoryRoute{}, false, nil
	}
	capital, capitalOK := rational(processor.claim.Configuration.Portfolio.StartingCapital.Value)
	if !capitalOK || capital.Sign() <= 0 {
		return evaluationInventoryRoute{}, false, fmt.Errorf("evaluation_inventory_capital_invalid")
	}
	bucket := event.LogicalTime / uint64(time.Minute)
	reverse := (bucket/2)%2 == 1
	sourceExchange, destinationExchange := "binance", "bybit"
	sourceBid, destinationAsk := binanceBids[0].Price, bybitAsks[0].Price
	if reverse {
		sourceExchange, destinationExchange = destinationExchange, sourceExchange
		sourceBid, destinationAsk = bybitBids[0].Price, binanceAsks[0].Price
	}
	quantity, quantityErr := evaluationInventoryQuantity(capital, sourceBid)
	if quantityErr != nil {
		return evaluationInventoryRoute{}, false, quantityErr
	}
	now := binance.receivedAt.UTC()
	if bybit.receivedAt.After(now) {
		now = bybit.receivedAt.UTC()
	}
	return evaluationInventoryRoute{instrument: instrument,
		source:      rebalancing.Node{Exchange: sourceExchange, Asset: instrument.Base},
		destination: rebalancing.Node{Exchange: destinationExchange, Asset: instrument.Base},
		sourceBid:   sourceBid, destinationAsk: destinationAsk, quantity: quantity, now: now}, true, nil
}

func (processor *evaluationMarketProcessor) inventoryBooks(event replay.Event) (domain.Instrument,
	*evaluationBook, *evaluationBook, bool, error) {
	instruments, err := evaluationInstruments()
	if err != nil {
		return domain.Instrument{}, nil, nil, false, err
	}
	bucket := event.LogicalTime / uint64(time.Minute)
	keys := []string{"BTCUSDT", "ETHUSDT"}
	instrument := instruments[keys[bucket%uint64(len(keys))]]
	binance := processor.books[evaluationBookKey("binance", instrument)]
	bybit := processor.books[evaluationBookKey("bybit", instrument)]
	maximumAge := uint64(5 * time.Second)
	for _, book := range []*evaluationBook{binance, bybit} {
		if book == nil || !book.valid || event.LogicalTime < book.logical ||
			event.LogicalTime-book.logical > maximumAge {
			return domain.Instrument{}, nil, nil, false, nil
		}
	}
	return instrument, binance, bybit, true, nil
}

func evaluationInventoryQuantity(capital *big.Rat, sourceBid domain.Price) (domain.Balance, error) {
	// Half of capital remains quote inventory; the other half is represented by
	// the two base assets on two venues. Each sampled base bucket therefore owns
	// exactly one eighth of the configured portfolio at the current source bid.
	baseBucket := new(big.Rat).Quo(capital, big.NewRat(8, 1))
	priceValue, priceOK := rational(sourceBid.String())
	if !priceOK || priceValue.Sign() <= 0 {
		return domain.Balance{}, fmt.Errorf("evaluation_inventory_capital_invalid")
	}
	quantity, quantityErr := domain.ParseBalance(floorEvaluationDecimal(
		new(big.Rat).Quo(baseBucket, priceValue), 18))
	if quantityErr != nil || quantity.String() == "0" {
		return domain.Balance{}, fmt.Errorf("evaluation_inventory_capital_invalid")
	}
	return quantity, nil
}

func (processor *evaluationMarketProcessor) inventoryRouteRequest(event replay.Event,
	route evaluationInventoryRoute, facts []rebalancing.Edge) inventoryrebalancing.Input {
	factPayload, _ := json.Marshal(facts)
	factHash := sha256.Sum256(factPayload)
	identity := evaluationHash(processor.claim.Manifest.Evaluation.CampaignID,
		processor.claim.Manifest.Evaluation.MemberID, fmt.Sprint(event.Ordinal))
	request := rebalancing.Request{ID: "evaluation-inventory-request-" + identity[:24], Source: route.source,
		Destination: route.destination, Quantity: route.quantity, DecisionTime: route.now,
		Configuration: processor.inventoryConfig, ConfigurationHash: processor.claim.Manifest.ConfigurationHash,
		FactSetHash: hex.EncodeToString(factHash[:]), NaturalReversals: []rebalancing.NaturalReversalPlan{{
			ID:                               "evaluation-natural-reversal-" + identity[:24],
			CrossExchangeArbitrageDecisionID: "evaluation-cross-decision-" + identity[:24],
			Source:                           route.source, Destination: route.destination, SellFactID: facts[0].ID, BuyFactID: facts[1].ID}}}
	snapshot := inventoryrebalancing.SealInventorySnapshot(inventoryrebalancing.InventorySnapshot{
		ID: "evaluation-inventory-snapshot-" + identity[:24], Source: route.source, Destination: route.destination,
		SourceExcess: route.quantity, DestinationDeficit: route.quantity, ObservedAt: route.now})
	return inventoryrebalancing.Input{Ordinal: event.Ordinal, LogicalTime: event.LogicalTime,
		Inventory: snapshot, Request: request, Facts: facts}
}

func (processor *evaluationMarketProcessor) inventoryTradeFact(role string, from, to rebalancing.Node,
	instrument string, price domain.Price, quantity domain.Balance, now time.Time) (rebalancing.Edge, error) {
	notional, ok := new(big.Rat).SetString(price.String())
	quantityValue, quantityOK := new(big.Rat).SetString(quantity.String())
	stressedFee, stressErr := evaluationFeeRate(processor.claim.CostStressBPS)
	feeRate, feeOK := new(big.Rat).SetString(stressedFee.String())
	if !ok || !quantityOK || !feeOK || stressErr != nil {
		return rebalancing.Edge{}, fmt.Errorf("evaluation_inventory_cost_invalid")
	}
	notional.Mul(notional, quantityValue)
	feeValue := new(big.Rat).Mul(new(big.Rat).Set(notional), feeRate)
	fee, feeErr := domain.ParseMoney(rationalString(feeValue))
	spreadValue := new(big.Rat).Mul(new(big.Rat).Set(notional), big.NewRat(1, 10_000))
	spread, spreadErr := domain.ParseMoney(rationalString(spreadValue))
	zero, zeroErr := domain.ParseMoney("0")
	riskScore, riskErr := domain.ParsePercent("0.01")
	confidence, confidenceErr := domain.ParsePercent("1")
	if feeErr != nil || spreadErr != nil || zeroErr != nil || riskErr != nil || confidenceErr != nil {
		return rebalancing.Edge{}, fmt.Errorf("evaluation_inventory_cost_invalid")
	}
	identity := evaluationHash(processor.claim.Manifest.Evaluation.CampaignID, role,
		from.Exchange, to.Exchange, instrument, price.String(), fmt.Sprint(now.UnixNano()))
	edge := rebalancing.Edge{ID: "evaluation-route-fact-" + identity[:24], Version: 1,
		Kind: rebalancing.TradeEdge, From: from, To: to, Instrument: instrument,
		MinimumQuantity: quantity, Available: true, Compatible: true,
		Costs: rebalancing.CostBreakdown{Fee: fee, Spread: spread, Depth: zero, Delay: zero,
			NetworkFee: zero, Compatibility: zero, VolatilityRisk: zero, OperationalRisk: zero},
		MinimumDuration: time.Second, MaximumDuration: 2 * time.Second, RiskScore: riskScore,
		Warnings: []string{"virtual portfolio evidence only", "no external action authorized"},
		ManualChecklist: []string{"Review the virtual imbalance.", "Verify both public books are current.",
			"Confirm fees and spread assumptions.", "Perform no action from this simulation."},
		Provenance: rebalancing.Provenance{Source: "recorded-public-books", Observer: "evaluation-campaign-worker",
			ObservedAt: now, ExpiresAt: now.Add(2 * time.Minute), Confidence: confidence,
			Approval: rebalancing.Approval{Approved: true, Actor: "balanced-full-v1-preset",
				Reference: "server-reviewed-advisory-route-policy", ApprovedAt: now}}}
	return rebalancing.SealEdge(edge), nil
}
