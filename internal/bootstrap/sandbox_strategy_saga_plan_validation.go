package bootstrap

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/replay"
	"axiom/internal/sandbox"
)

type sandboxSagaPlanParts struct {
	submissions  []sandbox.Submission
	reservations []sandbox.DurableReservation
	entrySafety  map[sandbox.AccountID]sandbox.EntrySafetySnapshot
	snapshots    map[sandbox.AccountID]sandbox.AccountSnapshotReference
}

func buildSandboxSagaPlanParts(facts SandboxSagaPlanFacts, material sandboxSagaMaterial,
	policy execution.DispatchPolicy, strategyID domain.StrategyID, policyHash string,
) (sandboxSagaPlanParts, error) {
	parts := sandboxSagaPlanParts{
		submissions:  make([]sandbox.Submission, 0, len(material.Legs)),
		reservations: make([]sandbox.DurableReservation, 0, len(material.Legs)),
		entrySafety:  make(map[sandbox.AccountID]sandbox.EntrySafetySnapshot, len(facts.Admissions)),
		snapshots:    make(map[sandbox.AccountID]sandbox.AccountSnapshotReference, len(facts.Snapshots)),
	}
	for index, leg := range material.Legs {
		submission, reservation, snapshot, admission, err := sandboxSagaPlanLeg(
			facts, material, policy, strategyID, policyHash, index, leg)
		if err != nil {
			return sandboxSagaPlanParts{}, err
		}
		parts.submissions = append(parts.submissions, submission)
		parts.reservations = append(parts.reservations, reservation)
		parts.entrySafety[submission.AccountID] = admission.Safety
		parts.snapshots[submission.AccountID] = snapshot
	}
	return parts, nil
}

func sandboxSagaPlanLeg(facts SandboxSagaPlanFacts, material sandboxSagaMaterial,
	policy execution.DispatchPolicy, strategyID domain.StrategyID, policyHash string,
	index int, leg sandboxSagaLegMaterial,
) (sandbox.Submission, sandbox.DurableReservation, sandbox.AccountSnapshotReference,
	sandbox.StrategySessionAdmission, error) {
	maximum, _ := domain.ParseNotional("10")
	admission, exists := facts.Admissions[leg.Exchange]
	if !exists || admission.Valid() != nil || admission.ApprovedAt != material.ApprovedAt ||
		leg.OrderID.Value() == "" || leg.Instrument.Product != domain.ProductSpot ||
		(leg.Side != domain.SideBuy && leg.Side != domain.SideSell) ||
		leg.Notional.Compare(maximum) > 0 || leg.Notional.String() == "0" {
		return sandbox.Submission{}, sandbox.DurableReservation{}, sandbox.AccountSnapshotReference{},
			sandbox.StrategySessionAdmission{}, fmt.Errorf("sandbox_saga_plan_leg_invalid")
	}
	calculated, err := domain.CalculateNotional(leg.LimitPrice, leg.Quantity, 18)
	if err != nil || calculated.Compare(leg.Notional) != 0 {
		return sandbox.Submission{}, sandbox.DurableReservation{}, sandbox.AccountSnapshotReference{},
			sandbox.StrategySessionAdmission{}, fmt.Errorf("sandbox_saga_plan_leg_invalid")
	}
	snapshot, exists := facts.Snapshots[admission.Work.Account.ID]
	if !exists || !validSandboxSagaPlanSnapshot(admission, snapshot) {
		return sandbox.Submission{}, sandbox.DurableReservation{}, sandbox.AccountSnapshotReference{},
			sandbox.StrategySessionAdmission{}, fmt.Errorf("sandbox_saga_plan_snapshot_invalid")
	}
	clientID := "ax-saga-" + sandboxSagaHash(material.PlanID.String(), []byte(strconv.Itoa(index)))[:24]
	requestHash := sandboxSagaHash("request", []byte(material.PlanID.String()), []byte(leg.OrderID.String()),
		[]byte(clientID), []byte(leg.Instrument.Symbol()), []byte(leg.Side),
		[]byte(leg.Quantity.String()), []byte(leg.LimitPrice.String()))
	submission := sandbox.Submission{PlanID: material.PlanID, OrderID: leg.OrderID,
		AccountID: admission.Work.Account.ID, AccountEpoch: admission.Work.Account.Epoch,
		ClientOrderID: clientID, StrategyID: strategyID, Instrument: leg.Instrument,
		Side: leg.Side, Quantity: leg.Quantity, LimitPrice: leg.LimitPrice, Notional: leg.Notional,
		Style: sandbox.OrderStyleLimitIOC, Action: sandbox.IntentEntry, RequestHash: requestHash,
		PolicyHash: policyHash, ApprovedAt: material.ApprovedAt}
	reservation := sandboxSagaReservation(submission)
	if !validSandboxSagaLegInventory(facts, admission, snapshot, submission, reservation, policy, index) {
		return sandbox.Submission{}, sandbox.DurableReservation{}, sandbox.AccountSnapshotReference{},
			sandbox.StrategySessionAdmission{}, fmt.Errorf("sandbox_saga_plan_inventory_invalid")
	}
	reference := sandbox.AccountSnapshotReference{AccountID: snapshot.AccountID, AccountEpoch: snapshot.Epoch,
		SnapshotHash: snapshot.SnapshotHash, ObservedAt: snapshot.ObservedAt}
	return submission, reservation, reference, admission, nil
}

