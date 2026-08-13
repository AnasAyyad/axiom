package backup

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RestoreEvidence is an authenticated, no-replace clean-restore verdict.
type RestoreEvidence struct {
	SchemaVersion          string    `json:"schema_version"`
	ArtifactName           string    `json:"artifact_name"`
	ArtifactSHA256         string    `json:"artifact_sha256"`
	DatabaseSchemaVersion  string    `json:"database_schema_version"`
	StartedAt              time.Time `json:"started_at"`
	CompletedAt            time.Time `json:"completed_at"`
	DurationSeconds        uint64    `json:"duration_seconds"`
	CleanTarget            bool      `json:"clean_target"`
	AccountingVerified     bool      `json:"accounting_verified"`
	MarketDataVerified     bool      `json:"market_data_verified"`
	MarketSegmentsVerified uint64    `json:"market_segments_verified"`
	MarketInventoryHash    string    `json:"market_inventory_hash"`
	EvidenceHash           string    `json:"evidence_hash"`
	AuthenticationTag      string    `json:"authentication_tag"`
}

// WriteRestoreEvidence records a successful restore only after all database
// integrity checks pass. The existing path can never be replaced.
func WriteRestoreEvidence(root string, manifest ArtifactManifest, started, completed time.Time,
	market MarketRecoveryEvidence, key [32]byte) (string, RestoreEvidence, error) {
	if !validBackupRoot(root) || started.IsZero() || completed.Before(started) ||
		started.Location() != time.UTC || completed.Location() != time.UTC || !verifyManifest(manifest, key) ||
		!validHash(market.InventoryHash) {
		return "", RestoreEvidence{}, fmt.Errorf("restore_evidence_invalid")
	}
	evidence := RestoreEvidence{SchemaVersion: "axiom.backup.restore-evidence.v1",
		ArtifactName: manifest.Spec.Name, ArtifactSHA256: manifest.SHA256,
		DatabaseSchemaVersion: manifest.Spec.SchemaVersion, StartedAt: started, CompletedAt: completed,
		DurationSeconds: uint64(completed.Sub(started).Truncate(time.Second).Seconds()),
		CleanTarget:     true, AccountingVerified: true, MarketDataVerified: market.VerifiedSegments > 0,
		MarketSegmentsVerified: market.VerifiedSegments, MarketInventoryHash: market.InventoryHash}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return "", RestoreEvidence{}, err
	}
	digest := sha256.Sum256(payload)
	evidence.EvidenceHash = hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(digest[:])
	evidence.AuthenticationTag = hex.EncodeToString(mac.Sum(nil))
	payload, err = json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return "", RestoreEvidence{}, err
	}
	payload = append(payload, '\n')
	name := "restore-" + completed.Format("20060102T150405.000000000Z") + ".evidence.json"
	path := filepath.Join(root, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o440)
	if err != nil {
		return "", RestoreEvidence{}, fmt.Errorf("restore_evidence_exists_or_unwritable")
	}
	if _, err = file.Write(payload); err != nil {
		_ = file.Close()
		return "", RestoreEvidence{}, err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return "", RestoreEvidence{}, err
	}
	if err = file.Close(); err != nil {
		return "", RestoreEvidence{}, err
	}
	if err = syncBackupDirectory(root); err != nil {
		return "", RestoreEvidence{}, err
	}
	return path, evidence, nil
}

// VerifyRestoreEvidence authenticates a persisted restore verdict.
func VerifyRestoreEvidence(evidence RestoreEvidence, key [32]byte) bool {
	if !validHash(evidence.EvidenceHash) || !validHash(evidence.AuthenticationTag) {
		return false
	}
	tag := evidence.AuthenticationTag
	hash := evidence.EvidenceHash
	evidence.AuthenticationTag, evidence.EvidenceHash = "", ""
	payload, err := json.Marshal(evidence)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(payload)
	if !hmac.Equal([]byte(hash), []byte(hex.EncodeToString(digest[:]))) {
		return false
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(digest[:])
	return hmac.Equal([]byte(tag), []byte(hex.EncodeToString(mac.Sum(nil))))
}

func validHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
