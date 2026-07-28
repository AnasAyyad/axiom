package sandbox

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"

	"github.com/cockroachdb/apd/v3"
)

// memoryDispatcherRepository is the deterministic C3 model repository. The
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
	repository.plans[plan.ID] = plan
	repository.planStates[plan.ID] = "APPROVED"
	for index, submission := range plan.Submissions {
		outboxID := fmt.Sprintf("%s-%02d", plan.ID, index)
		repository.outbox[outboxID] = SubmissionOutbox{
			ID: outboxID, Submission: submission, State: OutboxPending, UpdatedAt: plan.ApprovedAt,
		}
		repository.orderOutbox[submission.OrderID.String()] = outboxID
		repository.clientIDs[clientIdentity(submission)] = submission.OrderID.String()
		repository.reducers[submission.OrderID.String()] = prepared.reducers[index]
	}
	for _, reservation := range plan.Reservations {
		reservation.State = ReservationActive
		repository.reservations[reservation.ID] = reservation
	}
	repository.dailyReserved[prepared.day] = prepared.dailyTotal
	if err := kill.Hit(ctx, KillAfterPlanCommit); err != nil {
		return err
	}
	return nil
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
		len(plan.Submissions) == 0 || len(plan.Submissions) > 2 ||
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
	seenAccounts := make(map[AccountID]int)
	reducers := make([]*execution.OrderReducer, 0, len(plan.Submissions))
	planNotional := apd.New(0, 0)
	for index, submission := range plan.Submissions {
		seenAccounts[submission.AccountID]++
		reducer, value, err := repository.validatePlanLeg(
			plan, submission, plan.Reservations[index], maxOrder, armAccounts,
			openByAccount[submission.AccountID]+seenAccounts[submission.AccountID],
			openGlobal+index+1, limits,
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

func (repository *memoryDispatcherRepository) validatePlanLeg(
	plan ApprovedSandboxPlan,
	submission Submission,
	reservation DurableReservation,
	maxOrder domain.Notional,
	armAccounts map[AccountID]struct{},
	accountOpen, globalOpen int,
	limits SubmissionLimits,
) (*execution.OrderReducer, *apd.Decimal, error) {
	if submission.ApprovedAt != plan.ApprovedAt || submission.PlanID.String() != plan.ID ||
		submission.Validate(maxOrder) != nil {
		return nil, nil, contractError("plan_submission_invalid")
	}
	if _, armed := armAccounts[submission.AccountID]; !armed {
		return nil, nil, contractError("plan_account_not_armed")
	}
	exchange := exchangeForAccount(submission.AccountID)
	if err := validateMemoryEntryEvidence(plan, submission, exchange); err != nil {
		return nil, nil, err
	}
	key := clientIdentity(submission)
	if prior, exists := repository.clientIDs[key]; exists && prior != submission.OrderID.String() {
		return nil, nil, contractError("client_order_id_conflict")
	}
	if accountOpen > limits.MaximumOpenPerAccount {
		return nil, nil, contractError("account_open_cap")
	}
	if globalOpen > limits.MaximumOpenGlobal {
		return nil, nil, contractError("global_open_cap")
	}
	value, _, err := apd.NewFromString(submission.Notional.String())
	if err != nil {
		return nil, nil, contractError("notional_invalid")
	}
	reducer, err := approvedReducer(submission)
	if err != nil {
		return nil, nil, err
	}
	if reservation.ValidateFor(submission) != nil {
		return nil, nil, contractError("reservation_invalid")
	}
	return reducer, value, nil
}

func validateMemoryEntryEvidence(
	plan ApprovedSandboxPlan,
	submission Submission,
	exchange Exchange,
) error {
	if submission.Action != IntentEntry {
		if len(plan.EntrySafety) != 0 {
			return contractError("entry_safety_rejected")
		}
		return nil
	}
	eligibility, ok := plan.Eligibility[exchange]
	if !ok || !eligibility.Eligible ||
		eligibility.Exchange != string(exchange) ||
		eligibility.Instrument != submission.Instrument.Symbol() ||
		eligibility.ObservedAt.IsZero() ||
		eligibility.ObservedAt.Location() != time.UTC ||
		eligibility.ObservedAt.After(plan.ApprovedAt) ||
		plan.ApprovedAt.Sub(eligibility.ObservedAt) > 250*time.Millisecond {
		return contractError("public_ineligible")
	}
	safety, ok := plan.EntrySafety[submission.AccountID]
	if !ok || safety.ValidateFor(submission, exchange, plan.ApprovedAt) != nil {
		return contractError("entry_safety_rejected")
	}
	return nil
}

// ClaimOutbox applies the model fencing lease and returns a bounded page.
func (repository *memoryDispatcherRepository) ClaimOutbox(
	ctx context.Context,
	account AccountID,
	epoch uint64,
	owner string,
	fence uint64,
	now time.Time,
	ttl time.Duration,
	limit int,
	kill KillPoint,
) ([]SubmissionOutbox, error) {
	if err := kill.Hit(ctx, KillBeforeLeaseTransition); err != nil {
		return nil, err
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	ids := make([]string, 0)
	for id, record := range repository.outbox {
		if record.Submission.AccountID == account && record.Submission.AccountEpoch == epoch &&
			repository.entryClaimAllowed(record, now) &&
			(record.State == OutboxPending ||
				(record.State == OutboxClaimed && !record.ClaimExpiresAt.After(now) && record.FencingToken < fence)) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	result := make([]SubmissionOutbox, 0, len(ids))
	for _, id := range ids {
		record := repository.outbox[id]
		record.State, record.ClaimOwner, record.FencingToken = OutboxClaimed, owner, fence
		record.ClaimExpiresAt, record.UpdatedAt, record.Attempt = now.Add(ttl), now, record.Attempt+1
		repository.outbox[id] = record
		result = append(result, record)
	}
	if err := kill.Hit(ctx, KillAfterLeaseTransition); err != nil {
		return nil, err
	}
	return result, nil
}

// MarkSubmitting advances the canonical model reducer before network I/O.
func (repository *memoryDispatcherRepository) MarkSubmitting(
	ctx context.Context,
	id string,
	fence uint64,
	now time.Time,
	kill KillPoint,
) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	record, err := repository.claimed(id, fence)
	if err != nil {
		return err
	}
	if !repository.entryClaimAllowed(record, now) {
		return ErrEntryBlocked
	}
	reducer := repository.reducers[record.Submission.OrderID.String()]
	switch reducer.Snapshot().State {
	case execution.OrderSubmitting, execution.OrderAcknowledged,
		execution.OrderPartiallyFilled, execution.OrderCancelPending,
		execution.OrderFilled, execution.OrderCanceled, execution.OrderRejected,
		execution.OrderExpired, execution.OrderUnknown,
		execution.OrderRecoveryRequired, execution.OrderRecovered:
		record.UpdatedAt = now
		repository.outbox[id] = record
		return nil
	case execution.OrderApproved:
		// The canonical reducer transition is applied below.
	default:
		return contractError("order_recovery_state_invalid")
	}
	if err := kill.Hit(ctx, KillBeforeReducerUpdate); err != nil {
		return err
	}
	if _, err := reducer.Reduce(orderEvent(record.Submission, execution.OrderSubmitting, "dispatch", 5, now)); err != nil {
		return err
	}
	record.UpdatedAt = now
	repository.updateMemoryPlanState(record.Submission.PlanID.String(), id, record)
	repository.outbox[id] = record
	return kill.Hit(ctx, KillAfterReducerUpdate)
}

func (repository *memoryDispatcherRepository) entryClaimAllowed(
	record SubmissionOutbox,
	now time.Time,
) bool {
	if record.Submission.Action != IntentEntry {
		return true
	}
	plan, exists := repository.plans[record.Submission.PlanID.String()]
	return exists && plan.Arm.Active(now)
}

// MarkUnknown quarantines an ambiguous transport outcome without releasing capacity.
func (repository *memoryDispatcherRepository) MarkUnknown(
	ctx context.Context,
	id string,
	fence uint64,
	now time.Time,
	kill KillPoint,
) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	record, err := repository.claimed(id, fence)
	if err != nil {
		return err
	}
	if err := kill.Hit(ctx, KillBeforeReducerUpdate); err != nil {
		return err
	}
	if _, err := repository.reducers[record.Submission.OrderID.String()].Reduce(
		orderEvent(record.Submission, execution.OrderUnknown, "ambiguous_timeout", 6, now),
	); err != nil {
		return err
	}
	record.State, record.UpdatedAt = OutboxUnknown, now
	record.ClaimExpiresAt = time.Time{}
	repository.updateMemoryPlanState(record.Submission.PlanID.String(), id, record)
	repository.outbox[id] = record
	return kill.Hit(ctx, KillAfterReducerUpdate)
}
