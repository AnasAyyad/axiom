package operationalReadiness

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type databaseTelemetryStub struct {
	value DatabaseTelemetry
	err   error
}

func (stub databaseTelemetryStub) Observe(context.Context, time.Time, time.Time) (DatabaseTelemetry, error) {
	return stub.value, stub.err
}

type runtimeTelemetryStub struct {
	value RuntimeTelemetry
	err   error
}

func (stub runtimeTelemetryStub) Observe(context.Context, time.Time) (RuntimeTelemetry, error) {
	return stub.value, stub.err
}

type drillObservationStub struct {
	value DrillObservation
	err   error
}

func (stub drillObservationStub) Observe(time.Time) (DrillObservation, error) {
	return stub.value, stub.err
}

func TestLiveObserverBindsIndependentRealSources(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	observer := LiveObserver{
		Database: databaseTelemetryStub{value: DatabaseTelemetry{
			ObservedAt: now, DiskLevel: "NORMAL", DiskObservedAt: now,
		}},
		Runtime: runtimeTelemetryStub{value: RuntimeTelemetry{
			ObservedAt: now, DecodeBookP99Millis: 9, StrategyRiskP99Millis: 24,
			ResyncP95Millis: 14_000, ResidentMemoryBytes: 600, MemoryLimitBytes: 1200,
			AllDeclaredLoadHealthy: true, ServiceMemory: testServiceMemory(100, 50, 200),
		}},
		Drill:  drillObservationStub{value: passingDrillObservation(now)},
		Window: time.Hour,
	}
	sample, err := observer.Observe(context.Background(), 7, now)
	if err != nil {
		t.Fatal(err)
	}
	sample.ObserverLifecycleHash = strings.Repeat("d", 64)
	sample.ObserverAttempt = 1
	if !sample.ObservedAt.Equal(now) || sample.SourceRevision != 7 || sample.DecodeBookP99Millis != 9 ||
		!sample.AllDeclaredLoadHealthy || !sample.DatabaseCommitRPOZero ||
		!shaPattern.MatchString(sample.DatabaseEvidenceHash) ||
		!shaPattern.MatchString(sample.RuntimeEvidenceHash) ||
		!shaPattern.MatchString(sample.DrillEvidenceHash) || len(sampleFailureReasons(sample)) != 0 {
		t.Fatalf("unexpected live sample: %+v failures=%v", sample, sampleFailureReasons(sample))
	}
}

func TestLiveObserverSampleRoundTripsThroughFreshFileProbe(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	observer := LiveObserver{
		Database: databaseTelemetryStub{value: DatabaseTelemetry{
			ObservedAt: now, DiskLevel: "NORMAL", DiskObservedAt: now,
		}},
		Runtime: runtimeTelemetryStub{value: RuntimeTelemetry{
			ObservedAt: now, ResidentMemoryBytes: 600, MemoryLimitBytes: 1200,
			AllDeclaredLoadHealthy: true, ServiceMemory: testServiceMemory(100, 50, 200),
		}},
		Drill: drillObservationStub{value: passingDrillObservation(now)}, Window: time.Hour,
	}
	sample, err := observer.Observe(context.Background(), 9, now)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sample.json")
	if err = WriteLiveSample(path, sample); err != nil {
		t.Fatal(err)
	}
	read, err := (&FileProbe{Path: path}).Observe(context.Background(), 1, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if read.SourceRevision != 9 || !read.ObservedAt.IsZero() || read.Ordinal != 0 {
		t.Fatalf("unexpected normalized sample: %+v", read)
	}
}

func TestLiveObserverFailsClosedOnStaleOrIncompleteSource(t *testing.T) {
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	observer := LiveObserver{
		Database: databaseTelemetryStub{value: DatabaseTelemetry{
			ObservedAt: now, DiskLevel: "NORMAL", DiskObservedAt: now.Add(-3 * time.Minute),
		}},
		Runtime: runtimeTelemetryStub{value: RuntimeTelemetry{
			ObservedAt: now, ResidentMemoryBytes: 600, MemoryLimitBytes: 1200,
			AllDeclaredLoadHealthy: true, ServiceMemory: testServiceMemory(100, 50, 200),
		}},
		Drill: drillObservationStub{value: passingDrillObservation(now)}, Window: time.Hour,
	}
	if _, err := observer.Observe(context.Background(), 1, now); err == nil {
		t.Fatal("stale storage-pressure evidence was accepted")
	}
}

func TestWriteLiveSampleAtomicallyAdvancesRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.json")
	digest := strings.Repeat("a", 64)
	sample := Sample{SourceRevision: 1, DatabaseEvidenceHash: digest, RuntimeEvidenceHash: digest,
		DrillEvidenceHash: digest, MemoryLimitBytes: 1, DiskLevel: "NORMAL"}
	if err := WriteLiveSample(path, sample); err != nil {
		t.Fatal(err)
	}
	next, err := NextLiveSampleRevision(path)
	if err != nil || next != 2 {
		t.Fatalf("next revision = %d, %v", next, err)
	}
}

