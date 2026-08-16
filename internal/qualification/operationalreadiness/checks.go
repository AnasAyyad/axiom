package operationalReadiness

import "time"

// PreflightReport is a redacted, non-qualifying readiness-input verdict.
// It never represents a started formal clock or a D5 qualification result.
type PreflightReport struct {
	SchemaVersion               string    `json:"schema_version"`
	CheckedAt                   time.Time `json:"checked_at"`
	Mode                        Mode      `json:"mode"`
	ConfigurationContractPassed bool      `json:"configuration_contract_passed"`
	PreflightPassed             bool      `json:"preflight_passed"`
	SamplePassed                bool      `json:"sample_passed"`
	Ready                       bool      `json:"ready"`
	FormalClockStarted          bool      `json:"formal_clock_started"`
	Qualified                   bool      `json:"qualified"`
	PreflightFailures           []string  `json:"preflight_failures"`
	SampleFailures              []string  `json:"sample_failures"`
}

// CheckPreflightInputs evaluates one pre-clock preflight and live sample using
// the same thresholds as the formal runner. The result can never qualify D5.
func CheckPreflightInputs(preflight Preflight, sample Sample, mode Mode, checkedAt time.Time) PreflightReport {
	return CheckPreflightSources(&preflight, &sample, mode, checkedAt)
}

// CheckPreflightSources evaluates each available source independently and
// records bounded source-read failures without exposing payloads or raw errors.
func CheckPreflightSources(preflight *Preflight, sample *Sample, mode Mode, checkedAt time.Time) PreflightReport {
	preflightFailures := []string{}
	sampleFailures := []string{}
	if preflight == nil {
		preflightFailures = append(preflightFailures, "preflight_source_unavailable")
	} else {
		preflightFailures = preflightFailureReasons(*preflight, mode)
		preflightFailures = append(preflightFailures, preflightWindowFailureReasons(*preflight, checkedAt)...)
	}
	if sample == nil {
		sampleFailures = append(sampleFailures, "sample_unavailable")
	} else {
		sampleFailures = sampleFailureReasons(*sample)
	}
	return newPreflightReport(mode, checkedAt, preflightFailures, sampleFailures)
}

func newPreflightReport(mode Mode, checkedAt time.Time, preflightFailures, sampleFailures []string) PreflightReport {
	preflightPassed := len(preflightFailures) == 0
	samplePassed := len(sampleFailures) == 0
	return PreflightReport{
		SchemaVersion:                "axiom.operational_readiness.preflight-report.v1",
		CheckedAt:                    checkedAt,
		Mode:                         mode,
		ConfigurationContractPassed: true,
		PreflightPassed:              preflightPassed,
		SamplePassed:                 samplePassed,
		Ready:                        preflightPassed && samplePassed,
		FormalClockStarted:           false,
		Qualified:                    false,
		PreflightFailures:            preflightFailures,
		SampleFailures:               sampleFailures,
	}
}

func preflightFailureReasons(preflight Preflight, mode Mode) []string {
	checks := []struct {
		failed bool
		reason string
	}{
		{preflight.CheckedAt.IsZero() || preflight.CheckedAt.Location() != time.UTC, "checked_at_invalid"},
		{mode == ModeFormal && !preflight.ReferenceServerApproved, "reference_server_unapproved"},
		{!preflight.ClockSynchronized, "clock_unsynchronized"},
		{preflight.ClockThresholdMillis != ClockThresholdMillis, "clock_threshold_invalid"},
		{preflight.ClockOffsetMillis > preflight.ClockThresholdMillis, "clock_offset_exceeded"},
		{!preflight.RouteClockThresholdPassed, "route_clock_threshold_failed"},
		{!preflight.TLSValid, "tls_invalid"},
		{!preflight.PinnedImageDigests, "image_digests_unpinned"},
		{!preflight.NonRootExecution, "non_root_execution_failed"},
		{!preflight.ResourceLimitsPassed, "resource_limits_failed"},
		{!preflight.DiskCapacityPassed, "disk_capacity_failed"},
		{!preflight.RemoteBackupIndependent, "remote_backup_not_independent"},
		{preflight.BackupAgeSeconds > 24*60*60, "backup_stale"},
		{!preflight.CleanRestorePassed, "clean_restore_failed"},
		{preflight.CleanRestoreDurationSeconds > uint64(CleanRestoreRTO.Seconds()), "clean_restore_rto_exceeded"},
		{!preflight.MarketDataRecoveryPassed, "market_data_recovery_failed"},
		{!preflight.SchemaUpgradePassed, "schema_upgrade_failed"},
		{!preflight.RollbackForwardFixPassed, "rollback_forward_fix_failed"},
		{!preflight.SBOMPresent, "sbom_missing"},
		{!preflight.SecurityScanPassed, "security_scan_failed"},
		{!preflight.ProductionPrivateSubmissionImpossible, "production_private_submission_possible"},
	}
	return failedReasons(checks)
}

