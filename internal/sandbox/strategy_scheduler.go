package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"axiom/internal/domain"
)

// StrategySessionEvaluationState is the owner-facing outcome of one bounded
// automatic-session scheduling attempt. It is intentionally not an order
// state: an evaluation may wait or be blocked without creating a plan.
type StrategySessionEvaluationState string

// Durable outcomes for one scheduled strategy evaluation.
const (
	StrategySessionEvaluationWaiting   StrategySessionEvaluationState = "waiting"
	StrategySessionEvaluationEvaluated StrategySessionEvaluationState = "evaluated"
	StrategySessionEvaluationBlocked   StrategySessionEvaluationState = "blocked"
)

// StrategySessionEvaluation is one sanitized result for a scheduled work
// snapshot. Reason is a stable implementation reason, never an exchange
// payload, credential, or raw error string.
type StrategySessionEvaluation struct {
	Work         StrategySessionWork
	State        StrategySessionEvaluationState
	Reason       string
	EvidenceHash string
	OccurredAt   time.Time
}

// StrategySessionExecutionLease is the exact fenced engine authority supplied
// by the scheduler to one bounded evaluation. It authorizes durable scheduler
// evidence only; it grants no exchange adapter or order-submission capability.
type StrategySessionExecutionLease struct {
	Account AccountID
	Epoch   uint64
	Owner   string
	Fence   uint64
}

// ValidFor proves the lease belongs to the exact scheduled account work.
func (lease StrategySessionExecutionLease) ValidFor(work StrategySessionWork) error {
	if lease.Account == "" || lease.Epoch == 0 || lease.Owner == "" || lease.Fence == 0 ||
		lease.Account != work.Account.ID || lease.Epoch != work.Account.Epoch {
		return contractError("strategy_session_execution_lease_invalid")
	}
	return nil
}

// NewStrategySessionEvaluation builds a bounded owner-facing evaluation
// outcome. The default evidence hash commits to the exact work identity,
// outcome, and decision instant; callers with richer immutable evidence may
// replace it only with another valid SHA-256 value before recording.
func NewStrategySessionEvaluation(
	work StrategySessionWork,
	state StrategySessionEvaluationState,
	reason string,
	occurredAt time.Time,
) (StrategySessionEvaluation, error) {
	evaluation := StrategySessionEvaluation{Work: work, State: state, Reason: reason,
		EvidenceHash: strategySessionEvaluationEvidenceHash(work, state, reason, occurredAt),
		OccurredAt:   occurredAt}
	if evaluation.ValidFor(work, occurredAt) != nil {
		return StrategySessionEvaluation{}, contractError("strategy_session_evaluation_invalid")
	}
	return evaluation, nil
}

// ValidFor verifies the executor result has not substituted a work item or
// claimed an order-state transition.
func (evaluation StrategySessionEvaluation) ValidFor(
	work StrategySessionWork,
	now time.Time,
) error {
	if evaluation.Work != work || evaluation.OccurredAt.IsZero() ||
		evaluation.OccurredAt.Location() != time.UTC ||
		!evaluation.OccurredAt.Equal(now) ||
		!validStrategySessionEvaluationReason(evaluation.Reason) ||
		!hash256(evaluation.EvidenceHash) {
		return contractError("strategy_session_evaluation_invalid")
	}
	switch evaluation.State {
	case StrategySessionEvaluationWaiting, StrategySessionEvaluationEvaluated, StrategySessionEvaluationBlocked:
		return nil
	default:
		return contractError("strategy_session_evaluation_invalid")
	}
}

