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