func validSandboxSagaLegInventory(facts SandboxSagaPlanFacts, admission sandbox.StrategySessionAdmission,
	snapshot sandbox.AccountSnapshot, submission sandbox.Submission,
	reservation sandbox.DurableReservation, policy execution.DispatchPolicy, index int,
) bool {
	initial := policy == execution.DispatchConcurrent || index == 0
	if initial && !sandboxSnapshotCovers(snapshot, reservation) {
		return false
	}
	if policy != execution.DispatchConcurrent || submission.Side != domain.SideSell {
		return true
	}
	owned, exists := facts.OwnedInventory[submission.AccountID]
	required, err := domain.ParseBalance(submission.Quantity.String())
	return exists && err == nil && owned.ValidFor(admission, submission.Instrument.Base) == nil &&
		owned.Available.Compare(required) >= 0
}

func newApprovedSandboxSagaPlan(facts SandboxSagaPlanFacts, material sandboxSagaMaterial,
	policy execution.DispatchPolicy, policyHash string, parts sandboxSagaPlanParts,
) (sandbox.ApprovedSandboxPlan, error) {
	decisionPayload, _ := json.Marshal(struct {
		ID        string          `json:"id"`
		Ordinal   uint64          `json:"ordinal"`
		Action    string          `json:"action"`
		Candidate json.RawMessage `json:"candidate"`
	}{ID: material.CandidateID, Ordinal: material.Ordinal,
		Action: "entry", Candidate: material.CanonicalCandidate})
	decision, err := sandbox.NewStrategyDecisionEvidence(facts.Coordinator,
		replay.Event{Ordinal: material.Ordinal, LogicalTime: material.LogicalTime,
			Canonical: material.CanonicalInput}, decisionPayload)
	if err != nil {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("sandbox_saga_plan_decision_invalid")
	}
	pipeline := sandbox.ApprovalPipelineEvidence{IntentKind: sandbox.ApprovalStrategyIntent,
		IntentHash:        sandboxSagaHash("intent", material.CanonicalInput, material.CanonicalCandidate),
		AllocatorHash:     sandboxSagaHash("allocator", material.AllocationEvidence),
		RiskHash:          sandboxSagaHash("risk", material.RiskEvidence),
		PlannerHash:       sandboxSagaHash("planner", material.PlannerEvidence),
		AssetApprovalHash: sandboxSagaAssetApprovalHash(facts),
		RiskApproved:      true, AssetApproved: true, ObservedAt: material.ApprovedAt}
	expiresAt := material.ApprovedAt.Add(time.Duration(material.LifetimeNanos))
	plan := sandbox.ApprovedSandboxPlan{ID: material.PlanID.String(),
		SessionID: facts.Coordinator.Work.SessionID, Submissions: parts.submissions,
		Reservations: parts.reservations, Arm: facts.Coordinator.Arm,
		MarketEligibility: append([]sandbox.EligibilitySnapshot(nil), facts.MarketEligibility...),
		EntrySafety:       parts.entrySafety, AccountSnapshots: parts.snapshots, StrategyDecision: &decision,
		Pipeline: pipeline, ApprovedAt: material.ApprovedAt,
		ExecutionExpiresAt: &expiresAt,
		ConfigurationID:    facts.Coordinator.Work.ConfigurationID}
	plan.ApprovalHash = pipeline.HashFor(plan)
	if derived, deriveErr := sandbox.ValidatePlanSaga(plan); deriveErr != nil || derived != policy {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("sandbox_saga_plan_invalid")
	}
	return plan, nil
}

