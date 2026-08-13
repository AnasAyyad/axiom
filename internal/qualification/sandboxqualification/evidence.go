package sandboxQualification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func hashEvidence(evidence Evidence) (string, error) {
	copy := evidence
	copy.EvidenceHash = ""
	payload, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func hashValues(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

// WriteEvidenceNoReplace writes one redacted terminal object with O_EXCL,
// read-only permissions, and file/directory fsync. Existing evidence is never
// overwritten.
func WriteEvidenceNoReplace(path string, evidence Evidence) error {
	if !filepath.IsAbs(path) || evidence.EvidenceHash == "" ||
		evidence.ProfitabilityEvidence {
		return fmt.Errorf("sandbox_qualification_evidence_path_rejected")
	}
	payload, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	file, err := os.OpenFile(
		path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o440,
	)
	if err != nil {
		return err
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if _, err = file.Write(payload); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil {
		return err
	}
	remove = false
	return nil
}
