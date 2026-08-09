package sandboxQualification

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testClock struct{ now time.Time }

func (clock *testClock) Now() time.Time { return clock.now }
func (clock *testClock) Wait(_ context.Context, duration time.Duration) error {
	clock.now = clock.now.Add(duration)
	return nil
}

type testProbe struct {
	sample Sample
}

func (probe testProbe) Observe(
	_ context.Context,
	_ uint64,
	_ time.Time,
) (Sample, error) {
	return probe.sample, nil
}

type sequenceProbe struct {
	samples []Sample
	index   int
}

func (probe *sequenceProbe) Observe(
	_ context.Context,
	_ uint64,
	_ time.Time,
) (Sample, error) {
	index := min(probe.index, len(probe.samples)-1)
	probe.index++
	return probe.samples[index], nil
}

type testStore struct {
	finished Evidence
	samples  int
}

func (*testStore) Begin(context.Context, Config, time.Time) error { return nil }
func (store *testStore) AppendSample(context.Context, string, Sample) error {
	store.samples++
	return nil
}
func (store *testStore) Finish(_ context.Context, evidence Evidence) error {
	store.finished = evidence
	return nil
}

type testChaos struct{ failed string }

func (chaos testChaos) Events(
	_ context.Context,
	_ string,
) ([]ChaosEvent, error) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	result := make([]ChaosEvent, 0, len(RequiredChaosScenarios))
	for index, scenario := range RequiredChaosScenarios {
		outcome := "PASSED"
		if scenario == chaos.failed {
			outcome = "FAILED"
		}
		result = append(result, ChaosEvent{
			Scenario: scenario, Outcome: outcome,
			DeterministicSeedHash: strings.Repeat("a", 64),
			EvidenceHash:          strings.Repeat("b", 64),
			OccurredAt:            at.Add(time.Duration(index) * time.Nanosecond),
		})
	}
	return result, nil
}

func validTestConfig(t *testing.T, mode Mode, duration time.Duration) Config {
	t.Helper()
	return Config{
		Enabled: true,
		Identity: Identity{
			RunID: "sandbox_qualification-test", Mode: mode,
			CommitSHA:         strings.Repeat("a", 40),
			BuildHash:         strings.Repeat("d", 64),
			ExecutableHash:    strings.Repeat("b", 64),
			ConfigurationHash: strings.Repeat("c", 64),
			Accounts: []AccountIdentity{
				{
					ID: "binance-testnet", Exchange: "binance",
					Environment: "spot_testnet", AccountEpoch: 1,
					CredentialGeneration: 1,
					ConfigurationHash:    strings.Repeat("c", 64),
				},
				{
					ID: "bybit-demo", Exchange: "bybit",
					Environment: "demo", AccountEpoch: 1,
					CredentialGeneration: 1,
					ConfigurationHash:    strings.Repeat("c", 64),
				},
			},
		},
		Duration: duration, SampleInterval: time.Second,
		EvidencePath: filepath.Join(t.TempDir(), "terminal.json"),
	}
}

func healthySample() Sample {
	return Sample{
		ResidentMemoryBytes: 128 * 1024 * 1024,
		AllAccountsFresh:    true, AllLeasesHeld: true,
		PersistenceHealthy: true, RestartSafe: true, EntrySafe: true,
		CriticalAlertLatencyMillis: 100,
		RecoveryDurationMillis:     100,
	}
}

func TestSandboxQualificationSmokeProducesNonQualifiedImmutableEvidence(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: at}
	store := &testStore{}
	configuration := validTestConfig(t, ModeSmoke, 2*time.Second)
	evidence, err := (Runner{
		Clock: clock, Probe: testProbe{sample: healthySample()},
		Store: store, Chaos: testChaos{},
	}).Run(context.Background(), configuration)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.State != StateSmokePassed || evidence.Qualified ||
		evidence.ProfitabilityEvidence || evidence.ObservedDurationSeconds != 2 ||
		store.samples != 3 || store.finished.EvidenceHash == "" {
		t.Fatalf("unsafe smoke verdict: %+v", evidence)
	}
	info, err := os.Stat(configuration.EvidencePath)
	if err != nil || info.Mode().Perm() != 0o440 {
		t.Fatalf("evidence permissions: %v %v", info, err)
	}
	if err = WriteEvidenceNoReplace(
		configuration.EvidencePath, evidence,
	); !os.IsExist(err) {
		t.Fatalf("evidence overwrite was not rejected: %v", err)
	}
}

