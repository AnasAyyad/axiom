package bootstrap

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

func TestAssembleTriangularSandboxSagaPlanKeepsFutureInventoryDependent(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	facts := sagaPlanFacts(t, sandbox.StrategyTriangular, now)
	material := triangularSagaMaterial(t, now)
	plan, err := assembleSandboxSagaPlan(facts, material, execution.DispatchSequential)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := sandbox.ValidatePlanSaga(plan)
	if err != nil || policy != execution.DispatchSequential || len(plan.Submissions) != 3 ||
		len(plan.MarketEligibility) != 3 || plan.StrategyDecision == nil ||
		plan.ExecutionExpiresAt == nil || !plan.ExecutionExpiresAt.Equal(now.Add(250*time.Millisecond)) {
		t.Fatalf("triangular plan=%#v policy=%s error=%v", plan, policy, err)
	}
	if plan.Reservations[0].Asset != "USDT" || plan.Reservations[1].Asset != "BTC" ||
		plan.Reservations[2].Asset != "ETH" {
		t.Fatalf("triangular reservations=%#v", plan.Reservations)
	}
	// The initial snapshot intentionally contains no BTC or ETH. Accepting this
	// proves later legs are not falsely authorized by initial account ownership;
	// dispatcher progression must activate them from prior exact fills.
	if len(facts.Snapshots["binance-account"].Balances) != 1 {
		t.Fatal("triangular fixture unexpectedly pre-owned dependent inventory")
	}
}

func TestAssembleCrossExchangeSandboxSagaPlanRequiresOwnedSellInventory(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 5, 0, 0, time.UTC)
	facts := sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now)
	material := crossExchangeSagaMaterial(t, now)
	plan, err := assembleSandboxSagaPlan(facts, material, execution.DispatchConcurrent)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := sandbox.ValidatePlanSaga(plan)
	if err != nil || policy != execution.DispatchConcurrent || len(plan.Submissions) != 2 ||
		plan.Submissions[0].AccountID == plan.Submissions[1].AccountID ||
		plan.Reservations[1].Asset != "BTC" {
		t.Fatalf("cross plan=%#v policy=%s error=%v", plan, policy, err)
	}
	delete(facts.OwnedInventory, "bybit-account")
	if _, err = assembleSandboxSagaPlan(facts, material, execution.DispatchConcurrent); err == nil {
		t.Fatal("cross-exchange sell used account-wide inventory without strategy ownership")
	}
}

func sagaPlanFacts(
	t *testing.T,
	strategy string,
	now time.Time,
) SandboxSagaPlanFacts {
	t.Helper()
	exchanges := []sandbox.Exchange{sandbox.ExchangeBinance}
	if strategy == sandbox.StrategyCrossExchangeArbitrage {
		exchanges = append(exchanges, sandbox.ExchangeBybit)
	}
	accounts := make([]sandbox.AccountID, 0, len(exchanges))
	for _, exchange := range exchanges {
		accounts = append(accounts, sandbox.AccountID(string(exchange)+"-account"))
	}
	arm := sandbox.Arm{ID: "saga-arm", SessionID: "saga-session", AccountIDs: accounts,
		AuthorizationHash: strings.Repeat("1", 64), ActorUserID: "owner",
		ActorSessionID: "owner-session", ReasonHash: strings.Repeat("2", 64),
		CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(-time.Minute).Add(sandbox.ArmLifetime), Revision: 1}
	facts := SandboxSagaPlanFacts{Admissions: make(map[sandbox.Exchange]sandbox.StrategySessionAdmission),
		Snapshots:      make(map[sandbox.AccountID]sandbox.AccountSnapshot),
		RiskFacts:      make(map[sandbox.AccountID]sandbox.StrategyRiskFacts),
		OwnedInventory: make(map[sandbox.AccountID]sandbox.StrategyOwnedInventory)}
	zero, _ := domain.ParseBalance("0")
	for _, exchange := range exchanges {
		addSagaPlanAccountFacts(&facts, strategy, exchange, arm, now, zero)
	}
	if strategy == sandbox.StrategyTriangular {
		for _, instrument := range []string{"ETHBTC", "ETHUSDT"} {
			facts.MarketEligibility = append(facts.MarketEligibility, sandbox.EligibilitySnapshot{
				ObservedAt: now, Exchange: "binance", Instrument: instrument,
				BookHealth: "healthy", BookHealthy: true, BookFresh: true,
				BookEligible: true, ClockEligible: true, Eligible: true})
		}
	}
	facts.Coordinator = facts.Admissions[sandbox.ExchangeBinance]
	return facts
}

