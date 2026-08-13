package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestStrategySessionSchedulerReloadsExactConfigurationBeforeEvaluation(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	work := schedulerWork(now)
	executor := &schedulerExecutor{state: StrategySessionEvaluationWaiting, reason: "waiting_for_finalized_candle"}
	recorder := &schedulerEvaluationRecorder{}
	scheduler, err := NewStrategySessionScheduler(
		&schedulerWorkSource{work: []StrategySessionWork{work}},
		&schedulerConfigurationSource{record: schedulerConfiguration(work)}, executor, recorder,
		clock, work.Account.ID, work.Account.Epoch, "engine-owner", 9,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scheduler.Tick(context.Background())
	if err != nil || len(result.Evaluations) != 1 || executor.calls != 1 || recorder.calls != 1 ||
		result.Evaluations[0].State != StrategySessionEvaluationWaiting ||
		result.Evaluations[0].Reason != "waiting_for_finalized_candle" {
		t.Fatalf("result=%#v calls=%d error=%v", result, executor.calls, err)
	}
}

func TestStrategySessionSchedulerBlocksOnlyTheAffectedWork(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	first := schedulerWork(now)
	second := first
	second.SessionID = "second-session"
	second.ConfigurationID = "second-configuration"
	second.ConfigurationHash = strings.Repeat("b", 64)
	second.StrategyRevision = 2
	executor := &schedulerExecutor{state: StrategySessionEvaluationEvaluated, reason: "strategy_evaluated"}
	recorder := &schedulerEvaluationRecorder{}
	scheduler, err := NewStrategySessionScheduler(
		&schedulerWorkSource{work: []StrategySessionWork{first, second}},
		&schedulerConfigurationSource{records: map[SessionID]StrategySessionConfiguration{
			first.SessionID: schedulerConfiguration(first),
		}}, executor, recorder, clock, first.Account.ID, first.Account.Epoch, "engine-owner", 9,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scheduler.Tick(context.Background())
	if err != nil || len(result.Evaluations) != 2 || executor.calls != 1 || recorder.calls != 2 ||
		result.Evaluations[0].State != StrategySessionEvaluationEvaluated ||
		result.Evaluations[1].State != StrategySessionEvaluationBlocked ||
		result.Evaluations[1].Reason != "strategy_configuration_unavailable" {
		t.Fatalf("result=%#v calls=%d error=%v", result, executor.calls, err)
	}
}

func TestStrategySessionSchedulerFailsClosedForUnfencedOrDuplicateWork(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	work := schedulerWork(now)
	for _, test := range []struct {
		name string
		work []StrategySessionWork
		err  error
	}{
		{name: "source failure", err: errors.New("unavailable")},
		{name: "duplicate", work: []StrategySessionWork{work, work}},
		{name: "wrong account", work: []StrategySessionWork{func() StrategySessionWork { item := work; item.Account.ID = "other"; return item }()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheduler, newErr := NewStrategySessionScheduler(
				&schedulerWorkSource{work: test.work, err: test.err},
				&schedulerConfigurationSource{record: schedulerConfiguration(work)},
				&schedulerExecutor{state: StrategySessionEvaluationEvaluated, reason: "strategy_evaluated"},
				&schedulerEvaluationRecorder{},
				clock, work.Account.ID, work.Account.Epoch, "engine-owner", 9,
			)
			if newErr != nil {
				t.Fatal(newErr)
			}
			if _, tickErr := scheduler.Tick(context.Background()); tickErr == nil {
				t.Fatal("unsafe scheduler input accepted")
			}
		})
	}
}

func TestStrategySessionSchedulerBlocksUnsafeExecutorOutput(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	work := schedulerWork(now)
	scheduler, err := NewStrategySessionScheduler(
		&schedulerWorkSource{work: []StrategySessionWork{work}},
		&schedulerConfigurationSource{record: schedulerConfiguration(work)},
		&schedulerExecutor{state: StrategySessionEvaluationWaiting, reason: "adapter error: private payload"},
		&schedulerEvaluationRecorder{},
		clock, work.Account.ID, work.Account.Epoch, "engine-owner", 9,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scheduler.Tick(context.Background())
	if err != nil || len(result.Evaluations) != 1 ||
		result.Evaluations[0].State != StrategySessionEvaluationBlocked ||
		result.Evaluations[0].Reason != "strategy_evaluation_failed" {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestStrategySessionSchedulerFailsClosedWhenOutcomeCannotBeRecorded(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	work := schedulerWork(now)
	scheduler, err := NewStrategySessionScheduler(
		&schedulerWorkSource{work: []StrategySessionWork{work}},
		&schedulerConfigurationSource{record: schedulerConfiguration(work)},
		&schedulerExecutor{state: StrategySessionEvaluationWaiting, reason: "waiting_for_finalized_candle"},
		&schedulerEvaluationRecorder{err: errors.New("write failed")},
		clock, work.Account.ID, work.Account.Epoch, "engine-owner", 9,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = scheduler.Tick(context.Background()); err == nil {
		t.Fatal("unrecorded scheduler outcome accepted")
	}
}

type schedulerWorkSource struct {
	work []StrategySessionWork
	err  error
}

func (source *schedulerWorkSource) ActiveStrategySessionWork(
	context.Context, AccountID, uint64, string, uint64, time.Time, int,
) ([]StrategySessionWork, error) {
	return append([]StrategySessionWork(nil), source.work...), source.err
}

type schedulerConfigurationSource struct {
	record  StrategySessionConfiguration
	records map[SessionID]StrategySessionConfiguration
}

func (source *schedulerConfigurationSource) StrategySessionConfiguration(
	_ context.Context,
	work StrategySessionWork,
	_ time.Time,
) (StrategySessionConfiguration, error) {
	if source.records != nil {
		record, exists := source.records[work.SessionID]
		if !exists {
			return StrategySessionConfiguration{}, errors.New("missing")
		}
		return record, nil
	}
	return source.record, nil
}

type schedulerExecutor struct {
	calls  int
	state  StrategySessionEvaluationState
	reason string
}

type schedulerEvaluationRecorder struct {
	calls int
	err   error
}

func (recorder *schedulerEvaluationRecorder) RecordStrategySessionEvaluation(
	_ context.Context,
	_ string,
	_ uint64,
	_ StrategySessionEvaluation,
) error {
	recorder.calls++
	return recorder.err
}

func (executor *schedulerExecutor) EvaluateStrategySession(
	_ context.Context,
	work StrategySessionWork,
	_ StrategySessionConfiguration,
	lease StrategySessionExecutionLease,
	now time.Time,
) (StrategySessionEvaluation, error) {
	if lease.ValidFor(work) != nil {
		return StrategySessionEvaluation{}, errors.New("invalid execution lease")
	}
	executor.calls++
	return StrategySessionEvaluation{Work: work, State: executor.state,
		Reason: executor.reason, EvidenceHash: strategySessionEvaluationEvidenceHash(
			work, executor.state, executor.reason, now,
		), OccurredAt: now}, nil
}

func schedulerWork(now time.Time) StrategySessionWork {
	return StrategySessionWork{SessionID: "strategy-session", Strategy: StrategyTrend,
		Instrument: "BTCUSDT", Account: StrategySessionAccount{ID: "account", Epoch: 1, Exchange: ExchangeBinance},
		ConfigurationID: "configuration", ConfigurationHash: strings.Repeat("a", 64),
		StrategySetHash: strings.Repeat("c", 64), SessionRevision: 1, StrategyRevision: 1,
		ArmID: "arm", ArmRevision: 1, StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute)}
}

func schedulerConfiguration(work StrategySessionWork) StrategySessionConfiguration {
	return StrategySessionConfiguration{ID: work.ConfigurationID, Hash: work.ConfigurationHash,
		Payload: []byte(`{"schema_version":"sandbox_runtime"}`)}
}
