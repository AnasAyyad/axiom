package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/triangular"
)

// SandboxSagaPlanFacts are the complete non-secret account and public-market
// facts needed to project one centrally approved strategy saga into the
// durable dispatcher. A Cross-Exchange coordinator must supply both account
// admissions and strategy-owned sell inventory; no opposite-engine credential
// or adapter is part of this contract.
type SandboxSagaPlanFacts struct {
	Coordinator       sandbox.StrategySessionAdmission
	Admissions        map[sandbox.Exchange]sandbox.StrategySessionAdmission
	Snapshots         map[sandbox.AccountID]sandbox.AccountSnapshot
	RiskFacts         map[sandbox.AccountID]sandbox.StrategyRiskFacts
	MarketEligibility []sandbox.EligibilitySnapshot
	OwnedInventory    map[sandbox.AccountID]sandbox.StrategyOwnedInventory
}

type sandboxSagaLegMaterial struct {
	OrderID    domain.VirtualOrderID
	Exchange   sandbox.Exchange
	Instrument domain.Instrument
	Side       domain.Side
	Quantity   domain.Quantity
	LimitPrice domain.Price
	Notional   domain.Notional
}

type sandboxSagaMaterial struct {
	Strategy           string
	PlanID             domain.ExecutionPlanID
	CandidateID        string
	Ordinal            uint64
	LogicalTime        uint64
	ApprovedAt         time.Time
	LifetimeNanos      uint64
	CanonicalInput     json.RawMessage
	CanonicalCandidate json.RawMessage
	AllocationEvidence json.RawMessage
	RiskEvidence       json.RawMessage
	PlannerEvidence    json.RawMessage
	Legs               []sandboxSagaLegMaterial
}

// TriangularSandboxPlanBuilder is the credential-free bridge from the exact
// strategy-owned sequential saga to one durable three-leg dispatcher plan.
type TriangularSandboxPlanBuilder struct{ facts SandboxSagaPlanFacts }

// NewTriangularSandboxPlanBuilder binds durable facts to triangular planning.
func NewTriangularSandboxPlanBuilder(
	facts SandboxSagaPlanFacts,
) (*TriangularSandboxPlanBuilder, error) {
	if validateSandboxSagaFacts(facts, sandbox.StrategyTriangular, 1) != nil {
		return nil, fmt.Errorf("triangular_sandbox_plan_builder_invalid")
	}
	return &TriangularSandboxPlanBuilder{facts: facts}, nil
}

// BuildSandboxSagaPlan builds a bounded sequential triangular execution plan.
func (builder *TriangularSandboxPlanBuilder) BuildSandboxSagaPlan(
	ctx context.Context,
	planned backtest.SagaPlan,
) (sandbox.ApprovedSandboxPlan, error) {
	if builder == nil || ctx == nil {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("triangular_sandbox_plan_builder_invalid")
	}
	approved, err := triangular.DecodeApprovedSandboxSaga(planned)
	if err != nil {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("triangular_sandbox_plan_invalid")
	}
	legs, err := approved.SandboxLegs()
	if err != nil {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("triangular_sandbox_plan_invalid")
	}
	material := sandboxSagaMaterial{Strategy: sandbox.StrategyTriangular,
		PlanID: approved.Execution.ID, CandidateID: approved.Candidate.ID,
		Ordinal: approved.Input.Ordinal, LogicalTime: approved.Input.LogicalTime,
		ApprovedAt:    approved.Input.Now,
		LifetimeNanos: approved.Candidate.ExpiresOffsetNanos - approved.Input.LogicalTime}
	material.CanonicalInput, _ = json.Marshal(approved.Input)
	material.CanonicalCandidate, _ = json.Marshal(approved.Candidate)
	material.AllocationEvidence, _ = json.Marshal(approved.Claims)
	material.RiskEvidence, _ = json.Marshal(approved.Decision)
	material.PlannerEvidence, _ = json.Marshal(approved.Execution)
	material.Legs = make([]sandboxSagaLegMaterial, 0, len(legs))
	for _, leg := range legs {
		material.Legs = append(material.Legs, sandboxSagaLegMaterial{
			OrderID: leg.OrderID, Exchange: sandbox.Exchange(approved.Input.Exchange),
			Instrument: leg.Instrument, Side: leg.Side, Quantity: leg.Quantity,
			LimitPrice: leg.LimitPrice, Notional: leg.Notional,
		})
	}
	return assembleSandboxSagaPlan(builder.facts, material, execution.DispatchSequential)
}

// CrossExchangeSandboxPlanBuilder projects a paired strategy saga while
// retaining independent account snapshots, safety facts, and sell ownership.
type CrossExchangeSandboxPlanBuilder struct{ facts SandboxSagaPlanFacts }