func addSagaPlanAccountFacts(facts *SandboxSagaPlanFacts, strategy string, exchange sandbox.Exchange,
	arm sandbox.Arm, now time.Time, zero domain.Balance,
) {
	account := sandbox.AccountID(string(exchange) + "-account")
	work := sandbox.StrategySessionWork{SessionID: "saga-session", Strategy: strategy,
		Instrument: "BTCUSDT", Account: sandbox.StrategySessionAccount{ID: account, Epoch: 1, Exchange: exchange},
		ConfigurationID: "saga-configuration", ConfigurationHash: strings.Repeat("3", 64),
		StrategySetHash: strings.Repeat("4", 64), SessionRevision: 1, StrategyRevision: 1,
		ArmID: arm.ID, ArmRevision: arm.Revision, StartedAt: now.Add(-time.Minute), ArmExpiresAt: arm.ExpiresAt}
	eligibility := sandbox.EligibilitySnapshot{ObservedAt: now, Exchange: string(exchange),
		Instrument: "BTCUSDT", BookHealth: "healthy", BookHealthy: true, BookFresh: true,
		BookEligible: true, ClockEligible: true, Eligible: true}
	admission := sandbox.StrategySessionAdmission{Work: work, Arm: arm, Eligibility: eligibility,
		Safety: sandbox.EntrySafetySnapshot{AccountID: account, AccountEpoch: 1, Exchange: exchange,
			ObservedAt: now, State: sandbox.EngineArmed, ArmActive: true,
			GlobalIntegrationEnabled: true, GlobalSubmissionEnabled: true,
			ExchangeIntegrationEnabled: true, ExchangeSubmissionEnabled: true,
			PublicEligible: true, PrivateStreamHealthy: true, AccountStateFresh: true,
			ReconciliationClean: true, LeaseHeld: true, EvidenceHealthy: true,
			OpenCapacityAvailable: true, DailyCapacityAvailable: true},
		StartupCycle: 1, ApprovedAt: now}
	facts.Admissions[exchange] = admission
	addSagaPlanAccountBalanceFacts(facts, work, exchange, now, zero)
	facts.MarketEligibility = append(facts.MarketEligibility, eligibility)
}

func addSagaPlanAccountBalanceFacts(facts *SandboxSagaPlanFacts, work sandbox.StrategySessionWork,
	exchange sandbox.Exchange, now time.Time, zero domain.Balance,
) {
	account := work.Account.ID
	if exchange == sandbox.ExchangeBinance {
		available, _ := domain.ParseBalance("10")
		facts.Snapshots[account] = sandbox.AccountSnapshot{AccountID: account, Epoch: 1,
			Balances:   []sandbox.Balance{{Asset: "USDT", Available: available, Reserved: zero}},
			OrdersHash: strings.Repeat("5", 64), FillsHash: strings.Repeat("6", 64),
			SnapshotHash: strings.Repeat("7", 64), ObservedAt: now}
	} else {
		available, _ := domain.ParseBalance("0.1")
		facts.Snapshots[account] = sandbox.AccountSnapshot{AccountID: account, Epoch: 1,
			Balances:   []sandbox.Balance{{Asset: "BTC", Available: available, Reserved: zero}},
			OrdersHash: strings.Repeat("8", 64), FillsHash: strings.Repeat("9", 64),
			SnapshotHash: strings.Repeat("a", 64), ObservedAt: now}
		facts.OwnedInventory[account] = sandbox.StrategyOwnedInventory{SessionID: work.SessionID,
			AccountID: account, AccountEpoch: 1, Asset: "BTC", Available: available,
			EvidenceHash: strings.Repeat("b", 64), ObservedAt: now}
	}
	zeroMoney, _ := domain.ParseMoney("0")
	facts.RiskFacts[account] = sandbox.StrategyRiskFacts{AccountID: account, AccountEpoch: 1,
		SnapshotHash: facts.Snapshots[account].SnapshotHash, PolicyID: "risk-policy", PolicyVersion: 1,
		PolicyHash: strings.Repeat("d", 64), Policy: sizingFactsRiskPolicy("risk-policy", 1),
		MinimumReserve: zeroMoney, MaximumReserved: zeroMoney, ObservedAt: now}
	if work.Strategy == sandbox.StrategyCrossExchangeArbitrage {
		if _, exists := facts.OwnedInventory[account]; !exists {
			facts.OwnedInventory[account] = sandbox.StrategyOwnedInventory{SessionID: work.SessionID,
				AccountID: account, AccountEpoch: 1, Asset: "BTC", Available: zero,
				EvidenceHash: strings.Repeat("c", 64), ObservedAt: now}
		}
	}
}

