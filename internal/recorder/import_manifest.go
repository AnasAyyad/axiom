package recorder

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteImportedDatasetManifest creates one immutable revision-1 dataset from
// already finalized raw/canonical segment pairs. It is used by the official
// historical importer after every page artifact has been verified.
func WriteImportedDatasetManifest(root, datasetID, sessionID, exchange string,
	references []SegmentReference, createdAt time.Time) (DatasetManifest, error) {
	if !filepath.IsAbs(filepath.Clean(root)) || filepath.Clean(root) == string(filepath.Separator) ||
		len(references) == 0 || len(references)%2 != 0 || createdAt.IsZero() || createdAt.Location() != time.UTC {
		return DatasetManifest{}, recorderError("import_manifest_invalid")
	}
	if err := validateImportedReferences(references); err != nil {
		return DatasetManifest{}, err
	}
	count := uint64(len(references) / 2)
	manifest := DatasetManifest{SchemaVersion: datasetSchemaVersion, DatasetID: datasetID,
		SessionID: sessionID, Exchange: exchange, Revision: 1, CreatedAt: createdAt,
		Segments: cloneReferences(references), RawRecordCount: count, CanonicalCount: count, Complete: true}
	manifest.Hash = manifestHash(manifest)
	if validateManifest(manifest) != nil {
		return DatasetManifest{}, recorderError("import_manifest_invalid")
	}
	if _, err := VerifyDataset(root, manifest); err != nil {
		return DatasetManifest{}, recorderError("import_manifest_dataset_invalid")
	}
	return commitImportedManifest(root, manifest)
}

func validateImportedReferences(references []SegmentReference) error {
	var previous uint64
	for index := 0; index < len(references); index += 2 {
		wire, canonical := references[index], references[index+1]
		if wire.Kind != "wire" || canonical.Kind != "canonical" ||
			wire.Manifest.Spec.FirstOrdinal != wire.Manifest.Spec.LastOrdinal ||
			canonical.Manifest.Spec.FirstOrdinal != canonical.Manifest.Spec.LastOrdinal ||
			wire.Manifest.Spec.FirstOrdinal != canonical.Manifest.Spec.FirstOrdinal ||
			wire.Manifest.Spec.FirstOrdinal <= previous {
			return recorderError("import_manifest_segments_invalid")
		}
		previous = wire.Manifest.Spec.FirstOrdinal
	}
	return nil
}

func commitImportedManifest(root string, manifest DatasetManifest) (DatasetManifest, error) {
	name := fmt.Sprintf("%s-%06d.dataset.json", manifest.SessionID, manifest.Revision)
	final, partial := filepath.Join(root, name), filepath.Join(root, name+".partial")
	if existing, ok, err := readMatchingImportedManifest(final, manifest); err != nil || ok {
		return existing, err
	}
	if existing, ok, err := readMatchingImportedManifest(partial, manifest); err != nil {
		return DatasetManifest{}, err
	} else if ok {
		if err = os.Rename(partial, final); err != nil {
			return DatasetManifest{}, recorderIOError("manifest_rename_failed", "manifest_rename", err)
		}
		if err = syncRecorderDirectory(root); err != nil {
			return DatasetManifest{}, err
		}
		return existing, nil
	}
	if err := writeManifest(root, manifest); err != nil {
		return DatasetManifest{}, err
	}
	stored, ok, err := readMatchingImportedManifest(final, manifest)
	if err != nil || !ok {
		return DatasetManifest{}, recorderError("import_manifest_commit_invalid")
	}
	return stored, nil
}

func readMatchingImportedManifest(path string, wanted DatasetManifest) (DatasetManifest, bool, error) {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return DatasetManifest{}, false, nil
	} else if err != nil {
		return DatasetManifest{}, false, recorderIOError("manifest_read_failed", "manifest_read", err)
	}
	value, err := ReadManifest(path)
	if err != nil || value.Hash != wanted.Hash || !sameDatasetIdentity(value, wanted) ||
		value.Revision != wanted.Revision {
		return DatasetManifest{}, false, recorderError("import_manifest_immutable_conflict")
	}
	return value, true, nil
}

func syncRecorderDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return recorderIOError("manifest_sync_failed", "manifest_directory_open", err)
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return recorderIOError("manifest_sync_failed", "manifest_directory_sync", err)
	}
	return nil
}