func validateSandboxSagaFacts(
	facts SandboxSagaPlanFacts,
	strategy string,
	wantAccounts int,
) error {
	coordinator := facts.Coordinator
	if coordinator.Valid() != nil || coordinator.Work.Strategy != strategy ||
		len(facts.Admissions) != wantAccounts || len(facts.Snapshots) != wantAccounts ||
		len(facts.RiskFacts) != wantAccounts ||
		len(facts.MarketEligibility) == 0 {
		return fmt.Errorf("sandbox_saga_plan_facts_invalid")
	}
	for exchange, admission := range facts.Admissions {
		if admission.Valid() != nil || admission.Work.Account.Exchange != exchange ||
			admission.Work.SessionID != coordinator.Work.SessionID ||
			admission.Work.Strategy != strategy ||
			admission.Work.Instrument != coordinator.Work.Instrument ||
			admission.Work.ConfigurationID != coordinator.Work.ConfigurationID ||
			admission.Work.ConfigurationHash != coordinator.Work.ConfigurationHash ||
			admission.Work.StrategySetHash != coordinator.Work.StrategySetHash ||
			admission.Work.SessionRevision != coordinator.Work.SessionRevision ||
			admission.Work.StrategyRevision != coordinator.Work.StrategyRevision ||
			admission.ApprovedAt != coordinator.ApprovedAt ||
			!reflect.DeepEqual(admission.Arm, coordinator.Arm) {
			return fmt.Errorf("sandbox_saga_plan_facts_invalid")
		}
		snapshot, exists := facts.Snapshots[admission.Work.Account.ID]
		if !exists {
			return fmt.Errorf("sandbox_saga_plan_facts_invalid")
		}
		riskFacts, exists := facts.RiskFacts[admission.Work.Account.ID]
		if !exists || riskFacts.ValidFor(admission.Work, snapshot, coordinator.ApprovedAt) != nil {
			return fmt.Errorf("sandbox_saga_plan_facts_invalid")
		}
	}
	return nil
}

func sandboxSagaEligibilityMatches(
	facts SandboxSagaPlanFacts,
	legs []sandboxSagaLegMaterial,
) bool {
	expected := make(map[string]struct{}, len(legs))
	for _, leg := range legs {
		expected[string(leg.Exchange)+"\x00"+leg.Instrument.Symbol()] = struct{}{}
	}
	if len(expected) != len(facts.MarketEligibility) {
		return false
	}
	seen := make(map[string]struct{}, len(facts.MarketEligibility))
	for _, eligibility := range facts.MarketEligibility {
		key := eligibility.Exchange + "\x00" + eligibility.Instrument
		if _, wanted := expected[key]; !wanted || !eligibility.Eligible ||
			eligibility.ObservedAt.IsZero() || eligibility.ObservedAt.Location() != time.UTC ||
			eligibility.ObservedAt.After(facts.Coordinator.ApprovedAt) ||
			facts.Coordinator.ApprovedAt.Sub(eligibility.ObservedAt) > 250*time.Millisecond {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	for _, admission := range facts.Admissions {
		key := admission.Eligibility.Exchange + "\x00" + admission.Eligibility.Instrument
		if _, exists := seen[key]; !exists {
			return false
		}
	}
	return true
}

func validSandboxSagaPlanSnapshot(
	admission sandbox.StrategySessionAdmission,
	snapshot sandbox.AccountSnapshot,
) bool {
	return snapshot.Validate() == nil &&
		snapshot.AccountID == admission.Work.Account.ID &&
		snapshot.Epoch == admission.Work.Account.Epoch &&
		!snapshot.ObservedAt.After(admission.ApprovedAt) &&
		admission.ApprovedAt.Sub(snapshot.ObservedAt) <= 250*time.Millisecond
}

func sandboxSnapshotCovers(
	snapshot sandbox.AccountSnapshot,
	reservation sandbox.DurableReservation,
) bool {
	required, err := domain.ParseBalance(reservation.Quantity)
	if err != nil {
		return false
	}
	for _, balance := range snapshot.Balances {
		if string(balance.Asset) == reservation.Asset {
			return balance.Available.Compare(required) >= 0
		}
	}
	return false
}

func sandboxSagaReservation(submission sandbox.Submission) sandbox.DurableReservation {
	asset, quantity := string(submission.Instrument.Quote), submission.Notional.String()
	if submission.Side == domain.SideSell {
		asset, quantity = string(submission.Instrument.Base), submission.Quantity.String()
	}
	return sandbox.DurableReservation{ID: "saga-reservation-" + sandboxSagaHash(
		"reservation", []byte(submission.PlanID.String()), []byte(submission.OrderID.String()))[:24],
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		OrderID: submission.OrderID.String(), Asset: asset, Quantity: quantity}
}

func sandboxSagaAssetApprovalHash(facts SandboxSagaPlanFacts) string {
	values := make([]string, 0, len(facts.Snapshots)+len(facts.MarketEligibility))
	for _, snapshot := range facts.Snapshots {
		values = append(values, string(snapshot.AccountID)+"\x00"+snapshot.SnapshotHash)
	}
	for _, eligibility := range facts.MarketEligibility {
		values = append(values, eligibility.Exchange+"\x00"+eligibility.Instrument+"\x00"+
			eligibility.ObservedAt.Format(time.RFC3339Nano))
	}
	sort.Strings(values)
	encoded, _ := json.Marshal(values)
	return sandboxSagaHash("asset-approval", encoded)
}

func sandboxSagaHash(label string, values ...[]byte) string {
	parts := make([]string, 0, len(values)+1)
	parts = append(parts, label)
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strategyInputHash(parts...)
}

var _ sandbox.SagaSandboxPlanBuilder = (*TriangularSandboxPlanBuilder)(nil)
var _ sandbox.SagaSandboxPlanBuilder = (*CrossExchangeSandboxPlanBuilder)(nil)
