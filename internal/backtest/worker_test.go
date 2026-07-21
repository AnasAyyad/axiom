package backtest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/replay"
)

func TestWorkerRenewsLeaseAndPersistsCanonicalResult(t *testing.T) {
	store := newWorkerStore(workerClaim())
	worker, err := NewWorker(store, func(JobClaim) (Processor, error) {
		return &workerProcessor{renewed: store.renewed}, nil
	}, replay.RealPacer{})
	if err != nil {
		t.Fatal(err)
	}
	worker.heartbeat = time.Millisecond
	ok, err := worker.RunOne(context.Background())
	if err != nil || !ok {
		t.Fatalf("run = %t %v", ok, err)
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.renewals == 0 || store.completed != "job-worker" || !json.Valid(store.canonical) || store.failed != "" {
		t.Fatalf("store = %#v", store)
	}
}

func TestWorkerFailsClosedWhenLeaseIsLost(t *testing.T) {
	store := newWorkerStore(workerClaim())
	store.renewErr = errors.New("lease lost")
	worker, err := NewWorker(store, func(JobClaim) (Processor, error) {
		return &workerProcessor{waitForCancellation: true}, nil
	}, replay.RealPacer{})
	if err != nil {
		t.Fatal(err)
	}
	worker.heartbeat = time.Millisecond
	ok, err := worker.RunOne(context.Background())
	if !ok || err == nil || store.completed != "" || store.failed != "offline_run_failed" {
		t.Fatalf("lease loss = %t %v completed=%q failed=%q", ok, err, store.completed, store.failed)
	}
}

func TestReplayStepPausesAfterExactlyOneNewEvent(t *testing.T) {
	claim := workerClaim()
	claim.Manifest.Mode = "replay"
	claim.SingleStep = true
	claim.Source = &workerSequenceSource{}
	store := newWorkerStore(claim)
	worker, err := NewWorker(store, func(JobClaim) (Processor, error) { return immediateWorkerProcessor{}, nil }, replay.RealPacer{})
	if err != nil {
		t.Fatal(err)
	}
	if ok, runErr := worker.RunOne(context.Background()); runErr != nil || !ok || store.pausedOrdinal != 1 || store.completed != "" {
		t.Fatalf("first step = %t %v cursor=%d completed=%q", ok, runErr, store.pausedOrdinal, store.completed)
	}
	store.mutex.Lock()
	store.claimed = false
	store.claim.ResumeOrdinal = 1
	store.claim.Source = &workerSequenceSource{}
	store.pausedOrdinal = 0
	store.mutex.Unlock()
	if ok, runErr := worker.RunOne(context.Background()); runErr != nil || !ok || store.pausedOrdinal != 2 || store.completed != "" {
		t.Fatalf("second step = %t %v cursor=%d completed=%q", ok, runErr, store.pausedOrdinal, store.completed)
	}
}

type workerStore struct {
	mutex         sync.Mutex
	claim         JobClaim
	claimed       bool
	renewed       chan struct{}
	renewOnce     sync.Once
	renewals      int
	renewErr      error
	completed     string
	canonical     []byte
	failed        string
	pausedOrdinal uint64
	checkpoint    []byte
}

func newWorkerStore(claim JobClaim) *workerStore {
	return &workerStore{claim: claim, renewed: make(chan struct{})}
}

func (store *workerStore) Claim(context.Context) (JobClaim, bool, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.claimed {
		return JobClaim{}, false, nil
	}
	store.claimed = true
	return store.claim, true, nil
}

func (store *workerStore) Renew(context.Context, string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.renewals++
	if store.renewErr == nil {
		store.renewOnce.Do(func() { close(store.renewed) })
	}
	return store.renewErr
}

func (store *workerStore) Complete(_ context.Context, id string, _ CanonicalResult, canonical []byte) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.completed, store.canonical = id, append([]byte(nil), canonical...)
	return nil
}

