package execution

import (
	"context"

	"axiom/internal/domain"
)

// ApprovedIntent is an opaque risk-approved token. Its fields deliberately do
// not expose a strategy candidate that a planner could silently reinterpret.
type ApprovedIntent struct {
	DecisionID   domain.DecisionID
	ApprovalHash string
	PolicyHash   string
}

// PlannedLeg is one exact filter-valid simulated spot action.
type PlannedLeg struct {
	Index         uint32
	OrderID       domain.VirtualOrderID
	ClientOrderID string
	Instrument    domain.Instrument
	Side          domain.Side
	Quantity      domain.Quantity
	LimitPrice    domain.Price
	ExpiresAt     uint64
	Maker         bool
}

// SimulatedPlan is the only broker-submittable execution object in V1A.
type SimulatedPlan struct {
	ID                  domain.ExecutionPlanID
	Intent              ApprovedIntent
	Namespace           string
	DecisionLogicalTime uint64
	Legs                []PlannedLeg
}

// ExecutionPlanner converts only opaque approved intent into simulated plans.
type ExecutionPlanner interface {
	Plan(context.Context, ApprovedIntent) (SimulatedPlan, error)
}

// Broker can only schedule or cancel simulated plans. No external transport or
// exchange credential is part of this V1A contract.
type Broker interface {
	Submit(context.Context, SimulatedPlan) ([]OrderEvent, error)
	Cancel(context.Context, domain.VirtualOrderID, string) ([]OrderEvent, error)
}
