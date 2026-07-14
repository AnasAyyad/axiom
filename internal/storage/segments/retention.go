package segments

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// MinimumHotRetention is the initial V1 raw-market-data policy floor.
const MinimumHotRetention = 30 * 24 * time.Hour

// RetentionRecord is the complete deny-by-default deletion evidence for a segment.
type RetentionRecord struct {
	Manifest         Manifest
	FinalizedAt      time.Time
	Referenced       bool
	ActiveRun        bool
	LockedTest       bool
	IncidentHold     bool
	OwnerOrLegalHold bool
	BackupVerified   bool
	PendingDeletion  bool
}

// DeletionCandidate is immutable policy evidence, not authority to delete a file.
type DeletionCandidate struct {
	Path       string
	Checksum   string
	Cutoff     time.Time
	Reason     string
	PolicyHash string
}

// PlanDeletion derives a UTC cutoff from the non-weakenable retention policy
// and returns only fully verified candidates.
func PlanDeletion(records []RetentionRecord, now time.Time, retention time.Duration, policyHash string) ([]DeletionCandidate, error) {
	if now.IsZero() || now.Location() != time.UTC || retention < MinimumHotRetention || !validHash(policyHash) {
		return nil, fmt.Errorf("retention_policy_invalid")
	}
	cutoff := now.Add(-retention)
	candidates := make([]DeletionCandidate, 0)
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		if err := validateRetentionRecord(record); err != nil {
			return nil, err
		}
		if _, duplicate := seen[record.Manifest.Path]; duplicate {
			return nil, fmt.Errorf("retention_duplicate_manifest")
		}
		seen[record.Manifest.Path] = struct{}{}
		if !record.FinalizedAt.Before(cutoff) || !record.BackupVerified ||
			record.Referenced || record.ActiveRun || record.LockedTest || record.IncidentHold ||
			record.OwnerOrLegalHold || record.PendingDeletion {
			continue
		}
		candidates = append(candidates, DeletionCandidate{
			Path: record.Manifest.Path, Checksum: record.Manifest.Checksum, Cutoff: cutoff,
			Reason: "expired_unreferenced_verified_backup", PolicyHash: policyHash,
		})
	}
	sort.Slice(candidates, func(left, right int) bool { return candidates[left].Path < candidates[right].Path })
	return candidates, nil
}

func validateRetentionRecord(record RetentionRecord) error {
	if validateSpec(record.Manifest.Spec) != nil || record.Manifest.Path == "" || !validHash(record.Manifest.Checksum) ||
		record.Manifest.Format != "parquet" || record.Manifest.Compression != "zstd" || record.Manifest.Size <= 0 ||
		record.Manifest.OrderedContentHash != record.Manifest.Spec.OrderedContentHash ||
		filepath.Base(record.Manifest.Path) != record.Manifest.Path ||
		record.FinalizedAt.IsZero() || record.FinalizedAt.Location() != time.UTC ||
		record.FinalizedAt.Before(record.Manifest.Spec.EndedAt) {
		return fmt.Errorf("retention_record_invalid")
	}
	return nil
}