func TestHTTPRuntimeTelemetryRequiresEveryFreshMeasuredMetric(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/health") {
			_, _ = response.Write([]byte(`{"status":"ready"}`))
			return
		}
		role := strings.Split(strings.Trim(request.URL.Path, "/"), "/")[0]
		_, _ = fmt.Fprintln(response, "process_resident_memory_bytes 100")
		_, _ = fmt.Fprintln(response, "go_memstats_heap_alloc_bytes 50")
		if role == "recorder" {
			_, _ = fmt.Fprintln(response, "axiom_operational_readiness_decode_book_p99_milliseconds 9")
			_, _ = fmt.Fprintln(response, "axiom_operational_readiness_resync_p95_milliseconds 14000")
			_, _ = fmt.Fprintf(response, "axiom_operational_readiness_telemetry_observed_unixtime{component=\"collector\"} %d\n", now.Unix())
		}
		if role == "engine-shadow" {
			_, _ = fmt.Fprintln(response, "axiom_operational_readiness_strategy_risk_p99_milliseconds 24")
			_, _ = fmt.Fprintf(response, "axiom_operational_readiness_telemetry_observed_unixtime{component=\"strategy_risk\"} %d\n", now.Unix())
		}
	}))
	defer server.Close()
	source := HTTPRuntimeTelemetrySource{Client: server.Client()}
	for _, role := range requiredRuntimeMetricRoles {
		source.MetricTargets = append(source.MetricTargets, RuntimeMetricTarget{
			Role: role, URL: server.URL + "/" + role + "/metrics", MemoryLimitBytes: 200,
		})
		source.HealthTargets = append(source.HealthTargets, RuntimeHealthTarget{
			Role: role, URL: server.URL + "/" + role + "/health",
		})
	}
	telemetry, err := source.Observe(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if telemetry.DecodeBookP99Millis != 9 || telemetry.StrategyRiskP99Millis != 24 ||
		telemetry.ResyncP95Millis != 14_000 || telemetry.ResidentMemoryBytes != 600 ||
		telemetry.MemoryLimitBytes != 1_200 || !telemetry.AllDeclaredLoadHealthy {
		t.Fatalf("unexpected runtime telemetry: %+v", telemetry)
	}
}

func TestHTTPRuntimeTelemetryAttributesTemporaryReadinessLoss(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		role := strings.Split(strings.Trim(request.URL.Path, "/"), "/")[0]
		if strings.HasSuffix(request.URL.Path, "/health") {
			if role == "recorder" {
				_, _ = fmt.Fprintln(response, `{"status":"degraded"}`)
				return
			}
			_, _ = fmt.Fprintln(response, `{"status":"ready"}`)
			return
		}
		_, _ = fmt.Fprintln(response, "process_resident_memory_bytes 100")
		_, _ = fmt.Fprintln(response, "go_memstats_heap_alloc_bytes 50")
		_, _ = fmt.Fprintf(response, "axiom_operational_readiness_telemetry_observed_unixtime{component=\"collector\"} %d\n", now.Unix())
		_, _ = fmt.Fprintf(response, "axiom_operational_readiness_telemetry_observed_unixtime{component=\"strategy_risk\"} %d\n", now.Unix())
		_, _ = fmt.Fprintln(response, "axiom_operational_readiness_decode_book_p99_milliseconds 1")
		_, _ = fmt.Fprintln(response, "axiom_operational_readiness_resync_p95_milliseconds 1")
		_, _ = fmt.Fprintln(response, "axiom_operational_readiness_strategy_risk_p99_milliseconds 1")
	}))
	defer server.Close()
	source := HTTPRuntimeTelemetrySource{Client: server.Client()}
	for _, role := range requiredRuntimeMetricRoles {
		source.MetricTargets = append(source.MetricTargets, RuntimeMetricTarget{
			Role: role, URL: server.URL + "/" + role + "/metrics", MemoryLimitBytes: 200})
		source.HealthTargets = append(source.HealthTargets, RuntimeHealthTarget{
			Role: role, URL: server.URL + "/" + role + "/health"})
	}
	_, err := source.Observe(context.Background(), now)
	failure, ok := SourceFailureDetails(err)
	if !ok || failure.Source != "runtime" || failure.Stage != "health" || failure.Role != "recorder" ||
		failure.Reason != "not_ready" || !failure.Retryable {
		t.Fatalf("unexpected failure: %+v error=%v", failure, err)
	}
}

