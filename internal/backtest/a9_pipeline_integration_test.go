package backtest_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
	"axiom/internal/risk"
	"axiom/internal/simulation"
)

func TestA9RealAllocatorAndRiskComposeThroughSharedA8Pipeline(t *testing.T) {
	registry := portfolio.NewAssetRegistry()
	owned := pipelinePortfolio(t)
	liquidity := portfolio.NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("1")
	if err := liquidity.Open("combined-book", quantity); err != nil {
		t.Fatal(err)
	}
	allocator, _ := portfolio.NewAllocator(owned, registry, liquidity)
	pipelineAllocator, _ := portfolio.NewPipelineAllocator(allocator)
	vault := portfolio.NewApprovalVault()
	riskEngine, err := risk.NewEngine(&pipelineRiskAudit{}, pipelineRiskAlerts{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 16, 11, 0, 0, 0, time.UTC)
	if err = riskEngine.ManualTransition(risk.StateNormal, pipelineRecoveryEvidence(now)); err != nil {
		t.Fatal(err)
	}
	pipelineRisk, _ := risk.NewPipelineEngine(riskEngine, vault, registry, pipelineRiskInputs{at: now.Add(time.Second)})
	planner, _ := portfolio.NewEligibilityPlanner(pipelinePlanner{}, vault, registry)
	guard, _ := portfolio.NewBrokerGuard(owned, registry)
	processor, err := backtest.NewPipelineProcessor(backtest.PipelineDependencies{
		Strategy: pipelineStrategy{candidate: pipelineCandidate(t)}, Allocator: pipelineAllocator, Risk: pipelineRisk,
		Planner: planner, Broker: pipelineBroker{guard: guard}, Reduce: func(_ context.Context, _ backtest.AllocatedIntent, _ execution.SimulatedPlan, _ []execution.OrderEvent) (json.RawMessage, json.RawMessage, error) {
			return json.RawMessage(`[]`), json.RawMessage(`{"USDT":"399.9"}`), nil
		}, Metrics: func() backtest.Metrics { return backtest.Metrics{TotalNetReturn: "unavailable"} },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := processor.Process(context.Background(), replay.Event{LogicalTime: 1, Ordinal: 1, Canonical: []byte("{}")})
	if err != nil || result.Ordinal != 1 || len(result.Decision) == 0 || processor.Metrics().TotalNetReturn != "unavailable" {
		t.Fatalf("A9 shared pipeline = %#v %v", result, err)
	}
	key, _ := owned.BalanceKey("USDT")
	balance, _ := owned.Ledger().Balance(key)
	if balance.Available.String() != "399.9" || balance.Reserved.String() != "100.1" {
		t.Fatalf("A9 pipeline ownership = %#v", balance)
	}
}

func TestA9PipelineReleasesExclusiveClaimsAfterRiskRejection(t *testing.T) {
	owned := pipelinePortfolio(t)
	liquidity := portfolio.NewLiquidityPool()
	quantity, _ := domain.ParseQuantity("1")
	if err := liquidity.Open("combined-book", quantity); err != nil {
		t.Fatal(err)
	}
	allocator, _ := portfolio.NewAllocator(owned, portfolio.NewAssetRegistry(), liquidity)
	pipelineAllocator, _ := portfolio.NewPipelineAllocator(allocator)
	processor, err := backtest.NewPipelineProcessor(backtest.PipelineDependencies{
		Strategy: pipelineStrategy{candidate: pipelineCandidate(t)}, Allocator: pipelineAllocator,
		Risk: pipelineRejectingRisk{}, Planner: pipelinePlanner{}, Broker: pipelineBroker{},
		Reduce: func(context.Context, backtest.AllocatedIntent, execution.SimulatedPlan, []execution.OrderEvent) (json.RawMessage, json.RawMessage, error) {
			return nil, nil, nil
		}, Metrics: func() backtest.Metrics { return backtest.Metrics{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = processor.Process(context.Background(), replay.Event{LogicalTime: 1, Ordinal: 1, Canonical: []byte("{}")}); err == nil {
		t.Fatal("risk rejection was accepted")
	}
	key, _ := owned.BalanceKey("USDT")
	balance, _ := owned.Ledger().Balance(key)
	depth, _ := liquidity.Available("combined-book")
	fundsID, _ := domain.NewReservationID("pipeline-funds")
	liquidityID, _ := domain.NewReservationID("pipeline-liquidity")
	funds, _ := owned.Ledger().Reservation(fundsID)
	claim, _ := liquidity.Reservation(liquidityID)
	if balance.Available.String() != "500" || balance.Reserved.String() != "0" || depth.String() != "1" ||
		funds.State != accounting.ReservationReleased || claim.State != "released" {
		t.Fatalf("rejected pipeline ownership = %#v %#v %#v %#v", balance, depth, funds, claim)
	}
}

func pipelinePortfolio(t *testing.T) *portfolio.Portfolio {
	t.Helper()
	runID, _ := domain.NewRunID("pipeline-run")
	portfolioID, _ := domain.NewPortfolioID("pipeline-portfolio")
	accountID, _ := domain.NewVirtualAccountID("pipeline-account")
	result, err := portfolio.InitializeV1ATrend(runID, portfolioID, accountID, strings.Repeat("a", 64),
		accounting.NewMemoryJournal(), domain.EventTime{UTC: time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func pipelineCandidate(t *testing.T) backtest.Candidate {
	t.Helper()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("1")
	notional, _ := domain.ParseMoney("100.1")
	score, _ := domain.ParsePnL("1")
	funds, _ := domain.NewReservationID("pipeline-funds")
	liquidity, _ := domain.NewReservationID("pipeline-liquidity")
	payload, err := json.Marshal(portfolio.Candidate{ID: "pipeline-decision", Strategy: portfolio.V1AStrategy,
		Instrument: instrument,
		Side:       domain.SideBuy, Quantity: quantity, Notional: notional, Score: score,
		ScoreComponents: []portfolio.ScoreComponent{{Name: "worst_case", Value: score}}, BaseEligibility: 1,
		QuoteEligibility: 1, LiquidityDomain: "combined-book", LiquidityReservation: liquidity,
		FundsReservation: funds, Fence: 1})
	if err != nil {
		t.Fatal(err)
	}
	return backtest.Candidate{Ordinal: 1, Payload: payload}
}

type pipelineStrategy struct{ candidate backtest.Candidate }

func (strategy pipelineStrategy) Evaluate(context.Context, replay.Event) (backtest.Candidate, error) {
	return strategy.candidate, nil
}

type pipelineRiskInputs struct{ at time.Time }

type pipelineRejectingRisk struct{}

func (pipelineRejectingRisk) Approve(context.Context, backtest.AllocatedIntent) (execution.ApprovedIntent, error) {
	return execution.ApprovedIntent{}, errors.New("risk rejected")
}

func (inputs pipelineRiskInputs) Current() (risk.Observations, []risk.Policy, time.Time, error) {
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	return pipelineHealthyObservations(), []risk.Policy{policy}, inputs.at, nil
}

func pipelineHealthyObservations() risk.Observations {
	percentage := func(value string) *domain.Percent { parsed, _ := domain.ParsePercent(value); return &parsed }
	openOrders, quality := uint32(0), uint8(100)
	age, lag, drift := time.Millisecond, time.Millisecond, time.Millisecond
	healthy := false
	return risk.Observations{AccountDrawdown: percentage("0"), UTCDayLoss: percentage("0"),
		Rolling24HourLoss: percentage("0"), StrategyLoss: percentage("0"), AssetExposure: percentage("0"),
		CombinedExposure: percentage("0"), ExchangeExposure: percentage("0"), Reserve: percentage("1"),
		ReservedCapital: percentage("0"), Spread: percentage("0"), Slippage: percentage("0"), OpenOrders: &openOrders,
		BookAge: &age, QueueLag: &lag, ClockDrift: &drift, QualityScore: &quality,
		Health: risk.HealthInputs{Gap: &healthy, StaleData: &healthy, ReconciliationFault: &healthy,
			AccountingFault: &healthy, UnknownOrder: &healthy, PersistenceFault: &healthy,
			DiskFault: &healthy, APIError: &healthy, LeaseLost: &healthy}}
}

func pipelineRecoveryEvidence(at time.Time) risk.RecoveryEvidence {
	return risk.RecoveryEvidence{Reconciled: true, PersistenceHealthy: true, BooksFresh: true,
		UnknownOrdersResolved: true, Reauthenticated: true, AuditDurable: true,
		Actor: "owner", Reason: "pipeline qualification", At: at}
}

type pipelineRiskAudit struct{}

func (*pipelineRiskAudit) Append(risk.AuditEvent) error { return nil }

type pipelineRiskAlerts struct{}

func (pipelineRiskAlerts) Emit(string, risk.Action, risk.State) error { return nil }

type pipelinePlanner struct{}

func (pipelinePlanner) Plan(_ context.Context, intent execution.ApprovedIntent) (execution.SimulatedPlan, error) {
	planID, _ := domain.NewExecutionPlanID("pipeline-plan")
	orderID, _ := domain.NewVirtualOrderID("pipeline-order")
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("1")
	price, _ := domain.ParsePrice("100")
	return execution.SimulatedPlan{ID: planID, Intent: intent, Namespace: "combined-book", DecisionLogicalTime: 1,
		Legs: []execution.PlannedLeg{{OrderID: orderID, ClientOrderID: "pipeline-client", Instrument: instrument,
			Side: domain.SideBuy, Quantity: quantity, LimitPrice: price, ExpiresAt: 2}}}, nil
}

type pipelineBroker struct{ guard *portfolio.BrokerGuard }

func (broker pipelineBroker) Submit(_ context.Context, plan execution.SimulatedPlan) ([]execution.OrderEvent, error) {
	for _, leg := range plan.Legs {
		if _, err := broker.guard.Authorize(leg, simulation.BookState{}); err != nil {
			return nil, err
		}
	}
	return []execution.OrderEvent{}, nil
}

func (pipelineBroker) Cancel(context.Context, domain.VirtualOrderID, string) ([]execution.OrderEvent, error) {
	return []execution.OrderEvent{}, nil
}
