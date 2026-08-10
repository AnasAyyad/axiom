package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

// SandboxStrategySagaFactStore is the read-only durable boundary used by a
// multi-leg coordinator. It exposes no exchange adapter, peer fence owner, or
// submission method.
type SandboxStrategySagaFactStore interface {
	StrategySessionSagaWork(
		context.Context,
		sandbox.StrategySessionWork,
		time.Time,
	) ([]sandbox.StrategySessionWork, error)
	StrategySessionAdmission(
		context.Context,
		sandbox.StrategySessionWork,
		time.Time,
		[4]bool,
	) (sandbox.StrategySessionAdmission, error)
	StrategySessionSagaEligibility(
		context.Context,
		sandbox.StrategySessionWork,
		uint64,
		[]string,
		time.Time,
	) ([]sandbox.EligibilitySnapshot, error)
	sandbox.AccountSnapshotHistoryReader
	sandbox.StrategyRiskFactsSource
	sandbox.StrategyOwnedInventorySource
}

// SandboxStrategySagaFactsReader binds every account and public-readiness
// projection to one coordinator decision instant. Cross-exchange work is
// coordinated only by Binance; Bybit retains an independent credential-owning
// engine that later claims only its own durable outbox leg.
type SandboxStrategySagaFactsReader struct {
	store   SandboxStrategySagaFactStore
	product config.Configuration
}

// NewSandboxStrategySagaFactsReader validates and exposes durable saga facts.
func NewSandboxStrategySagaFactsReader(
	store SandboxStrategySagaFactStore,
	product config.Configuration,
) (*SandboxStrategySagaFactsReader, error) {
	if store == nil || config.Validate(product) != nil || product.SchemaVersion != config.SchemaVersionSandboxRuntime {
		return nil, fmt.Errorf("sandbox_strategy_saga_facts_source_invalid")
	}
	return &SandboxStrategySagaFactsReader{store: store, product: product}, nil
}

// SandboxStrategySagaPlanFacts returns only a complete immutable fact set. A
// missing peer lease, stale snapshot, stale market, disabled exchange, or
// invalid strategy-owned inventory fails closed before strategy evaluation.
func (reader *SandboxStrategySagaFactsReader) SandboxStrategySagaPlanFacts(
	ctx context.Context,
	coordinator sandbox.StrategySessionWork,
	lease sandbox.StrategySessionExecutionLease,
	now time.Time,
) (SandboxSagaPlanFacts, error) {
	if reader == nil || reader.store == nil || ctx == nil ||
		coordinator.ValidAt(now) != nil || lease.ValidFor(coordinator) != nil ||
		(coordinator.Strategy != sandbox.StrategyTriangular &&
			coordinator.Strategy != sandbox.StrategyCrossExchangeArbitrage) ||
		(coordinator.Strategy == sandbox.StrategyCrossExchangeArbitrage &&
			coordinator.Account.Exchange != sandbox.ExchangeBinance) {
		return SandboxSagaPlanFacts{}, fmt.Errorf("sandbox_strategy_saga_facts_source_invalid")
	}
	work, err := reader.store.StrategySessionSagaWork(ctx, coordinator, now)
	if err != nil {
		return SandboxSagaPlanFacts{}, fmt.Errorf("sandbox_strategy_saga_work_unavailable")
	}
	wantAccounts := 1
	if coordinator.Strategy == sandbox.StrategyCrossExchangeArbitrage {
		wantAccounts = 2
	}
	if len(work) != wantAccounts {
		return SandboxSagaPlanFacts{}, fmt.Errorf("sandbox_strategy_saga_work_unavailable")
	}
	facts := SandboxSagaPlanFacts{
		Admissions:     make(map[sandbox.Exchange]sandbox.StrategySessionAdmission, wantAccounts),
		Snapshots:      make(map[sandbox.AccountID]sandbox.AccountSnapshot, wantAccounts),
		RiskFacts:      make(map[sandbox.AccountID]sandbox.StrategyRiskFacts, wantAccounts),
		OwnedInventory: make(map[sandbox.AccountID]sandbox.StrategyOwnedInventory, wantAccounts),
	}
	marketKeys := make(map[string]struct{}, wantAccounts*3)
	for _, item := range work {
		member, memberErr := reader.sagaPlanMemberFacts(ctx, item, now)
		if memberErr != nil {
			return SandboxSagaPlanFacts{}, memberErr
		}
		if memberErr = addSandboxSagaMemberFacts(&facts, member, coordinator, marketKeys); memberErr != nil {
			return SandboxSagaPlanFacts{}, memberErr
		}
	}
	if validateSandboxSagaFacts(facts, coordinator.Strategy, wantAccounts) != nil {
		return SandboxSagaPlanFacts{}, fmt.Errorf("sandbox_strategy_saga_facts_invalid")
	}
	return facts, nil
}