func TestFileProbePreservesObserverCauseForStalledRevision(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	directory := t.TempDir()
	samplePath := filepath.Join(directory, "sample.json")
	statusPath := filepath.Join(directory, "observer-status.json")
	if err := WriteLiveSample(samplePath, Sample{SourceRevision: 7, ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	probe := &FileProbe{Path: samplePath, StatusPath: statusPath}
	if _, err := probe.Observe(context.Background(), 1, now); err != nil {
		t.Fatal(err)
	}
	cause := SourceFailure{Source: "runtime", Stage: "health", Role: "recorder", Reason: "not_ready", Retryable: true}
	if err := WriteObserverStatus(statusPath, ObserverStatus{SchemaVersion: ObserverStatusSchema,
		UpdatedAt: now.Add(time.Second), LastAttemptAt: now.Add(time.Second), LastSuccessAt: now,
		PublishedRevision: 7, Attempt: 2, ConsecutiveFailures: 1, LastFailure: &cause}); err != nil {
		t.Fatal(err)
	}
	_, err := probe.Observe(context.Background(), 2, now.Add(time.Second))
	failure, ok := probeFailureDetails(err)
	if !ok || failure.Reason != "revision_not_advanced" || failure.SourceCause.Role != "recorder" ||
		failure.SourceCause.Reason != "not_ready" || !failure.Retryable {
		t.Fatalf("unexpected probe failure: %+v error=%v", failure, err)
	}
}

func TestFileProbeDoesNotRetryNonRetryableObserverCause(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	directory := t.TempDir()
	samplePath := filepath.Join(directory, "sample.json")
	statusPath := filepath.Join(directory, "observer-status.json")
	if err := WriteLiveSample(samplePath, Sample{SourceRevision: 7, ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	probe := &FileProbe{Path: samplePath, StatusPath: statusPath}
	if _, err := probe.Observe(context.Background(), 1, now); err != nil {
		t.Fatal(err)
	}
	cause := SourceFailure{Source: "runtime", Stage: "metrics", Role: "recorder",
		Reason: "metric_value_invalid", Retryable: false}
	if err := WriteObserverStatus(statusPath, ObserverStatus{SchemaVersion: ObserverStatusSchema,
		UpdatedAt: now.Add(time.Second), LastAttemptAt: now.Add(time.Second), LastSuccessAt: now,
		PublishedRevision: 7, Attempt: 2, ConsecutiveFailures: 1, LastFailure: &cause}); err != nil {
		t.Fatal(err)
	}
	_, err := probe.Observe(context.Background(), 2, now.Add(time.Second))
	failure, ok := probeFailureDetails(err)
	if !ok || failure.Retryable || failure.SourceCause.Reason != "metric_value_invalid" {
		t.Fatalf("unexpected probe failure: %+v error=%v", failure, err)
	}
}

func passingDrillObservation(now time.Time) DrillObservation {
	return DrillObservation{
		SchemaVersion: DrillObservationSchema, ObservedAt: now,
		CriticalAlertMillis: 5_000, ExternalAlertP95Millis: 60_000,
		GracefulShutdownMillis: 60_000, ShadowRecoveryMillis: 300_000,
		SandboxRecoveryMillis: 600_000, DatabaseCommitRPOZero: true,
		RecorderWithinFlushRPO: true, HeavyJobsRejectedAtHigh: true,
		RecordingPausedAtCritical: true, JournalAuditWritable: true,
		DeclaredLoadExercised: true,
	}
}
