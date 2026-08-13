package sandbox

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestStrategyRiskValuationProducesConservativeCompleteObservation(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work := validStrategyRiskWork(t, now)
	snapshot := validStrategyRiskSnapshot(t, work, now)
	facts := validStrategyRiskFacts(t, work, snapshot, now)
	market, admission := strategyRiskProjectionMarketAndAdmission(t, work, now)
	valuation := strategyRiskProjectionValuation(t, work, snapshot, market, facts, admission, now)
	observation, err := valuation.Observation(work, snapshot, market, facts, admission, now)
	if err != nil {
		t.Fatal(err)
	}
	if observation.AccountDrawdown.String() != "0.166666666666666667" ||
		observation.UTCDayLoss.String() != "0.09090909090909091" ||
		observation.Rolling24HourLoss.String() != "0" ||
		observation.StrategyLoss.String() != "0.07" ||
		observation.AssetExposure.String() != "0.2" ||
		observation.CombinedExposure.String() != "0.2" ||
		observation.ExchangeExposure.String() != "0.25" ||
		observation.Reserve.String() != "0.75" ||
		observation.ReservedCapital.String() != "0.05" ||
		observation.Spread.String() != "0.01" || observation.Slippage.String() != "0.001" ||
		observation.OpenOrders != 1 || observation.BookAge != 10*time.Millisecond ||
		observation.ClockDrift != 3*time.Millisecond || observation.QualityScore != 100 ||
		observation.Gap || observation.StaleData || observation.ReconciliationFault ||
		observation.AccountingFault || observation.UnknownOrder || observation.PersistenceFault ||
		observation.DiskFault || observation.APIError || observation.LeaseLost {
		t.Fatalf("unexpected projected observation: %#v", observation)
	}
	if valuation.EvidenceHash() == "" || StrategyRiskAdmissionHash(admission) != valuation.AdmissionHash {
		t.Fatal("valuation evidence does not bind exact admission")
	}
}

func TestStrategyRiskValuationRequiresBaselineAndExactAccountingState(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work := validStrategyRiskWork(t, now)
	snapshot := validStrategyRiskSnapshot(t, work, now)
	facts := validStrategyRiskFacts(t, work, snapshot, now)
	market, admission := strategyRiskProjectionMarketAndAdmission(t, work, now)
	valuation := strategyRiskProjectionValuation(t, work, snapshot, market, facts, admission, now)
	valuation.Purpose = StrategyRiskValuationBaseline
	if _, err := valuation.Observation(work, snapshot, market, facts, admission, now); err == nil {
		t.Fatal("baseline valuation authorized a central-risk observation")
	}
	valuation = strategyRiskProjectionValuation(t, work, snapshot, market, facts, admission, now)
	valuation.AccountingState = StrategyRiskAccountingNoFills
	valuation.AccountingProjectionHash = ""
	if err := valuation.ValidFor(work, snapshot, market, facts, admission, now); err == nil {
		t.Fatal("nonzero strategy position accepted as a no-fill accounting state")
	}
	valuation = strategyRiskProjectionValuation(t, work, snapshot, market, facts, admission, now)
	valuation.StrategyTotalPnL = mustRiskProjectionPnL(t, "-5")
	if err := valuation.ValidFor(work, snapshot, market, facts, admission, now); err == nil {
		t.Fatal("inconsistent realized plus unrealized PnL accepted")
	}
}

func strategyRiskProjectionMarketAndAdmission(
	t *testing.T,
	work StrategySessionWork,
	now time.Time,
) (StrategyMarketInput, StrategySessionAdmission) {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	market := StrategyMarketInput{Instrument: instrument,
		Metadata: exchangecontracts.InstrumentRecord{RawPayloadHash: strings.Repeat("7", 64)},
		Book: exchangecontracts.BookSnapshot{Instrument: instrument, LastSequence: 10,
			ReceivedAt:     domain.EventTime{UTC: now.Add(-10 * time.Millisecond), Sequence: 9},
			Bids:           []exchangecontracts.PriceLevel{{Price: mustRiskProjectionPrice(t, "100"), Quantity: mustRiskProjectionQuantity(t, "1")}},
			Asks:           []exchangecontracts.PriceLevel{{Price: mustRiskProjectionPrice(t, "101"), Quantity: mustRiskProjectionQuantity(t, "1")}},
			RawPayloadHash: strings.Repeat("8", 64)},
		Candles:    map[string][]exchangecontracts.Candle{"4h": {{RawPayloadHash: strings.Repeat("9", 64)}}},
		ObservedAt: domain.EventTime{UTC: now, Sequence: 10}}
	arm := Arm{ID: work.ArmID, SessionID: work.SessionID, AccountIDs: []AccountID{work.Account.ID},
		AuthorizationHash: strings.Repeat("a", 64), ActorUserID: "owner", ActorSessionID: "owner-session",
		ReasonHash: strings.Repeat("b", 64), CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(14 * time.Minute),
		Revision: work.ArmRevision}
	admission := StrategySessionAdmission{Work: work, Arm: arm,
		Eligibility: EligibilitySnapshot{ObservedAt: now, Exchange: string(work.Account.Exchange),
			Instrument: work.Instrument, BookHealth: "healthy", BookHealthy: true, BookFresh: true,
			BookEligible: true, ClockEligible: true, ClockObservedAt: now,
			ClockOffset: -3 * time.Millisecond, ClockUncertainty: time.Millisecond, Eligible: true},
		Safety: EntrySafetySnapshot{AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
			Exchange: work.Account.Exchange, ObservedAt: now, State: EngineArmed, ArmActive: true,
			GlobalIntegrationEnabled: true, GlobalSubmissionEnabled: true,
			ExchangeIntegrationEnabled: true, ExchangeSubmissionEnabled: true,
			PublicEligible: true, PrivateStreamHealthy: true, AccountStateFresh: true,
			ReconciliationClean: true, LeaseHeld: true, EvidenceHealthy: true,
			OpenCapacityAvailable: true, DailyCapacityAvailable: true},
		StartupCycle: 3, ApprovedAt: now}
	if admission.Valid() != nil {
		t.Fatal("risk projection test admission invalid")
	}
	return market, admission
}

