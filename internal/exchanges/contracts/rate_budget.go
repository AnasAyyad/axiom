package exchangecontracts

import (
	"sync"
	"time"
)

// BudgetClass determines whether reserved recovery capacity is available.
type BudgetClass string

// Public work cannot consume the recovery reserve.
const (
	BudgetPublic   BudgetClass = "public"
	BudgetRecovery BudgetClass = "recovery"
)

// BudgetConfig defines an integer weighted token budget.
type BudgetConfig struct {
	Capacity        uint64
	RecoveryReserve uint64
	RefillAmount    uint64
	RefillInterval  time.Duration
}

// BudgetDecision is one deterministic admission result.
type BudgetDecision struct {
	Granted    bool
	RetryAfter time.Duration
	Remaining  uint64
}

// RateBudget is a shared deterministic weighted budget.
type RateBudget struct {
	mutex     sync.Mutex
	config    BudgetConfig
	available uint64
	updatedAt time.Duration
}

// NewRateBudget constructs a full budget at a nonnegative logical time.
func NewRateBudget(config BudgetConfig, now time.Duration) (*RateBudget, error) {
	if !validBudgetConfig(config) || now < 0 {
		return nil, NewError(ErrorValidation, OperationCapability, 0)
	}
	return &RateBudget{config: config, available: config.Capacity, updatedAt: now}, nil
}

// TryAcquire admits weighted work without sleeping or overspending the reserve.
func (budget *RateBudget) TryAcquire(now time.Duration, class BudgetClass, weight uint64) (BudgetDecision, error) {
	if budget == nil || now < 0 || weight == 0 || (class != BudgetPublic && class != BudgetRecovery) {
		return BudgetDecision{}, NewError(ErrorValidation, OperationCapability, 0)
	}
	budget.mutex.Lock()
	defer budget.mutex.Unlock()
	if now < budget.updatedAt {
		return BudgetDecision{}, NewError(ErrorTimestamp, OperationCapability, 0)
	}
	if weight > budget.config.Capacity {
		return BudgetDecision{}, NewError(ErrorFilter, OperationCapability, 0)
	}
	budget.refill(now)
	spendable := budget.available
	if class == BudgetPublic {
		if spendable <= budget.config.RecoveryReserve {
			spendable = 0
		} else {
			spendable -= budget.config.RecoveryReserve
		}
	}
	if weight <= spendable {
		budget.available -= weight
		return BudgetDecision{Granted: true, Remaining: budget.available}, nil
	}
	deficit := weight - spendable
	intervals := (deficit + budget.config.RefillAmount - 1) / budget.config.RefillAmount
	return BudgetDecision{
		RetryAfter: durationProduct(intervals, budget.config.RefillInterval),
		Remaining:  budget.available,
	}, nil
}

func (budget *RateBudget) refill(now time.Duration) {
	elapsed := now - budget.updatedAt
	intervals := uint64(elapsed / budget.config.RefillInterval)
	if intervals == 0 {
		return
	}
	missing := budget.config.Capacity - budget.available
	additions := missing
	if intervals <= ^uint64(0)/budget.config.RefillAmount &&
		intervals*budget.config.RefillAmount < missing {
		additions = intervals * budget.config.RefillAmount
	}
	if additions > missing {
		additions = missing
	}
	budget.available += additions
	budget.updatedAt += durationProduct(intervals, budget.config.RefillInterval)
}

func validBudgetConfig(config BudgetConfig) bool {
	return config.Capacity > 0 && config.RecoveryReserve < config.Capacity &&
		config.RefillAmount > 0 && config.RefillAmount <= config.Capacity && config.RefillInterval > 0
}

func durationProduct(multiplier uint64, duration time.Duration) time.Duration {
	maximum := uint64(^uint64(0) >> 1)
	if uint64(duration) > 0 && multiplier > maximum/uint64(duration) {
		return time.Duration(maximum)
	}
	return time.Duration(multiplier * uint64(duration))
}
