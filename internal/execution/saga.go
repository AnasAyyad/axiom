package execution

import (
	"sort"

	"axiom/internal/domain"
)

// DispatchPolicy controls deterministic leg dependency handling.
type DispatchPolicy string

// Supported multi-leg dispatch policies.
const (
	DispatchSequential DispatchPolicy = "sequential"
	DispatchConcurrent DispatchPolicy = "concurrent"
)

// PlanState is one durable execution-saga lifecycle state.
type PlanState string

// Supported execution-saga states.
const (
	PlanPlanned          PlanState = "planned"
	PlanActive           PlanState = "active"
	PlanCompleted        PlanState = "completed"
	PlanFailed           PlanState = "failed"
	PlanRecoveryRequired PlanState = "recovery_required"
	PlanRecovered        PlanState = "recovered"
	PlanQuarantined      PlanState = "quarantined"
)

// SagaLeg persists one plan leg and its dependency.
type SagaLeg struct {
	Index     uint32                `json:"index"`
	OrderID   domain.VirtualOrderID `json:"order_id"`
	DependsOn *uint32               `json:"depends_on,omitempty"`
	State     OrderState            `json:"state"`
}

// Exposure records exact stranded or remaining owned inventory.
type Exposure struct {
	Asset    domain.AssetSymbol `json:"asset"`
	Quantity domain.Balance     `json:"quantity"`
}

// RecoveryAttempt is one immutable bounded saga recovery fact.
type RecoveryAttempt struct {
	Attempt     uint32             `json:"attempt"`
	Action      string             `json:"action"`
	Disposition string             `json:"disposition"`
	LossAsset   domain.AssetSymbol `json:"loss_asset"`
	Loss        domain.Balance     `json:"loss"`
}

// Saga is one immutable snapshot of a multi-leg execution aggregate.
type Saga struct {
	ID                domain.ExecutionPlanID `json:"id"`
	Policy            DispatchPolicy         `json:"policy"`
	State             PlanState              `json:"state"`
	Legs              []SagaLeg              `json:"legs"`
	Reservations      []domain.ReservationID `json:"reservations"`
	RemainingExposure []Exposure             `json:"remaining_exposure"`
	RecoveryAttempts  []RecoveryAttempt      `json:"recovery_attempts"`
	FinalDisposition  string                 `json:"final_disposition"`
	Revision          uint64                 `json:"revision"`
}

// SagaReducer serializes leg, recovery, and final-disposition facts.
type SagaReducer struct{ saga Saga }

// NewSaga constructs one validated persisted execution plan.
func NewSaga(
	id domain.ExecutionPlanID,
	policy DispatchPolicy,
	legs []SagaLeg,
	reservations []domain.ReservationID,
) (*SagaReducer, error) {
	if id.Value() == "" || !validDispatch(policy) || len(legs) == 0 || len(reservations) == 0 {
		return nil, executionError("saga_invalid")
	}
	legs = append([]SagaLeg(nil), legs...)
	sort.Slice(legs, func(left, right int) bool { return legs[left].Index < legs[right].Index })
	if !validSagaLegs(legs) || !validReservationIDs(reservations) {
		return nil, executionError("saga_invalid")
	}
	return &SagaReducer{saga: Saga{ID: id, Policy: policy, State: PlanPlanned,
		Legs: legs, Reservations: append([]domain.ReservationID(nil), reservations...), Revision: 1}}, nil
}

// Snapshot returns a defensive immutable saga copy.
func (reducer *SagaReducer) Snapshot() Saga { return cloneSaga(reducer.saga) }

// Activate marks a planned saga dispatchable.
func (reducer *SagaReducer) Activate() error {
	if reducer.saga.State != PlanPlanned {
		return executionError("saga_activation_rejected")
	}
	reducer.saga.State, reducer.saga.Revision = PlanActive, reducer.saga.Revision+1
	return nil
}

// ApplyOrder merges one canonical leg order snapshot and derives plan state.
func (reducer *SagaReducer) ApplyOrder(order Order, exposure []Exposure) error {
	if reducer.saga.State != PlanActive && reducer.saga.State != PlanRecoveryRequired {
		return executionError("saga_order_rejected")
	}
	index := -1
	for candidate := range reducer.saga.Legs {
		if reducer.saga.Legs[candidate].OrderID == order.Identity.ID {
			index = candidate
			break
		}
	}
	if index < 0 || order.Identity.PlanID != reducer.saga.ID || !validExposure(exposure) {
		return executionError("saga_order_rejected")
	}
	reducer.saga.Legs[index].State = order.State
	reducer.saga.RemainingExposure = append([]Exposure(nil), exposure...)
	reducer.deriveState()
	reducer.saga.Revision++
	return nil
}

