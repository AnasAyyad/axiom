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
			ResyncP95Millis: 14_000, ResidentMemoryBytes: 100, MemoryLimitBytes: 200,
			AllDeclaredLoadHealthy: true,
		}},
		Drill:  drillObservationStub{value: passingDrillObservation(now)},
		Window: time.Hour,
	}
	sample, err := observer.Observe(context.Background(), 7, now)
	if err != nil {
		t.Fatal(err)
	}
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
			ObservedAt: now, MemoryLimitBytes: 200, AllDeclaredLoadHealthy: true,
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
			ObservedAt: now, MemoryLimitBytes: 200, AllDeclaredLoadHealthy: true,
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
