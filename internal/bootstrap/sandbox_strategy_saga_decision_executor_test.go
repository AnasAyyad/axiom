package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/replay"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/triangular"
)

func TestSandboxStrategySagaDecisionExecutorWaitsForAtomicRiskProjection(t *testing.T) {
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	facts := sagaPlanFacts(t, sandbox.StrategyTriangular, now)
	facts, record := bindSagaFactsConfiguration(t, facts, enabledSagaProduct(t))
	work := facts.Coordinator.Work
	marketReader, _ := NewSandboxSagaMarketInputReader(sagaMarketSource(t, now, triangularSagaKeys(t)))
	market, err := marketReader.ReadTriangular(context.Background(), work, now)
	if err != nil {
		t.Fatal(err)
	}
	factsSource := &sagaExecutorFactsSource{facts: facts}
	markets := &sagaExecutorMarketReader{triangular: market}
	riskProjector := &sagaExecutorRiskProjector{}
	recorder := &sagaExecutorDecisionRecorder{}
	executor, err := NewSandboxStrategySagaDecisionExecutor(factsSource, markets,
		riskProjector, &sagaExecutorPipelines{}, recorder)
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := executor.EvaluateStrategySession(context.Background(), work, record,
		readinessExecutionLease(work), now)
	if err != nil || evaluation.State != sandbox.StrategySessionEvaluationWaiting ||
		evaluation.Reason != "waiting_for_multileg_risk" || factsSource.calls != 1 ||
		markets.calls != 1 || riskProjector.calls != 1 || recorder.calls != 0 {
		t.Fatalf("evaluation=%#v error=%v facts=%d market=%d risk=%d decisions=%d",
			evaluation, err, factsSource.calls, markets.calls, riskProjector.calls, recorder.calls)
	}
}

func TestSandboxStrategySagaDecisionExecutorProjectsBothCrossAccountsAtomically(t *testing.T) {
	now := time.Date(2026, 8, 9, 18, 3, 0, 0, time.UTC)
	facts := sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now)
	facts, record := bindSagaFactsConfiguration(t, facts, enabledSagaProduct(t))
	work := facts.Coordinator.Work
	instrument := sagaInstrument(t, work.Instrument)
	marketReader, _ := NewSandboxSagaMarketInputReader(sagaMarketSource(t, now, []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: instrument}, {Exchange: "bybit", Instrument: instrument},
	}))
	market, err := marketReader.ReadCrossExchange(context.Background(), work, now)
	if err != nil {
		t.Fatal(err)
	}
	factsSource := &sagaExecutorFactsSource{facts: facts}
	markets := &sagaExecutorMarketReader{cross: market}
	riskProjector := &sagaExecutorRiskProjector{}
	executor, err := NewSandboxStrategySagaDecisionExecutor(factsSource, markets,
		riskProjector, &sagaExecutorPipelines{}, &sagaExecutorDecisionRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := executor.EvaluateStrategySession(context.Background(), work, record,
		readinessExecutionLease(work), now)
	if err != nil || evaluation.State != sandbox.StrategySessionEvaluationWaiting ||
		evaluation.Reason != "waiting_for_multileg_risk" || riskProjector.calls != 1 ||
		riskProjector.members != 2 || markets.calls != 1 {
		t.Fatalf("evaluation=%#v error=%v risk=%d members=%d market=%d",
			evaluation, err, riskProjector.calls, riskProjector.members, markets.calls)
	}
}

