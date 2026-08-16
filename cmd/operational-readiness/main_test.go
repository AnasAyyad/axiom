package main

import (
	"testing"

	"axiom/internal/qualification/operationalreadiness"
)

func TestManifestContractCannotOmitOrDuplicateDeclaredCoverage(t *testing.T) {
	source := runFile{DurationSeconds: 604800, SampleIntervalSeconds: 60}
	manifest := testManifest{SchemaVersion: "axiom.operational_readiness.test-manifest.v1",
		DurationSeconds: 604800, SampleIntervalSeconds: 60,
		ClockOffsetThresholdMillis: operationalReadiness.ClockThresholdMillis,
		DeclaredLoad: []string{
			"collectors_and_recording", "coherent_sampling", "strategies_allocator_risk_accounting",
			"shadow_and_virtual_execution", "api_sse_and_ui", "lab_jobs",
			"reports_exports_and_alerts", "encrypted_backup_and_clean_restore", "resource_limits",
		},
		ZeroTolerance: []string{
			"stale_decision", "uninvalidated_gap", "duplicate_order", "lost_fill",
			"double_posted_fill", "unbalanced_journal", "replay_mismatch",
			"production_private_submission", "prohibited_capability",
		},
		IndependentVerdicts: []string{
			"coherent market-data qualification", "sandbox order and reconciliation qualification",
		},
	}
	if err := validateTestManifest(manifest, source); err != nil {
		t.Fatal(err)
	}
	manifest.DeclaredLoad[len(manifest.DeclaredLoad)-1] = manifest.DeclaredLoad[0]
	if err := validateTestManifest(manifest, source); err == nil {
		t.Fatal("duplicated declared load accepted")
	}
}

func TestCheckedInManifestMatchesRunnerContract(t *testing.T) {
	const manifestPath = "../../deploy/config/operational-readiness-test-manifest-v1.json"
	const schedulePath = "../../deploy/config/operational-readiness-fault-schedule-v1.json"
	var manifest testManifest
	if err := readJSON(manifestPath, &manifest); err != nil {
		t.Fatal(err)
	}
	source := runFile{DurationSeconds: 604800, SampleIntervalSeconds: 60}
	if err := validateTestManifest(manifest, source); err != nil {
		t.Fatal(err)
	}
	scheduleHash, err := fileHash(schedulePath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.FaultScheduleSHA256 != scheduleHash {
		t.Fatal("checked-in manifest does not bind the checked-in fault schedule")
	}
}

func TestPreflightCheckModeIsExplicit(t *testing.T) {
	t.Setenv("AXIOM_OPERATIONAL_READINESS_PREFLIGHT_CHECK", "1")
	enabled, err := preflightCheckEnabled()
	if err != nil || !enabled {
		t.Fatalf("enabled=%t error=%v", enabled, err)
	}
	t.Setenv("AXIOM_OPERATIONAL_READINESS_PREFLIGHT_CHECK", "yes")
	if _, err = preflightCheckEnabled(); err == nil {
		t.Fatal("ambiguous preflight check mode accepted")
	}
}