func triangularSagaMaterial(t *testing.T, now time.Time) sandboxSagaMaterial {
	t.Helper()
	planID, _ := domain.NewExecutionPlanID("automatic-triangular-plan")
	return sandboxSagaMaterial{Strategy: sandbox.StrategyTriangular, PlanID: planID,
		CandidateID: "triangular-candidate", Ordinal: 1, LogicalTime: 1,
		ApprovedAt: now, LifetimeNanos: uint64(250 * time.Millisecond),
		CanonicalInput:     []byte(`{"input":"triangular"}`),
		CanonicalCandidate: []byte(`{"candidate":"triangular"}`),
		AllocationEvidence: []byte(`{"allocation":"active"}`),
		RiskEvidence:       []byte(`{"risk":"approved"}`),
		PlannerEvidence:    []byte(`{"policy":"sequential"}`),
		Legs: []sandboxSagaLegMaterial{
			sagaLeg(t, "automatic-triangular-leg-1", sandbox.ExchangeBinance, "BTC", "USDT", domain.SideBuy, "0.1", "100"),
			sagaLeg(t, "automatic-triangular-leg-2", sandbox.ExchangeBinance, "ETH", "BTC", domain.SideBuy, "1", "0.1"),
			sagaLeg(t, "automatic-triangular-leg-3", sandbox.ExchangeBinance, "ETH", "USDT", domain.SideSell, "1", "10"),
		}}
}

func crossExchangeSagaMaterial(t *testing.T, now time.Time) sandboxSagaMaterial {
	t.Helper()
	planID, _ := domain.NewExecutionPlanID("automatic-cross-plan")
	return sandboxSagaMaterial{Strategy: sandbox.StrategyCrossExchangeArbitrage, PlanID: planID,
		CandidateID: "cross-candidate", Ordinal: 1, LogicalTime: 1,
		ApprovedAt: now, LifetimeNanos: uint64(250 * time.Millisecond),
		CanonicalInput:     []byte(`{"input":"cross"}`),
		CanonicalCandidate: []byte(`{"candidate":"cross"}`),
		AllocationEvidence: []byte(`{"allocation":"active"}`),
		RiskEvidence:       []byte(`{"risk":"approved"}`),
		PlannerEvidence:    []byte(`{"policy":"concurrent"}`),
		Legs: []sandboxSagaLegMaterial{
			sagaLeg(t, "automatic-cross-leg-1", sandbox.ExchangeBinance, "BTC", "USDT", domain.SideBuy, "0.1", "100"),
			sagaLeg(t, "automatic-cross-leg-2", sandbox.ExchangeBybit, "BTC", "USDT", domain.SideSell, "0.1", "100"),
		}}
}

func sagaLeg(
	t *testing.T,
	id string,
	exchange sandbox.Exchange,
	base, quote string,
	side domain.Side,
	quantityText, priceText string,
) sandboxSagaLegMaterial {
	t.Helper()
	orderID, _ := domain.NewVirtualOrderID(id)
	instrument, _ := domain.NewSpotInstrument(domain.AssetSymbol(base), domain.AssetSymbol(quote))
	quantity, _ := domain.ParseQuantity(quantityText)
	price, _ := domain.ParsePrice(priceText)
	notional, err := domain.CalculateNotional(price, quantity, 18)
	if err != nil {
		t.Fatal(err)
	}
	return sandboxSagaLegMaterial{OrderID: orderID, Exchange: exchange,
		Instrument: instrument, Side: side, Quantity: quantity,
		LimitPrice: price, Notional: notional}
}
