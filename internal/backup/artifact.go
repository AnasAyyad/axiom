package backup

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var artifactNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// ArtifactSpec fixes non-secret restore-point identity.
type ArtifactSpec struct {
	Name             string    `json:"name"`
	Database         string    `json:"database"`
	SchemaVersion    string    `json:"schema_version"`
	ToolVersion      string    `json:"tool_version"`
	ValidatorVersion string    `json:"validator_version"`
	WALBoundary      string    `json:"wal_boundary"`
	StartedAt        time.Time `json:"started_at"`
}

// ArtifactManifest authenticates the finalized encrypted object inventory.
type ArtifactManifest struct {
	Spec        ArtifactSpec `json:"spec"`
	Path        string       `json:"path"`
	SHA256      string       `json:"sha256"`
	Size        int64        `json:"size"`
	Encryption  string       `json:"encryption"`
	CompletedAt time.Time    `json:"completed_at"`
	ManifestMAC string       `json:"manifest_mac"`
}

// CreateArtifact encrypts, syncs, atomically renames, and manifests one dump.
func CreateArtifact(root string, spec ArtifactSpec, source io.Reader, key [32]byte) (ArtifactManifest, error) {
	clean, err := validateArtifactInput(root, spec)
	if err != nil || source == nil {
		return ArtifactManifest{}, fmt.Errorf("backup_artifact_invalid")
	}
	if err = os.MkdirAll(clean, 0o750); err != nil {
		return ArtifactManifest{}, fmt.Errorf("backup_destination_unavailable")
	}
	partial := filepath.Join(clean, spec.Name+".dump.aesgcm.partial")
	final := filepath.Join(clean, spec.Name+".dump.aesgcm")
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return ArtifactManifest{}, fmt.Errorf("backup_partial_create_failed")
	}
	writeErr := Encrypt(file, source, key)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return ArtifactManifest{}, writeErr
	}
	if err = os.Rename(partial, final); err != nil {
		return ArtifactManifest{}, fmt.Errorf("backup_atomic_rename_failed")
	}
	if err = syncBackupDirectory(clean); err != nil {
		return ArtifactManifest{}, err
	}
	digest, size, err := backupDigest(final)
	if err != nil {
		return ArtifactManifest{}, err
	}
	manifest := ArtifactManifest{
		Spec: spec, Path: filepath.Base(final), SHA256: digest, Size: size,
		Encryption: "AES-256-GCM-framed-v1", CompletedAt: time.Now().UTC(),
	}
	manifest.ManifestMAC = signManifest(manifest, key)
	if err = writeArtifactManifest(clean, manifest); err != nil {
		return ArtifactManifest{}, err
	}
	return manifest, nil
}

// RestoreArtifact verifies inventory and authentication before yielding bytes.
func RestoreArtifact(root string, manifest ArtifactManifest, destination io.Writer, key [32]byte) error {
	clean, err := validateArtifactManifest(root, manifest, key)
	if err != nil || destination == nil {
		return fmt.Errorf("restore_artifact_invalid")
	}
	path := filepath.Join(clean, manifest.Path)
	digest, size, err := backupDigest(path)
	if err != nil || digest != manifest.SHA256 || size != manifest.Size {
		return fmt.Errorf("restore_artifact_checksum_failed")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("restore_artifact_unavailable")
	}
	defer file.Close()
	return Decrypt(destination, file, key)
}

// QuarantineArtifact removes a fully authenticated but application-invalid
// object from the completed restore inventory without deleting its evidence.
func QuarantineArtifact(root string, manifest ArtifactManifest, key [32]byte) error {
	clean, err := validateArtifactManifest(root, manifest, key)
	if err != nil || manifest.Path != manifest.Spec.Name+".dump.aesgcm" {
		return fmt.Errorf("backup_quarantine_invalid")
	}
	manifestPath := filepath.Join(clean, manifest.Spec.Name+".manifest.json")
	loaded, err := ReadArtifactManifest(manifestPath)
	if err != nil || loaded != manifest {
		return fmt.Errorf("backup_quarantine_invalid")
	}
	if err = RestoreArtifact(clean, manifest, io.Discard, key); err != nil {
		return fmt.Errorf("backup_quarantine_invalid")
	}
	manifestQuarantine := manifestPath + ".invalid"
	if err = os.Rename(manifestPath, manifestQuarantine); err != nil {
		return fmt.Errorf("backup_quarantine_manifest_failed")
	}
	if err = syncBackupDirectory(clean); err != nil {
		return err
	}
	artifactPath := filepath.Join(clean, manifest.Path)
	if err = os.Rename(artifactPath, artifactPath+".invalid"); err != nil {
		return fmt.Errorf("backup_quarantine_artifact_failed")
	}
	return syncBackupDirectory(clean)
}

