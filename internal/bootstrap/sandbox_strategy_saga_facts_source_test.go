package bootstrap

import (
	"context"
	"fmt"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

func TestSandboxStrategySagaFactsReaderBuildsCompleteTriangularAndPairedFacts(t *testing.T) {
	for _, strategy := range []string{sandbox.StrategyTriangular, sandbox.StrategyCrossExchangeArbitrage} {
		t.Run(strategy, func(t *testing.T) {
			now := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)
			want := sagaPlanFacts(t, strategy, now)
			store := newSagaFactsStore(want)
			product := enabledSagaProduct(t)
			reader, err := NewSandboxStrategySagaFactsReader(store, product)
			if err != nil {
				t.Fatal(err)
			}
			coordinator := want.Coordinator.Work
			lease := sandbox.StrategySessionExecutionLease{Account: coordinator.Account.ID,
				Epoch: coordinator.Account.Epoch, Owner: "binance-coordinator", Fence: 7}
			facts, err := reader.SandboxStrategySagaPlanFacts(context.Background(), coordinator, lease, now)
			if err != nil || len(facts.Admissions) != len(want.Admissions) ||
				len(facts.Snapshots) != len(want.Snapshots) ||
				len(facts.RiskFacts) != len(want.RiskFacts) ||
				len(facts.MarketEligibility) != len(want.MarketEligibility) ||
				facts.Coordinator.Work != coordinator {
				t.Fatalf("facts=%#v error=%v", facts, err)
			}
			if strategy == sandbox.StrategyTriangular && len(facts.OwnedInventory) != 0 {
				t.Fatal("triangular facts invented initial dependent inventory")
			}
			if strategy == sandbox.StrategyCrossExchangeArbitrage && len(facts.OwnedInventory) != 2 {
				t.Fatalf("paired inventory facts=%d", len(facts.OwnedInventory))
			}
		})
	}
}

func TestSandboxStrategySagaFactsReaderRejectsBybitCrossCoordinatorAndMissingPeerFacts(t *testing.T) {
	now := time.Date(2026, 8, 9, 13, 5, 0, 0, time.UTC)
	want := sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now)
	store := newSagaFactsStore(want)
	reader, err := NewSandboxStrategySagaFactsReader(store, enabledSagaProduct(t))
	if err != nil {
		t.Fatal(err)
	}
	bybit := want.Admissions[sandbox.ExchangeBybit].Work
	lease := sandbox.StrategySessionExecutionLease{Account: bybit.Account.ID,
		Epoch: bybit.Account.Epoch, Owner: "bybit-worker", Fence: 8}
	if _, err = reader.SandboxStrategySagaPlanFacts(context.Background(), bybit, lease, now); err == nil {
		t.Fatal("Bybit engine became the paired strategy coordinator")
	}
	coordinator := want.Coordinator.Work
	lease = sandbox.StrategySessionExecutionLease{Account: coordinator.Account.ID,
		Epoch: coordinator.Account.Epoch, Owner: "binance-worker", Fence: 7}
	delete(store.eligibilities, want.Admissions[sandbox.ExchangeBybit].Work.Account.ID)
	if _, err = reader.SandboxStrategySagaPlanFacts(context.Background(), coordinator, lease, now); err == nil {
		t.Fatal("paired facts accepted missing peer market readiness")
	}
}

type sagaFactsStore struct {
	work          []sandbox.StrategySessionWork
	admissions    map[sandbox.AccountID]sandbox.StrategySessionAdmission
	snapshots     map[sandbox.AccountID]sandbox.AccountSnapshot
	eligibilities map[sandbox.AccountID][]sandbox.EligibilitySnapshot
	inventory     map[sandbox.AccountID]sandbox.StrategyOwnedInventory
	riskFacts     map[sandbox.AccountID]sandbox.StrategyRiskFacts
}

