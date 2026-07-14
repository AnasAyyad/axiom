package segments

import (
	"strings"
	"testing"
	"time"
)

func TestRetentionPlanningIsDenyByDefaultAndCanonical(t *testing.T) {
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	cutoff := now.Add(-MinimumHotRetention)
	policy := strings.Repeat("b", 64)
	base := RetentionRecord{
		Manifest:    retentionManifest("eligible.parquet"),
		FinalizedAt: cutoff.Add(-time.Hour), BackupVerified: true,
	}
	records := []RetentionRecord{base}
	for index, mutate := range []func(*RetentionRecord){
		func(value *RetentionRecord) { value.Referenced = true },
		func(value *RetentionRecord) { value.ActiveRun = true },
		func(value *RetentionRecord) { value.LockedTest = true },
		func(value *RetentionRecord) { value.IncidentHold = true },
		func(value *RetentionRecord) { value.OwnerOrLegalHold = true },
		func(value *RetentionRecord) { value.BackupVerified = false },
		func(value *RetentionRecord) { value.PendingDeletion = true },
	} {
		blocked := base
		blocked.Manifest.Path = "blocked-" + string(rune('a'+index)) + ".parquet"
		mutate(&blocked)
		records = append(records, blocked)
	}
	candidates, err := PlanDeletion(records, now, MinimumHotRetention, policy)
	if err != nil || len(candidates) != 1 || candidates[0].Path != "eligible.parquet" {
		t.Fatalf("candidates = %#v, %v", candidates, err)
	}
}

func TestRetentionRejectsWeakPolicyNonUTCTimeAndDuplicateManifest(t *testing.T) {
	policy := strings.Repeat("b", 64)
	if _, err := PlanDeletion(nil, time.Now(), MinimumHotRetention, policy); err == nil {
		t.Fatal("non-UTC policy time accepted")
	}
	now := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	if _, err := PlanDeletion(nil, now, MinimumHotRetention-time.Hour, policy); err == nil {
		t.Fatal("retention below thirty days accepted")
	}
	record := RetentionRecord{
		Manifest:    retentionManifest("same.parquet"),
		FinalizedAt: now.Add(-MinimumHotRetention - time.Hour), BackupVerified: true,
	}
	if _, err := PlanDeletion([]RetentionRecord{record, record}, now, MinimumHotRetention, policy); err == nil {
		t.Fatal("duplicate manifest accepted")
	}
	invalid := record
	invalid.Manifest.Compression = "gzip"
	if _, err := PlanDeletion([]RetentionRecord{invalid}, now, MinimumHotRetention, policy); err == nil {
		t.Fatal("non-Zstd manifest accepted for deletion")
	}
}

func retentionManifest(path string) Manifest {
	spec := segmentSpec()
	return Manifest{
		Spec: spec, Path: path, Checksum: strings.Repeat("a", 64),
		OrderedContentHash: spec.OrderedContentHash, Size: 128, Format: "parquet", Compression: "zstd",
	}
}
