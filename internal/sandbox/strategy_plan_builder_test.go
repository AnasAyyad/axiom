package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
)

func TestSingleVenueStrategyPlanBuilderBindsSharedStagesToExactAdmission(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	builder, err := NewSingleVenueStrategyPlanBuilder(strategyPlanAdmission(now), strategyPlanSnapshot(t, now, "1"), strategyPlanInventory(t, now, "1"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.BuildStrategyPlan(context.Background(), strategyPlanMaterial(t))
	if err != nil {
		t.Fatal(err)
	}
	if plan.SessionID != "session" || len(plan.Submissions) != 1 ||
		plan.Submissions[0].Style != OrderStyleLimitIOC ||
		plan.Submissions[0].Action != IntentEntry ||
		plan.Reservations[0].Asset != "USDT" ||
		plan.AccountSnapshots["binance-account"].SnapshotHash != strings.Repeat("f", 64) ||
		plan.StrategyDecision == nil || plan.StrategyDecision.DecisionID != strategyPipelineApproved(t).DecisionID.String() ||
		plan.Pipeline.IntentKind != ApprovalStrategyIntent ||
		plan.Pipeline.ValidateFor(plan) != nil ||
		plan.ApprovalHash != plan.Pipeline.HashFor(plan) {
		t.Fatalf("plan=%#v", plan)
	}
	if err = newMemoryDispatcherRepository().ApprovePlan(
		context.Background(), plan, strategyPipelineLimits(), NoKillPoint{},
	); err != nil {
		t.Fatalf("built plan was not accepted by durable-plan contract: %v", err)
	}
}

func TestStrategyPlanRequiresFreshAccountSnapshotReference(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	builder, err := NewSingleVenueStrategyPlanBuilder(strategyPlanAdmission(now), strategyPlanSnapshot(t, now, "1"), strategyPlanInventory(t, now, "1"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.BuildStrategyPlan(context.Background(), strategyPlanMaterial(t))
	if err != nil {
		t.Fatal(err)
	}
	delete(plan.AccountSnapshots, "binance-account")
	plan.ApprovalHash = plan.Pipeline.HashFor(plan)
	if plan.Pipeline.ValidateFor(plan) == nil {
		t.Fatal("strategy plan without snapshot proof accepted")
	}
}

func TestStrategyPlanDecisionEvidenceRejectsTampering(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	builder, err := NewSingleVenueStrategyPlanBuilder(strategyPlanAdmission(now), strategyPlanSnapshot(t, now, "1"), strategyPlanInventory(t, now, "1"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := builder.BuildStrategyPlan(context.Background(), strategyPlanMaterial(t))
	if err != nil {
		t.Fatal(err)
	}
	plan.StrategyDecision.CanonicalDecision = []byte(`{"id":"decision:sandbox-strategy-decision","ordinal":1,"action":"entry","candidate":{},"reason":"tampered"}`)
	if plan.StrategyDecision.ValidForPlan(plan) == nil {
		t.Fatal("tampered strategy decision evidence was accepted")
	}
	plan.StrategyDecision.DecisionHash = strategyDecisionHash(plan.StrategyDecision.CanonicalDecision)
	if plan.StrategyDecision.ValidForPlan(plan) != nil ||
		plan.Pipeline.HashFor(plan) == plan.ApprovalHash {
		t.Fatal("strategy decision evidence was not bound to approval hash")
	}
}

func TestSingleVenueStrategyPlanBuilderRejectsMultiLegAndMismatchedInstrument(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	builder, err := NewSingleVenueStrategyPlanBuilder(strategyPlanAdmission(now), strategyPlanSnapshot(t, now, "1"), strategyPlanInventory(t, now, "1"))
	if err != nil {
		t.Fatal(err)
	}
	material := strategyPlanMaterial(t)
	material.Plan.Legs = append(material.Plan.Legs, material.Plan.Legs[0])
	if _, err = builder.BuildStrategyPlan(context.Background(), material); err == nil {
		t.Fatal("multi-leg strategy plan accepted by single-venue builder")
	}
	material = strategyPlanMaterial(t)
	eth, err := domain.NewSpotInstrument("ETH", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	material.Plan.Legs[0].Instrument = eth
	if _, err = builder.BuildStrategyPlan(context.Background(), material); err == nil {
		t.Fatal("mismatched instrument accepted")
	}
}

func TestSingleVenueStrategyPlanBuilderReservesOwnedBaseForExit(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	builder, err := NewSingleVenueStrategyPlanBuilder(strategyPlanAdmission(now), strategyPlanSnapshot(t, now, "0.009"), strategyPlanInventory(t, now, "0.009"))
	if err != nil {
		t.Fatal(err)
	}
	material := strategyPlanMaterial(t)
	material.Plan.Legs[0].Side = domain.SideSell
	if _, err = builder.BuildStrategyPlan(context.Background(), material); err == nil {
		t.Fatal("unowned sell plan accepted")
	}
}

func TestSingleVenueStrategyPlanBuilderAllowsOnlyStrategyOwnedExit(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	builder, err := NewSingleVenueStrategyPlanBuilder(strategyPlanAdmission(now), strategyPlanSnapshot(t, now, "1"), strategyPlanInventory(t, now, "1"))
	if err != nil {
		t.Fatal(err)
	}
	material := strategyPlanMaterial(t)
	material.Plan.Legs[0].Side = domain.SideSell
	plan, err := builder.BuildStrategyPlan(context.Background(), material)
	if err != nil || plan.Submissions[0].Action != IntentExit ||
		plan.Reservations[0].Asset != "BTC" ||
		plan.Reservations[0].Quantity != material.Plan.Legs[0].Quantity.String() ||
		len(plan.EntrySafety) != 0 {
		t.Fatalf("plan=%#v error=%v", plan, err)
	}
}

func TestSingleVenueStrategyPlanBuilderBindsFreshAccountSnapshotIntoEvidence(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	admission := strategyPlanAdmission(now)
	firstSnapshot := strategyPlanSnapshot(t, now, "1")
	secondSnapshot := firstSnapshot
	secondSnapshot.SnapshotHash = strings.Repeat("1", 64)
	first, err := NewSingleVenueStrategyPlanBuilder(admission, firstSnapshot, strategyPlanInventory(t, now, "1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewSingleVenueStrategyPlanBuilder(admission, secondSnapshot, strategyPlanInventory(t, now, "1"))
	if err != nil {
		t.Fatal(err)
	}
	firstPlan, err := first.BuildStrategyPlan(context.Background(), strategyPlanMaterial(t))
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, err := second.BuildStrategyPlan(context.Background(), strategyPlanMaterial(t))
	if err != nil {
		t.Fatal(err)
	}
	if firstPlan.Pipeline.AssetApprovalHash == secondPlan.Pipeline.AssetApprovalHash ||
		firstPlan.ApprovalHash == secondPlan.ApprovalHash {
		t.Fatal("account snapshot identity was not bound to plan evidence")
	}
}

func TestSingleVenueStrategyPlanBuilderRejectsAccountOnlyInventoryForExit(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	builder, err := NewSingleVenueStrategyPlanBuilder(strategyPlanAdmission(now),
		strategyPlanSnapshot(t, now, "1"), strategyPlanInventory(t, now, "0"))
	if err != nil {
		t.Fatal(err)
	}
	material := strategyPlanMaterial(t)
	material.Plan.Legs[0].Side = domain.SideSell
	if _, err = builder.BuildStrategyPlan(context.Background(), material); err == nil {
		t.Fatal("account-owned but session-unowned exit accepted")
	}
}

func TestStrategyOwnedInventoryRequiresExactSessionAccountAndDecisionTime(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	admission := strategyPlanAdmission(now)
	for _, mutate := range []func(*StrategyOwnedInventory){
		func(value *StrategyOwnedInventory) { value.SessionID = "other-session" },
		func(value *StrategyOwnedInventory) { value.AccountEpoch++ },
		func(value *StrategyOwnedInventory) { value.ObservedAt = now.Add(-time.Millisecond) },
		func(value *StrategyOwnedInventory) { value.EvidenceHash = "not-a-hash" },
	} {
		inventory := strategyPlanInventory(t, now, "1")
		mutate(&inventory)
		if inventory.ValidFor(admission, "BTC") == nil {
			t.Fatalf("invalid strategy inventory accepted: %#v", inventory)
		}
	}
}

func strategyPlanAdmission(now time.Time) StrategySessionAdmission {
	return StrategySessionAdmission{
		Work: StrategySessionWork{SessionID: "session", Strategy: StrategyTrend, Instrument: "BTCUSDT",
			Account:         StrategySessionAccount{ID: "binance-account", Epoch: 7, Exchange: ExchangeBinance},
			ConfigurationID: "configuration", ConfigurationHash: strings.Repeat("c", 64), StrategySetHash: strings.Repeat("a", 64),
			SessionRevision: 3, StrategyRevision: 4, ArmID: "arm", ArmRevision: 5,
			StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute)},
		Arm:         strategySessionTestArm(now),
		Eligibility: EligibilitySnapshot{ObservedAt: now, Exchange: "binance", Instrument: "BTCUSDT", Eligible: true},
		Safety: EntrySafetySnapshot{AccountID: "binance-account", AccountEpoch: 7, Exchange: ExchangeBinance,
			ObservedAt: now, State: EngineArmed, ArmActive: true, GlobalIntegrationEnabled: true,
			GlobalSubmissionEnabled: true, ExchangeIntegrationEnabled: true, ExchangeSubmissionEnabled: true,
			PublicEligible: true, PrivateStreamHealthy: true, AccountStateFresh: true,
			ReconciliationClean: true, LeaseHeld: true, EvidenceHealthy: true,
			OpenCapacityAvailable: true, DailyCapacityAvailable: true},
		StartupCycle: 1, ApprovedAt: now,
	}
}

func strategyPlanMaterial(t *testing.T) StrategyPipelineMaterial {
	t.Helper()
	planID, err := domain.NewExecutionPlanID("strategy-pipeline-plan")
	if err != nil {
		t.Fatal(err)
	}
	orderID, err := domain.NewVirtualOrderID("strategy-pipeline-order")
	if err != nil {
		t.Fatal(err)
	}
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	quantity, err := domain.ParseQuantity("0.01")
	if err != nil {
		t.Fatal(err)
	}
	price, err := domain.ParsePrice("100")
	if err != nil {
		t.Fatal(err)
	}
	approved := strategyPipelineApproved(t)
	return StrategyPipelineMaterial{Event: strategyPipelineEvent(), DecisionEvidence: []byte(`{"id":"decision:sandbox-strategy-decision","ordinal":1,"action":"entry","candidate":{}}`),
		Candidate: backtest.Candidate{Ordinal: 1, Payload: []byte(`{"candidate":true}`)},
		Allocated: backtest.AllocatedIntent{Ordinal: 1, Payload: []byte(`{"allocation":true}`)},
		Approved:  approved, Plan: execution.SimulatedPlan{ID: planID, Intent: approved, Namespace: "strategy-pipeline",
			DecisionLogicalTime: 1, Legs: []execution.PlannedLeg{{Index: 0, OrderID: orderID,
				ClientOrderID: "strategy-client-1", Instrument: instrument, Side: domain.SideBuy,
				Quantity: quantity, LimitPrice: price}}}}
}

func strategyPlanSnapshot(t *testing.T, now time.Time, btc string) AccountSnapshot {
	t.Helper()
	availableBTC, err := domain.ParseBalance(btc)
	if err != nil {
		t.Fatal(err)
	}
	availableUSDT, err := domain.ParseBalance("10")
	if err != nil {
		t.Fatal(err)
	}
	zero, err := domain.ParseBalance("0")
	if err != nil {
		t.Fatal(err)
	}
	return AccountSnapshot{AccountID: "binance-account", Epoch: 7,
		Balances: []Balance{{Asset: "BTC", Available: availableBTC, Reserved: zero},
			{Asset: "USDT", Available: availableUSDT, Reserved: zero}},
		OrdersHash: strings.Repeat("d", 64), FillsHash: strings.Repeat("e", 64),
		SnapshotHash: strings.Repeat("f", 64), ObservedAt: now}
}

func strategyPlanInventory(t *testing.T, now time.Time, available string) StrategyOwnedInventory {
	t.Helper()
	balance, err := domain.ParseBalance(available)
	if err != nil {
		t.Fatal(err)
	}
	return StrategyOwnedInventory{SessionID: "session", AccountID: "binance-account",
		AccountEpoch: 7, Asset: "BTC", Available: balance,
		EvidenceHash: strings.Repeat("b", 64), ObservedAt: now}
}
