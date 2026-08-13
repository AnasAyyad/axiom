package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
)

func TestTrendPositionProjectionUsesPartialActualEntryAndExitFills(t *testing.T) {
	configuration, err := trend.NewConfiguration(config.DefaultConfiguration().Trend)
	if err != nil {
		t.Fatal(err)
	}
	atr, _ := domain.ParsePrice("2")
	entryPrice, _ := domain.ParsePrice("100")
	entryQuantity, _ := domain.ParseQuantity("0.4")
	fee, _ := domain.ParseFee("0")
	fillID, _ := domain.NewVirtualFillID("trend-partial-entry")
	entry := sandbox.StrategyPlanExecution{CumulativeQuantity: entryQuantity,
		Fills: []execution.FillFact{{ID: fillID, Quantity: entryQuantity, Price: entryPrice, Fee: fee, Ordinal: 1}}}
	position, err := applyTrendExecution(trend.PositionState{}, trend.Decision{
		Action: trend.ActionEntry, Explanation: trend.Explanation{ATR14: atr},
	}, entry, configuration)
	if err != nil || !position.Open || position.Quantity.Compare(entryQuantity) != 0 ||
		position.ActualEntryPrice.Compare(entryPrice) != 0 || position.InitialStop.String() != "95" {
		t.Fatalf("partial entry position=%#v error=%v", position, err)
	}
	exitQuantity, _ := domain.ParseQuantity("0.1")
	position, err = applyTrendExecution(position, trend.Decision{
		Action: trend.ActionExit, CooldownStart: 3,
	}, sandbox.StrategyPlanExecution{CumulativeQuantity: exitQuantity}, configuration)
	if err != nil || position.Quantity.String() != "0.3" || position.CooldownRemaining != 0 {
		t.Fatalf("partial exit position=%#v error=%v", position, err)
	}
	position, err = applyTrendExecution(position, trend.Decision{
		Action: trend.ActionExit, CooldownStart: 3,
	}, sandbox.StrategyPlanExecution{CumulativeQuantity: position.Quantity}, configuration)
	if err != nil || position.Open || position.CooldownRemaining != 3 {
		t.Fatalf("full exit position=%#v error=%v", position, err)
	}
}

func TestMeanReversionPositionProjectionDoesNotAverageDownOrRoundExit(t *testing.T) {
	configuration, err := meanreversion.NewConfiguration(config.DefaultMultiStrategyConfiguration().MeanReversion)
	if err != nil {
		t.Fatal(err)
	}
	entryPrice, _ := domain.ParsePrice("100")
	atr, _ := domain.ParsePrice("2")
	quantity, _ := domain.ParseQuantity("0.4")
	position, err := meanreversion.OpenPosition(entryPrice, atr, quantity, configuration)
	if err != nil {
		t.Fatal(err)
	}
	exitQuantity, _ := domain.ParseQuantity("0.1")
	position, err = applyMeanReversionExecution(position, meanreversion.Decision{
		Action: meanreversion.ActionExit, CooldownStart: 3,
	}, sandbox.StrategyPlanExecution{CumulativeQuantity: exitQuantity}, configuration)
	if err != nil || position.Quantity.String() != "0.3" || position.CooldownRemaining != 0 {
		t.Fatalf("partial exit position=%#v error=%v", position, err)
	}
	if _, err = applyMeanReversionExecution(position, meanreversion.Decision{
		Action: meanreversion.ActionEntry, Explanation: meanreversion.Explanation{ATR14: atr},
	}, sandbox.StrategyPlanExecution{CumulativeQuantity: exitQuantity}, configuration); err == nil {
		t.Fatal("entry while position remains open was accepted")
	}
}

func TestTrendPositionProjectorReplaysCanonicalDecisionAndActualFill(t *testing.T) {
	fixture := trendPositionProjectionFixture(t)
	projector, err := NewSandboxStrategyPositionProjector(
		projectorJournal{entries: []sandbox.StrategyDecisionJournalEntry{fixture.entry}},
		projectorExecutions{facts: map[string]sandbox.StrategyPlanExecution{fixture.entry.PlanID: fixture.execution}},
	)
	if err != nil {
		t.Fatal(err)
	}
	position, err := projector.TrendPosition(context.Background(), fixture.work,
		fixture.configuration, fixture.now)
	if err != nil || !position.Open || position.Quantity.Compare(fixture.quantity) != 0 ||
		position.ActualEntryPrice.Compare(fixture.price) != 0 {
		t.Fatalf("projected position=%#v error=%v", position, err)
	}
}

