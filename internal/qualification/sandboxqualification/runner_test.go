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

type recoveryTestStore struct {
	testStore
	events []RecoveryEvent
}

func (store *recoveryTestStore) AppendRecoveryEvent(_ context.Context, event RecoveryEvent) error {
	store.events = append(store.events, event)
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
		store.samples != 2 || store.finished.EvidenceHash == "" {
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

func TestSandboxQualificationWaitsForFirstPostStartReconciliationSample(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	clock := &testClock{now: at}
	store := &testStore{}
	evidence, err := (Runner{
		Clock: clock, Probe: testProbe{sample: healthySample()},
		Store: store, Chaos: testChaos{},
	}).Run(context.Background(), validTestConfig(t, ModeSmoke, 2*time.Second))
	if err != nil || evidence.State != StateSmokePassed ||
		evidence.ObservedDurationSeconds != 2 || store.samples != 2 {
		t.Fatalf("first post-start sample was not delayed safely: state=%s duration=%d samples=%d err=%v", evidence.State, evidence.ObservedDurationSeconds, store.samples, err)
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
		validTestConfig(t, ModeSmoke, 2*time.Second),
	)
	if err == nil || evidence.Qualified ||
		!evidence.SLO.PositiveMemoryLeakTrend ||
		!hasFailure(evidence, "memory_leak") {
		t.Fatalf("positive memory leak trend qualified: %+v %v", evidence, err)
	}
}

func TestSandboxQualificationContinuesClockAcrossOnePermittedAccountRecovery(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	deadline := at.Add(2 * time.Minute)
	active := healthySample()
	active.AllAccountsFresh, active.EntrySafe, active.RecoveryActive = false, false, true
	active.Accounts = []AccountObservation{
		{
			ID: "binance-testnet", Exchange: "binance", Environment: "spot_testnet",
			Epoch: 1, State: "DEGRADED", StreamHealthy: true, EvidenceHealthy: true,
			LeaseHeld: true, AccountSafe: true, ReconciliationClean: false,
			RecoveryState: "active", RecoveryEvent: "detected",
			IncidentSource: "reconciliation",
			FailureKind:    "transient_outage", CauseCode: "http_503",
			DeadlineAt: &deadline,
		},
		{ID: "bybit-demo", Exchange: "bybit", Environment: "demo", Epoch: 1,
			State: "READY_PAUSED", StreamHealthy: true, EvidenceHealthy: true,
			LeaseHeld: true, AccountSafe: true, ReconciliationClean: true,
			RecoveryState: "not_required"},
	}
	firstClean := active
	firstClean.Accounts = append([]AccountObservation(nil), active.Accounts...)
	firstClean.Accounts[0].RecoveryEvent = "first_clean_check"
	firstClean.Accounts[0].CleanCheckCount = 1
	recovered := healthySample()
	recovered.Accounts = append([]AccountObservation(nil), active.Accounts...)
	recovered.Accounts[0].State = "READY_PAUSED"
	recovered.Accounts[0].RecoveryState = "recovered"
	recovered.Accounts[0].RecoveryEvent = "recovered"
	recovered.Accounts[0].CleanCheckCount = 2
	recovered.Accounts[0].ReconciliationClean = true
	recovered.Accounts[0].RecoveryTimestamp = &at
	recovered.RecoveryActive = false
	evidence, err := (Runner{
		Clock: &testClock{now: at},
		Probe: &sequenceProbe{samples: []Sample{active, firstClean, recovered}},
		Store: &testStore{}, Chaos: testChaos{},
	}).Run(context.Background(), validTestConfig(t, ModeSmoke, 2*time.Second))
	if err != nil || evidence.State != StateSmokePassed || len(evidence.Failures) != 0 ||
		evidence.ObservedDurationSeconds != 3 {
		t.Fatalf("permitted recovery poisoned run: state=%s failures=%+v err=%v", evidence.State, evidence.Failures, err)
	}
}

func TestSandboxQualificationPersistsRuntimeDerivedRecoveryLifecycleBetweenSamples(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	configuration := validTestConfig(t, ModeSmoke, time.Minute)
	store := &recoveryTestStore{}
	sample := runtimeDerivedRecoverySample(at)
	evidence := newEvidence(configuration, at)
	(Runner{Store: store}).appendRecoveryEvents(
		context.Background(), configuration, &evidence, sample,
	)
	if len(store.events) != 3 || len(evidence.RecoveryEvents) != 3 ||
		store.events[0].Event != "detected" ||
		store.events[1].Event != "first_clean_check" ||
		store.events[2].Event != "recovered" ||
		store.events[2].IncidentSource != "private_stream" {
		t.Fatalf("recovery lifecycle=%+v", store.events)
	}
	for _, event := range store.events {
		if len(event.EvidenceHash) != 64 || event.FailureKind != "transient_outage" ||
			event.CauseCode != "private_stream_receive_failed" {
			t.Fatalf("unredacted or unbound recovery event=%+v", event)
		}
	}
}

func runtimeDerivedRecoverySample(at time.Time) Sample {
	deadline := at.Add(2 * time.Minute)
	recoveredAt := at.Add(32 * time.Second)
	sample := healthySample()
	sample.ObservedAt = at.Add(33 * time.Second)
	sample.Accounts = []AccountObservation{{
		ID: "bybit-demo", Exchange: "bybit", Environment: "demo", Epoch: 1,
		State: "READY_PAUSED", StreamHealthy: true, EvidenceHealthy: true,
		LeaseHeld: true, AccountSafe: true, ReconciliationClean: true,
		RecoveryState: "recovered", RecoveryEvent: "recovered",
		IncidentSource: "private_stream", FailureKind: "transient_outage",
		CauseCode: "private_stream_receive_failed", DeadlineAt: &deadline,
		CleanCheckCount: 2, RecoveryTimestamp: &recoveredAt,
		RecoveryEvents: runtimeDerivedRecoveryEvents(at, deadline, recoveredAt),
	}}
	return sample
}

func runtimeDerivedRecoveryEvents(
	at, deadline, recoveredAt time.Time,
) []AccountRecoveryEvent {
	return []AccountRecoveryEvent{
		{
			Event: "detected", State: "active", IncidentSource: "private_stream",
			FailureKind: "transient_outage", CauseCode: "private_stream_receive_failed",
			DeadlineAt: deadline, OccurredAt: at.Add(time.Second),
		},
		{
			Event: "first_clean_check", State: "active",
			IncidentSource: "private_stream", FailureKind: "transient_outage",
			CauseCode: "private_stream_receive_failed", DeadlineAt: deadline,
			CleanCheckCount: 1, OccurredAt: at.Add(2 * time.Second),
		},
		{
			Event: "recovered", State: "recovered", IncidentSource: "private_stream",
			FailureKind: "transient_outage", CauseCode: "private_stream_receive_failed",
			DeadlineAt: deadline, CleanCheckCount: 2,
			RecoveryTimestamp: &recoveredAt, OccurredAt: recoveredAt,
		},
	}
}

func TestSandboxQualificationRecoveryTerminalStatesFailClosed(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	deadline := at.Add(2 * time.Minute)
	sample := healthySample()
	sample.AllAccountsFresh, sample.EntrySafe = false, false
	sample.RecoveryActive = false
	sample.Accounts = []AccountObservation{{
		ID: "binance-testnet", Exchange: "binance", Environment: "spot_testnet",
		Epoch: 1, State: "DEGRADED", StreamHealthy: true, EvidenceHealthy: true,
		LeaseHeld: true, AccountSafe: true, RecoveryState: "expired",
		FailureKind: "transient_outage", CauseCode: "http_503", DeadlineAt: &deadline,
	}}
	evidence, err := (Runner{
		Clock: &testClock{now: at}, Probe: testProbe{sample: sample},
		Store: &testStore{}, Chaos: testChaos{},
	}).Run(context.Background(), validTestConfig(t, ModeSmoke, time.Second))
	if err == nil || evidence.State != StateFailed ||
		!hasFailure(evidence, "recovery_expired") {
		t.Fatalf("expired recovery qualified: %+v err=%v", evidence, err)
	}
}

func TestSandboxQualificationRecoveryDoesNotMaskAnotherUnsafeAccount(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	deadline := at.Add(2 * time.Minute)
	sample := healthySample()
	sample.AllAccountsFresh, sample.EntrySafe, sample.RecoveryActive = false, false, true
	sample.Accounts = []AccountObservation{
		{
			ID: "binance-testnet", Exchange: "binance", Environment: "spot_testnet",
			Epoch: 1, State: "DEGRADED", StreamHealthy: true, EvidenceHealthy: true,
			LeaseHeld: true, AccountSafe: true, RecoveryState: "active",
			IncidentSource: "reconciliation",
			FailureKind:    "transient_outage", CauseCode: "http_503", DeadlineAt: &deadline,
		},
		{
			ID: "bybit-demo", Exchange: "bybit", Environment: "demo",
			Epoch: 1, State: "LOCKED", StreamHealthy: true, EvidenceHealthy: true,
			LeaseHeld: true, AccountSafe: false, ReconciliationClean: false,
			RecoveryState: "not_required",
		},
	}
	evidence, err := (Runner{
		Clock: &testClock{now: at}, Probe: testProbe{sample: sample},
		Store: &testStore{}, Chaos: testChaos{},
	}).Run(context.Background(), validTestConfig(t, ModeSmoke, time.Second))
	if err == nil || evidence.State != StateFailed ||
		!hasFailure(evidence, "stale_data") {
		t.Fatalf("unrelated unsafe account was masked: %+v err=%v", evidence, err)
	}
}

func TestSandboxQualificationAllowsDisconnectedStreamOnlyBeforeItsFirstCleanCheck(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 1, 0, time.UTC)
	deadline := at.Add(time.Minute)
	sample := healthySample()
	sample.ObservedAt = at
	sample.AllAccountsFresh, sample.EntrySafe, sample.RecoveryActive = false, false, true
	sample.Accounts = []AccountObservation{
		{
			ID: "binance-testnet", Exchange: "binance", Environment: "spot_testnet",
			Epoch: 1, State: "DEGRADED", StreamHealthy: false, EvidenceHealthy: true,
			LeaseHeld: true, AccountSafe: true, ReconciliationClean: false,
			RecoveryState: "active", IncidentSource: "private_stream",
			FailureKind: "transient_outage", CauseCode: "private_stream_receive_failed",
			DeadlineAt: &deadline,
		},
		{
			ID: "bybit-demo", Exchange: "bybit", Environment: "demo", Epoch: 1,
			State: "READY_PAUSED", StreamHealthy: true, EvidenceHealthy: true,
			LeaseHeld: true, AccountSafe: true, ReconciliationClean: true,
			RecoveryState: "not_required",
		},
	}
	if !sampleAllowsActiveRecovery(sample) {
		t.Fatal("bounded private-stream reconnect was rejected before its first check")
	}
	bothAccounts := sample
	bothAccounts.Accounts = append([]AccountObservation(nil), sample.Accounts...)
	bothAccounts.Accounts[1] = AccountObservation{
		ID: "bybit-demo", Exchange: "bybit", Environment: "demo", Epoch: 1,
		State: "DEGRADED", StreamHealthy: true, EvidenceHealthy: true,
		LeaseHeld: true, AccountSafe: true, ReconciliationClean: false,
		RecoveryState: "active", IncidentSource: "reconciliation",
		FailureKind: "maintenance", CauseCode: "exchange_maintenance",
		DeadlineAt: &deadline,
	}
	if !sampleAllowsActiveRecovery(bothAccounts) {
		t.Fatal("the per-account policy rejected two independently safe recoveries")
	}
	sample.Accounts[0].CleanCheckCount = 1
	if sampleAllowsActiveRecovery(sample) {
		t.Fatal("clean check was accepted while the private stream was unhealthy")
	}
}

func TestSandboxQualificationActiveRecoveryPastDeadlineFailsExpired(t *testing.T) {
	at := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	deadline := at.Add(500 * time.Millisecond)
	sample := healthySample()
	sample.AllAccountsFresh, sample.EntrySafe, sample.RecoveryActive = false, false, true
	sample.Accounts = []AccountObservation{{
		ID: "binance-testnet", Exchange: "binance", Environment: "spot_testnet",
		Epoch: 1, State: "DEGRADED", StreamHealthy: true, EvidenceHealthy: true,
		LeaseHeld: true, AccountSafe: true, ReconciliationClean: false,
		RecoveryState: "active", IncidentSource: "reconciliation",
		FailureKind: "maintenance", CauseCode: "exchange_maintenance",
		DeadlineAt: &deadline,
	}}
	evidence, err := (Runner{
		Clock: &testClock{now: at}, Probe: testProbe{sample: sample},
		Store: &testStore{}, Chaos: testChaos{},
	}).Run(context.Background(), validTestConfig(t, ModeSmoke, time.Second))
	if err == nil || !hasFailure(evidence, "recovery_expired") {
		t.Fatalf("expired active recovery did not fail: %+v error=%v", evidence, err)
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
		sample.RecoveryDurationMillis = 120001
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
