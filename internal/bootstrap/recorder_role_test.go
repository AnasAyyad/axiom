package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/storage/pressure"
)

func TestRecorderRoleCompositionIsPublicBoundedAndDeterministic(t *testing.T) {
	started := time.Unix(1_700_000_000, 0).UTC()
	first, repeated := recorderSession("instance-a", started), recorderSession("instance-a", started)
	if first != repeated || first == recorderSession("instance-a", started.Add(time.Nanosecond)) {
		t.Fatal("recorder session identity is not deterministic and collision-resistant")
	}
	if recorderDatasetID(first) != recorderDatasetID(repeated) ||
		recorderDatasetID(first) == recorderDatasetID(recorderSession("instance-a", started.Add(time.Nanosecond))) {
		t.Fatal("recorder dataset identity is not session-scoped and deterministic")
	}
	clock, _ := domain.NewReplayClock(started)
	runtimeConfig := config.Runtime{InstanceID: "instance-a", Recorder: config.RecorderRuntime{
		Root: t.TempDir(), FlushInterval: 5 * time.Minute, QueueCapacity: 8192, BookDepth: 1000}}
	work, err := newRecorderRoleWork(context.Background(), nil, runtimeConfig, config.DefaultConfiguration(), clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(work.collectors) != 2 || work.Ready() {
		t.Fatalf("recorder role universe/readiness = %d/%t", len(work.collectors), work.Ready())
	}
}

func TestRecorderStoragePressureFailsClosedAtCritical(t *testing.T) {
	policy := pressure.Policy{HighFreeBytes: 10 << 30, CriticalFreeBytes: 5 << 30,
		SampleInterval: 15 * time.Second}
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	observation, _ := policy.Classify(4<<30, 100<<30, now)
	store := &pressureWriterStub{}
	work := &recorderRoleWork{root: t.TempDir(), pressurePolicy: policy, pressureStore: store,
		pressureProbe: func(string, time.Time) (pressure.Observation, error) { return observation, nil }}
	critical, err := work.observeStoragePressure(context.Background(),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || !critical || store.observations != 1 {
		t.Fatalf("critical=%t observations=%d error=%v", critical, store.observations, err)
	}
}

type pressureWriterStub struct{ observations int }

func (store *pressureWriterStub) Observe(_ context.Context, observation pressure.Observation,
	_ pressure.Policy) (postgresstore.OperationalReadinessStoragePressureState, bool, error) {
	store.observations++
	return postgresstore.OperationalReadinessStoragePressureState{Observation: observation, Revision: 2,
		SourceInstance: "test-recorder"}, true, nil
}

func TestExchangeExpansionRecorderRoleComposesBothPublicExchangesAndNativeTriangle(t *testing.T) {
	started := time.Unix(1_700_000_000, 0).UTC()
	clock, _ := domain.NewReplayClock(started)
	runtimeConfig := config.Runtime{InstanceID: "instance-exchange_expansion", Recorder: config.RecorderRuntime{
		Root: t.TempDir(), CollectorRegion: "test-region", FlushInterval: 5 * time.Minute, QueueCapacity: 8192, BookDepth: 1000}}
	product := config.DefaultMultiStrategyConfiguration()
	work, err := newRecorderRoleWork(context.Background(), nil, runtimeConfig, product, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(work.collectors) != 3 || len(work.bybitCollectors) != 3 || work.bybitClient == nil ||
		work.bybitRecorder == nil || work.Ready() {
		t.Fatalf("exchange expansion recorder composition = binance:%d bybit:%d ready:%t",
			len(work.collectors), len(work.bybitCollectors), work.Ready())
	}
	for _, exchange := range product.PublicExchanges() {
		if len(exchange.CandleIntervals) != 3 || exchange.Instruments[2].Base != "ETH" ||
			exchange.Instruments[2].Quote != "BTC" {
			t.Fatalf("exchange expansion exchange graph = %#v", exchange)
		}
	}
}
