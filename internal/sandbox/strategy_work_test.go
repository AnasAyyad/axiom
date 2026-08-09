package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestStrategySessionWorkAcceptsOnlyLiveExactAccountSnapshots(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work := StrategySessionWork{
		SessionID: "session", Strategy: StrategyTrend, Instrument: "BTCUSDT",
		Account:         StrategySessionAccount{ID: "binance-account", Epoch: 7, Exchange: ExchangeBinance},
		ConfigurationID: "configuration", ConfigurationHash: strings.Repeat("c", 64), StrategySetHash: strings.Repeat("a", 64),
		SessionRevision: 3, StrategyRevision: 4, ArmID: "arm", ArmRevision: 5,
		StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute),
	}
	if err := work.ValidAt(now); err != nil {
		t.Fatal(err)
	}
	work.ArmExpiresAt = now
	if err := work.ValidAt(now); err == nil {
		t.Fatal("expired arm work snapshot accepted")
	}
	work.ArmExpiresAt = now.Add(time.Minute)
	work.Account.Epoch = 0
	if err := work.ValidAt(now); err == nil {
		t.Fatal("epoch-less work snapshot accepted")
	}
	work.Account.Epoch = 7
	work.ArmID = ""
	if err := work.ValidAt(now); err == nil {
		t.Fatal("arm-identity-less work snapshot accepted")
	}
	work.ArmID = "arm"
	work.ArmRevision = 0
	if err := work.ValidAt(now); err == nil {
		t.Fatal("arm-revision-less work snapshot accepted")
	}
}

func TestStrategySessionWorkRejectsAdvisoryAndOrderShapedValues(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work := StrategySessionWork{
		SessionID: "session", Strategy: "inventory-rebalancing", Instrument: "ETHUSDT",
		Account:         StrategySessionAccount{ID: "bybit-account", Epoch: 2, Exchange: ExchangeBybit},
		ConfigurationID: "configuration", ConfigurationHash: strings.Repeat("c", 64), StrategySetHash: strings.Repeat("b", 64),
		SessionRevision: 1, StrategyRevision: 1, ArmID: "arm", ArmRevision: 1,
		StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute),
	}
	if err := work.ValidAt(now); err == nil {
		t.Fatal("advisory strategy work snapshot accepted")
	}
}

func TestStrategySessionAdmissionRequiresExactHealthyDecisionEvidence(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	admission := StrategySessionAdmission{
		Work: StrategySessionWork{
			SessionID: "session", Strategy: StrategyTrend, Instrument: "BTCUSDT",
			Account:         StrategySessionAccount{ID: "binance-account", Epoch: 7, Exchange: ExchangeBinance},
			ConfigurationID: "configuration", ConfigurationHash: strings.Repeat("c", 64), StrategySetHash: strings.Repeat("a", 64),
			SessionRevision: 3, StrategyRevision: 4, ArmID: "arm", ArmRevision: 5,
			StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute),
		},
		Arm: strategySessionTestArm(now),
		Eligibility: EligibilitySnapshot{
			ObservedAt: now, Exchange: "binance", Instrument: "BTCUSDT", Eligible: true,
		},
		Safety: EntrySafetySnapshot{
			AccountID: "binance-account", AccountEpoch: 7, Exchange: ExchangeBinance,
			ObservedAt: now, State: EngineArmed, ArmActive: true,
			GlobalIntegrationEnabled: true, GlobalSubmissionEnabled: true,
			ExchangeIntegrationEnabled: true, ExchangeSubmissionEnabled: true,
			PublicEligible: true, PrivateStreamHealthy: true, AccountStateFresh: true,
			ReconciliationClean: true, LeaseHeld: true, EvidenceHealthy: true,
			OpenCapacityAvailable: true, DailyCapacityAvailable: true,
		},
		StartupCycle: 1, ApprovedAt: now,
	}
	if err := admission.Valid(); err != nil {
		t.Fatal(err)
	}
	admission.Work.ArmRevision++
	admission.Safety.AccountEpoch++
	if err := admission.Valid(); err == nil {
		t.Fatal("mismatched exact account evidence accepted")
	}
	admission.Safety.AccountEpoch = 7
	admission.Eligibility.ObservedAt = now.Add(-time.Second)
	if err := admission.Valid(); err == nil {
		t.Fatal("stale public eligibility accepted")
	}
}

func TestStrategySessionConfigurationRequiresExactImmutableWorkBinding(t *testing.T) {
	work := StrategySessionWork{ConfigurationID: "configuration", ConfigurationHash: strings.Repeat("a", 64)}
	configuration := StrategySessionConfiguration{ID: "configuration", Hash: strings.Repeat("a", 64), Payload: []byte(`{"schema_version":"test"}`)}
	if !configuration.ValidFor(work) {
		t.Fatal("exact configuration binding rejected")
	}
	configuration.Hash = strings.Repeat("b", 64)
	if configuration.ValidFor(work) {
		t.Fatal("mismatched configuration hash accepted")
	}
	configuration.Hash, configuration.Payload = work.ConfigurationHash, []byte(`not-json`)
	if configuration.ValidFor(work) {
		t.Fatal("invalid canonical configuration accepted")
	}
}

func strategySessionTestArm(now time.Time) Arm {
	return Arm{ID: "arm", SessionID: "session", AccountIDs: []AccountID{"binance-account"},
		AuthorizationHash: strings.Repeat("b", 64), ActorUserID: "owner", ActorSessionID: "owner-session",
		ReasonHash: strings.Repeat("c", 64), CreatedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(14 * time.Minute), Revision: 5}
}
