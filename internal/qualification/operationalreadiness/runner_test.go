package operationalReadiness

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSmokeRunnerProducesAuthenticatedNonQualifyingEvidence(t *testing.T) {
	configuration := testConfig(t)
	clock := &testClock{now: time.Date(2026, 8, 4, 12, 0, 1, 0, time.UTC)}
	preflight := testPreflight(clock.now.Add(-time.Second))
	store := &memoryStore{}
	runner := Runner{Clock: clock, Preflight: staticPreflight{preflight}, Probe: &safeProbe{},
		Faults: staticFaults{events: passedFaults(configuration, clock.now)}, Store: store}
	evidence, err := runner.Run(context.Background(), configuration)
	if err != nil || evidence.State != StateSmokePassed || evidence.Qualified ||
		evidence.ProfitabilityEvidence || evidence.EvidenceHash == "" || evidence.Signature == "" ||
		len(evidence.Samples) < 2 || store.finished != 1 {
		t.Fatalf("evidence=%+v error=%v store=%+v", evidence, err, store)
	}
	for index := 1; index < len(evidence.Samples); index++ {
		if evidence.Samples[index].PriorSampleHash != evidence.Samples[index-1].SampleHash {
			t.Fatal("sample hash chain is broken")
		}
	}
}

func TestFormalConfigurationCannotUseSmokeDurationOrDirtySource(t *testing.T) {
	configuration := testConfig(t)
	configuration.Identity.Mode = ModeFormal
	configuration.Identity.SourceDirty = true
	if ValidateConfig(configuration) == nil {
		t.Fatal("dirty short formal run accepted")
	}
	configuration.Identity.SourceDirty = false
	configuration.Duration = FormalDuration
	configuration.SampleInterval = time.Minute
	configuration.FaultSchedule.Faults[len(configuration.FaultSchedule.Faults)-1].OffsetSeconds = uint64(FormalDuration.Seconds() - 1)
	if ValidateConfig(configuration) != nil {
		t.Fatal("exact formal configuration rejected")
	}
}

func TestRunnerFailsOnCorrectnessDefectAndCannotQualify(t *testing.T) {
	configuration := testConfig(t)
	clock := &testClock{now: time.Date(2026, 8, 4, 12, 0, 1, 0, time.UTC)}
	probe := &safeProbe{mutate: func(sample *Sample) { sample.LostFills = 1 }}
	runner := Runner{Clock: clock, Preflight: staticPreflight{testPreflight(clock.now.Add(-time.Second))},
		Probe: probe, Faults: staticFaults{events: passedFaults(configuration, clock.now)}, Store: &memoryStore{}}
	evidence, err := runner.Run(context.Background(), configuration)
	if err == nil || err.Error() != "operational_readiness_qualification_failed" {
		t.Fatal("defective run passed")
	}
	if evidence.Qualified || evidence.State != StateFailed || len(evidence.Failures) != 1 || evidence.Failures[0].Reason != "lost_fill" {
		t.Fatalf("defective evidence=%+v error=%v", evidence, err)
	}
}

func TestRunnerWaitsForBoundedObserverRecoveryWithoutResettingClock(t *testing.T) {
	configuration := testConfig(t)
	clock := &testClock{now: time.Date(2026, 8, 4, 12, 0, 1, 0, time.UTC)}
	probe := &recoveringProbe{failuresRemaining: 2}
	runner := Runner{Clock: clock, Preflight: staticPreflight{testPreflight(clock.now.Add(-time.Second))},
		Probe: probe, Faults: staticFaults{events: passedFaults(configuration, clock.now)}, Store: &memoryStore{}}
	evidence, err := runner.Run(context.Background(), configuration)
	if err != nil || evidence.State != StateSmokePassed || evidence.Qualified || probe.calls < 3 ||
		evidence.ObservedDurationSeconds < int64(configuration.Duration.Seconds()) {
		t.Fatalf("evidence=%+v error=%v calls=%d", evidence, err, probe.calls)
	}
}

