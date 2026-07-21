package portfolio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/simulation"
)

// ApprovalRecord keeps eligibility facts behind an opaque approved intent.
type ApprovalRecord struct {
	DecisionID domain.DecisionID
	Assets     []AssetEligibility
	PolicyHash string
}

// ApprovalVault issues and resolves tamper-evident approved intents.
type ApprovalVault struct {
	mutex   sync.RWMutex
	records map[string]ApprovalRecord
}

// NewApprovalVault constructs an empty approval boundary.
func NewApprovalVault() *ApprovalVault {
	return &ApprovalVault{records: make(map[string]ApprovalRecord)}
}

// Issue creates one opaque approval token from current approved asset facts.
func (vault *ApprovalVault) Issue(record ApprovalRecord) (execution.ApprovedIntent, error) {
	if record.DecisionID.Value() == "" || record.PolicyHash == "" || len(record.Assets) == 0 {
		return execution.ApprovedIntent{}, portfolioError("approval_record_invalid")
	}
	for _, asset := range record.Assets {
		if asset.Asset == "" || asset.Status != domain.AssetApproved || asset.Version == 0 {
			return execution.ApprovedIntent{}, portfolioError("approval_record_invalid")
		}
	}
	record.Assets = append([]AssetEligibility(nil), record.Assets...)
	encoded, _ := json.Marshal(record)
	digest := sha256.Sum256(encoded)
	hash := hex.EncodeToString(digest[:])
	vault.mutex.Lock()
	defer vault.mutex.Unlock()
	if _, exists := vault.records[hash]; exists {
		return execution.ApprovedIntent{}, portfolioError("approval_duplicate")
	}
	vault.records[hash] = record
	return execution.ApprovedIntent{DecisionID: record.DecisionID, ApprovalHash: hash, PolicyHash: record.PolicyHash}, nil
}

func (vault *ApprovalVault) resolve(intent execution.ApprovedIntent) (ApprovalRecord, bool) {
	vault.mutex.RLock()
	defer vault.mutex.RUnlock()
	record, exists := vault.records[intent.ApprovalHash]
	record.Assets = append([]AssetEligibility(nil), record.Assets...)
	return record, exists && record.DecisionID == intent.DecisionID && record.PolicyHash == intent.PolicyHash
}

// EligibilityPlanner rechecks approval assets before plan construction.
type EligibilityPlanner struct {
	delegate execution.ExecutionPlanner
	vault    *ApprovalVault
	registry AssetRegistry
}

// NewEligibilityPlanner constructs the second asset-status enforcement boundary.
func NewEligibilityPlanner(
	delegate execution.ExecutionPlanner,
	vault *ApprovalVault,
	registry AssetRegistry,
) (*EligibilityPlanner, error) {
	if delegate == nil || vault == nil || registry == nil {
		return nil, portfolioError("eligibility_planner_invalid")
	}
	return &EligibilityPlanner{delegate: delegate, vault: vault, registry: registry}, nil
}

// Plan rejects status changes before delegating to exact simulated planning.
func (planner *EligibilityPlanner) Plan(ctx context.Context, intent execution.ApprovedIntent) (execution.SimulatedPlan, error) {
	record, exists := planner.vault.resolve(intent)
	if !exists {
		return execution.SimulatedPlan{}, portfolioError("approval_unknown")
	}
	for _, asset := range record.Assets {
		if err := requireApproved(planner.registry, asset.Asset, asset.Version); err != nil {
			return execution.SimulatedPlan{}, err
		}
	}
	plan, err := planner.delegate.Plan(ctx, intent)
	if err != nil || !planAssetsApproved(plan, record.Assets) {
		return execution.SimulatedPlan{}, portfolioError("plan_eligibility_rejected")
	}
	return plan, nil
}

// BrokerGuard performs the final current-status and owned-sell check.
type BrokerGuard struct {
	portfolio *Portfolio
	registry  AssetRegistry
}

// NewBrokerGuard constructs the third asset-status enforcement boundary.
func NewBrokerGuard(portfolio *Portfolio, registry AssetRegistry) (*BrokerGuard, error) {
	if portfolio == nil || registry == nil {
		return nil, portfolioError("broker_guard_invalid")
	}
	return &BrokerGuard{portfolio: portfolio, registry: registry}, nil
}

// Authorize rejects anything except currently approved spot assets and owned sells.
func (guard *BrokerGuard) Authorize(leg execution.PlannedLeg, _ simulation.BookState) (domain.Balance, error) {
	for _, asset := range []domain.AssetSymbol{leg.Instrument.Base, leg.Instrument.Quote} {
		current, exists := guard.registry.Current(asset)
		if !exists || current.Status != domain.AssetApproved || current.Version == 0 {
			return domain.Balance{}, portfolioError("broker_asset_not_approved")
		}
	}
	zero, _ := domain.ParseBalance("0")
	if leg.Side == domain.SideBuy {
		return zero, nil
	}
	key, exists := guard.portfolio.BalanceKey(leg.Instrument.Base)
	if !exists {
		return domain.Balance{}, portfolioError("unowned_sell_rejected")
	}
	balance, exists := guard.portfolio.ledger.Balance(key)
	if !exists {
		return domain.Balance{}, portfolioError("unowned_sell_rejected")
	}
	owned, err := balance.Available.Add(balance.Reserved)
	if err != nil || owned.Compare(zero) <= 0 {
		return domain.Balance{}, portfolioError("unowned_sell_rejected")
	}
	return owned, nil
}

func planAssetsApproved(plan execution.SimulatedPlan, assets []AssetEligibility) bool {
	approved := make(map[domain.AssetSymbol]struct{}, len(assets))
	for _, asset := range assets {
		approved[asset.Asset] = struct{}{}
	}
	for _, leg := range plan.Legs {
		if _, exists := approved[leg.Instrument.Base]; !exists {
			return false
		}
		if _, exists := approved[leg.Instrument.Quote]; !exists {
			return false
		}
	}
	return len(plan.Legs) > 0
}

var _ execution.ExecutionPlanner = (*EligibilityPlanner)(nil)
var _ simulation.BoundaryGuard = (*BrokerGuard)(nil)