func (store *workerStore) Fail(_ context.Context, _ string, reason string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.failed = reason
	return nil
}

func (store *workerStore) Control(context.Context, string) (JobControl, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return JobControl{SingleStep: store.claim.SingleStep, ResumeOrdinal: store.claim.ResumeOrdinal}, nil
}

func (store *workerStore) Pause(_ context.Context, _ string, ordinal uint64, checkpoint []byte) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.pausedOrdinal, store.checkpoint = ordinal, append([]byte(nil), checkpoint...)
	return nil
}

type workerSource struct{ sent bool }

func (source *workerSource) Next() (replay.Event, bool, error) {
	if source.sent {
		return replay.Event{}, false, nil
	}
	source.sent = true
	return replay.Event{LogicalTime: 1, Ordinal: 1, Canonical: []byte(`{"input":"verified"}`)}, true, nil
}

func (source *workerSource) SeekOrdinal(uint64) error { return nil }

type workerSequenceSource struct{ next uint64 }

func (source *workerSequenceSource) Next() (replay.Event, bool, error) {
	source.next++
	if source.next > 2 {
		return replay.Event{}, false, nil
	}
	return replay.Event{LogicalTime: source.next, Ordinal: source.next,
		Canonical: []byte(`{"input":"verified"}`)}, true, nil
}

func (source *workerSequenceSource) SeekOrdinal(uint64) error { return nil }

type immediateWorkerProcessor struct{}

func (immediateWorkerProcessor) Process(_ context.Context, event replay.Event) (EventResult, error) {
	return EventResult{Ordinal: event.Ordinal, Decision: []byte(`{}`), Orders: []byte(`[]`), Balances: []byte(`{}`)}, nil
}
func (immediateWorkerProcessor) Metrics() Metrics { return Metrics{TotalNetReturn: "0"} }

type workerProcessor struct {
	renewed             <-chan struct{}
	waitForCancellation bool
}

func (processor *workerProcessor) Process(ctx context.Context, event replay.Event) (EventResult, error) {
	if processor.waitForCancellation {
		<-ctx.Done()
		return EventResult{}, ctx.Err()
	}
	select {
	case <-ctx.Done():
		return EventResult{}, ctx.Err()
	case <-processor.renewed:
		return EventResult{Ordinal: event.Ordinal, Decision: []byte(`{}`), Orders: []byte(`[]`), Balances: []byte(`{}`)}, nil
	}
}

func (*workerProcessor) Metrics() Metrics {
	return Metrics{TotalNetReturn: "0", MaximumDrawdown: "0", CurrentDrawdown: "0"}
}

func workerClaim() JobClaim {
	hash := strings.Repeat("a", 64)
	runID, _ := domain.NewRunID("worker")
	return JobClaim{ID: "job-worker", Source: &workerSource{}, TimingMode: replay.MaximumTiming, Acceleration: 1,
		Manifest: RunManifest{RunID: runID,
			Mode: "backtest", CodeCommit: strings.Repeat("b", 40), Build: CurrentBuildIdentity(nil, hash, hash),
			Dataset: DatasetDescriptor{DatasetID: "dataset-worker", ManifestHash: hash, Revision: 1,
				SourceCommit: strings.Repeat("c", 40), SchemaVersion: "dataset-v1", ParserVersion: "parser-v1",
				NormalizationVersion: "normalizer-v1", SegmentHashes: []string{hash}, RecordCount: 1,
				Complete: true, Confidence: ConfidenceB}, ConfigurationHash: hash, Seed: "seed-worker",
			SchedulerVersion: "scheduler-v1", SerializationVersion: "canonical-json-v1",
			Models: ModelNamespace{ID: "models-worker", MarketContext: "production-public", LiquidityDomain: "combined",
				FeeDomain: "fee-v1", LatencyDomain: "latency-v1", FillDomain: "fill-v1"}, StartingBalanceHash: hash}}
}

var _ ControlledJobStore = (*workerStore)(nil)