func TestRunnerFailsWithExactSourceCauseAfterRecoveryBudget(t *testing.T) {
	configuration := testConfig(t)
	clock := &testClock{now: time.Date(2026, 8, 4, 12, 0, 1, 0, time.UTC)}
	probe := &recoveringProbe{failuresRemaining: 1 << 20}
	runner := Runner{Clock: clock, Preflight: staticPreflight{testPreflight(clock.now.Add(-time.Second))},
		Probe: probe, Faults: staticFaults{}, Store: &memoryStore{}}
	evidence, err := runner.Run(context.Background(), configuration)
	if err == nil || evidence.State != StateFailed || len(evidence.Failures) != 1 {
		t.Fatalf("evidence=%+v error=%v", evidence, err)
	}
	failure := evidence.Failures[0]
	if failure.Reason != "sample_unavailable" || failure.CauseCode != "revision_not_advanced" ||
		failure.Source != "runtime" || failure.Stage != "health" || failure.Role != "recorder" ||
		evidence.ObservedDurationSeconds != int64(sampleAcquisitionTimeout.Seconds()) {
		t.Fatalf("unexpected terminal failure: %+v", failure)
	}
}

func TestSampleAcquisitionRecoveryBoundary(t *testing.T) {
	for _, test := range []struct {
		name     string
		failures int
		wantPass bool
	}{
		{name: "last retry before deadline", failures: 59, wantPass: true},
		{name: "recovery exactly at deadline", failures: 60, wantPass: true},
		{name: "deadline exceeded", failures: 61, wantPass: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			clock := &testClock{now: time.Date(2026, 8, 4, 12, 0, 1, 0, time.UTC)}
			probe := &recoveringProbe{failuresRemaining: test.failures}
			runner := Runner{Clock: clock, Probe: probe}
			_, err, aborted := runner.acquireSample(context.Background(), 1, clock.now)
			if (err == nil) != test.wantPass || aborted {
				t.Fatalf("failures=%d pass=%t error=%v aborted=%t calls=%d elapsed=%s",
					test.failures, test.wantPass, err, aborted, probe.calls,
					clock.now.Sub(time.Date(2026, 8, 4, 12, 0, 1, 0, time.UTC)))
			}
		})
	}
}

func TestMemoryTrendUsesPostWarmupPerServiceWindows(t *testing.T) {
	started := time.Date(2026, 8, 4, 0, 0, 0, 0, time.UTC)
	stable := memoryTrendEvidence(started, "", 0)
	evaluateMemory(&stable)
	if len(stable.Failures) != 0 || len(stable.MemoryTrends) != len(requiredRuntimeMetricRoles) {
		t.Fatalf("stable memory failed: %+v", stable)
	}
	leaking := memoryTrendEvidence(started, "recorder", 24<<20)
	evaluateMemory(&leaking)
	if len(leaking.Failures) != 1 || leaking.Failures[0].Reason != "memory_leak" ||
		leaking.Failures[0].Role != "recorder" {
		t.Fatalf("leak was not attributed: %+v", leaking)
	}
}

func TestRunnerRejectsFaultEvidenceFromAnotherRun(t *testing.T) {
	configuration := testConfig(t)
	clock := &testClock{now: time.Date(2026, 8, 4, 12, 0, 1, 0, time.UTC)}
	events := passedFaults(configuration, clock.now)
	events[0].RunID = "different-run"
	runner := Runner{Clock: clock, Preflight: staticPreflight{testPreflight(clock.now.Add(-time.Second))},
		Probe: &safeProbe{}, Faults: staticFaults{events: events}, Store: &memoryStore{}}
	evidence, err := runner.Run(context.Background(), configuration)
	if err == nil || evidence.Qualified || evidence.State != StateFailed ||
		len(evidence.Failures) != 1 || evidence.Failures[0].Reason != "fault_schedule_incomplete" {
		t.Fatalf("foreign fault evidence=%+v error=%v", evidence, err)
	}
}

func TestPreflightRejectsWeakClockThresholdMissingMarketRecoveryAndStaleness(t *testing.T) {
	at := time.Date(2026, 8, 4, 12, 0, 1, 0, time.UTC)
	preflight := testPreflight(at.Add(-time.Second))
	preflight.ClockThresholdMillis = ClockThresholdMillis + 1
	if validatePreflight(preflight, ModeSmoke) == nil {
		t.Fatal("weakened clock threshold accepted")
	}
	preflight = testPreflight(at.Add(-time.Second))
	preflight.RouteClockThresholdPassed = false
	if validatePreflight(preflight, ModeSmoke) == nil {
		t.Fatal("route clock threshold breach accepted by runner")
	}
	preflight = testPreflight(at.Add(-time.Second))
	preflight.MarketDataRecoveryPassed = false
	if validatePreflight(preflight, ModeSmoke) == nil {
		t.Fatal("missing market-data recovery accepted")
	}
	configuration := testConfig(t)
	clock := &testClock{now: at}
	preflight = testPreflight(at.Add(-MaximumPreflightAge - time.Second))
	runner := Runner{Clock: clock, Preflight: staticPreflight{preflight}, Probe: &safeProbe{},
		Faults: staticFaults{events: passedFaults(configuration, at)}, Store: &memoryStore{}}
	if _, err := runner.Run(context.Background(), configuration); err == nil {
		t.Fatal("stale preflight started a readiness clock")
	}
}

