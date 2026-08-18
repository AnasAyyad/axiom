package recorder

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"axiom/internal/storage/segments"
)

// ArtifactRecovery is the bounded startup evidence for files excluded from
// the last complete cumulative dataset manifest.
type ArtifactRecovery struct {
	SegmentNames []string
	Proven       []segments.Manifest
	Moved        []string
}

// ArtifactQuarantine persists database-side quarantine before filesystem
// evidence is moved out of the live recorder root.
type ArtifactQuarantine func(names []string, proven []segments.Manifest) error

type recoveryArtifactPlan struct {
	paths []string
	names map[string]struct{}
}

// QuarantineUncommittedArtifacts reconciles the live recorder root against
// the last complete manifest. It preserves every uncommitted file under the
// root quarantine directory and never mutates committed segment files.
func QuarantineUncommittedArtifacts(root, sessionID string, committed DatasetManifest,
	quarantine ArtifactQuarantine) (ArtifactRecovery, error) {
	clean := filepath.Clean(root)
	if err := validateRecoveryConfiguration(clean, sessionID, committed); err != nil {
		return ArtifactRecovery{}, err
	}
	plan, err := planRecoveryArtifacts(clean, sessionID, committed)
	if err != nil {
		return ArtifactRecovery{}, err
	}
	if len(plan.paths) == 0 {
		return ArtifactRecovery{}, nil
	}
	result := ArtifactRecovery{SegmentNames: sortedRecoveryNames(plan.names)}
	result.Proven, err = inspectRecoveryProofs(clean, result.SegmentNames)
	if err != nil {
		return ArtifactRecovery{}, err
	}
	if len(result.SegmentNames) > 0 {
		if quarantine == nil {
			return ArtifactRecovery{}, recorderError("recovery_quarantine_unavailable")
		}
		if err = quarantine(result.SegmentNames, result.Proven); err != nil {
			return ArtifactRecovery{}, recorderError("recovery_quarantine_persist_failed")
		}
	}
	result.Moved, err = moveRecoveryArtifacts(clean, plan.paths)
	return result, err
}

func validateRecoveryConfiguration(root, sessionID string, committed DatasetManifest) error {
	if !filepath.IsAbs(root) || root == string(filepath.Separator) || !identifierPattern.MatchString(sessionID) {
		return recorderError("recovery_configuration_invalid")
	}
	if committed.Revision > 0 && (committed.SessionID != sessionID || VerifyManifestChain(root, committed) != nil) {
		return recorderError("recovery_manifest_invalid")
	}
	return nil
}

func planRecoveryArtifacts(root, sessionID string, committed DatasetManifest) (recoveryArtifactPlan, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return recoveryArtifactPlan{}, recorderIOError("recovery_scan_failed", "recovery_scan", err)
	}
	referenced := make(map[string]struct{}, len(committed.Segments))
	for _, reference := range committed.Segments {
		referenced[reference.Manifest.Spec.Name] = struct{}{}
	}
	plan := recoveryArtifactPlan{names: make(map[string]struct{})}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		segmentName, segmentArtifact := recoverySegmentName(name, sessionID)
		manifestPartial := strings.HasPrefix(name, sessionID+"-") && strings.HasSuffix(name, ".dataset.json.partial")
		if !segmentArtifact && !manifestPartial {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return recoveryArtifactPlan{}, recorderError("recovery_artifact_unsafe")
		}
		if segmentArtifact {
			if _, committedSegment := referenced[segmentName]; committedSegment && strings.HasSuffix(name, ".parquet") {
				continue
			}
			if _, committedSegment := referenced[segmentName]; !committedSegment {
				plan.names[segmentName] = struct{}{}
			}
		}
		plan.paths = append(plan.paths, filepath.Join(root, name))
	}
	return plan, nil
}

func inspectRecoveryProofs(root string, names []string) ([]segments.Manifest, error) {
	finalizer, err := segments.NewFinalizer(root, nil)
	if err != nil {
		return nil, recorderError("recovery_finalizer_invalid")
	}
	result := make([]segments.Manifest, 0, len(names))
	for _, name := range names {
		manifest, found, inspectErr := finalizer.InspectProof(name)
		if inspectErr != nil {
			continue
		}
		if found {
			result = append(result, manifest)
		}
	}
	return result, nil
}

func moveRecoveryArtifacts(root string, paths []string) ([]string, error) {
	sort.Strings(paths)
	quarantineRoot := filepath.Join(root, "quarantine")
	if err := os.MkdirAll(quarantineRoot, 0o750); err != nil {
		return nil, recorderIOError("recovery_quarantine_failed", "recovery_quarantine_create", err)
	}
	moved := make([]string, 0, len(paths))
	for _, source := range paths {
		destination, moveErr := recorderQuarantineDestination(quarantineRoot, filepath.Base(source))
		if moveErr != nil {
			return moved, moveErr
		}
		if moveErr = os.Rename(source, destination); moveErr != nil {
			return moved, recorderIOError("recovery_quarantine_failed", "recovery_quarantine_move", moveErr)
		}
		moved = append(moved, filepath.Base(destination))
	}
	if err := syncRecorderDirectory(quarantineRoot); err != nil {
		return moved, err
	}
	if err := syncRecorderDirectory(root); err != nil {
		return moved, err
	}
	return moved, nil
}

func recoverySegmentName(name, sessionID string) (string, bool) {
	if !strings.HasPrefix(name, sessionID+"-") {
		return "", false
	}
	for _, suffix := range []string{".parquet.partial", ".parquet", ".proof"} {
		if strings.HasSuffix(name, suffix) {
			segmentName := strings.TrimSuffix(name, suffix)
			return segmentName, identifierPattern.MatchString(segmentName)
		}
	}
	return "", false
}

func sortedRecoveryNames(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func recorderQuarantineDestination(root, name string) (string, error) {
	for ordinal := 0; ordinal < 10_000; ordinal++ {
		suffix := ".quarantined"
		if ordinal > 0 {
			suffix = fmt.Sprintf(".quarantined.%04d", ordinal)
		}
		candidate := filepath.Join(root, name+suffix)
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", recorderIOError("recovery_quarantine_failed", "recovery_quarantine_inspect", err)
		}
	}
	return "", recorderError("recovery_quarantine_exhausted")
}
