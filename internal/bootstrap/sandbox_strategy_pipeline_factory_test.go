package bootstrap

import (
	"context"
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/risk"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/trend"
)

func TestSandboxStrategyPipelineFactoryBuildsBoundSharedStagesBeforeEvaluation(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work, market, facts := validSandboxTrendInputBuilderFacts(t, now)
	facts.MaximumReserved = facts.CentralRiskFacts.MaximumReserved
	admission := sizingFactsAdmission(work, now)
	zero, err := domain.ParseBalance("0")
	if err != nil {
		t.Fatal(err)
	}
	inventory := sandbox.StrategyOwnedInventory{SessionID: work.SessionID, AccountID: work.Account.ID,
		AccountEpoch: work.Account.Epoch, Asset: market.Instrument.Base, Available: zero,
		EvidenceHash: strings.Repeat("9", 64), ObservedAt: now}
	observation := pipelineFactoryRiskObservation(t, work, facts, market, now)
	source := &pipelineFactoryObservationSource{observation: observation}
	riskSource := &pipelineFactoryRiskSource{engine: normalPipelineFactoryRiskEngine(t, now)}
	factory, err := NewSandboxStrategyPipelineFactory(source, riskSource, pipelineFactoryRepository{})
	if err != nil {
		t.Fatal(err)
	}
	dependencies, err := factory.SandboxStrategyPipelineDependencies(
		context.Background(), admission, facts, market, inventory, pipelineFactoryTrendAdapter(t),
	)
	if err != nil || source.calls != 1 || riskSource.calls != 1 || dependencies.Allocator == nil || dependencies.Risk == nil ||
		dependencies.Planner == nil || dependencies.Store == nil || dependencies.Kill == nil {
		t.Fatalf("dependencies=%#v calls=%d error=%v", dependencies, source.calls, err)
	}
}

func normalPipelineFactoryRiskEngine(t *testing.T, now time.Time) *risk.Engine {
	t.Helper()
	engine, err := risk.NewEngine(&pipelineFactoryRiskAudit{}, pipelineFactoryRiskAlerts{})
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.ManualTransition(risk.StateNormal, risk.RecoveryEvidence{Reconciled: true,
		PersistenceHealthy: true, BooksFresh: true, UnknownOrdersResolved: true,
		Reauthenticated: true, AuditDurable: true, Actor: "engine-startup",
		Reason: "restored durable normal posture", At: now}); err != nil {
		t.Fatal(err)
	}
	return engine
}