func strategyRiskProjectionValuation(
	t *testing.T,
	work StrategySessionWork,
	snapshot AccountSnapshot,
	market StrategyMarketInput,
	facts StrategyRiskFacts,
	admission StrategySessionAdmission,
	now time.Time,
) StrategyRiskValuation {
	t.Helper()
	projectionHash := strings.Repeat("c", 64)
	valuation := StrategyRiskValuation{Purpose: StrategyRiskValuationEvaluated,
		StrategySessionID: work.SessionID, StrategyRevision: work.StrategyRevision,
		AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch, Instrument: work.Instrument,
		SnapshotHash: snapshot.SnapshotHash, MarketHash: StrategyMarketEvidenceHash(market),
		PolicyID: facts.PolicyID, PolicyVersion: facts.PolicyVersion, PolicyHash: facts.PolicyHash,
		AccountingState: StrategyRiskAccountingComplete, AccountingEvidenceHash: projectionHash,
		AccountingProjectionHash: projectionHash, MarkPrice: mustRiskProjectionPrice(t, "100"),
		AccountEquity: mustRiskProjectionMoney(t, "100"), VolatileAssetValue: mustRiskProjectionMoney(t, "20"),
		CombinedVolatileValue: mustRiskProjectionMoney(t, "20"), CommittedBuyValue: mustRiskProjectionMoney(t, "5"),
		ExchangeRiskValue: mustRiskProjectionMoney(t, "25"), ReserveValue: mustRiskProjectionMoney(t, "75"),
		ReservedValue: mustRiskProjectionMoney(t, "5"), StrategyPositionQuantity: mustRiskProjectionBalance(t, "0.1"),
		StrategyPositionValue: mustRiskProjectionMoney(t, "10"), StrategyTotalCost: mustRiskProjectionMoney(t, "13"),
		StrategyRealizedPnL: mustRiskProjectionPnL(t, "-1"), StrategyUnrealizedPnL: mustRiskProjectionPnL(t, "-3"),
		StrategyTotalPnL: mustRiskProjectionPnL(t, "-4"), AccountPeakEquity: mustRiskProjectionMoney(t, "120"),
		UTCDayBaselineEquity: mustRiskProjectionMoney(t, "110"), Rolling24HourBaselineEquity: mustRiskProjectionMoney(t, "100"),
		StrategyPeakPnL: mustRiskProjectionPnL(t, "3"), OpenOrders: 1,
		Slippage: mustRiskProjectionPercent(t, "0.001"), ReconciliationID: "reconciliation",
		ReconciliationHash: strings.Repeat("d", 64), StorageRevision: 2,
		StorageObservedAt: now.Add(-time.Second), EngineStartupCycle: admission.StartupCycle,
		AdmissionHash: StrategyRiskAdmissionHash(admission), ObservedAt: now}
	if valuation.ValidFor(work, snapshot, market, facts, admission, now) != nil {
		t.Fatal("risk projection test valuation invalid")
	}
	return valuation
}

func mustRiskProjectionMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	parsed, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func mustRiskProjectionPnL(t *testing.T, value string) domain.PnL {
	t.Helper()
	parsed, err := domain.ParsePnL(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func mustRiskProjectionPrice(t *testing.T, value string) domain.Price {
	t.Helper()
	parsed, err := domain.ParsePrice(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func mustRiskProjectionQuantity(t *testing.T, value string) domain.Quantity {
	t.Helper()
	parsed, err := domain.ParseQuantity(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func mustRiskProjectionBalance(t *testing.T, value string) domain.Balance {
	t.Helper()
	parsed, err := domain.ParseBalance(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func mustRiskProjectionPercent(t *testing.T, value string) domain.Percent {
	t.Helper()
	parsed, err := domain.ParsePercent(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