// NewCrossExchangeSandboxPlanBuilder binds durable facts to paired planning.
func NewCrossExchangeSandboxPlanBuilder(
	facts SandboxSagaPlanFacts,
) (*CrossExchangeSandboxPlanBuilder, error) {
	if validateSandboxSagaFacts(facts, sandbox.StrategyCrossExchangeArbitrage, 2) != nil ||
		facts.Coordinator.Work.Account.Exchange != sandbox.ExchangeBinance {
		return nil, fmt.Errorf("cross_exchange_sandbox_plan_builder_invalid")
	}
	return &CrossExchangeSandboxPlanBuilder{facts: facts}, nil
}

// BuildSandboxSagaPlan builds a bounded paired cross-exchange execution plan.
func (builder *CrossExchangeSandboxPlanBuilder) BuildSandboxSagaPlan(
	ctx context.Context,
	planned backtest.SagaPlan,
) (sandbox.ApprovedSandboxPlan, error) {
	if builder == nil || ctx == nil {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("cross_exchange_sandbox_plan_builder_invalid")
	}
	approved, err := crossarb.DecodeApprovedSandboxSaga(planned)
	if err != nil {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("cross_exchange_sandbox_plan_invalid")
	}
	legs, err := approved.SandboxLegs()
	if err != nil {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("cross_exchange_sandbox_plan_invalid")
	}
	material := sandboxSagaMaterial{Strategy: sandbox.StrategyCrossExchangeArbitrage,
		PlanID: approved.Execution.ID, CandidateID: approved.Candidate.ID,
		Ordinal: approved.Input.Ordinal, LogicalTime: approved.Input.LogicalTime,
		ApprovedAt:    approved.Input.Now,
		LifetimeNanos: approved.Candidate.ExpiresOffsetNanos - approved.Input.LogicalTime}
	material.CanonicalInput, _ = json.Marshal(approved.Input)
	material.CanonicalCandidate, _ = json.Marshal(approved.Candidate)
	material.AllocationEvidence, _ = json.Marshal(approved.Claims)
	material.RiskEvidence, _ = json.Marshal(approved.Decision)
	material.PlannerEvidence, _ = json.Marshal(approved.Execution)
	material.Legs = make([]sandboxSagaLegMaterial, 0, len(legs))
	for _, leg := range legs {
		material.Legs = append(material.Legs, sandboxSagaLegMaterial{
			OrderID: leg.OrderID, Exchange: sandbox.Exchange(leg.Exchange),
			Instrument: leg.Instrument, Side: leg.Side, Quantity: leg.Quantity,
			LimitPrice: leg.LimitPrice, Notional: leg.Notional,
		})
	}
	return assembleSandboxSagaPlan(builder.facts, material, execution.DispatchConcurrent)
}

func assembleSandboxSagaPlan(
	facts SandboxSagaPlanFacts,
	material sandboxSagaMaterial,
	policy execution.DispatchPolicy,
) (sandbox.ApprovedSandboxPlan, error) {
	if !validSandboxSagaMaterial(facts, material, policy) {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("sandbox_saga_plan_material_invalid")
	}
	strategyID, err := domain.NewStrategyID(material.Strategy)
	if err != nil {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("sandbox_saga_plan_material_invalid")
	}
	policyHash := sandboxSagaHash("risk-policy", material.RiskEvidence)
	parts, err := buildSandboxSagaPlanParts(facts, material, policy, strategyID, policyHash)
	if err != nil {
		return sandbox.ApprovedSandboxPlan{}, err
	}
	if !sandboxSagaEligibilityMatches(facts, material.Legs) {
		return sandbox.ApprovedSandboxPlan{}, fmt.Errorf("sandbox_saga_plan_market_invalid")
	}
	return newApprovedSandboxSagaPlan(facts, material, policy, policyHash, parts)
}

func validSandboxSagaMaterial(facts SandboxSagaPlanFacts, material sandboxSagaMaterial,
	policy execution.DispatchPolicy,
) bool {
	wantLegs, wantAccounts := 3, 1
	if policy == execution.DispatchConcurrent {
		wantLegs, wantAccounts = 2, 2
	}
	return validateSandboxSagaFacts(facts, material.Strategy, wantAccounts) == nil &&
		material.PlanID.Value() != "" && material.CandidateID != "" &&
		material.Ordinal != 0 && material.LogicalTime != 0 &&
		!material.ApprovedAt.IsZero() && material.ApprovedAt.Location() == time.UTC &&
		material.LifetimeNanos != 0 && material.LifetimeNanos <= uint64(250*time.Millisecond) &&
		json.Valid(material.CanonicalInput) && json.Valid(material.CanonicalCandidate) &&
		json.Valid(material.AllocationEvidence) && json.Valid(material.RiskEvidence) &&
		json.Valid(material.PlannerEvidence) && len(material.Legs) == wantLegs &&
		facts.Coordinator.ApprovedAt == material.ApprovedAt
}
