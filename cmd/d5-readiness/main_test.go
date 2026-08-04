package main

import (
	"testing"

	"axiom/internal/qualification/d5"
)

func TestManifestContractCannotOmitOrDuplicateDeclaredCoverage(t *testing.T) {
	source := runFile{DurationSeconds: 604800, SampleIntervalSeconds: 60}
	manifest := testManifest{SchemaVersion: "axiom.d5.test-manifest.v1",
		DurationSeconds: 604800, SampleIntervalSeconds: 60,
		ClockOffsetThresholdMillis: d5.ClockThresholdMillis,
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
			"B2 market-data qualification", "C6 sandbox order and reconciliation qualification",
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
