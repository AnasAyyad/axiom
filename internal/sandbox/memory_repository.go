package sandbox

import (
	"context"
	"fmt"
	"sync"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"

	"github.com/cockroachdb/apd/v3"
)

// memoryDispatcherRepository is the deterministic dispatcher recovery model repository. The
// PostgreSQL implementation must pass the same behavior suite.
type memoryDispatcherRepository struct {
	mutex           sync.Mutex
	outbox          map[string]SubmissionOutbox
	orderOutbox     map[string]string
	clientIDs       map[string]string
	reducers        map[string]*execution.OrderReducer
	privateEvents   map[string]PrivateEvent
	reducedEvents   map[string]bool
	reservations    map[string]DurableReservation
	dailyReserved   map[string]*apd.Decimal
	plans           map[string]ApprovedSandboxPlan
	planStates      map[string]string
	reconciliations map[string]ReconciliationResult
}

func newMemoryDispatcherRepository() *memoryDispatcherRepository {
	return &memoryDispatcherRepository{
		outbox: map[string]SubmissionOutbox{}, orderOutbox: map[string]string{},
		clientIDs: map[string]string{}, reducers: map[string]*execution.OrderReducer{},
		privateEvents: map[string]PrivateEvent{}, reducedEvents: map[string]bool{},
		reservations:  map[string]DurableReservation{},
		dailyReserved: map[string]*apd.Decimal{}, plans: map[string]ApprovedSandboxPlan{},
		planStates: map[string]string{}, reconciliations: map[string]ReconciliationResult{},
	}
}