func signManifest(manifest ArtifactManifest, key [32]byte) string {
	manifest.ManifestMAC = ""
	encoded, _ := json.Marshal(manifest)
	digest := hmac.New(sha256.New, key[:])
	_, _ = digest.Write([]byte("axiom-backup-manifest-v1\x00"))
	_, _ = digest.Write(encoded)
	return hex.EncodeToString(digest.Sum(nil))
}

func verifyManifest(manifest ArtifactManifest, key [32]byte) bool {
	provided, err := hex.DecodeString(manifest.ManifestMAC)
	if err != nil || len(provided) != sha256.Size {
		return false
	}
	expected, _ := hex.DecodeString(signManifest(manifest, key))
	return hmac.Equal(provided, expected)
}

// ReadArtifactManifest loads one confined non-secret inventory document.
func ReadArtifactManifest(path string) (ArtifactManifest, error) {
	return readArtifactManifest(path, false)
}

func readArtifactManifest(path string, deleting bool) (ArtifactManifest, error) {
	expectedSuffix := ".manifest.json"
	if deleting {
		expectedSuffix += ".deleting"
	}
	if !filepath.IsAbs(path) || !strings.HasSuffix(filepath.Base(path), expectedSuffix) {
		return ArtifactManifest{}, fmt.Errorf("backup_manifest_path_invalid")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ArtifactManifest{}, fmt.Errorf("backup_manifest_unavailable")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return ArtifactManifest{}, fmt.Errorf("backup_manifest_unavailable")
	}
	var manifest ArtifactManifest
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ArtifactManifest{}, fmt.Errorf("backup_manifest_invalid")
	}
	return manifest, nil
}

func validateArtifactManifest(root string, manifest ArtifactManifest, key [32]byte) (string, error) {
	clean, err := validateArtifactInput(root, manifest.Spec)
	if err != nil || filepath.Base(manifest.Path) != manifest.Path ||
		manifest.Encryption != "AES-256-GCM-framed-v1" || len(manifest.SHA256) != 64 || manifest.Size <= 0 ||
		manifest.CompletedAt.IsZero() || manifest.CompletedAt.Location() != time.UTC ||
		manifest.CompletedAt.Before(manifest.Spec.StartedAt) || !verifyManifest(manifest, key) {
		return "", fmt.Errorf("backup_manifest_invalid")
	}
	return clean, nil
}

func validateArtifactInput(root string, spec ArtifactSpec) (string, error) {
	clean := filepath.Clean(root)
	if !validBackupRoot(clean) || !artifactNamePattern.MatchString(spec.Name) ||
		!validArtifactMetadata(spec.Database, 63) || !validArtifactMetadata(spec.SchemaVersion, 128) ||
		!validArtifactMetadata(spec.ToolVersion, 128) || !validArtifactMetadata(spec.ValidatorVersion, 128) ||
		!validArtifactMetadata(spec.WALBoundary, 32) ||
		spec.StartedAt.IsZero() || spec.StartedAt.Location() != time.UTC || spec.StartedAt.After(time.Now().UTC()) {
		return "", fmt.Errorf("backup_artifact_invalid")
	}
	return clean, nil
}

func validArtifactMetadata(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func writeArtifactManifest(root string, manifest ArtifactManifest) error {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("backup_manifest_invalid")
	}
	partial := filepath.Join(root, manifest.Spec.Name+".manifest.json.partial")
	final := filepath.Join(root, manifest.Spec.Name+".manifest.json")
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("backup_manifest_create_failed")
	}
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("backup_manifest_write_failed")
	}
	if err = os.Rename(partial, final); err != nil {
		return fmt.Errorf("backup_manifest_rename_failed")
	}
	return syncBackupDirectory(root)
}

func backupDigest(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("backup_artifact_unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("backup_artifact_unavailable")
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil {
		return "", 0, fmt.Errorf("backup_artifact_read_failed")
	}
	return hex.EncodeToString(digest.Sum(nil)), size, nil
}

func syncBackupDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("backup_destination_unavailable")
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil {
		return fmt.Errorf("backup_destination_sync_failed")
	}
	return nil
}
