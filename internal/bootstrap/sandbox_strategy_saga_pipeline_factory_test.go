package bootstrap

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/replay"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/crossarb"
)

func TestSandboxStrategySagaPipelineFactoryBuildsCredentialFreeTriangularPipeline(t *testing.T) {
	now := time.Date(2026, 8, 9, 17, 30, 0, 0, time.UTC)
	facts := sagaPlanFacts(t, sandbox.StrategyTriangular, now)
	work := facts.Coordinator.Work
	marketReader, _ := NewSandboxSagaMarketInputReader(sagaMarketSource(t, now, []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: sagaInstrument(t, "BTCUSDT")},
		{Exchange: "binance", Instrument: sagaInstrument(t, "ETHBTC")},
		{Exchange: "binance", Instrument: sagaInstrument(t, "ETHUSDT")},
	}))
	market, err := marketReader.ReadTriangular(context.Background(), work, now)
	if err != nil {
		t.Fatal(err)
	}
	riskMarket, _ := market.RiskMarket(work)
	riskInputs := sagaRiskInputsForBuilder(t, facts,
		map[sandbox.AccountID]sandbox.StrategyMarketInput{work.Account.ID: riskMarket}, now)
	input, err := BuildTriangularSandboxInput(work, enabledSagaProduct(t), market, facts, riskInputs)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := risk.NewRestoredEngine(risk.StateNormal, &pipelineFactoryRiskAudit{}, pipelineFactoryRiskAlerts{})
	if err != nil {
		t.Fatal(err)
	}
	riskSource := &pipelineFactoryRiskSource{engine: engine}
	factory, err := NewSandboxStrategySagaPipelineFactory(riskSource, pipelineFactoryRepository{})
	if err != nil {
		t.Fatal(err)
	}
	lease := sandbox.StrategySessionExecutionLease{Account: work.Account.ID,
		Epoch: work.Account.Epoch, Owner: "binance-saga-engine", Fence: 9}
	pipeline, err := factory.Triangular(context.Background(), lease, input, facts)
	if err != nil || pipeline == nil || riskSource.calls != 1 {
		t.Fatalf("pipeline=%#v calls=%d error=%v", pipeline, riskSource.calls, err)
	}
	foreign := lease
	foreign.Account = "another-account"
	if _, err = factory.Triangular(context.Background(), foreign, input, facts); err == nil {
		t.Fatal("foreign coordinator fence accepted")
	}
}

func TestCrossExchangeSagaPipelinePersistsAConcurrentPlanWithinBothNotionalCaps(t *testing.T) {
	now := time.Date(2026, 8, 9, 22, 45, 0, 0, time.UTC)
	facts := sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now)
	work := facts.Coordinator.Work
	input, ten := crossSagaPipelineTestInput(t, now, facts,
		favorableCrossMarketSource(t, now, sagaInstrument(t, work.Instrument)))
	if input.QuoteBudget.Compare(ten) >= 0 {
		t.Fatalf("profitable ratio did not down-scale paired budget: %s", input.QuoteBudget.String())
	}
	engine, err := risk.NewRestoredEngine(risk.StateNormal, &pipelineFactoryRiskAudit{}, pipelineFactoryRiskAlerts{})
	if err != nil {
		t.Fatal(err)
	}
	repository := &capturingSagaRepository{}
	factory, err := NewSandboxStrategySagaPipelineFactory(
		&pipelineFactoryRiskSource{engine: engine}, repository,
	)
	if err != nil {
		t.Fatal(err)
	}
	lease := sandbox.StrategySessionExecutionLease{Account: work.Account.ID,
		Epoch: work.Account.Epoch, Owner: "binance-cross-coordinator", Fence: 12}
	pipeline, err := factory.CrossExchange(context.Background(), lease, input, facts)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(input)
	result, err := pipeline.Process(context.Background(), replay.Event{Ordinal: input.Ordinal,
		LogicalTime: input.LogicalTime, Canonical: canonical})
	if err != nil || result.SandboxPlan.ID == "" || repository.calls != 1 ||
		len(repository.plan.Submissions) != 2 {
		t.Fatalf("result=%#v plan=%#v calls=%d error=%v", result, repository.plan, repository.calls, err)
	}
	maximum, _ := domain.ParseNotional("10")
	for _, submission := range repository.plan.Submissions {
		if submission.Notional.Compare(maximum) > 0 {
			t.Fatalf("submission exceeded cap: %#v", submission)
		}
	}
}