// ApprovePlan atomically applies the model plan, reservations, cap, and outbox.
func (repository *memoryDispatcherRepository) ApprovePlan(
	ctx context.Context,
	plan ApprovedSandboxPlan,
	limits SubmissionLimits,
	kill KillPoint,
) error {
	if kill == nil {
		kill = NoKillPoint{}
	}
	if err := kill.Hit(ctx, KillBeforePlanCommit); err != nil {
		return err
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	prepared, err := repository.validatePlan(plan, limits)
	if err != nil {
		return err
	}
	if _, exists := repository.plans[plan.ID]; exists {
		return contractError("plan_duplicate")
	}
	repository.storeApprovedPlan(plan, prepared)
	if err := kill.Hit(ctx, KillAfterPlanCommit); err != nil {
		return err
	}
	return nil
}

func (repository *memoryDispatcherRepository) storeApprovedPlan(plan ApprovedSandboxPlan, prepared preparedPlan) {
	repository.plans[plan.ID] = plan
	repository.planStates[plan.ID] = "APPROVED"
	policy, _ := ValidatePlanSaga(plan)
	for index, submission := range plan.Submissions {
		outboxID := fmt.Sprintf("%s-%02d", plan.ID, index)
		state := OutboxPending
		var dependsOn *uint32
		if policy == execution.DispatchSequential && index > 0 {
			state = OutboxWaiting
			dependency := uint32(index - 1)
			dependsOn = &dependency
		}
		repository.outbox[outboxID] = SubmissionOutbox{
			ID: outboxID, Submission: submission, LegIndex: uint32(index),
			DependsOn: dependsOn, State: state, UpdatedAt: plan.ApprovedAt,
		}
		repository.orderOutbox[submission.OrderID.String()] = outboxID
		repository.clientIDs[clientIdentity(submission)] = submission.OrderID.String()
		repository.reducers[submission.OrderID.String()] = prepared.reducers[index]
	}
	for index, reservation := range plan.Reservations {
		reservation.State = ReservationActive
		if policy == execution.DispatchSequential && index > 0 {
			reservation.State = ReservationWaiting
		}
		repository.reservations[reservation.ID] = reservation
	}
	repository.dailyReserved[prepared.day] = prepared.dailyTotal
}

type preparedPlan struct {
	reducers   []*execution.OrderReducer
	day        string
	dailyTotal *apd.Decimal
}

func (repository *memoryDispatcherRepository) validatePlan(
	plan ApprovedSandboxPlan,
	limits SubmissionLimits,
) (preparedPlan, error) {
	maxOrder, maxDaily, err := validatePlanHeader(plan, limits)
	if err != nil {
		return preparedPlan{}, err
	}
	if _, err = ValidatePlanSaga(plan); err != nil {
		return preparedPlan{}, err
	}
	reducers, planNotional, err := repository.validatePlanLegs(plan, limits, maxOrder)
	if err != nil {
		return preparedPlan{}, err
	}
	day := plan.ApprovedAt.Format("2006-01-02")
	total := apd.New(0, 0)
	if prior := repository.dailyReserved[day]; prior != nil {
		total.Set(prior)
	}
	if _, err = apd.BaseContext.Add(total, total, planNotional); err != nil || total.Cmp(maxDaily) > 0 {
		return preparedPlan{}, contractError("daily_cap")
	}
	return preparedPlan{reducers: reducers, day: day, dailyTotal: total}, nil
}

func validatePlanHeader(
	plan ApprovedSandboxPlan,
	limits SubmissionLimits,
) (domain.Notional, *apd.Decimal, error) {
	entry, actionErr := RequiresEntryArm(plan)
	if plan.ID == "" || plan.SessionID == "" || plan.ApprovedAt.IsZero() ||
		plan.ApprovedAt.Location() != time.UTC ||
		plan.Arm.Validate() != nil || actionErr != nil ||
		(entry && !plan.Arm.Active(plan.ApprovedAt)) ||
		plan.ApprovalHash == "" || plan.ConfigurationID == "" ||
		len(plan.Submissions) == 0 || len(plan.Submissions) > 3 ||
		len(plan.Submissions) != len(plan.Reservations) {
		return domain.Notional{}, nil, contractError("plan_invalid")
	}
	if err := plan.Pipeline.ValidateFor(plan); err != nil {
		return domain.Notional{}, nil, err
	}
	maxOrder, err := domain.ParseNotional(limits.MaximumOrderNotional)
	if err != nil || limits.MaximumOpenPerAccount != 1 || limits.MaximumOpenGlobal != 2 {
		return domain.Notional{}, nil, contractError("limits_invalid")
	}
	maxDaily, _, err := apd.NewFromString(limits.MaximumDailyNotional)
	if err != nil || maxDaily.Sign() <= 0 {
		return domain.Notional{}, nil, contractError("limits_invalid")
	}
	return maxOrder, maxDaily, nil
}

func (repository *memoryDispatcherRepository) validatePlanLegs(
	plan ApprovedSandboxPlan,
	limits SubmissionLimits,
	maxOrder domain.Notional,
) ([]*execution.OrderReducer, *apd.Decimal, error) {
	exchanges := make(map[AccountID]Exchange, len(plan.Submissions))
	for _, submission := range plan.Submissions {
		exchanges[submission.AccountID] = exchangeForAccount(submission.AccountID)
	}
	if err := ValidateSubmissionTopology(plan.Submissions, exchanges); err != nil {
		return nil, nil, err
	}
	armAccounts := make(map[AccountID]struct{}, len(plan.Arm.AccountIDs))
	for _, account := range plan.Arm.AccountIDs {
		armAccounts[account] = struct{}{}
	}
	openByAccount, openGlobal := repository.openCapacity()
	policy, err := ValidatePlanSaga(plan)
	if err != nil {
		return nil, nil, err
	}
	if err = validateInitialPlanCapacity(plan, policy, openByAccount, openGlobal, limits); err != nil {
		return nil, nil, err
	}
	reducers := make([]*execution.OrderReducer, 0, len(plan.Submissions))
	planNotional := apd.New(0, 0)
	for index, submission := range plan.Submissions {
		reducer, value, err := repository.validatePlanLeg(
			plan, submission, plan.Reservations[index], maxOrder, armAccounts,
			0, 0, limits,
		)
		if err != nil {
			return nil, nil, err
		}
		if _, err = apd.BaseContext.Add(planNotional, planNotional, value); err != nil {
			return nil, nil, contractError("notional_invalid")
		}
		reducers = append(reducers, reducer)
	}
	return reducers, planNotional, nil
}

func validateInitialPlanCapacity(plan ApprovedSandboxPlan, policy execution.DispatchPolicy,
	openByAccount map[AccountID]int, openGlobal int, limits SubmissionLimits,
) error {
	initialByAccount := make(map[AccountID]int)
	initialGlobal := 0
	for index, submission := range plan.Submissions {
		if policy == execution.DispatchConcurrent || index == 0 {
			initialByAccount[submission.AccountID]++
			initialGlobal++
		}
	}
	if openGlobal+initialGlobal > limits.MaximumOpenGlobal {
		return contractError("global_open_cap")
	}
	for account, count := range initialByAccount {
		if openByAccount[account]+count > limits.MaximumOpenPerAccount {
			return contractError("account_open_cap")
		}
	}
	return nil
}