func TestFileStoreRefusesClockResetOrOverwrite(t *testing.T) {
	configuration := testConfig(t)
	root := t.TempDir()
	store := &FileStore{Root: root}
	started := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	if err := store.Begin(context.Background(), configuration, testPreflight(started), started); err != nil {
		t.Fatal(err)
	}
	second := &FileStore{Root: root}
	if err := second.Begin(context.Background(), configuration, testPreflight(started), started); err == nil {
		t.Fatal("existing run directory was overwritten")
	}
	if info, err := os.Stat(filepath.Join(root, configuration.Identity.RunID, "start.json")); err != nil || info.Mode().Perm() != 0o440 {
		t.Fatalf("start evidence mode=%v error=%v", info, err)
	}
}

func testConfig(t *testing.T) Config {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	images := map[string]string{}
	for _, name := range requiredImages {
		images[name] = "registry.invalid/axiom-" + name + "@sha256:" + digest
	}
	return Config{Enabled: true, Identity: Identity{RunID: "operational_readiness-smoke-20260804", Mode: ModeSmoke,
		SourceSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ImageDigests: images,
		ConfigurationHash: digest, ServerIdentity: digest, DatasetIdentity: digest, TestManifestHash: digest},
		Duration: 5 * time.Second, SampleInterval: time.Second, EvidenceRoot: t.TempDir(), SigningKey: key,
		DeclaredLoad: DeclaredLoad{true, true, true, true, true, true, true, true, true},
		FaultSchedule: FaultSchedule{SchemaVersion: "axiom.operational_readiness.fault-schedule.v1", Faults: []FaultSpec{
			{Scenario: "recorder-kill", OffsetSeconds: 0}, {Scenario: "database-restart", OffsetSeconds: 1},
			{Scenario: "exchange-gap", OffsetSeconds: 2}, {Scenario: "backup-restore", OffsetSeconds: 3},
			{Scenario: "disk-critical", OffsetSeconds: 4},
		}}}
}

func testPreflight(at time.Time) Preflight {
	return Preflight{CheckedAt: at, ReferenceServerApproved: false, ClockSynchronized: true,
		ClockThresholdMillis: ClockThresholdMillis, ClockOffsetMillis: 1, RouteClockThresholdPassed: true,
		TLSValid: true, PinnedImageDigests: true, NonRootExecution: true,
		ResourceLimitsPassed: true, DiskCapacityPassed: true, RemoteBackupIndependent: true,
		BackupAgeSeconds: 60, CleanRestorePassed: true, CleanRestoreDurationSeconds: 60,
		MarketDataRecoveryPassed: true,
		SchemaUpgradePassed:      true, RollbackForwardFixPassed: true, SBOMPresent: true,
		SecurityScanPassed: true, ProductionPrivateSubmissionImpossible: true}
}

type testClock struct{ now time.Time }

func (clock *testClock) Now() time.Time { return clock.now }
func (clock *testClock) Wait(_ context.Context, duration time.Duration) error {
	clock.now = clock.now.Add(duration)
	return nil
}

type staticPreflight struct{ value Preflight }

func (source staticPreflight) Check(context.Context) (Preflight, error) { return source.value, nil }

type safeProbe struct {
	revision uint64
	mutate   func(*Sample)
}

type recoveringProbe struct {
	failuresRemaining int
	calls             int
	safe              safeProbe
}

func (probe *recoveringProbe) Observe(ctx context.Context, ordinal uint64, at time.Time) (Sample, error) {
	probe.calls++
	if probe.failuresRemaining > 0 {
		probe.failuresRemaining--
		return Sample{}, &ProbeFailure{Reason: "revision_not_advanced", Retryable: true,
			SourceCause: SourceFailure{Source: "runtime", Stage: "health", Role: "recorder",
				Reason: "not_ready", Retryable: true}}
	}
	return probe.safe.Observe(ctx, ordinal, at)
}