type trendPositionProjectionTestFixture struct {
	work          sandbox.StrategySessionWork
	configuration trend.Configuration
	entry         sandbox.StrategyDecisionJournalEntry
	execution     sandbox.StrategyPlanExecution
	quantity      domain.Quantity
	price         domain.Price
	now           time.Time
}

func trendPositionProjectionFixture(t *testing.T) trendPositionProjectionTestFixture {
	t.Helper()
	configuration := config.DefaultConfiguration()
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	input := ownerConsoleE2ETrendInput(t, configuration, now)
	trendConfiguration, err := trend.NewConfiguration(configuration.Trend)
	if err != nil {
		t.Fatal(err)
	}
	evaluator, err := trend.NewEvaluator(trendConfiguration)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := evaluator.Evaluate(input)
	if err != nil || decision.Action != trend.ActionEntry || decision.Candidate == nil {
		t.Fatalf("fixture decision=%#v error=%v", decision, err)
	}
	canonicalInput, _ := json.Marshal(input)
	canonicalDecision, _ := json.Marshal(decision)
	work := sandbox.StrategySessionWork{SessionID: "projector-session", Strategy: sandbox.StrategyTrend,
		Instrument: "BTCUSDT", Account: sandbox.StrategySessionAccount{ID: "account", Epoch: 1, Exchange: sandbox.ExchangeBinance},
		ConfigurationID: "configuration", ConfigurationHash: projectorHash("configuration"), StrategySetHash: projectorHash("strategy-set"),
		SessionRevision: 1, StrategyRevision: 1, ArmID: "arm", ArmRevision: 1,
		StartedAt: input.Now.Add(-time.Minute), ArmExpiresAt: input.Now.Add(time.Minute)}
	entry := sandbox.StrategyDecisionJournalEntry{Evidence: sandbox.StrategyDecisionEvidence{
		SessionID: work.SessionID, AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
		StrategyRevision: work.StrategyRevision, Strategy: work.Strategy, Instrument: work.Instrument,
		DecisionID: decision.ID.String(), EventOrdinal: input.Ordinal, EventLogicalTime: input.LogicalTime,
		CanonicalInput: canonicalInput, CanonicalDecision: canonicalDecision,
		InputHash: projectorHash(string(canonicalInput)), DecisionHash: projectorHash(string(canonicalDecision)),
	}, PlanID: "plan", OccurredAt: input.Now}
	actualPrice, _ := domain.ParsePrice("301.5")
	fillID, _ := domain.NewVirtualFillID("projector-fill")
	fee, _ := domain.ParseFee("0")
	executionFact := sandbox.StrategyPlanExecution{PlanID: entry.PlanID, Side: domain.SideBuy,
		RequestedQuantity: decision.Candidate.Quantity, CumulativeQuantity: decision.Candidate.Quantity,
		Fills:      []execution.FillFact{{ID: fillID, Quantity: decision.Candidate.Quantity, Price: actualPrice, Fee: fee, Ordinal: 1}},
		ObservedAt: input.Now}
	executionFact.EvidenceHash = sandbox.StrategyPlanExecutionEvidenceHash(executionFact)
	return trendPositionProjectionTestFixture{work: work, configuration: trendConfiguration,
		entry: entry, execution: executionFact, quantity: decision.Candidate.Quantity,
		price: actualPrice, now: input.Now}
}

type projectorJournal struct {
	entries []sandbox.StrategyDecisionJournalEntry
}

func (source projectorJournal) StrategyDecisionJournal(
	context.Context,
	sandbox.StrategySessionWork,
	time.Time,
) ([]sandbox.StrategyDecisionJournalEntry, error) {
	return append([]sandbox.StrategyDecisionJournalEntry(nil), source.entries...), nil
}

type projectorExecutions struct {
	facts map[string]sandbox.StrategyPlanExecution
}

func (source projectorExecutions) StrategyPlanExecution(
	_ context.Context,
	_ sandbox.StrategySessionWork,
	entry sandbox.StrategyDecisionJournalEntry,
	_ time.Time,
) (sandbox.StrategyPlanExecution, error) {
	return source.facts[entry.PlanID], nil
}

func projectorHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
