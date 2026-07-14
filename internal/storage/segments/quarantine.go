package segments

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// QuarantineInvalidProofs removes corrupt or mismatched recovery evidence and
// its associated file from service. Valid proof/file pairs remain recoverable.
func (finalizer *Finalizer) QuarantineInvalidProofs() ([]string, error) {
	proofs, err := filepath.Glob(filepath.Join(finalizer.root, "*.proof"))
	if err != nil {
		return nil, fmt.Errorf("segment_proof_scan_failed")
	}
	quarantine := filepath.Join(finalizer.root, "quarantine")
	moved := make([]string, 0)
	for _, proofPath := range proofs {
		if finalizer.proofIsValid(proofPath) {
			continue
		}
		if err = os.MkdirAll(quarantine, 0o750); err != nil {
			return moved, fmt.Errorf("segment_quarantine_unavailable")
		}
		name := strings.TrimSuffix(filepath.Base(proofPath), ".proof")
		for _, candidate := range []string{
			filepath.Join(finalizer.root, name+".parquet.partial"),
			filepath.Join(finalizer.root, name+".parquet"),
			proofPath,
		} {
			if _, statErr := os.Lstat(candidate); os.IsNotExist(statErr) {
				continue
			} else if statErr != nil {
				return moved, fmt.Errorf("segment_quarantine_failed")
			}
			destination, moveErr := moveToQuarantine(candidate, quarantine)
			if moveErr != nil {
				return moved, fmt.Errorf("segment_quarantine_failed")
			}
			moved = append(moved, destination)
		}
	}
	if len(moved) > 0 {
		if err = syncDirectory(quarantine); err != nil {
			return moved, err
		}
		if err = syncDirectory(finalizer.root); err != nil {
			return moved, err
		}
	}
	return moved, nil
}

func (finalizer *Finalizer) proofIsValid(proofPath string) bool {
	info, err := os.Lstat(proofPath)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	encoded, err := os.ReadFile(proofPath)
	if err != nil {
		return false
	}
	var value proof
	if json.Unmarshal(encoded, &value) != nil || validateSpec(value.Manifest.Spec) != nil {
		return false
	}
	partial, final, expectedProof := finalizer.paths(value.Manifest.Spec.Name)
	if proofPath != expectedProof {
		return false
	}
	path := final
	if _, err = os.Lstat(final); os.IsNotExist(err) {
		path = partial
	} else if err != nil {
		return false
	}
	actual, err := inspectFile(value.Manifest.Spec, path, filepath.Base(final))
	return err == nil && actual == value.Manifest
}

func moveToQuarantine(source, quarantine string) (string, error) {
	destination := filepath.Join(quarantine, filepath.Base(source)+".quarantined")
	if _, err := os.Lstat(destination); err == nil || !os.IsNotExist(err) {
		return "", fmt.Errorf("segment_quarantine_collision")
	}
	if err := os.Rename(source, destination); err != nil {
		return "", err
	}
	return destination, nil
}