// AddRecovery records one ordered attempt while recovery is required.
func (reducer *SagaReducer) AddRecovery(attempt RecoveryAttempt) error {
	want := uint32(len(reducer.saga.RecoveryAttempts) + 1)
	if reducer.saga.State != PlanRecoveryRequired || attempt.Attempt != want || attempt.Action == "" ||
		attempt.Disposition == "" {
		return executionError("recovery_attempt_rejected")
	}
	reducer.saga.RecoveryAttempts = append(reducer.saga.RecoveryAttempts, attempt)
	reducer.saga.Revision++
	return nil
}

// ResolveRecovery closes exposure or quarantines unresolved state.
func (reducer *SagaReducer) ResolveRecovery(disposition string, quarantined bool) error {
	if reducer.saga.State != PlanRecoveryRequired || disposition == "" || len(reducer.saga.RecoveryAttempts) == 0 {
		return executionError("recovery_resolution_rejected")
	}
	reducer.saga.State = PlanRecovered
	if quarantined {
		reducer.saga.State = PlanQuarantined
	}
	reducer.saga.FinalDisposition, reducer.saga.Revision = disposition, reducer.saga.Revision+1
	return nil
}

func (reducer *SagaReducer) deriveState() {
	filled, terminal, failed := 0, 0, 0
	for _, leg := range reducer.saga.Legs {
		switch leg.State {
		case OrderFilled:
			filled++
			terminal++
		case OrderCanceled, OrderRejected, OrderExpired:
			failed++
			terminal++
		case OrderPartiallyFilled, OrderUnknown, OrderRecoveryRequired:
			failed++
		}
	}
	if filled == len(reducer.saga.Legs) {
		reducer.saga.State, reducer.saga.FinalDisposition = PlanCompleted, "all_legs_filled"
	} else if filled > 0 || (failed > 0 && len(reducer.saga.RemainingExposure) > 0) {
		reducer.saga.State = PlanRecoveryRequired
	} else if terminal == len(reducer.saga.Legs) {
		reducer.saga.State, reducer.saga.FinalDisposition = PlanFailed, "no_leg_filled"
	}
}

func validDispatch(policy DispatchPolicy) bool {
	return policy == DispatchSequential || policy == DispatchConcurrent
}

func validSagaLegs(legs []SagaLeg) bool {
	seen := make(map[string]struct{}, len(legs))
	for index, leg := range legs {
		if leg.Index != uint32(index) || leg.OrderID.Value() == "" || leg.State != OrderCreated {
			return false
		}
		if _, duplicate := seen[leg.OrderID.String()]; duplicate {
			return false
		}
		seen[leg.OrderID.String()] = struct{}{}
		if leg.DependsOn != nil && (*leg.DependsOn >= leg.Index || policyIndexInvalid(*leg.DependsOn, len(legs))) {
			return false
		}
	}
	return true
}

func policyIndexInvalid(index uint32, length int) bool { return uint64(index) >= uint64(length) }

func validReservationIDs(ids []domain.ReservationID) bool {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id.Value() == "" {
			return false
		}
		if _, duplicate := seen[id.String()]; duplicate {
			return false
		}
		seen[id.String()] = struct{}{}
	}
	return true
}

func validExposure(exposure []Exposure) bool {
	seen := make(map[domain.AssetSymbol]struct{}, len(exposure))
	for _, item := range exposure {
		if _, duplicate := seen[item.Asset]; duplicate {
			return false
		}
		seen[item.Asset] = struct{}{}
		if _, err := domain.ParseAssetSymbol(string(item.Asset)); err != nil {
			return false
		}
	}
	return true
}

func cloneSaga(saga Saga) Saga {
	saga.Legs = append([]SagaLeg(nil), saga.Legs...)
	saga.Reservations = append([]domain.ReservationID(nil), saga.Reservations...)
	saga.RemainingExposure = append([]Exposure(nil), saga.RemainingExposure...)
	saga.RecoveryAttempts = append([]RecoveryAttempt(nil), saga.RecoveryAttempts...)
	for index := range saga.Legs {
		if saga.Legs[index].DependsOn != nil {
			value := *saga.Legs[index].DependsOn
			saga.Legs[index].DependsOn = &value
		}
	}
	return saga
}
