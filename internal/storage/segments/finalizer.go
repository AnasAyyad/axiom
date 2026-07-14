package segments

import (
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

var segmentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

// Stage identifies one injectable crash boundary.
type Stage string

// Finalization boundaries used by kill-point qualification.
const (
	StageCreated         Stage = "created"
	StageWritten         Stage = "written"
	StageSynced          Stage = "synced"
	StageProofSynced     Stage = "proof_synced"
	StageRenamed         Stage = "renamed"
	StageDirectorySynced Stage = "directory_synced"
	StageManifestReady   Stage = "manifest_ready"
)

// Spec fixes immutable segment identity and compatibility evidence.
type Spec struct {
	Name                 string
	SchemaVersion        string
	ParserVersion        string
	NormalizationVersion string
	OrderedContentHash   string
	FirstOrdinal         uint64
	LastOrdinal          uint64
	RecordCount          uint64
	StartedAt            time.Time
	EndedAt              time.Time
}

// Manifest is the verified ready-file metadata persisted transactionally.
type Manifest struct {
	Spec               Spec   `json:"spec"`
	Path               string `json:"path"`
	Checksum           string `json:"checksum"`
	OrderedContentHash string `json:"ordered_content_hash"`
	Size               int64  `json:"size"`
	Format             string `json:"format"`
	Compression        string `json:"compression"`
}

type proof struct {
	Manifest Manifest `json:"manifest"`
}

// Writer emits one complete internally Zstd-compressed Parquet segment and
// returns the SHA-256 hash of canonical rows in their encoded order.
type Writer func(io.Writer) (string, error)

// Committer durably and idempotently registers one verified ready manifest.
type Committer func(Manifest) error

// KillPoint injects termination immediately after a named boundary.
type KillPoint func(Stage) error

// Finalizer confines finalization to one caller-owned storage root.
type Finalizer struct {
	root string
	kill KillPoint
}

// NewFinalizer validates and fixes an absolute segment storage root.
func NewFinalizer(root string, kill KillPoint) (*Finalizer, error) {
	clean := filepath.Clean(root)
	if !filepath.IsAbs(clean) || clean == string(filepath.Separator) {
		return nil, fmt.Errorf("segment_root_invalid")
	}
	return &Finalizer{root: clean, kill: kill}, nil
}

// Finalize performs write, sync, proof, rename, parent sync, and manifest commit.
func (finalizer *Finalizer) Finalize(spec Spec, writer Writer, commit Committer) (Manifest, error) {
	if err := validateSpec(spec); err != nil || writer == nil || commit == nil {
		return Manifest{}, fmt.Errorf("segment_input_invalid")
	}
	if err := os.MkdirAll(finalizer.root, 0o750); err != nil {
		return Manifest{}, fmt.Errorf("segment_root_unavailable")
	}
	partial, final, proofPath := finalizer.paths(spec.Name)
	orderedHash, err := finalizer.writePartial(partial, writer)
	if err != nil {
		return Manifest{}, err
	}
	if orderedHash != spec.OrderedContentHash {
		return Manifest{}, fmt.Errorf("segment_ordered_content_mismatch")
	}
	manifest, err := inspectFile(spec, partial, filepath.Base(final))
	if err != nil {
		return Manifest{}, err
	}
	if err = finalizer.promote(partial, final, proofPath, manifest, commit); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (finalizer *Finalizer) writePartial(partial string, writer Writer) (string, error) {
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", fmt.Errorf("segment_partial_create_failed")
	}
	var orderedHash string
	if err = finalizer.after(StageCreated); err == nil {
		orderedHash, err = writer(file)
	}
	if err == nil && !validHash(orderedHash) {
		err = fmt.Errorf("segment_ordered_content_invalid")
	}
	if err == nil {
		err = finalizer.after(StageWritten)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err = finalizer.after(StageSynced); err != nil {
		return "", err
	}
	return orderedHash, nil
}

func (finalizer *Finalizer) promote(partial, final, proofPath string, manifest Manifest, commit Committer) error {
	var err error
	if err = writeProof(proofPath, proof{Manifest: manifest}); err != nil {
		return err
	}
	if err = finalizer.after(StageProofSynced); err != nil {
		return err
	}
	if err = os.Rename(partial, final); err != nil {
		return fmt.Errorf("segment_atomic_rename_failed")
	}
	if err = finalizer.after(StageRenamed); err != nil {
		return err
	}
	if err = syncDirectory(finalizer.root); err != nil {
		return err
	}
	if err = finalizer.after(StageDirectorySynced); err != nil {
		return err
	}
	if err = commit(manifest); err != nil {
		return fmt.Errorf("segment_manifest_commit_failed")
	}
	if err = finalizer.after(StageManifestReady); err != nil {
		return err
	}
	if err = os.Remove(proofPath); err != nil {
		return fmt.Errorf("segment_proof_cleanup_failed")
	}
	return syncDirectory(finalizer.root)
}

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

// QuarantineUnprovedPartials moves files without a valid proof out of service.
func (finalizer *Finalizer) QuarantineUnprovedPartials() ([]string, error) {
	partials, err := filepath.Glob(filepath.Join(finalizer.root, "*.partial"))
	if err != nil {
		return nil, fmt.Errorf("segment_partial_scan_failed")
	}
	quarantine := filepath.Join(finalizer.root, "quarantine")
	if len(partials) > 0 {
		if err = os.MkdirAll(quarantine, 0o750); err != nil {
			return nil, fmt.Errorf("segment_quarantine_unavailable")
		}
	}
	moved := make([]string, 0, len(partials))
	for _, partial := range partials {
		name := strings.TrimSuffix(filepath.Base(partial), ".parquet.partial")
		if _, statErr := os.Stat(filepath.Join(finalizer.root, name+".proof")); statErr == nil {
			continue
		}
		destination, moveErr := moveToQuarantine(partial, quarantine)
		if moveErr != nil {
			return moved, fmt.Errorf("segment_quarantine_failed")
		}
		moved = append(moved, destination)
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

func (finalizer *Finalizer) recoverProof(proofPath string, commit Committer) (Manifest, error) {
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
	if path == partial {
		if err = os.Rename(partial, final); err != nil {
			return Manifest{}, fmt.Errorf("segment_recovery_rename_failed")
		}
		if err = syncDirectory(finalizer.root); err != nil {
			return Manifest{}, err
		}
	}
	if err = commit(value.Manifest); err != nil {
		return Manifest{}, fmt.Errorf("segment_manifest_commit_failed")
	}
	if err = os.Remove(proofPath); err != nil {
		return Manifest{}, fmt.Errorf("segment_proof_cleanup_failed")
	}
	if err = syncDirectory(finalizer.root); err != nil {
		return Manifest{}, err
	}
	return value.Manifest, nil
}

func (finalizer *Finalizer) paths(name string) (string, string, string) {
	partial := filepath.Join(finalizer.root, name+".parquet.partial")
	final := filepath.Join(finalizer.root, name+".parquet")
	return partial, final, filepath.Join(finalizer.root, name+".proof")
}

func (finalizer *Finalizer) after(stage Stage) error {
	if finalizer.kill != nil {
		return finalizer.kill(stage)
	}
	return nil
}

func validateSpec(spec Spec) error {
	if !segmentNamePattern.MatchString(spec.Name) || spec.SchemaVersion == "" || spec.ParserVersion == "" ||
		spec.NormalizationVersion == "" || !validHash(spec.OrderedContentHash) ||
		spec.FirstOrdinal == 0 || spec.LastOrdinal < spec.FirstOrdinal ||
		spec.RecordCount == 0 || spec.StartedAt.IsZero() || spec.EndedAt.Before(spec.StartedAt) ||
		spec.StartedAt.Location() != time.UTC || spec.EndedAt.Location() != time.UTC {
		return fmt.Errorf("segment_spec_invalid")
	}
	return nil
}

func inspectFile(spec Spec, path, manifestPath string) (Manifest, error) {
	pathInfo, err := os.Lstat(path)
	if err != nil || !pathInfo.Mode().IsRegular() {
		return Manifest{}, fmt.Errorf("segment_inspection_failed")
	}
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("segment_inspection_failed")
	}
	defer file.Close()
	digest := sha256.New()
	info, err := file.Stat()
	if err != nil || info.Size() < 8 {
		return Manifest{}, fmt.Errorf("segment_parquet_invalid")
	}
	var header, footer [4]byte
	if _, err = file.ReadAt(header[:], 0); err != nil {
		return Manifest{}, fmt.Errorf("segment_parquet_invalid")
	}
	if _, err = file.ReadAt(footer[:], info.Size()-4); err != nil || string(header[:]) != "PAR1" || string(footer[:]) != "PAR1" {
		return Manifest{}, fmt.Errorf("segment_parquet_invalid")
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return Manifest{}, fmt.Errorf("segment_inspection_failed")
	}
	size, err := io.Copy(digest, file)
	if err != nil || size == 0 {
		return Manifest{}, fmt.Errorf("segment_inspection_failed")
	}
	hash := hex.EncodeToString(digest.Sum(nil))
	return Manifest{
		Spec: spec, Path: manifestPath, Checksum: hash, OrderedContentHash: spec.OrderedContentHash,
		Size: size, Format: "parquet", Compression: "zstd",
	}, nil
}

func validHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func writeProof(path string, value proof) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("segment_proof_encode_failed")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return fmt.Errorf("segment_proof_create_failed")
	}
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("segment_proof_write_failed")
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("segment_directory_unavailable")
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil {
		return fmt.Errorf("segment_directory_sync_failed")
	}
	return nil
}