func (probe *safeProbe) Observe(_ context.Context, _ uint64, _ time.Time) (Sample, error) {
	probe.revision++
	digest := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	sample := Sample{SourceRevision: probe.revision, DatabaseEvidenceHash: digest, RuntimeEvidenceHash: digest,
		DrillEvidenceHash: digest, ObserverLifecycleHash: digest, ObserverAttempt: probe.revision,
		DecodeBookP99Millis: 10, StrategyRiskP99Millis: 25,
		ResyncP95Millis: 15_000, CriticalAlertMillis: 5_000, ExternalAlertP95Millis: 60_000,
		GracefulShutdownMillis: 60_000, ShadowRecoveryMillis: 300_000, SandboxRecoveryMillis: 600_000,
		DatabaseCommitRPOZero: true, RecorderWithinFlushRPO: true, ResidentMemoryBytes: 6 * (100 << 20),
		MemoryLimitBytes: 6 * (512 << 20), ServiceMemory: testServiceMemory(100<<20, 20<<20, 512<<20),
		DiskLevel: "NORMAL", HeavyJobsRejectedAtHigh: true,
		RecordingPausedAtCritical: true, JournalAuditWritable: true, AllDeclaredLoadHealthy: true}
	if probe.mutate != nil {
		probe.mutate(&sample)
	}
	return sample, nil
}

func testServiceMemory(resident, heap, limit uint64) []ServiceMemory {
	result := make([]ServiceMemory, 0, len(requiredRuntimeMetricRoles))
	for _, role := range requiredRuntimeMetricRoles {
		result = append(result, ServiceMemory{Role: role, ResidentMemoryBytes: resident,
			HeapAllocBytes: heap, MemoryLimitBytes: limit})
	}
	return result
}

func memoryTrendEvidence(started time.Time, leakingRole string, dailyGrowth uint64) Evidence {
	evidence := Evidence{Identity: Identity{RunID: "memory-trend"}, StartedAt: started,
		EndedAt: started.Add(FormalDuration), RequiredDurationSeconds: int64(FormalDuration.Seconds()),
		ObservedDurationSeconds: int64(FormalDuration.Seconds())}
	for at := started; !at.After(evidence.EndedAt); at = at.Add(10 * time.Minute) {
		elapsedDays := uint64(at.Sub(started) / (24 * time.Hour))
		serviceMemory := make([]ServiceMemory, 0, len(requiredRuntimeMetricRoles))
		for _, role := range requiredRuntimeMetricRoles {
			heap := uint64(100<<20) + uint64((at.Sub(started)/(10*time.Minute))%6)*(2<<20)
			if role == leakingRole {
				heap += elapsedDays * dailyGrowth
			}
			serviceMemory = append(serviceMemory, ServiceMemory{Role: role,
				ResidentMemoryBytes: heap + (20 << 20), HeapAllocBytes: heap, MemoryLimitBytes: 512 << 20})
		}
		evidence.Samples = append(evidence.Samples, Sample{ObservedAt: at, ServiceMemory: serviceMemory})
	}
	return evidence
}

type staticFaults struct{ events []FaultEvent }

func (source staticFaults) Events(context.Context, string) ([]FaultEvent, error) {
	return source.events, nil
}
func passedFaults(configuration Config, at time.Time) []FaultEvent {
	events := make([]FaultEvent, len(configuration.FaultSchedule.Faults))
	digest := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	for index, fault := range configuration.FaultSchedule.Faults {
		events[index] = FaultEvent{RunID: configuration.Identity.RunID, Scenario: fault.Scenario,
			State: "passed", OccurredAt: at.Add(time.Duration(fault.OffsetSeconds) * time.Second), EvidenceHash: digest}
	}
	return events
}

type memoryStore struct {
	samples  []Sample
	finished int
}

func (*memoryStore) Begin(context.Context, Config, Preflight, time.Time) error { return nil }
func (store *memoryStore) AppendSample(_ context.Context, _ string, sample Sample) error {
	store.samples = append(store.samples, sample)
	return nil
}
func (store *memoryStore) Finish(_ context.Context, evidence Evidence) error {
	store.finished++
	payload, _ := json.Marshal(evidence)
	if len(payload) == 0 {
		return errors.New("empty")
	}
	return nil
}
