package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func writeSandboxCanaryEvidence(
	directory string,
	evidence *sandboxCanaryQualificationEvidence,
) (string, error) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		!filepath.IsAbs(directory) || info.Mode().Perm()&0o002 != 0 {
		return "", fmt.Errorf("sandbox_canary_evidence_directory_invalid")
	}
	encoded, err := encodeSandboxCanaryEvidence(evidence)
	if err != nil {
		return "", err
	}
	path := filepath.Join(
		filepath.Clean(directory),
		string(evidence.Exchange)+"-"+evidence.CanaryID+".json",
	)
	if err = persistSandboxCanaryEvidence(directory, path, encoded); err != nil {
		return "", err
	}
	return evidence.EvidenceID, nil
}

func encodeSandboxCanaryEvidence(
	evidence *sandboxCanaryQualificationEvidence,
) ([]byte, error) {
	canonical, err := json.Marshal(evidence)
	if err != nil {
		return nil, fmt.Errorf("sandbox_canary_evidence_encode_failed")
	}
	digest := sha256.Sum256(canonical)
	evidence.EvidenceID = hex.EncodeToString(digest[:])
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("sandbox_canary_evidence_encode_failed")
	}
	return append(encoded, '\n'), nil
}

func persistSandboxCanaryEvidence(
	directory, path string,
	encoded []byte,
) error {
	file, err := os.CreateTemp(filepath.Clean(directory), ".v1c-canary-")
	if err != nil {
		return fmt.Errorf("sandbox_canary_evidence_create_failed")
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err = file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("sandbox_canary_evidence_create_failed")
	}
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("sandbox_canary_evidence_write_failed")
	}
	if err = os.Chmod(temporaryPath, 0o440); err != nil {
		return fmt.Errorf("sandbox_canary_evidence_seal_failed")
	}
	if err = os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("sandbox_canary_evidence_create_failed")
	}
	directoryHandle, err := os.Open(filepath.Clean(directory))
	if err != nil {
		return fmt.Errorf("sandbox_canary_evidence_seal_failed")
	}
	err = directoryHandle.Sync()
	closeErr = directoryHandle.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("sandbox_canary_evidence_seal_failed")
	}
	return nil
}