func pipelineFactoryTrendAdapter(t *testing.T) *trend.Adapter {
	t.Helper()
	product, err := config.DefaultSandboxConfiguration(config.ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	configured, err := trend.NewConfiguration(product.Trend)
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := trend.NewEvaluator(configured)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := trend.NewAdapter(evaluator)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestSandboxStrategyPipelineFactoryRefusesPausedRiskBeforeObservationRead(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work, market, facts := validSandboxTrendInputBuilderFacts(t, now)
	admission := sizingFactsAdmission(work, now)
	zero, _ := domain.ParseBalance("0")
	inventory := sandbox.StrategyOwnedInventory{SessionID: work.SessionID, AccountID: work.Account.ID,
		AccountEpoch: work.Account.Epoch, Asset: market.Instrument.Base, Available: zero,
		EvidenceHash: strings.Repeat("9", 64), ObservedAt: now}
	source := &pipelineFactoryObservationSource{}
	engine, _ := risk.NewEngine(&pipelineFactoryRiskAudit{}, pipelineFactoryRiskAlerts{})
	riskSource := &pipelineFactoryRiskSource{engine: engine}
	factory, err := NewSandboxStrategyPipelineFactory(source, riskSource, pipelineFactoryRepository{})
	if err != nil {
		t.Fatal(err)
	}
	configured, _ := trend.NewConfiguration(config.DefaultConfiguration().Trend)
	evaluator, _ := trend.NewEvaluator(configured)
	adapter, _ := trend.NewAdapter(evaluator)
	if _, err = factory.SandboxStrategyPipelineDependencies(context.Background(), admission,
		facts, market, inventory, adapter); err == nil || source.calls != 0 || riskSource.calls != 1 {
		t.Fatalf("paused engine composition error=%v observation calls=%d risk calls=%d", err, source.calls, riskSource.calls)
	}
}

func TestStrategyAllocationLimitsSubtractExistingReservedCapital(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	_, _, facts := validSandboxTrendInputBuilderFacts(t, now)
	available, _ := domain.ParseBalance("100")
	reserved, _ := domain.ParseBalance("9")
	facts.AccountSnapshot.Balances = []sandbox.Balance{{Asset: "USDT", Available: available, Reserved: reserved}}
	facts.MaximumReserved = mustSizingFactsMoney(t, "15")
	facts.MaximumOrderNotional = mustSizingFactsMoney(t, "10")
	limits, err := strategyAllocationLimits(facts)
	if err != nil || limits.MaximumReserved.String() != "6" || limits.MaximumOrderAmount.String() != "6" {
		t.Fatalf("limits=%#v error=%v", limits, err)
	}
}

type pipelineFactoryObservationSource struct {
	observation sandbox.StrategyRiskObservation
	calls       int
}

type pipelineFactoryRiskSource struct {
	engine *risk.Engine
	calls  int
}

func (source *pipelineFactoryRiskSource) SandboxStrategyRiskEngine(
	context.Context,
	time.Time,
) (*risk.Engine, error) {
	source.calls++
	return source.engine, nil
}

func (source *pipelineFactoryObservationSource) StrategyRiskObservation(
	context.Context,
	sandbox.StrategySessionWork,
	sandbox.AccountSnapshot,
	sandbox.StrategyMarketInput,
	sandbox.StrategyRiskFacts,
	time.Time,
) (sandbox.StrategyRiskObservation, error) {
	source.calls++
	return source.observation, nil
}

func pipelineFactoryRiskObservation(
	t *testing.T,
	work sandbox.StrategySessionWork,
	facts SandboxStrategySizingFacts,
	market sandbox.StrategyMarketInput,
	now time.Time,
) sandbox.StrategyRiskObservation {
	t.Helper()
	zero, err := domain.ParsePercent("0")
	if err != nil {
		t.Fatal(err)
	}
	one, err := domain.ParsePercent("1")
	if err != nil {
		t.Fatal(err)
	}
	return sandbox.StrategyRiskObservation{StrategySessionID: work.SessionID,
		StrategyRevision: facts.PositionRevision, AccountID: work.Account.ID,
		AccountEpoch: work.Account.Epoch, SnapshotHash: facts.AccountSnapshot.SnapshotHash,
		MarketHash: sandbox.StrategyMarketEvidenceHash(market), Instrument: work.Instrument,
		PolicyID: facts.RiskPolicyID, PolicyVersion: facts.RiskPolicyVersion,
		PolicyHash: facts.RiskPolicyHash, AccountDrawdown: zero, UTCDayLoss: zero,
		Rolling24HourLoss: zero, StrategyLoss: zero, AssetExposure: zero,
		CombinedExposure: zero, ExchangeExposure: zero, Reserve: one,
		ReservedCapital: zero, Spread: zero, Slippage: zero, QualityScore: 100,
		ObservedAt: now}
}

type pipelineFactoryRiskAudit struct{}

func (*pipelineFactoryRiskAudit) Append(risk.AuditEvent) error { return nil }

type pipelineFactoryRiskAlerts struct{}

func (pipelineFactoryRiskAlerts) Emit(string, risk.Action, risk.State) error { return nil }

type pipelineFactoryRepository struct{}

func (pipelineFactoryRepository) ApprovePlan(context.Context, sandbox.ApprovedSandboxPlan, sandbox.SubmissionLimits, sandbox.KillPoint) error {
	return nil
}
func (pipelineFactoryRepository) ClaimOutbox(context.Context, sandbox.AccountID, uint64, string, uint64, time.Time, time.Duration, int, sandbox.KillPoint) ([]sandbox.SubmissionOutbox, error) {
	return nil, nil
}
func (pipelineFactoryRepository) MarkSubmitting(context.Context, string, uint64, time.Time, sandbox.KillPoint) error {
	return nil
}
func (pipelineFactoryRepository) MarkUnknown(context.Context, string, uint64, time.Time, sandbox.KillPoint) error {
	return nil
}
func (pipelineFactoryRepository) MarkCancelPending(context.Context, sandbox.AccountID, uint64, string, string, uint64, time.Time, sandbox.KillPoint) (string, error) {
	return "", nil
}
func (pipelineFactoryRepository) MarkCancelUnknown(context.Context, string, uint64, time.Time, sandbox.KillPoint) error {
	return nil
}
func (pipelineFactoryRepository) AppendPrivateEvent(context.Context, string, uint64, sandbox.PrivateEvent, sandbox.KillPoint) error {
	return nil
}
