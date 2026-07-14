package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MinimumRetainedGenerations is the V1 disaster-recovery floor. Callers may
// retain more generations, but may not weaken this minimum.
const MinimumRetainedGenerations = 14

type artifactGeneration struct {
	path     string
	manifest ArtifactManifest
}

// PruneArtifacts verifies every completed restore point before removing the
// oldest generations. A malformed or unauthenticated inventory fails the whole
// operation closed; partial artifacts are never counted as restore points.
func PruneArtifacts(root string, key [32]byte, retain int) ([]string, error) {
	clean := filepath.Clean(root)
	if !validBackupRoot(clean) || retain < MinimumRetainedGenerations {
		return nil, fmt.Errorf("backup_retention_invalid")
	}
	if err := resumeArtifactDeletions(clean, key); err != nil {
		return nil, err
	}
	generations, err := verifiedArtifactGenerations(clean, key)
	if err != nil {
		return nil, err
	}
	if len(generations) <= retain {
		return nil, nil
	}
	sort.Slice(generations, func(left, right int) bool {
		leftTime := generations[left].manifest.Spec.StartedAt
		rightTime := generations[right].manifest.Spec.StartedAt
		if leftTime.Equal(rightTime) {
			return generations[left].manifest.Spec.Name < generations[right].manifest.Spec.Name
		}
		return leftTime.Before(rightTime)
	})
	removed := make([]string, 0, len(generations)-retain)
	for _, generation := range generations[:len(generations)-retain] {
		if err := deleteArtifactGeneration(clean, generation.path, generation.manifest); err != nil {
			return removed, err
		}
		removed = append(removed, generation.manifest.Spec.Name)
	}
	return removed, nil
}

func verifiedArtifactGenerations(root string, key [32]byte) ([]artifactGeneration, error) {
	paths, err := filepath.Glob(filepath.Join(root, "*.manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("backup_retention_scan_failed")
	}
	generations := make([]artifactGeneration, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		manifest, readErr := readArtifactManifest(path, false)
		if readErr != nil || filepath.Base(path) != manifest.Spec.Name+".manifest.json" ||
			manifest.Path != manifest.Spec.Name+".dump.aesgcm" {
			return nil, fmt.Errorf("backup_retention_inventory_invalid")
		}
		if _, duplicate := seen[manifest.Spec.Name]; duplicate {
			return nil, fmt.Errorf("backup_retention_inventory_invalid")
		}
		seen[manifest.Spec.Name] = struct{}{}
		if restoreErr := RestoreArtifact(root, manifest, io.Discard, key); restoreErr != nil {
			return nil, fmt.Errorf("backup_retention_inventory_invalid")
		}
		generations = append(generations, artifactGeneration{path: path, manifest: manifest})
	}
	return generations, nil
}

func resumeArtifactDeletions(root string, key [32]byte) error {
	paths, err := filepath.Glob(filepath.Join(root, "*.manifest.json.deleting"))
	if err != nil {
		return fmt.Errorf("backup_retention_scan_failed")
	}
	for _, path := range paths {
		manifest, readErr := readArtifactManifest(path, true)
		if readErr != nil || filepath.Base(path) != manifest.Spec.Name+".manifest.json.deleting" ||
			manifest.Path != manifest.Spec.Name+".dump.aesgcm" {
			return fmt.Errorf("backup_retention_recovery_invalid")
		}
		if _, validateErr := validateArtifactManifest(root, manifest, key); validateErr != nil {
			return fmt.Errorf("backup_retention_recovery_invalid")
		}
		artifact := filepath.Join(root, manifest.Path)
		if _, statErr := os.Stat(artifact); statErr == nil {
			if restoreErr := RestoreArtifact(root, manifest, io.Discard, key); restoreErr != nil {
				return fmt.Errorf("backup_retention_recovery_invalid")
			}
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("backup_retention_recovery_failed")
		}
		if err = finishArtifactDeletion(root, artifact, path); err != nil {
			return err
		}
	}
	return nil
}

func deleteArtifactGeneration(root, manifestPath string, manifest ArtifactManifest) error {
	tombstone := manifestPath + ".deleting"
	if err := os.Rename(manifestPath, tombstone); err != nil {
		return fmt.Errorf("backup_retention_mark_failed")
	}
	if err := syncBackupDirectory(root); err != nil {
		return err
	}
	return finishArtifactDeletion(root, filepath.Join(root, manifest.Path), tombstone)
}

func finishArtifactDeletion(root, artifact, tombstone string) error {
	if err := os.Remove(artifact); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("backup_retention_artifact_delete_failed")
	}
	if err := os.Remove(tombstone); err != nil {
		return fmt.Errorf("backup_retention_manifest_delete_failed")
	}
	return syncBackupDirectory(root)
}

func validBackupRoot(path string) bool {
	return filepath.IsAbs(path) && path != string(filepath.Separator) &&
		!strings.ContainsRune(path, '\x00')
}
