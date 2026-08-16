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

func TestViennaRehearsalWarnsOnRouteClockButCannotQualify(t *testing.T) {
	checkedAt := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	preflight := testPreflight(checkedAt.Add(-time.Minute))
	preflight.ReferenceServerApproved = true
	preflight.RouteClockThresholdPassed = false
	sample, err := (&safeProbe{}).Observe(context.Background(), 1, checkedAt)
	if err != nil {
		t.Fatal(err)
	}
	report := CheckPreflightSourcesForProfile(
		&preflight, &sample, ModeFormal, PreflightProfileViennaRehearsal, checkedAt,
	)
	if !report.Ready || !report.PreflightPassed || !report.SamplePassed ||
		report.Qualified || report.FormalClockStarted ||
		containsReason(report.PreflightFailures, "route_clock_threshold_failed") ||
		!containsReason(report.Warnings, "route_clock_threshold_exceeded") {
		t.Fatalf("unexpected rehearsal report: %+v", report)
	}
}

func TestViennaRehearsalWarnsOnFinalHostInfrastructureOnly(t *testing.T) {
	checkedAt := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	preflight := testPreflight(checkedAt.Add(-time.Minute))
	preflight.ReferenceServerApproved = true
	preflight.TLSValid = false
	preflight.RemoteBackupIndependent = false
	preflight.BackupAgeSeconds = 48 * 60 * 60
	preflight.CleanRestorePassed = false
	preflight.CleanRestoreDurationSeconds = uint64(CleanRestoreRTO.Seconds()) + 1
	preflight.MarketDataRecoveryPassed = false
	sample, err := (&safeProbe{}).Observe(context.Background(), 1, checkedAt)
	if err != nil {
		t.Fatal(err)
	}
	report := CheckPreflightSourcesForProfile(
		&preflight, &sample, ModeFormal, PreflightProfileViennaRehearsal, checkedAt,
	)
	warnings := []string{
		"tls_skipped_private_rehearsal",
		"remote_backup_skipped_disposable_rehearsal",
		"backup_freshness_skipped_disposable_rehearsal",
		"clean_restore_skipped_disposable_rehearsal",
		"clean_restore_rto_skipped_disposable_rehearsal",
		"market_data_recovery_skipped_disposable_rehearsal",
	}
	if !report.Ready || !report.PreflightPassed || report.Qualified || report.FormalClockStarted {
		t.Fatalf("unexpected rehearsal report: %+v", report)
	}
	for _, warning := range warnings {
		if !containsReason(report.Warnings, warning) {
			t.Fatalf("missing warning %q in %+v", warning, report)
		}
	}
}

func TestViennaRehearsalKeepsPortableGatesStrict(t *testing.T) {
	checkedAt := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	preflight := testPreflight(checkedAt.Add(-time.Minute))
	preflight.ReferenceServerApproved = true
	preflight.PinnedImageDigests = false
	preflight.ResourceLimitsPassed = false
	preflight.SchemaUpgradePassed = false
	preflight.RollbackForwardFixPassed = false
	preflight.SBOMPresent = false
	preflight.SecurityScanPassed = false
	sample, err := (&safeProbe{}).Observe(context.Background(), 1, checkedAt)
	if err != nil {
		t.Fatal(err)
	}
	report := CheckPreflightSourcesForProfile(
		&preflight, &sample, ModeFormal, PreflightProfileViennaRehearsal, checkedAt,
	)
	for _, failure := range []string{
		"image_digests_unpinned", "resource_limits_failed", "schema_upgrade_failed",
		"rollback_forward_fix_failed", "sbom_missing", "security_scan_failed",
	} {
		if !containsReason(report.PreflightFailures, failure) {
			t.Fatalf("portable gate %q was waived: %+v", failure, report)
		}
	}
}

func TestStrictPreflightRejectsFinalHostInfrastructureFailures(t *testing.T) {
	checkedAt := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	preflight := testPreflight(checkedAt.Add(-time.Minute))
	preflight.ReferenceServerApproved = true
	preflight.TLSValid = false
	preflight.RemoteBackupIndependent = false
	preflight.BackupAgeSeconds = 48 * 60 * 60
	preflight.CleanRestorePassed = false
	preflight.MarketDataRecoveryPassed = false
	sample, err := (&safeProbe{}).Observe(context.Background(), 1, checkedAt)
	if err != nil {
		t.Fatal(err)
	}
	report := CheckPreflightSourcesForProfile(
		&preflight, &sample, ModeFormal, PreflightProfileStrict, checkedAt,
	)
	for _, failure := range []string{
		"tls_invalid", "remote_backup_not_independent", "backup_stale",
		"clean_restore_failed", "market_data_recovery_failed",
	} {
		if !containsReason(report.PreflightFailures, failure) {
			t.Fatalf("strict gate %q was not enforced: %+v", failure, report)
		}
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("strict report emitted warnings: %+v", report)
	}
}

func TestStrictPreflightStillRejectsRouteClockThreshold(t *testing.T) {
	checkedAt := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	preflight := testPreflight(checkedAt.Add(-time.Minute))
	preflight.ReferenceServerApproved = true
	preflight.RouteClockThresholdPassed = false
	sample, err := (&safeProbe{}).Observe(context.Background(), 1, checkedAt)
	if err != nil {
		t.Fatal(err)
	}
	report := CheckPreflightSourcesForProfile(
		&preflight, &sample, ModeFormal, PreflightProfileStrict, checkedAt,
	)
	if report.Ready || !containsReason(report.PreflightFailures, "route_clock_threshold_failed") ||
		len(report.Warnings) != 0 {
		t.Fatalf("unexpected strict report: %+v", report)
	}
}

func TestUnknownPreflightProfileFailsClosed(t *testing.T) {
	checkedAt := time.Date(2026, 8, 16, 8, 0, 0, 0, time.UTC)
	preflight := testPreflight(checkedAt.Add(-time.Minute))
	preflight.ReferenceServerApproved = true
	sample, err := (&safeProbe{}).Observe(context.Background(), 1, checkedAt)
	if err != nil {
		t.Fatal(err)
	}
	report := CheckPreflightSourcesForProfile(
		&preflight, &sample, ModeFormal, PreflightProfile("unknown"), checkedAt,
	)
	if report.Ready || !containsReason(report.PreflightFailures, "preflight_profile_invalid") {
		t.Fatalf("unexpected invalid-profile report: %+v", report)
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
