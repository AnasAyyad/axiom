package inventoryrebalancing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"time"

	"axiom/internal/backtest"
	platformconfig "axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/rebalancing"
	"axiom/internal/replay"
)

var inventoryIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,191}$`)

// InventorySnapshot is immutable evidence of the imbalance evaluated by one
// recommendation. It does not grant custody or permission to move assets.
type InventorySnapshot struct {
	ID                 string           `json:"id"`
	Source             rebalancing.Node `json:"source"`
	Destination        rebalancing.Node `json:"destination"`
	SourceExcess       domain.Balance   `json:"source_excess"`
	DestinationDeficit domain.Balance   `json:"destination_deficit"`
	ObservedAt         time.Time        `json:"observed_at"`
	CanonicalHash      string           `json:"canonical_hash"`
}

// Input is one recorded inventory imbalance, reviewed route-fact set, and
// deterministic optimizer request.
type Input struct {
	Ordinal     uint64              `json:"ordinal"`
	LogicalTime uint64              `json:"logical_time"`
	Inventory   InventorySnapshot   `json:"inventory"`
	Request     rebalancing.Request `json:"request"`
	Facts       []rebalancing.Edge  `json:"facts"`
}

// SealInventorySnapshot returns a copy carrying its canonical evidence hash.
func SealInventorySnapshot(snapshot InventorySnapshot) InventorySnapshot {
	snapshot.CanonicalHash = inventorySnapshotHash(snapshot)
	return snapshot
}

type evaluation struct {
	Input          Input                       `json:"input"`
	Outcome        string                      `json:"outcome"`
	Reason         string                      `json:"reason"`
	Recommendation *rebalancing.Recommendation `json:"recommendation,omitempty"`
	Diagnostics    rebalancing.Diagnostics     `json:"diagnostics"`
}

type allocatedEvaluation struct {
	Evaluation evaluation `json:"evaluation"`
	Owned      bool       `json:"owned"`
}

type reviewedEvaluation struct {
	Allocation allocatedEvaluation `json:"allocation"`
	Status     string              `json:"status"`
}

type plannedEvaluation struct {
	Risk        reviewedEvaluation `json:"risk"`
	ManualSteps []string           `json:"manual_steps"`
}

type accountedEvaluation struct {
	Plan plannedEvaluation `json:"plan"`
}

// Runtime implements every stage of the shared advisory pipeline without
// exposing a broker, order adapter, transfer executor, or mutable journal.
type Runtime struct {
	configuration     rebalancing.Configuration
	configurationHash string
}

// NewRuntime binds the optimizer to one reviewed immutable configuration.
func NewRuntime(reviewed platformconfig.RebalancingConfiguration, configurationHash string) (*Runtime, error) {
	configuration, err := rebalancing.ConfigurationFromReviewed(reviewed)
	if err != nil || !validHash(configurationHash) {
		return nil, fmt.Errorf("inventory_rebalancing_configuration_invalid")
	}
	return &Runtime{configuration: configuration, configurationHash: configurationHash}, nil
}

// EvaluateAdvisory runs the real optimizer and preserves explained no-route
// outcomes as successful no-action decisions.
func (runtime *Runtime) EvaluateAdvisory(
	ctx context.Context,
	event replay.Event,
) (backtest.AdvisoryCandidate, error) {
	if ctx == nil || runtime == nil || event.Ordinal == 0 || event.LogicalTime == 0 {
		return backtest.AdvisoryCandidate{}, fmt.Errorf("inventory_rebalancing_input_invalid")
	}
	var input Input
	if json.Unmarshal(event.Canonical, &input) != nil || !runtime.validInput(input, event) {
		return backtest.AdvisoryCandidate{}, fmt.Errorf("inventory_rebalancing_input_invalid")
	}
	graph, err := rebalancing.NewGraph(input.Facts)
	if err != nil {
		return backtest.AdvisoryCandidate{}, fmt.Errorf("inventory_rebalancing_facts_invalid")
	}
	recommendation, diagnostics, optimizeErr := graph.Optimize(input.Request)
	result := evaluation{Input: input, Outcome: "recommended", Reason: "eligible_advisory_route_found",
		Recommendation: &recommendation, Diagnostics: diagnostics}
	if optimizeErr != nil {
		var routeFailure *rebalancing.Error
		if !errors.As(optimizeErr, &routeFailure) {
			return backtest.AdvisoryCandidate{}, fmt.Errorf("inventory_rebalancing_evaluation_failed")
		}
		result.Outcome, result.Reason, result.Recommendation = "no_action", optimizeErr.Error(), nil
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return backtest.AdvisoryCandidate{}, fmt.Errorf("inventory_rebalancing_output_invalid")
	}
	decision, err := json.Marshal(struct {
		Outcome        string                      `json:"outcome"`
		Reason         string                      `json:"reason"`
		AdvisoryOnly   bool                        `json:"advisory_only"`
		Recommendation *rebalancing.Recommendation `json:"recommendation,omitempty"`
		Diagnostics    rebalancing.Diagnostics     `json:"diagnostics"`
	}{result.Outcome, result.Reason, true, result.Recommendation, result.Diagnostics})
	if err != nil {
		return backtest.AdvisoryCandidate{}, fmt.Errorf("inventory_rebalancing_output_invalid")
	}
	return backtest.AdvisoryCandidate{Ordinal: event.Ordinal, Decision: decision, Payload: payload}, nil
}

// AllocateAdvisory verifies the recorded inventory ownership and reserves no
// funds, quantities, or venue capacity.
func (runtime *Runtime) AllocateAdvisory(
	_ context.Context,
	candidate backtest.AdvisoryCandidate,
) (backtest.AdvisoryAllocation, error) {
	var value evaluation
	if json.Unmarshal(candidate.Payload, &value) != nil || candidate.Ordinal != value.Input.Ordinal ||
		!runtime.validInventory(value.Input.Inventory, value.Input.Request) {
		return backtest.AdvisoryAllocation{}, fmt.Errorf("inventory_rebalancing_allocation_invalid")
	}
	allocated := allocatedEvaluation{Evaluation: value, Owned: true}
	payload, _ := json.Marshal(allocated)
	evidence, _ := json.Marshal(struct {
		InventorySnapshotID   string `json:"inventory_snapshot_id"`
		InventorySnapshotHash string `json:"inventory_snapshot_hash"`
		OwnershipConfirmed    bool   `json:"ownership_confirmed"`
		ReservationsCreated   int    `json:"reservations_created"`
	}{value.Input.Inventory.ID, value.Input.Inventory.CanonicalHash, true, 0})
	return backtest.AdvisoryAllocation{Ordinal: candidate.Ordinal, Evidence: evidence, Payload: payload}, nil
}

// ReviewAdvisory records central policy review without creating executable
// approval. A no-route outcome remains blocked and safe.
func (runtime *Runtime) ReviewAdvisory(
	_ context.Context,
	allocation backtest.AdvisoryAllocation,
) (backtest.AdvisoryRiskDecision, error) {
	var value allocatedEvaluation
	if json.Unmarshal(allocation.Payload, &value) != nil || !value.Owned || allocation.Ordinal != value.Evaluation.Input.Ordinal {
		return backtest.AdvisoryRiskDecision{}, fmt.Errorf("inventory_rebalancing_risk_invalid")
	}
	status := "approved_for_manual_review"
	if value.Evaluation.Recommendation == nil {
		status = "blocked_no_route"
	} else if !value.Evaluation.Recommendation.AdvisoryOnly ||
		value.Evaluation.Recommendation.RiskScore.Compare(runtime.configuration.MaximumRiskScore) > 0 ||
		value.Evaluation.Recommendation.TotalCost.Compare(runtime.configuration.MaximumTotalCost) > 0 {
		return backtest.AdvisoryRiskDecision{}, fmt.Errorf("inventory_rebalancing_risk_invalid")
	}
	reviewed := reviewedEvaluation{Allocation: value, Status: status}
	payload, _ := json.Marshal(reviewed)
	evidence, _ := json.Marshal(struct {
		Status              string `json:"status"`
		ExecutionAuthorized bool   `json:"execution_authorized"`
		AdvisoryOnly        bool   `json:"advisory_only"`
	}{status, false, true})
	return backtest.AdvisoryRiskDecision{Ordinal: allocation.Ordinal, Evidence: evidence, Payload: payload}, nil
}

// PlanAdvisory exposes only operator checklist text and cannot represent an
// external action authorization.
func (runtime *Runtime) PlanAdvisory(
	_ context.Context,
	riskDecision backtest.AdvisoryRiskDecision,
) (backtest.AdvisoryPlan, error) {
	var value reviewedEvaluation
	if json.Unmarshal(riskDecision.Payload, &value) != nil || riskDecision.Ordinal != value.Allocation.Evaluation.Input.Ordinal {
		return backtest.AdvisoryPlan{}, fmt.Errorf("inventory_rebalancing_plan_invalid")
	}
	steps := []string{"No route was eligible; do not move inventory."}
	if recommendation := value.Allocation.Evaluation.Recommendation; recommendation != nil {
		steps = append([]string(nil), recommendation.ManualChecklist...)
	}
	planned := plannedEvaluation{Risk: value, ManualSteps: steps}
	payload, _ := json.Marshal(planned)
	evidence, _ := json.Marshal(struct {
		ManualSteps           []string `json:"manual_steps"`
		OrderCount            int      `json:"order_count"`
		TransferCount         int      `json:"transfer_count"`
		ExternalActionAllowed bool     `json:"external_action_allowed"`
	}{steps, 0, 0, false})
	return backtest.AdvisoryPlan{Ordinal: riskDecision.Ordinal, Evidence: evidence, Payload: payload,
		ExternalActionAllowed: false}, nil
}

// RecordAdvisory returns the immutable imbalance view and records no journal
// entry, position change, reservation, order, fill, or fee.
func (runtime *Runtime) RecordAdvisory(
	_ context.Context,
	plan backtest.AdvisoryPlan,
) (backtest.AdvisoryAccountingRecord, error) {
	var value plannedEvaluation
	if json.Unmarshal(plan.Payload, &value) != nil || plan.Ordinal != value.Risk.Allocation.Evaluation.Input.Ordinal ||
		plan.ExternalActionAllowed {
		return backtest.AdvisoryAccountingRecord{}, fmt.Errorf("inventory_rebalancing_accounting_invalid")
	}
	inventory := value.Risk.Allocation.Evaluation.Input.Inventory
	payload, _ := json.Marshal(accountedEvaluation{Plan: value})
	balances, _ := json.Marshal(map[string]string{
		inventoryNodeKey(inventory.Source) + ":excess":       inventory.SourceExcess.String(),
		inventoryNodeKey(inventory.Destination) + ":deficit": inventory.DestinationDeficit.String(),
	})
	evidence, _ := json.Marshal(struct {
		JournalEntries   int  `json:"journal_entries"`
		PositionsChanged bool `json:"positions_changed"`
		BalancesChanged  bool `json:"balances_changed"`
	}{0, false, false})
	return backtest.AdvisoryAccountingRecord{Ordinal: plan.Ordinal, Evidence: evidence, Payload: payload,
		Balances: balances, MutationRecorded: false}, nil
}

// ReconcileAdvisory confirms the complete absence of external and accounting
// side effects after the recommendation has been projected.
func (runtime *Runtime) ReconcileAdvisory(
	_ context.Context,
	record backtest.AdvisoryAccountingRecord,
) (backtest.AdvisoryReconciliation, error) {
	var value accountedEvaluation
	if json.Unmarshal(record.Payload, &value) != nil || record.Ordinal != value.Plan.Risk.Allocation.Evaluation.Input.Ordinal ||
		record.MutationRecorded {
		return backtest.AdvisoryReconciliation{}, fmt.Errorf("inventory_rebalancing_reconciliation_invalid")
	}
	evidence, _ := json.Marshal(struct {
		InventorySnapshotHash     string `json:"inventory_snapshot_hash"`
		NoOrders                  bool   `json:"no_orders"`
		NoFills                   bool   `json:"no_fills"`
		NoJournalMutation         bool   `json:"no_journal_mutation"`
		NoExternalActionConfirmed bool   `json:"no_external_action_confirmed"`
	}{value.Plan.Risk.Allocation.Evaluation.Input.Inventory.CanonicalHash, true, true, true, true})
	return backtest.AdvisoryReconciliation{Ordinal: record.Ordinal, Evidence: evidence,
		NoExternalActionConfirmed: true}, nil
}

func (runtime *Runtime) validInput(input Input, event replay.Event) bool {
	if input.Ordinal != event.Ordinal || input.LogicalTime != event.LogicalTime ||
		input.Request.ConfigurationHash != runtime.configurationHash ||
		!reflect.DeepEqual(input.Request.Configuration, runtime.configuration) || len(input.Facts) == 0 {
		return false
	}
	return runtime.validInventory(input.Inventory, input.Request)
}

func (runtime *Runtime) validInventory(snapshot InventorySnapshot, request rebalancing.Request) bool {
	zero, _ := domain.ParseBalance("0")
	return runtime != nil && inventoryIdentifier.MatchString(snapshot.ID) &&
		snapshot.Source == request.Source && snapshot.Destination == request.Destination &&
		snapshot.SourceExcess.Compare(zero) > 0 && snapshot.DestinationDeficit.Compare(zero) > 0 &&
		snapshot.SourceExcess.Compare(request.Quantity) >= 0 && snapshot.DestinationDeficit.Compare(request.Quantity) >= 0 &&
		!snapshot.ObservedAt.IsZero() && snapshot.ObservedAt.Location() == time.UTC &&
		!snapshot.ObservedAt.After(request.DecisionTime) && validHash(snapshot.CanonicalHash) &&
		snapshot.CanonicalHash == inventorySnapshotHash(snapshot)
}

func inventorySnapshotHash(snapshot InventorySnapshot) string {
	snapshot.CanonicalHash = ""
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func validHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func inventoryNodeKey(node rebalancing.Node) string {
	return node.Exchange + "@" + string(node.Asset)
}

var (
	_ backtest.AdvisoryStrategy   = (*Runtime)(nil)
	_ backtest.AdvisoryAllocator  = (*Runtime)(nil)
	_ backtest.AdvisoryRiskEngine = (*Runtime)(nil)
	_ backtest.AdvisoryPlanner    = (*Runtime)(nil)
	_ backtest.AdvisoryAccounting = (*Runtime)(nil)
	_ backtest.AdvisoryReconciler = (*Runtime)(nil)
)