func TestSandboxQualificationFailsClosedOnProductionTargetAndCap(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: at}
	sample := healthySample()
	sample.ProductionTargetObserved = true
	sample.LargestOrderMicrounits = 10_000_001
	sample.ResidentMemoryBytes = 512 * 1024 * 1024
	configuration := validTestConfig(t, ModeSmoke, 2*time.Second)
	evidence, err := (Runner{
		Clock: clock, Probe: testProbe{sample: sample},
		Store: &testStore{}, Chaos: testChaos{},
	}).Run(context.Background(), configuration)
	if err == nil || evidence.State != StateFailed || evidence.Qualified {
		t.Fatalf("unsafe failure verdict: %+v %v", evidence, err)
	}
	reasons := make(map[string]bool)
	for _, failure := range evidence.Failures {
		reasons[failure.Reason] = true
	}
	if !reasons["production_target"] || !reasons["cap_violation"] {
		t.Fatalf("missing failures: %+v", evidence.Failures)
	}
}

func TestSandboxQualificationFailsClosedOnPositiveMemoryLeakTrend(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	first, last := healthySample(), healthySample()
	first.ResidentMemoryBytes = 128 * 1024 * 1024
	last.ResidentMemoryBytes = 256 * 1024 * 1024
	evidence, err := (Runner{
		Clock: &testClock{now: at},
		Probe: &sequenceProbe{samples: []Sample{first, last}},
		Store: &testStore{},
		Chaos: testChaos{},
	}).Run(
		context.Background(),
		validTestConfig(t, ModeSmoke, time.Second),
	)
	if err == nil || evidence.Qualified ||
		!evidence.SLO.PositiveMemoryLeakTrend ||
		!hasFailure(evidence, "memory_leak") {
		t.Fatalf("positive memory leak trend qualified: %+v %v", evidence, err)
	}
}

var sandboxQualificationSampleInvariantCases = []struct {
	name   string
	reason string
	change func(*Sample)
}{
	{"duplicate create", "duplicate_create", func(sample *Sample) { sample.DuplicateCreates = 1 }},
	{"lost fill", "lost_fill", func(sample *Sample) { sample.LostFills = 1 }},
	{"double posted fill", "double_posted_fill", func(sample *Sample) { sample.DoublePostedFills = 1 }},
	{"unresolved unknown", "unresolved_unknown", func(sample *Sample) {
		sample.UnknownOrders, sample.OldestUnknownSeconds = 1, 30
	}},
	{"reconciliation mismatch", "reconciliation_mismatch", func(sample *Sample) {
		sample.ReconciliationMismatches = 1
	}},
	{"suspense", "suspense", func(sample *Sample) { sample.SuspenseItems = 1 }},
	{"stale account", "stale_data", func(sample *Sample) { sample.AllAccountsFresh = false }},
	{"lease loss", "lease_loss", func(sample *Sample) { sample.AllLeasesHeld = false }},
	{"persistence", "persistence_failure", func(sample *Sample) { sample.PersistenceHealthy = false }},
	{"unsafe restart", "unsafe_restart", func(sample *Sample) { sample.RestartSafe = false }},
	{"production target", "production_target", func(sample *Sample) {
		sample.ProductionTargetObserved = true
	}},
	{"per order cap", "cap_violation", func(sample *Sample) { sample.LargestOrderMicrounits = 10_000_001 }},
	{"daily cap", "cap_violation", func(sample *Sample) { sample.DailySubmittedMicrounits = 50_000_001 }},
	{"account open cap", "cap_violation", func(sample *Sample) { sample.MaximumAccountOpen = 2 }},
	{"global open cap", "cap_violation", func(sample *Sample) { sample.GlobalOpen = 3 }},
	{"critical alert SLO", "critical_alert_slo", func(sample *Sample) {
		sample.CriticalAlertLatencyMillis = 5001
	}},
	{"recovery SLO", "unsafe_restart", func(sample *Sample) {
		sample.RecoveryDurationMillis = 600001
	}},
}

