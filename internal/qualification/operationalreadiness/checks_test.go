package operationalReadiness

import (
	"context"
	"testing"
	"time"
)

func TestPreflightReportCanNeverQualifyOrStartClock(t *testing.T) {
	checkedAt := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	preflight := testPreflight(checkedAt.Add(-time.Minute))
	preflight.ReferenceServerApproved = true
	sample, err := (&safeProbe{}).Observe(context.Background(), 1, checkedAt)
	if err != nil {
		t.Fatal(err)
	}
	report := CheckPreflightInputs(preflight, sample, ModeFormal, checkedAt)
	if !report.Ready || !report.PreflightPassed || !report.SamplePassed ||
		report.Qualified || report.FormalClockStarted {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestPreflightReportListsStableInputFailures(t *testing.T) {
	checkedAt := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	preflight := testPreflight(checkedAt.Add(-MaximumPreflightAge - time.Second))
	preflight.ReferenceServerApproved = false
	sample, err := (&safeProbe{mutate: func(sample *Sample) {
		sample.LostFills = 1
		sample.MemoryLimitBytes = 0
	}}).Observe(context.Background(), 1, checkedAt)
	if err != nil {
		t.Fatal(err)
	}
	report := CheckPreflightInputs(preflight, sample, ModeFormal, checkedAt)
	if report.Ready || report.Qualified || report.FormalClockStarted ||
		!containsReason(report.PreflightFailures, "reference_server_unapproved") ||
		!containsReason(report.PreflightFailures, "preflight_stale") ||
		!containsReason(report.SampleFailures, "lost_fill") ||
		!containsReason(report.SampleFailures, "resource_limit") {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestUnavailablePreflightReportIsFailClosedAndRedacted(t *testing.T) {
	checkedAt := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	report := CheckPreflightSources(nil, nil, ModeFormal, checkedAt)
	if report.Ready || report.Qualified || report.FormalClockStarted ||
		!containsReason(report.PreflightFailures, "preflight_source_unavailable") ||
		!containsReason(report.SampleFailures, "sample_unavailable") {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestCheckedInFaultScheduleMatchesRunnerContract(t *testing.T) {
	var schedule FaultSchedule
	if err := readStrictJSON("../../../deploy/config/operational-readiness-fault-schedule-v1.json", &schedule); err != nil {
		t.Fatal(err)
	}
	if err := validateSchedule(schedule, FormalDuration); err != nil {
		t.Fatal(err)
	}
}
