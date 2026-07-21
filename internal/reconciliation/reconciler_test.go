package reconciliation

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/domain"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
)

func TestReconcilerPersistsIncidentSuspenseAndQuarantineWithoutOverwrite(t *testing.T) {
	journal := accounting.NewMemoryJournal()
	cases, incidents, quarantine := &memoryCases{}, &memoryIncidents{}, &memoryQuarantine{}
	runID, _ := domain.NewRunID("run-one")
	portfolioID, _ := domain.NewPortfolioID("trend-one")
	reconciler, err := NewReconciler(cases, incidents, quarantine, journal, Context{RunID: runID,
		PortfolioID: portfolioID, ConfigurationHash: strings.Repeat("a", 64), Owner: "trend-one"})
	if err != nil {
		t.Fatal(err)
	}
	expected, actual := matchingStates(), matchingStates()
	actual.Orders = strings.Repeat("b", 64)
	asset, _ := domain.ParseAssetSymbol("BTC")
	quantity, _ := domain.ParseBalance("0.1")
	actual.Differences = []Discrepancy{{Category: "balance", Classification: UnknownFact,
		Asset: asset, Quantity: quantity, Critical: true}}
	result, err := reconciler.Reconcile("portfolio:trend-one", expected, actual,
		time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC))
	if err != nil || result.State != "quarantined" || len(cases.items) != 1 || len(incidents.items) != 1 ||
		len(quarantine.scopes) != 1 || len(journal.Transactions()) != 1 {
		t.Fatalf("reconciliation = %#v %v", result, err)
	}
	if expected.Orders != strings.Repeat("a", 64) || actual.Orders != strings.Repeat("b", 64) {
		t.Fatal("reconciliation overwrote source history")
	}
}

func TestStartupRecoveryUsesExistingOrderAndEndsReadyPaused(t *testing.T) {
	clock, _ := runtimecore.NewDeterministicClock(1)
	gate := runtimecore.NewSafetyGate()
	runtimeRecovery, err := runtimecore.NewRecoveryGate(clock, gate)
	if err != nil {
		t.Fatal(err)
	}
	audit, alerts := &riskAudit{}, &riskAlerts{}
	riskEngine, _ := risk.NewEngine(audit, alerts)
	evidence := &memoryRecoveryEvidence{}
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	recovery, err := NewStartupRecovery(runtimeRecovery, riskEngine, evidence, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range runtimecore.RecoverySequence() {
		if err = recovery.Complete(stage, strings.Repeat("a", 64)); err != nil {
			t.Fatal(err)
		}
	}
	if err = recovery.AdministrativeReady(now.Add(time.Minute)); err != nil || riskEngine.State() != risk.StatePaused ||
		!runtimeRecovery.Snapshot().Ready || len(evidence.stages) != len(runtimecore.RecoverySequence()) || gate.Accepting() {
		t.Fatal("startup recovery did not end ready and paused")
	}
}

func matchingStates() State {
	hash := strings.Repeat("a", 64)
	return State{Orders: hash, Fills: hash, Reservations: hash, Balances: hash,
		Positions: hash, Ownership: hash, Journal: hash, Projections: hash}
}

type memoryCases struct{ items []Case }

func (store *memoryCases) Create(value Case) error {
	store.items = append(store.items, cloneCase(value))
	return nil
}

type memoryIncidents struct{ items []string }

func (sink *memoryIncidents) Create(scope, reason string, _ time.Time) (string, error) {
	sink.items = append(sink.items, scope+":"+reason)
	return "incident-one", nil
}

type memoryQuarantine struct{ scopes []string }

func (gate *memoryQuarantine) Block(scope, reason string) error {
	gate.scopes = append(gate.scopes, scope+":"+reason)
	return nil
}

type memoryRecoveryEvidence struct{ stages []runtimecore.RecoveryStage }

func (store *memoryRecoveryEvidence) Append(stage runtimecore.RecoveryStage, _ string) error {
	store.stages = append(store.stages, stage)
	return nil
}

type riskAudit struct{ events []risk.AuditEvent }

func (sink *riskAudit) Append(event risk.AuditEvent) error {
	sink.events = append(sink.events, event)
	return nil
}

type riskAlerts struct{}

func (*riskAlerts) Emit(string, risk.Action, risk.State) error { return nil }