func TestSandboxQualificationFailsClosedForEverySampleInvariant(t *testing.T) {
	for _, test := range sandboxQualificationSampleInvariantCases {
		t.Run(test.name, func(t *testing.T) {
			at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
			sample := healthySample()
			test.change(&sample)
			evidence, err := (Runner{
				Clock: &testClock{now: at},
				Probe: testProbe{sample: sample},
				Store: &testStore{},
				Chaos: testChaos{},
			}).Run(
				context.Background(),
				validTestConfig(t, ModeSmoke, time.Second),
			)
			if err == nil || evidence.State != StateFailed ||
				!hasFailure(evidence, test.reason) {
				t.Fatalf(
					"failure %s not closed: state=%s error=%v failures=%+v",
					test.reason,
					evidence.State,
					err,
					evidence.Failures,
				)
			}
		})
	}
}

func hasFailure(evidence Evidence, reason string) bool {
	for _, failure := range evidence.Failures {
		if failure.Reason == reason {
			return true
		}
	}
	return false
}

func TestSandboxQualificationFailedChaosCannotQualify(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	evidence, err := (Runner{
		Clock: &testClock{now: at},
		Probe: testProbe{sample: healthySample()},
		Store: &testStore{},
		Chaos: testChaos{failed: "database_failure"},
	}).Run(
		context.Background(),
		validTestConfig(t, ModeSmoke, time.Second),
	)
	if err == nil || evidence.Qualified ||
		!hasFailure(evidence, "evidence_failure") {
		t.Fatalf("failed chaos qualified: %+v %v", evidence, err)
	}
}

func TestSandboxQualificationFormalRequiresExactDurationCleanSourceAndTwoEnvironments(t *testing.T) {
	configuration := validTestConfig(t, ModeFormal, FormalDuration)
	configuration.Identity.ImageHash = "sha256:" + strings.Repeat("e", 64)
	if err := ValidateConfig(configuration); err != nil {
		t.Fatal(err)
	}
	configuration.Duration--
	if ValidateConfig(configuration) == nil {
		t.Fatal("short formal duration accepted")
	}
	configuration.Duration = FormalDuration
	configuration.Identity.SourceDirty = true
	if ValidateConfig(configuration) == nil {
		t.Fatal("dirty formal source accepted")
	}
	configuration.Identity.SourceDirty = false
	configuration.Identity.ImageHash = ""
	if ValidateConfig(configuration) == nil {
		t.Fatal("formal identity without image hash accepted")
	}
	configuration.Identity.ImageHash = "sha256:" + strings.Repeat("e", 64)
	configuration.Identity.Accounts[1].Environment = "production"
	if ValidateConfig(configuration) == nil {
		t.Fatal("production environment accepted")
	}
}

type passingHarness struct{}

func (passingHarness) Exercise(
	_ context.Context,
	scenario, seed string,
) (bool, string, error) {
	return true, scenario + ":" + seed, nil
}

func TestSandboxQualificationDeterministicChaosCoversEveryRequiredScenario(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	first, err := RunDeterministicChaos(
		context.Background(), passingHarness{}, "seed-1", at,
	)
	if err != nil || !validateChaos(first) {
		t.Fatalf("chaos rejected: %v", err)
	}
	second, err := RunDeterministicChaos(
		context.Background(), passingHarness{}, "seed-1", at,
	)
	if err != nil || first[0].EvidenceHash != second[0].EvidenceHash {
		t.Fatalf("chaos was not deterministic: %v", err)
	}
}
