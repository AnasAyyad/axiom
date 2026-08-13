package bootstrap

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/replay"
	"axiom/internal/risk"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/trend"
)

func TestSandboxStrategySizingFactsReaderBindsSnapshotAdmissionSequenceAndLease(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work, _ := readinessWorkAndConfiguration(t, sandbox.StrategyTrend, now)
	product, err := config.DefaultSandboxConfiguration(config.ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	store := sizingFactsBaseStore(t, work, now)
	reader, err := NewSandboxStrategySizingFactsReader(store)
	if err != nil {
		t.Fatal(err)
	}
	admission := sizingFactsAdmission(work, now)
	lease := readinessExecutionLease(work)
	facts, err := reader.SandboxStrategySizingFacts(
		context.Background(), work, product, admission, lease, now)
	if err != nil || facts.ValidFor(work, now) != nil || facts.MaximumOrderNotional.String() != "10" ||
		facts.MinimumReserve.String() != "75" || facts.MaximumReserved.String() != "425" ||
		facts.RiskPolicyID != "risk-policy" || facts.RiskPolicyVersion != 7 ||
		facts.RiskPolicyHash != strings.Repeat("6", 64) || facts.FencingToken != lease.Fence ||
		facts.AssetEligibility != store.assetEligibility || facts.EvaluationOrdinal != 1 ||
		facts.EvaluationLogicalTime != 1 || facts.ConfigurationHash != work.ConfigurationHash {
		t.Fatalf("facts=%#v error=%v", facts, err)
	}
	foreign := lease
	foreign.Fence++
	foreignFacts, foreignErr := reader.SandboxStrategySizingFacts(
		context.Background(), work, product, admission, foreign, now)
	if foreignErr != nil || foreignFacts.FencingToken != foreign.Fence || foreignFacts.FencingToken == lease.Fence {
		t.Fatalf("facts=%#v error=%v", foreignFacts, foreignErr)
	}
	store.snapshot.ObservedAt = now.Add(-251 * time.Millisecond)
	if _, err = reader.SandboxStrategySizingFacts(
		context.Background(), work, product, admission, lease, now); err == nil {
		t.Fatal("stale immutable account snapshot accepted")
	}
}

func sizingFactsBaseStore(t *testing.T, work sandbox.StrategySessionWork, now time.Time) *sizingFactsStore {
	t.Helper()
	available, err := domain.ParseBalance("500")
	if err != nil {
		t.Fatal(err)
	}
	return &sizingFactsStore{snapshot: sandbox.AccountSnapshot{
		AccountID: work.Account.ID, Epoch: work.Account.Epoch,
		Balances: []sandbox.Balance{{Asset: "USDT", Available: available}}, OrdersHash: strings.Repeat("1", 64),
		FillsHash: strings.Repeat("2", 64), SnapshotHash: strings.Repeat("3", 64), ObservedAt: now,
	}, assetEligibility: 1, riskFacts: sandbox.StrategyRiskFacts{AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
		SnapshotHash: strings.Repeat("3", 64), PolicyID: "risk-policy", PolicyVersion: 7,
		PolicyHash: strings.Repeat("6", 64), Policy: sizingFactsRiskPolicy("risk-policy", 7), MinimumReserve: mustSizingFactsMoney(t, "75"),
		MaximumReserved: mustSizingFactsMoney(t, "425"), ObservedAt: now}}
}

func TestSandboxStrategySizingFactsReaderRestoresPriorFinalizedCandleTrigger(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work, _ := readinessWorkAndConfiguration(t, sandbox.StrategyTrend, now)
	admission := sizingFactsAdmission(work, now)
	product, store, canonical := priorTriggerSizingFactsFixture(t, work, admission, now)
	reader, err := NewSandboxStrategySizingFactsReader(store)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := reader.SandboxStrategySizingFacts(
		context.Background(), work, product, admission, readinessExecutionLease(work), now)
	expected, triggerErr := sandboxStrategyEvaluationTriggerFromCanonicalInput(work, canonical)
	if err != nil || triggerErr != nil || facts.ValidFor(work, now) != nil ||
		facts.EvaluationOrdinal != 5 || facts.EvaluationLogicalTime != 10 ||
		facts.PriorEvaluationTriggerHash != expected || !projectorHash256(expected) {
		t.Fatalf("facts=%#v expected=%q error=%v trigger_error=%v", facts, expected, err, triggerErr)
	}
}

func priorTriggerSizingFactsFixture(t *testing.T, work sandbox.StrategySessionWork,
	admission sandbox.StrategySessionAdmission, now time.Time,
) (config.Configuration, *sizingFactsStore, []byte) {
	t.Helper()
	product, err := config.DefaultSandboxConfiguration(config.ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	price, err := domain.ParsePrice("100")
	if err != nil {
		t.Fatal(err)
	}
	input := trend.Input{Ordinal: 4, LogicalTime: 9, Instrument: instrument,
		Candles: readinessCandles(instrument, "4h", now, price, 200)}
	canonical, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := sandbox.NewStrategyDecisionEvidence(admission,
		replay.Event{Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: canonical},
		[]byte(`{"id":"decision:prior","ordinal":4,"action":"none"}`))
	if err != nil {
		t.Fatal(err)
	}
	store := sizingFactsBaseStore(t, work, now)
	store.entries = []sandbox.StrategyDecisionJournalEntry{{Evidence: evidence, OccurredAt: now}}
	return product, store, canonical
}

func TestSandboxStrategySizingFactsReaderRejectsMissingOrMismatchedRiskFacts(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work, _ := readinessWorkAndConfiguration(t, sandbox.StrategyTrend, now)
	product, err := config.DefaultSandboxConfiguration(config.ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	available, err := domain.ParseBalance("500")
	if err != nil {
		t.Fatal(err)
	}
	store := &sizingFactsStore{snapshot: sandbox.AccountSnapshot{AccountID: work.Account.ID, Epoch: work.Account.Epoch,
		Balances: []sandbox.Balance{{Asset: "USDT", Available: available}}, OrdersHash: strings.Repeat("1", 64),
		FillsHash: strings.Repeat("2", 64), SnapshotHash: strings.Repeat("3", 64), ObservedAt: now}, assetEligibility: 1}
	reader, err := NewSandboxStrategySizingFactsReader(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reader.SandboxStrategySizingFacts(context.Background(), work, product, sizingFactsAdmission(work, now), readinessExecutionLease(work), now); err == nil {
		t.Fatal("missing durable risk facts accepted")
	}
	store.riskFacts = sandbox.StrategyRiskFacts{AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
		SnapshotHash: strings.Repeat("9", 64), PolicyID: "risk-policy", PolicyVersion: 1,
		PolicyHash: strings.Repeat("6", 64), Policy: sizingFactsRiskPolicy("risk-policy", 1), MinimumReserve: mustSizingFactsMoney(t, "75"),
		MaximumReserved: mustSizingFactsMoney(t, "425"), ObservedAt: now}
	if _, err = reader.SandboxStrategySizingFacts(context.Background(), work, product, sizingFactsAdmission(work, now), readinessExecutionLease(work), now); err == nil {
		t.Fatal("risk facts bound to another snapshot accepted")
	}
}

func TestSandboxStrategySizingFactsReaderRejectsMissingCurrentAssetEligibility(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work, _ := readinessWorkAndConfiguration(t, sandbox.StrategyTrend, now)
	product, err := config.DefaultSandboxConfiguration(config.ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	available, err := domain.ParseBalance("500")
	if err != nil {
		t.Fatal(err)
	}
	store := &sizingFactsStore{snapshot: sandbox.AccountSnapshot{AccountID: work.Account.ID, Epoch: work.Account.Epoch,
		Balances: []sandbox.Balance{{Asset: "USDT", Available: available}}, OrdersHash: strings.Repeat("1", 64),
		FillsHash: strings.Repeat("2", 64), SnapshotHash: strings.Repeat("3", 64), ObservedAt: now},
		riskFacts: sandbox.StrategyRiskFacts{AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
			SnapshotHash: strings.Repeat("3", 64), PolicyID: "risk-policy", PolicyVersion: 1,
			PolicyHash: strings.Repeat("6", 64), Policy: sizingFactsRiskPolicy("risk-policy", 1), MinimumReserve: mustSizingFactsMoney(t, "75"),
			MaximumReserved: mustSizingFactsMoney(t, "425"), ObservedAt: now}}
	reader, err := NewSandboxStrategySizingFactsReader(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reader.SandboxStrategySizingFacts(context.Background(), work, product,
		sizingFactsAdmission(work, now), readinessExecutionLease(work), now); err == nil ||
		!strings.Contains(err.Error(), "asset_eligibility") {
		t.Fatalf("missing current asset eligibility error=%v", err)
	}
}

func sizingFactsRiskPolicy(id string, version uint64) risk.Policy {
	policy := risk.DefaultGlobalPolicy()
	policy.ID, policy.Version, policy.State = id, version, risk.StateNormal
	return policy
}

type sizingFactsStore struct {
	snapshot         sandbox.AccountSnapshot
	entries          []sandbox.StrategyDecisionJournalEntry
	riskFacts        sandbox.StrategyRiskFacts
	assetEligibility uint64
}

func (store *sizingFactsStore) SandboxStrategyAssetEligibility(
	context.Context,
	sandbox.StrategySessionWork,
	time.Time,
) (uint64, error) {
	if store.assetEligibility == 0 {
		return 0, context.Canceled
	}
	return store.assetEligibility, nil
}

func (store *sizingFactsStore) StrategyRiskFacts(
	context.Context,
	sandbox.StrategySessionWork,
	sandbox.AccountSnapshot,
	time.Time,
) (sandbox.StrategyRiskFacts, error) {
	return store.riskFacts, nil
}

func mustSizingFactsMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	money, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatal(err)
	}
	return money
}

func (store *sizingFactsStore) LatestAccountSnapshot(
	context.Context,
	sandbox.AccountID,
	uint64,
) (sandbox.AccountSnapshot, bool, error) {
	return store.snapshot, true, nil
}

func (store *sizingFactsStore) StrategyDecisionJournal(
	context.Context,
	sandbox.StrategySessionWork,
	time.Time,
) ([]sandbox.StrategyDecisionJournalEntry, error) {
	return append([]sandbox.StrategyDecisionJournalEntry(nil), store.entries...), nil
}

func sizingFactsAdmission(work sandbox.StrategySessionWork, now time.Time) sandbox.StrategySessionAdmission {
	return sandbox.StrategySessionAdmission{Work: work, Arm: sandbox.Arm{ID: work.ArmID, SessionID: work.SessionID,
		AccountIDs: []sandbox.AccountID{work.Account.ID}, AuthorizationHash: strings.Repeat("4", 64),
		ActorUserID: "operator", ActorSessionID: "operator-session", ReasonHash: strings.Repeat("5", 64),
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Minute).Add(sandbox.ArmLifetime), Revision: work.ArmRevision},
		Eligibility: sandbox.EligibilitySnapshot{ObservedAt: now, Exchange: string(work.Account.Exchange), Instrument: work.Instrument,
			BookHealth: "healthy", BookHealthy: true, BookFresh: true, BookEligible: true, ClockEligible: true, Eligible: true},
		Safety: sandbox.EntrySafetySnapshot{AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
			Exchange: work.Account.Exchange, ObservedAt: now, State: sandbox.EngineArmed, ArmActive: true,
			GlobalIntegrationEnabled: true, GlobalSubmissionEnabled: true, ExchangeIntegrationEnabled: true,
			ExchangeSubmissionEnabled: true, PublicEligible: true, PrivateStreamHealthy: true,
			AccountStateFresh: true, ReconciliationClean: true, LeaseHeld: true, EvidenceHealthy: true,
			OpenCapacityAvailable: true, DailyCapacityAvailable: true}, StartupCycle: 1, ApprovedAt: now}
}
