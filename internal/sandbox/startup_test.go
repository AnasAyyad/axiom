package sandbox

import (
	"errors"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

type lifecycleCapture struct {
	events []exchangecontracts.CollectorLifecycleEvidence
	failAt int
}

func (capture *lifecycleCapture) AppendCollectorLifecycle(
	event exchangecontracts.CollectorLifecycleEvidence,
) error {
	if capture.failAt > 0 && len(capture.events)+1 == capture.failAt {
		return errors.New("evidence_failed")
	}
	capture.events = append(capture.events, event)
	return nil
}

func TestSandboxStartupIsOrderedFailClosedAndStopsReadyPaused(t *testing.T) {
	sink := &lifecycleCapture{}
	gate, err := NewStartupGate(ExchangeBinance, sink, 1)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	observation := completeStartupObservation(at)
	if err := gate.Complete(StartupEnterLocked, observation); err == nil {
		t.Fatal("out-of-order stage accepted")
	}
	for _, stage := range SandboxStartupSequence() {
		if err := gate.Complete(stage, observation); err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
	}
	if gate.State() != EngineReadyPaused || len(sink.events) != len(SandboxStartupSequence()) {
		t.Fatalf("startup state=%s evidence=%d", gate.State(), len(sink.events))
	}
}

func TestStartupEvidenceAndCombinedEligibilityFailClosed(t *testing.T) {
	at := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	sink := &lifecycleCapture{failAt: 1}
	gate, _ := NewStartupGate(ExchangeBybit, sink, 1)
	if err := gate.Complete(StartupAcquireLease, completeStartupObservation(at)); err == nil ||
		gate.State() != EngineLocked {
		t.Fatal("evidence failure did not lock startup")
	}
	sink = &lifecycleCapture{}
	gate, _ = NewStartupGate(ExchangeBybit, sink, 1)
	observation := completeStartupObservation(at)
	for _, stage := range SandboxStartupSequence()[:8] {
		if err := gate.Complete(stage, observation); err != nil {
			t.Fatal(err)
		}
	}
	observation.Eligibility.Eligible = false
	if err := gate.Complete(StartupSynchronizeFiltersBookClock, observation); err == nil {
		t.Fatal("independently healthy-looking but ineligible snapshot accepted")
	}
}

func TestEntryFailuresDoNotDisableCancellation(t *testing.T) {
	snapshot := EntrySafetySnapshot{
		State: EngineDegraded, LeaseHeld: true,
	}
	if snapshot.CanSubmitEntry() == nil {
		t.Fatal("degraded entry accepted")
	}
	if !snapshot.CanCancel() {
		t.Fatal("cancellation blocked by entry health")
	}
}

func TestEntryRequiresIndependentGlobalAndExchangeEnablement(t *testing.T) {
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	snapshot := EntrySafetySnapshot{
		AccountID: "binance-testnet-a", AccountEpoch: 1,
		Exchange: ExchangeBinance, ObservedAt: now,
		State: EngineArmed, ArmActive: true,
		GlobalIntegrationEnabled: true, GlobalSubmissionEnabled: true,
		ExchangeIntegrationEnabled: true, ExchangeSubmissionEnabled: true,
		PublicEligible: true, PrivateStreamHealthy: true, AccountStateFresh: true,
		ReconciliationClean: true, LeaseHeld: true, EvidenceHealthy: true,
		OpenCapacityAvailable: true, DailyCapacityAvailable: true,
	}
	if err := snapshot.CanSubmitEntry(); err != nil {
		t.Fatalf("fully enabled healthy entry rejected: %v", err)
	}
	gates := []*bool{
		&snapshot.GlobalIntegrationEnabled,
		&snapshot.GlobalSubmissionEnabled,
		&snapshot.ExchangeIntegrationEnabled,
		&snapshot.ExchangeSubmissionEnabled,
	}
	for index, gate := range gates {
		*gate = false
		if snapshot.CanSubmitEntry() == nil {
			t.Fatalf("enablement gate %d was bypassed", index)
		}
		*gate = true
	}
}

func completeStartupObservation(at time.Time) StartupObservation {
	return StartupObservation{
		At: at, LeaseHeld: true, BuildValid: true, ConfigurationValid: true,
		CredentialValid: true, AccountGenerationOK: true, OutboxRecovered: true,
		InboxRecovered: true, ExchangeStateLoaded: true, UnknownResolvedOrQuarantined: true,
		JournalReconciled: true, ReservationsReconciled: true, FiltersSynchronized: true,
		Eligibility: EligibilitySnapshot{
			ObservedAt: at, Exchange: "test", Instrument: "BTCUSDT",
			BookHealthy: true, BookFresh: true, BookEligible: true,
			ClockEligible: true, Eligible: true,
		},
		PrivateStreamHealthy: true, EvidenceHealthy: true,
	}
}