type capturingSagaRepository struct {
	pipelineFactoryRepository
	plan  sandbox.ApprovedSandboxPlan
	calls int
}

func (repository *capturingSagaRepository) ApprovePlan(
	_ context.Context,
	plan sandbox.ApprovedSandboxPlan,
	_ sandbox.SubmissionLimits,
	_ sandbox.KillPoint,
) error {
	repository.calls++
	repository.plan = plan
	return nil
}

func TestSandboxStrategySagaPipelineFactoryBuildsCredentialIsolatedCrossPipeline(t *testing.T) {
	now := time.Date(2026, 8, 9, 22, 30, 0, 0, time.UTC)
	facts := sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now)
	bybit := facts.Admissions[sandbox.ExchangeBybit]
	work := facts.Coordinator.Work
	instrument := sagaInstrument(t, work.Instrument)
	input, _ := crossSagaPipelineTestInput(t, now, facts, sagaMarketSource(t, now, []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: instrument}, {Exchange: "bybit", Instrument: instrument},
	}))
	engine, err := risk.NewRestoredEngine(risk.StateNormal, &pipelineFactoryRiskAudit{}, pipelineFactoryRiskAlerts{})
	if err != nil {
		t.Fatal(err)
	}
	riskSource := &pipelineFactoryRiskSource{engine: engine}
	factory, err := NewSandboxStrategySagaPipelineFactory(riskSource, pipelineFactoryRepository{})
	if err != nil {
		t.Fatal(err)
	}
	lease := sandbox.StrategySessionExecutionLease{Account: work.Account.ID,
		Epoch: work.Account.Epoch, Owner: "binance-cross-coordinator", Fence: 11}
	pipeline, err := factory.CrossExchange(context.Background(), lease, input, facts)
	if err != nil || pipeline == nil || riskSource.calls != 1 {
		t.Fatalf("pipeline=%#v calls=%d error=%v", pipeline, riskSource.calls, err)
	}
	foreign := lease
	foreign.Account = bybit.Work.Account.ID
	if _, err = factory.CrossExchange(context.Background(), foreign, input, facts); err == nil {
		t.Fatal("peer account masqueraded as coordinator")
	}
}

func crossSagaPipelineTestInput(t *testing.T, now time.Time, facts SandboxSagaPlanFacts,
	source *sagaMarketViewSource,
) (crossarb.Input, domain.Balance) {
	t.Helper()
	bybit := facts.Admissions[sandbox.ExchangeBybit]
	snapshot := facts.Snapshots[bybit.Work.Account.ID]
	ten, _ := domain.ParseBalance("10")
	zero, _ := domain.ParseBalance("0")
	snapshot.Balances = append(snapshot.Balances,
		sandbox.Balance{Asset: "USDT", Available: ten, Reserved: zero})
	facts.Snapshots[bybit.Work.Account.ID] = snapshot
	work := facts.Coordinator.Work
	reader, _ := NewSandboxSagaMarketInputReader(source)
	market, err := reader.ReadCrossExchange(context.Background(), work, now)
	if err != nil {
		t.Fatal(err)
	}
	riskMarkets := make(map[sandbox.AccountID]sandbox.StrategyMarketInput, 2)
	for _, admission := range facts.Admissions {
		riskMarkets[admission.Work.Account.ID], err = market.RiskMarket(admission.Work)
		if err != nil {
			t.Fatal(err)
		}
	}
	riskInputs := sagaRiskInputsForBuilder(t, facts, riskMarkets, now)
	input, err := BuildCrossExchangeSandboxInput(work, enabledSagaProduct(t), market, facts, riskInputs)
	if err != nil {
		t.Fatal(err)
	}
	return input, ten
}