func preflightWindowFailureReasons(preflight Preflight, checkedAt time.Time) []string {
	if checkedAt.IsZero() || checkedAt.Location() != time.UTC {
		return []string{"preflight_check_time_invalid"}
	}
	if preflight.CheckedAt.After(checkedAt) {
		return []string{"preflight_checked_at_in_future"}
	}
	if checkedAt.Sub(preflight.CheckedAt) > MaximumPreflightAge {
		return []string{"preflight_stale"}
	}
	return []string{}
}

func sampleFailureReasons(sample Sample) []string {
	checks := []struct {
		failed bool
		reason string
	}{
		{sample.StaleDecisions > 0, "stale_decision"},
		{sample.UninvalidatedGaps > 0, "uninvalidated_gap"},
		{sample.DuplicateOrders > 0, "duplicate_order"},
		{sample.LostFills > 0, "lost_fill"},
		{sample.DoublePostedFills > 0, "double_posted_fill"},
		{sample.UnbalancedJournals > 0, "unbalanced_journal"},
		{sample.ReplayMismatches > 0, "replay_mismatch"},
		{sample.DecodeBookP99Millis > 10 || sample.StrategyRiskP99Millis > 25 || sample.ResyncP95Millis > 15_000, "latency_slo"},
		{sample.CriticalAlertMillis > uint64(CriticalAlertSLO.Milliseconds()) || sample.ExternalAlertP95Millis > uint64(ExternalAlertSLO.Milliseconds()), "alert_slo"},
		{sample.GracefulShutdownMillis > uint64(GracefulShutdownSLO.Milliseconds()), "shutdown_slo"},
		{sample.ShadowRecoveryMillis > uint64(ShadowRecoveryRTO.Milliseconds()) || sample.SandboxRecoveryMillis > uint64(SandboxRecoveryRTO.Milliseconds()), "recovery_rto"},
		{!sample.DatabaseCommitRPOZero || !sample.RecorderWithinFlushRPO, "rpo_breach"},
		{sample.MemoryLimitBytes == 0 || sample.ResidentMemoryBytes > sample.MemoryLimitBytes || !sample.AllDeclaredLoadHealthy, "resource_limit"},
		{sample.DiskLevel != "NORMAL" && sample.DiskLevel != "HIGH" && sample.DiskLevel != "CRITICAL", "disk_pressure_unsafe"},
		{sample.DiskLevel == "HIGH" && !sample.HeavyJobsRejectedAtHigh, "disk_pressure_unsafe"},
		{sample.DiskLevel == "CRITICAL" && (!sample.RecordingPausedAtCritical || !sample.JournalAuditWritable), "disk_pressure_unsafe"},
		{sample.ProductionTargetObserved, "production_target"},
		{sample.ProhibitedCapabilityObserved, "prohibited_capability"},
	}
	return failedReasons(checks)
}

func failedReasons(checks []struct {
	failed bool
	reason string
}) []string {
	reasons := []string{}
	for _, check := range checks {
		if check.failed && !containsReason(reasons, check.reason) {
			reasons = append(reasons, check.reason)
		}
	}
	return reasons
}

func containsReason(reasons []string, wanted string) bool {
	for _, reason := range reasons {
		if reason == wanted {
			return true
		}
	}
	return false
}