type sandboxSagaMemberFacts struct {
	work          sandbox.StrategySessionWork
	admission     sandbox.StrategySessionAdmission
	snapshot      sandbox.AccountSnapshot
	risk          sandbox.StrategyRiskFacts
	eligibilities []sandbox.EligibilitySnapshot
	inventory     *sandbox.StrategyOwnedInventory
}

func (reader *SandboxStrategySagaFactsReader) sagaPlanMemberFacts(ctx context.Context,
	work sandbox.StrategySessionWork, now time.Time,
) (sandboxSagaMemberFacts, error) {
	switches, enabled := canarySwitches(reader.product, work.Account.Exchange)
	if !enabled {
		return sandboxSagaMemberFacts{}, fmt.Errorf("sandbox_strategy_saga_exchange_disabled")
	}
	admission, err := reader.store.StrategySessionAdmission(ctx, work, now, switches)
	if err != nil || admission.Valid() != nil || admission.Work != work || !admission.ApprovedAt.Equal(now) {
		return sandboxSagaMemberFacts{}, fmt.Errorf("sandbox_strategy_saga_admission_unavailable")
	}
	snapshot, found, err := reader.store.LatestAccountSnapshot(ctx, work.Account.ID, work.Account.Epoch)
	if err != nil || !found || !validSandboxSagaPlanSnapshot(admission, snapshot) {
		return sandboxSagaMemberFacts{}, fmt.Errorf("sandbox_strategy_saga_snapshot_unavailable")
	}
	instruments := sandboxSagaRequiredInstruments(work)
	eligibilities, err := reader.store.StrategySessionSagaEligibility(
		ctx, work, admission.StartupCycle, instruments, now)
	if err != nil || len(eligibilities) != len(instruments) {
		return sandboxSagaMemberFacts{}, fmt.Errorf("sandbox_strategy_saga_eligibility_unavailable")
	}
	riskFacts, err := reader.store.StrategyRiskFacts(ctx, work, snapshot, now)
	if err != nil || riskFacts.ValidFor(work, snapshot, now) != nil {
		return sandboxSagaMemberFacts{}, fmt.Errorf("sandbox_strategy_saga_risk_facts_unavailable")
	}
	member := sandboxSagaMemberFacts{work: work, admission: admission, snapshot: snapshot,
		risk: riskFacts, eligibilities: eligibilities}
	if err = reader.loadSagaMemberInventory(ctx, &member, now); err != nil {
		return sandboxSagaMemberFacts{}, err
	}
	return member, nil
}

func (reader *SandboxStrategySagaFactsReader) loadSagaMemberInventory(ctx context.Context,
	member *sandboxSagaMemberFacts, now time.Time,
) error {
	if member.work.Strategy != sandbox.StrategyCrossExchangeArbitrage {
		return nil
	}
	asset, err := sandboxSagaBaseAsset(member.work.Instrument)
	if err != nil {
		return fmt.Errorf("sandbox_strategy_saga_inventory_invalid")
	}
	inventory, err := reader.store.StrategyOwnedInventory(ctx, member.work, asset, now)
	if err != nil || inventory.ValidFor(member.admission, asset) != nil {
		return fmt.Errorf("sandbox_strategy_saga_inventory_unavailable")
	}
	member.inventory = &inventory
	return nil
}

func addSandboxSagaMemberFacts(facts *SandboxSagaPlanFacts, member sandboxSagaMemberFacts,
	coordinator sandbox.StrategySessionWork, marketKeys map[string]struct{},
) error {
	if _, duplicate := facts.Admissions[member.work.Account.Exchange]; duplicate {
		return fmt.Errorf("sandbox_strategy_saga_topology_invalid")
	}
	for _, eligibility := range member.eligibilities {
		key := eligibility.Exchange + "\x00" + eligibility.Instrument
		if _, duplicate := marketKeys[key]; duplicate {
			return fmt.Errorf("sandbox_strategy_saga_eligibility_invalid")
		}
		marketKeys[key] = struct{}{}
		facts.MarketEligibility = append(facts.MarketEligibility, eligibility)
	}
	facts.Admissions[member.work.Account.Exchange] = member.admission
	facts.Snapshots[member.work.Account.ID] = member.snapshot
	facts.RiskFacts[member.work.Account.ID] = member.risk
	if member.inventory != nil {
		facts.OwnedInventory[member.work.Account.ID] = *member.inventory
	}
	if member.work == coordinator {
		facts.Coordinator = member.admission
	}
	return nil
}

func sandboxSagaRequiredInstruments(work sandbox.StrategySessionWork) []string {
	if work.Strategy == sandbox.StrategyTriangular {
		return []string{"BTCUSDT", "ETHBTC", "ETHUSDT"}
	}
	return []string{work.Instrument}
}

func sandboxSagaBaseAsset(instrument string) (domain.AssetSymbol, error) {
	parsed, err := sandboxStrategyReadinessInstrument(instrument)
	if err != nil {
		return "", err
	}
	return parsed.Base, nil
}
