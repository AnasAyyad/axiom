package evaluation

import (
	"context"
	"errors"
	"testing"
	"time"
)

type workerStore struct {
	claim  Claim
	result Outcome
}

func (store *workerStore) Claim(context.Context) (Claim, bool, error) { return store.claim, true, nil }
func (*workerStore) Renew(context.Context, Claim) error               { return nil }
func (store *workerStore) Apply(_ context.Context, _ Claim, value Outcome) error {
	store.result = value
	return nil
}

type workerExecutor struct {
	outcome Outcome
	err     error
}

func (value workerExecutor) Execute(context.Context, Claim) (Outcome, error) {
	return value.outcome, value.err
}

func TestWorkerPausesFailedStageForSameCampaignRetry(t *testing.T) {
	campaign := Campaign{ID: "campaign-1", Preset: BalancedFullV1, State: StateRunning,
		CurrentStage: StageBacktestMatrix, Revision: 4}
	store := &workerStore{claim: Claim{Campaign: campaign, ClaimEpoch: 1}}
	worker, err := NewWorker(store, workerExecutor{err: errors.New("storage unavailable")})
	if err != nil {
		t.Fatal(err)
	}
	if worked, runErr := worker.RunOne(context.Background()); runErr != nil || !worked {
		t.Fatalf("worked=%t err=%v", worked, runErr)
	}
	if store.result.Kind != OutcomePaused || store.result.Reason != ReasonPersistenceFailed {
		t.Fatalf("result=%#v", store.result)
	}
}

func TestWorkerDefersRetryWhenPausedStageStillFails(t *testing.T) {
	campaign := Campaign{ID: "campaign-1", Preset: BalancedFullV1, State: StatePausedRecoverable,
		CurrentStage: StageRecorderQualify, Revision: 5}
	store := &workerStore{claim: Claim{Campaign: campaign, ClaimEpoch: 1}}
	worker, _ := NewWorker(store, workerExecutor{err: errors.New("database unavailable")})
	if worked, runErr := worker.RunOne(context.Background()); runErr != nil || !worked {
		t.Fatalf("worked=%t err=%v", worked, runErr)
	}
	if store.result.Kind != OutcomeRetryDeferred || store.result.Reason != ReasonPersistenceFailed {
		t.Fatalf("result=%#v", store.result)
	}
}

func TestCanceledCampaignCanOnlyCreatePartialReport(t *testing.T) {
	campaign := Campaign{ID: "campaign-1", Preset: BalancedFullV1, State: StateCanceled, Revision: 4}
	store := &workerStore{claim: Claim{Campaign: campaign, ClaimEpoch: 1}}
	worker, _ := NewWorker(store, workerExecutor{outcome: Outcome{Kind: OutcomeWaiting}})
	_, _ = worker.RunOne(context.Background())
	if store.result.Kind != OutcomeBlocked {
		t.Fatalf("result=%#v", store.result)
	}
	report, err := NewReport("partial", VerdictBlocked, ReasonCanceled, "Campaign canceled with preserved evidence.",
		map[string]any{"completed_stages": []string{}}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	worker, _ = NewWorker(store, workerExecutor{outcome: Outcome{Kind: OutcomePartialReported, Report: &report}})
	_, _ = worker.RunOne(context.Background())
	if store.result.Kind != OutcomePartialReported {
		t.Fatalf("result=%#v", store.result)
	}
}

type leaseWorkerStore struct {
	workerStore
	renewed int
	release chan struct{}
}

func (store *leaseWorkerStore) Renew(context.Context, Claim) error {
	store.renewed++
	if store.renewed == 2 {
		close(store.release)
	}
	return nil
}

type leaseWorkerExecutor struct{ release <-chan struct{} }

func (executor leaseWorkerExecutor) Execute(ctx context.Context, _ Claim) (Outcome, error) {
	select {
	case <-executor.release:
		return Outcome{Kind: OutcomeWaiting}, nil
	case <-ctx.Done():
		return Outcome{}, ctx.Err()
	}
}

func TestWorkerRenewsCampaignClaimDuringStageIO(t *testing.T) {
	campaign := Campaign{ID: "campaign-1", Preset: BalancedFullV1, State: StateRunning,
		CurrentStage: StageHistoricalImport, Revision: 1}
	store := &leaseWorkerStore{workerStore: workerStore{claim: Claim{Campaign: campaign, ClaimEpoch: 1}},
		release: make(chan struct{})}
	worker, _ := NewWorker(store, leaseWorkerExecutor{release: store.release})
	worker.heartbeat = time.Millisecond
	worked, err := worker.RunOne(context.Background())
	if err != nil || !worked || store.renewed < 2 || store.result.Kind != OutcomeWaiting {
		t.Fatalf("worked=%t renewed=%d result=%#v err=%v", worked, store.renewed, store.result, err)
	}
}
