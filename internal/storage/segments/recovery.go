package segments

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Recover verifies proofs, finalizes provable partials, and recommits manifests.
func (finalizer *Finalizer) Recover(commit Committer) ([]Manifest, error) {
	if commit == nil {
		return nil, fmt.Errorf("segment_commit_missing")
	}
	proofs, err := filepath.Glob(filepath.Join(finalizer.root, "*.proof"))
	if err != nil {
		return nil, fmt.Errorf("segment_recovery_scan_failed")
	}
	result := make([]Manifest, 0, len(proofs))
	for _, proofPath := range proofs {
		manifest, recoverErr := finalizer.recoverProof(proofPath, commit)
		if recoverErr != nil {
			return result, recoverErr
		}
		result = append(result, manifest)
	}
	return result, nil
}

// RecoverPrefix recovers only proof-backed segments whose names begin with a
// caller-owned safe prefix. This lets independent durable workers share one
// storage root without adopting or mutating another recorder's artifacts.
func (finalizer *Finalizer) RecoverPrefix(prefix string, commit Committer) ([]Manifest, error) {
	if commit == nil || prefix == "" || len(prefix) > 96 || !segmentNamePattern.MatchString(prefix+"x") {
		return nil, fmt.Errorf("segment_recovery_prefix_invalid")
	}
	proofs, err := filepath.Glob(filepath.Join(finalizer.root, prefix+"*.proof"))
	if err != nil {
		return nil, fmt.Errorf("segment_recovery_scan_failed")
	}
	result := make([]Manifest, 0, len(proofs))
	for _, proofPath := range proofs {
		manifest, recoverErr := finalizer.recoverProof(proofPath, commit)
		if recoverErr != nil {
			return result, recoverErr
		}
		if !strings.HasPrefix(manifest.Spec.Name, prefix) {
			return result, fmt.Errorf("segment_recovery_prefix_mismatch")
		}
		result = append(result, manifest)
	}
	return result, nil
}

// InspectProof returns the verified immutable manifest for one proof-backed
// final or partial file without mutating either artifact.
func (finalizer *Finalizer) InspectProof(name string) (Manifest, bool, error) {
	if !segmentNamePattern.MatchString(name) {
		return Manifest{}, false, fmt.Errorf("segment_proof_name_invalid")
	}
	_, _, proofPath := finalizer.paths(name)
	if _, err := os.Lstat(proofPath); os.IsNotExist(err) {
		return Manifest{}, false, nil
	} else if err != nil {
		return Manifest{}, false, fmt.Errorf("segment_proof_unavailable")
	}
	manifest, err := finalizer.inspectProof(proofPath)
	return manifest, true, err
}

func (finalizer *Finalizer) recoverProof(proofPath string, commit Committer) (Manifest, error) {
	manifest, err := finalizer.inspectProof(proofPath)
	if err != nil {
		return Manifest{}, err
	}
	partial, final, _ := finalizer.paths(manifest.Spec.Name)
	path := final
	if _, err = os.Stat(final); os.IsNotExist(err) {
		path = partial
	}
	if path == partial {
		if err = os.Rename(partial, final); err != nil {
			return Manifest{}, fmt.Errorf("segment_recovery_rename_failed")
		}
		if err = syncDirectory(finalizer.root); err != nil {
			return Manifest{}, err
		}
	}
	if err = commit(manifest); err != nil {
		return Manifest{}, fmt.Errorf("segment_manifest_commit_failed")
	}
	if err = os.Remove(proofPath); err != nil {
		return Manifest{}, fmt.Errorf("segment_proof_cleanup_failed")
	}
	if err = syncDirectory(finalizer.root); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (finalizer *Finalizer) inspectProof(proofPath string) (Manifest, error) {
	info, err := os.Lstat(proofPath)
	if err != nil || !info.Mode().IsRegular() {
		return Manifest{}, fmt.Errorf("segment_proof_unavailable")
	}
	encoded, err := os.ReadFile(proofPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("segment_proof_unavailable")
	}
	var value proof
	if err = json.Unmarshal(encoded, &value); err != nil || validateSpec(value.Manifest.Spec) != nil {
		return Manifest{}, fmt.Errorf("segment_proof_invalid")
	}
	partial, final, expectedProof := finalizer.paths(value.Manifest.Spec.Name)
	if expectedProof != proofPath {
		return Manifest{}, fmt.Errorf("segment_proof_path_invalid")
	}
	path := final
	if _, err = os.Stat(final); os.IsNotExist(err) {
		path = partial
	}
	actual, err := inspectFile(value.Manifest.Spec, path, filepath.Base(final))
	if err != nil || actual != value.Manifest {
		return Manifest{}, fmt.Errorf("segment_proof_mismatch")
	}
	return value.Manifest, nil
}