func strategySessionEvaluationEvidenceHash(
	work StrategySessionWork,
	state StrategySessionEvaluationState,
	reason string,
	occurredAt time.Time,
) string {
	values := []string{string(work.SessionID), work.Strategy, work.Instrument,
		string(work.Account.ID), fmt.Sprintf("%d", work.Account.Epoch),
		string(state), reason, occurredAt.UTC().Format(time.RFC3339Nano)}
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

// validStrategySessionEvaluationReason permits a short, semantic reason token
// only. Scheduler output is later suitable for the owner timeline; raw
// adapter errors, endpoint details, and private payload fragments cannot be
// passed through this contract.
func validStrategySessionEvaluationReason(reason string) bool {
	if len(reason) == 0 || len(reason) > 96 {
		return false
	}
	for _, value := range reason {
		if (value < 'a' || value > 'z') && (value < '0' || value > '9') && value != '_' {
			return false
		}
	}
	return true
}

// StrategySessionExecutor owns strategy-specific public input construction and
// the shared allocation/risk/planning pipeline. It receives immutable session
// configuration but no scheduler permission to submit directly to an adapter.
type StrategySessionExecutor interface {
	EvaluateStrategySession(
		context.Context,
		StrategySessionWork,
		StrategySessionConfiguration,
		StrategySessionExecutionLease,
		time.Time,
	) (StrategySessionEvaluation, error)
}

// StrategySessionEvaluationRecorder appends a sanitized scheduler outcome
// only while the exact account fence, arm, and work revisions remain current.
// It never changes session state or submits an order.
type StrategySessionEvaluationRecorder interface {
	RecordStrategySessionEvaluation(context.Context, string, uint64, StrategySessionEvaluation) error
}

// StrategySessionScheduleResult contains all independent session outcomes for
// a single fenced engine tick. A blocked session does not hide other armed
// sessions; every executor still owns its own fail-closed pipeline.
type StrategySessionScheduleResult struct {
	OccurredAt  time.Time
	Evaluations []StrategySessionEvaluation
}

// StrategySessionScheduler enumerates only one engine account's fenced,
// actively armed work and reloads the exact immutable configuration before
// each evaluation. It has no exchange adapter or dispatcher dependency.
type StrategySessionScheduler struct {
	workSource          StrategySessionWorkSource
	configurationSource StrategySessionConfigurationSource
	executor            StrategySessionExecutor
	recorder            StrategySessionEvaluationRecorder
	clock               domain.Clock
	account             AccountID
	epoch               uint64
	owner               string
	fence               uint64
}

// NewStrategySessionScheduler constructs a fail-closed scheduler for exactly
// one authenticated engine account epoch.
func NewStrategySessionScheduler(
	workSource StrategySessionWorkSource,
	configurationSource StrategySessionConfigurationSource,
	executor StrategySessionExecutor,
	recorder StrategySessionEvaluationRecorder,
	clock domain.Clock,
	account AccountID,
	epoch uint64,
	owner string,
	fence uint64,
) (*StrategySessionScheduler, error) {
	if workSource == nil || configurationSource == nil || executor == nil || recorder == nil ||
		clock == nil || account == "" || epoch == 0 || owner == "" || fence == 0 {
		return nil, contractError("strategy_session_scheduler_invalid")
	}
	return &StrategySessionScheduler{workSource: workSource,
		configurationSource: configurationSource, executor: executor, recorder: recorder, clock: clock,
		account: account, epoch: epoch, owner: owner, fence: fence}, nil
}

// Tick evaluates a bounded deterministic work list. Work/configuration
// failures are returned as per-session blocked outcomes so the owner can see
// why nothing happened; an inability to enumerate fenced work is an engine
// error and remains fail-closed.
func (scheduler *StrategySessionScheduler) Tick(
	ctx context.Context,
) (StrategySessionScheduleResult, error) {
	if scheduler == nil || ctx == nil {
		return StrategySessionScheduleResult{}, contractError("strategy_session_scheduler_invalid")
	}
	now := scheduler.clock.Now()
	if now.Validate() != nil {
		return StrategySessionScheduleResult{}, contractError("strategy_session_scheduler_invalid")
	}
	work, err := scheduler.workSource.ActiveStrategySessionWork(
		ctx, scheduler.account, scheduler.epoch, scheduler.owner, scheduler.fence,
		now.UTC, 16,
	)
	if err != nil {
		return StrategySessionScheduleResult{}, fmt.Errorf("strategy_session_work_unavailable")
	}
	result := StrategySessionScheduleResult{OccurredAt: now.UTC,
		Evaluations: make([]StrategySessionEvaluation, 0, len(work))}
	seen := make(map[string]struct{}, len(work))
	for _, item := range work {
		key := string(item.SessionID) + "\x00" + item.Instrument
		if item.ValidAt(now.UTC) != nil || item.Account.ID != scheduler.account ||
			item.Account.Epoch != scheduler.epoch {
			return StrategySessionScheduleResult{}, contractError("strategy_session_work_invalid")
		}
		if _, duplicate := seen[key]; duplicate {
			return StrategySessionScheduleResult{}, contractError("strategy_session_work_invalid")
		}
		seen[key] = struct{}{}
		evaluation := scheduler.evaluateWorkItem(ctx, item, now.UTC)
		if err = scheduler.record(ctx, evaluation); err != nil {
			return StrategySessionScheduleResult{}, err
		}
		result.Evaluations = append(result.Evaluations, evaluation)
	}
	return result, nil
}

func (scheduler *StrategySessionScheduler) evaluateWorkItem(ctx context.Context,
	item StrategySessionWork, now time.Time,
) StrategySessionEvaluation {
	configuration, err := scheduler.configurationSource.StrategySessionConfiguration(ctx, item, now)
	if err != nil || !configuration.ValidFor(item) {
		return blockedStrategySessionEvaluation(item, now, "strategy_configuration_unavailable")
	}
	lease := StrategySessionExecutionLease{Account: scheduler.account, Epoch: scheduler.epoch,
		Owner: scheduler.owner, Fence: scheduler.fence}
	evaluation, err := scheduler.executor.EvaluateStrategySession(ctx, item, configuration, lease, now)
	if err != nil || evaluation.ValidFor(item, now) != nil {
		return blockedStrategySessionEvaluation(item, now, "strategy_evaluation_failed")
	}
	return evaluation
}

func (scheduler *StrategySessionScheduler) record(
	ctx context.Context,
	evaluation StrategySessionEvaluation,
) error {
	if err := scheduler.recorder.RecordStrategySessionEvaluation(
		ctx, scheduler.owner, scheduler.fence, evaluation,
	); err != nil {
		return contractError("strategy_session_evaluation_record_failed")
	}
	return nil
}

func blockedStrategySessionEvaluation(
	work StrategySessionWork,
	now time.Time,
	reason string,
) StrategySessionEvaluation {
	return StrategySessionEvaluation{Work: work, State: StrategySessionEvaluationBlocked,
		Reason: reason, EvidenceHash: strategySessionEvaluationEvidenceHash(
			work, StrategySessionEvaluationBlocked, reason, now,
		), OccurredAt: now}
}