func TestSandboxStrategySagaDecisionExecutorKeepsBybitAsPeerOnly(t *testing.T) {
	now := time.Date(2026, 8, 9, 18, 5, 0, 0, time.UTC)
	work, _ := readinessWorkAndConfiguration(t, sandbox.StrategyCrossExchangeArbitrage, now)
	work.Account.ID = "bybit-account"
	work.Account.Exchange = sandbox.ExchangeBybit
	product, err := config.DefaultSandboxConfiguration(config.ModeDemo)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := config.NewSnapshot(product, config.SourceAdmin, "bybit-saga-test", &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(product)
	if err != nil {
		t.Fatal(err)
	}
	work.ConfigurationHash = snapshot.Hash()
	record := sandbox.StrategySessionConfiguration{ID: work.ConfigurationID, Hash: snapshot.Hash(), Payload: payload}
	executor, err := NewSandboxStrategySagaDecisionExecutor(&sagaExecutorFactsSource{},
		&sagaExecutorMarketReader{}, &sagaExecutorRiskProjector{}, &sagaExecutorPipelines{},
		&sagaExecutorDecisionRecorder{})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := executor.EvaluateStrategySession(context.Background(), work, record,
		readinessExecutionLease(work), now)
	if err != nil || evaluation.State != sandbox.StrategySessionEvaluationWaiting ||
		evaluation.Reason != "waiting_for_binance_coordinator" {
		t.Fatalf("evaluation=%#v error=%v", evaluation, err)
	}
}

func TestSandboxStrategySagaDecisionExecutorRecordsBoundedNoPlanEvidence(t *testing.T) {
	now := time.Date(2026, 8, 9, 18, 10, 0, 0, time.UTC)
	facts := sagaPlanFacts(t, sandbox.StrategyTriangular, now)
	work := facts.Coordinator.Work
	recorder := &sagaExecutorDecisionRecorder{}
	executor := &SandboxStrategySagaDecisionExecutor{decisions: recorder}
	event := replay.Event{Ordinal: 7, LogicalTime: 11, Canonical: []byte(`{"input":"complete"}`)}
	lease := readinessExecutionLease(work)
	if err := executor.recordNoPlan(context.Background(), lease, facts.Coordinator,
		event, "central_risk_rejected", now); err != nil {
		t.Fatal(err)
	}
	if recorder.calls != 1 || recorder.evidence.DecisionID == "" ||
		recorder.evidence.EventOrdinal != event.Ordinal || recorder.evidence.EventLogicalTime != event.LogicalTime {
		t.Fatalf("recorder=%#v", recorder)
	}
	if state, reason, record := sandboxSagaPipelineOutcome(
		fmt.Errorf("sandbox:strategy_saga_pipeline_risk_failed")); state != sandbox.StrategySessionEvaluationEvaluated || reason != "central_risk_rejected" || !record {
		t.Fatalf("state=%s reason=%s record=%t", state, reason, record)
	}
}

type sagaExecutorFactsSource struct {
	facts SandboxSagaPlanFacts
	calls int
}

func (source *sagaExecutorFactsSource) SandboxStrategySagaPlanFacts(
	context.Context,
	sandbox.StrategySessionWork,
	sandbox.StrategySessionExecutionLease,
	time.Time,
) (SandboxSagaPlanFacts, error) {
	source.calls++
	return source.facts, nil
}

type sagaExecutorMarketReader struct {
	triangular SandboxTriangularMarketInput
	cross      SandboxCrossExchangeMarketInput
	calls      int
}

func (reader *sagaExecutorMarketReader) ReadTriangular(
	context.Context,
	sandbox.StrategySessionWork,
	time.Time,
) (SandboxTriangularMarketInput, error) {
	reader.calls++
	return reader.triangular, nil
}

func (reader *sagaExecutorMarketReader) ReadCrossExchange(
	context.Context,
	sandbox.StrategySessionWork,
	time.Time,
) (SandboxCrossExchangeMarketInput, error) {
	reader.calls++
	return reader.cross, nil
}

type sagaExecutorRiskProjector struct {
	calls   int
	members int
}

func (projector *sagaExecutorRiskProjector) ProjectStrategySagaRiskInputs(
	_ context.Context,
	_ sandbox.StrategySessionExecutionLease,
	_ sandbox.StrategySessionWork,
	members []sandbox.StrategySagaRiskProjectionMember,
	_ time.Time,
) (*sandbox.StrategySagaRiskInputs, error) {
	projector.calls++
	projector.members = len(members)
	return nil, fmt.Errorf("risk baseline unavailable")
}

func (*sagaExecutorPipelines) CrossExchange(
	context.Context,
	sandbox.StrategySessionExecutionLease,
	crossarb.Input,
	SandboxSagaPlanFacts,
) (*sandbox.SagaPlanPipeline, error) {
	return nil, fmt.Errorf("not reached")
}

type sagaExecutorPipelines struct{}

func (*sagaExecutorPipelines) Triangular(
	context.Context,
	sandbox.StrategySessionExecutionLease,
	triangular.Input,
	SandboxSagaPlanFacts,
) (*sandbox.SagaPlanPipeline, error) {
	return nil, fmt.Errorf("not reached")
}

type sagaExecutorDecisionRecorder struct {
	calls    int
	evidence sandbox.StrategyDecisionEvidence
}

func (recorder *sagaExecutorDecisionRecorder) RecordSandboxStrategyDecision(
	_ context.Context,
	_ string,
	_ uint64,
	_ sandbox.StrategySessionWork,
	evidence sandbox.StrategyDecisionEvidence,
	_ time.Time,
) error {
	recorder.calls++
	recorder.evidence = evidence
	return nil
}

func bindSagaFactsConfiguration(
	t *testing.T,
	facts SandboxSagaPlanFacts,
	product config.Configuration,
) (SandboxSagaPlanFacts, sandbox.StrategySessionConfiguration) {
	t.Helper()
	snapshot, err := config.NewSnapshot(product, config.SourceAdmin, "saga-test", &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(product)
	if err != nil {
		t.Fatal(err)
	}
	for exchange, admission := range facts.Admissions {
		admission.Work.ConfigurationHash = snapshot.Hash()
		facts.Admissions[exchange] = admission
		if admission.Work.Account.ID == facts.Coordinator.Work.Account.ID {
			facts.Coordinator = admission
		}
	}
	return facts, sandbox.StrategySessionConfiguration{ID: facts.Coordinator.Work.ConfigurationID,
		Hash: snapshot.Hash(), Payload: payload}
}

func triangularSagaKeys(t *testing.T) []runtimecore.MarketKey {
	t.Helper()
	return []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: sagaInstrument(t, "BTCUSDT")},
		{Exchange: "binance", Instrument: sagaInstrument(t, "ETHBTC")},
		{Exchange: "binance", Instrument: sagaInstrument(t, "ETHUSDT")},
	}
}
