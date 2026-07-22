package bootstrap

import (
	"context"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
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

func TestB1RecorderRoleComposesBothPublicExchangesAndNativeTriangle(t *testing.T) {
	started := time.Unix(1_700_000_000, 0).UTC()
	clock, _ := domain.NewReplayClock(started)
	runtimeConfig := config.Runtime{InstanceID: "instance-b1", Recorder: config.RecorderRuntime{
		Root: t.TempDir(), CollectorRegion: "test-region", FlushInterval: 5 * time.Minute, QueueCapacity: 8192, BookDepth: 1000}}
	product := config.DefaultV1BConfiguration()
	work, err := newRecorderRoleWork(context.Background(), nil, runtimeConfig, product, clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(work.collectors) != 3 || len(work.bybitCollectors) != 3 || work.bybitClient == nil ||
		work.bybitRecorder == nil || work.Ready() {
		t.Fatalf("B1 recorder composition = binance:%d bybit:%d ready:%t",
			len(work.collectors), len(work.bybitCollectors), work.Ready())
	}
	for _, exchange := range product.PublicExchanges() {
		if len(exchange.CandleIntervals) != 3 || exchange.Instruments[2].Base != "ETH" ||
			exchange.Instruments[2].Quote != "BTC" {
			t.Fatalf("B1 exchange graph = %#v", exchange)
		}
	}
}