func newSagaFactsStore(facts SandboxSagaPlanFacts) *sagaFactsStore {
	store := &sagaFactsStore{admissions: make(map[sandbox.AccountID]sandbox.StrategySessionAdmission),
		snapshots:     make(map[sandbox.AccountID]sandbox.AccountSnapshot),
		eligibilities: make(map[sandbox.AccountID][]sandbox.EligibilitySnapshot),
		inventory:     make(map[sandbox.AccountID]sandbox.StrategyOwnedInventory),
		riskFacts:     make(map[sandbox.AccountID]sandbox.StrategyRiskFacts)}
	for _, admission := range facts.Admissions {
		account := admission.Work.Account.ID
		store.work = append(store.work, admission.Work)
		store.admissions[account] = admission
		store.snapshots[account] = facts.Snapshots[account]
		store.inventory[account] = facts.OwnedInventory[account]
		store.riskFacts[account] = facts.RiskFacts[account]
		for _, eligibility := range facts.MarketEligibility {
			if eligibility.Exchange == string(admission.Work.Account.Exchange) {
				store.eligibilities[account] = append(store.eligibilities[account], eligibility)
			}
		}
	}
	return store
}

func (store *sagaFactsStore) StrategySessionSagaWork(
	context.Context,
	sandbox.StrategySessionWork,
	time.Time,
) ([]sandbox.StrategySessionWork, error) {
	return append([]sandbox.StrategySessionWork(nil), store.work...), nil
}

func (store *sagaFactsStore) StrategySessionAdmission(
	_ context.Context,
	work sandbox.StrategySessionWork,
	_ time.Time,
	_ [4]bool,
) (sandbox.StrategySessionAdmission, error) {
	admission, exists := store.admissions[work.Account.ID]
	if !exists {
		return sandbox.StrategySessionAdmission{}, fmt.Errorf("admission missing")
	}
	return admission, nil
}

func (store *sagaFactsStore) StrategySessionSagaEligibility(
	_ context.Context,
	work sandbox.StrategySessionWork,
	_ uint64,
	instruments []string,
	_ time.Time,
) ([]sandbox.EligibilitySnapshot, error) {
	values, exists := store.eligibilities[work.Account.ID]
	if !exists || len(values) != len(instruments) {
		return nil, fmt.Errorf("eligibility missing")
	}
	return append([]sandbox.EligibilitySnapshot(nil), values...), nil
}

func (store *sagaFactsStore) LatestAccountSnapshot(
	_ context.Context,
	account sandbox.AccountID,
	_ uint64,
) (sandbox.AccountSnapshot, bool, error) {
	snapshot, exists := store.snapshots[account]
	return snapshot, exists, nil
}

func (store *sagaFactsStore) StrategyOwnedInventory(
	_ context.Context,
	work sandbox.StrategySessionWork,
	_ domain.AssetSymbol,
	_ time.Time,
) (sandbox.StrategyOwnedInventory, error) {
	inventory, exists := store.inventory[work.Account.ID]
	if !exists {
		return sandbox.StrategyOwnedInventory{}, fmt.Errorf("inventory missing")
	}
	return inventory, nil
}

func (store *sagaFactsStore) StrategyRiskFacts(
	_ context.Context,
	work sandbox.StrategySessionWork,
	_ sandbox.AccountSnapshot,
	_ time.Time,
) (sandbox.StrategyRiskFacts, error) {
	facts, exists := store.riskFacts[work.Account.ID]
	if !exists {
		return sandbox.StrategyRiskFacts{}, fmt.Errorf("risk facts missing")
	}
	return facts, nil
}

func enabledSagaProduct(t *testing.T) config.Configuration {
	t.Helper()
	product, err := config.DefaultSandboxConfiguration(config.ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	product.Sandbox.IntegrationsEnabled = true
	product.Sandbox.SubmissionEnabled = true
	for index := range product.Sandbox.Exchanges {
		product.Sandbox.Exchanges[index].IntegrationEnabled = true
		product.Sandbox.Exchanges[index].SubmissionEnabled = true
	}
	return product
}

var _ SandboxStrategySagaFactStore = (*sagaFactsStore)(nil)
