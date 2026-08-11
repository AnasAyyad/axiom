package recorder

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
)

// LatestManifest returns the highest verified manifest revision for one exact
// session. Absence is a normal new-session result.
func LatestManifest(root, sessionID string) (DatasetManifest, bool, error) {
	if !filepath.IsAbs(filepath.Clean(root)) || !identifierPattern.MatchString(sessionID) {
		return DatasetManifest{}, false, recorderError("resume_configuration_invalid")
	}
	paths, err := filepath.Glob(filepath.Join(root, sessionID+"-*.dataset.json"))
	if err != nil {
		return DatasetManifest{}, false, recorderError("resume_scan_failed")
	}
	sort.Strings(paths)
	var latest DatasetManifest
	for _, path := range paths {
		name := filepath.Base(path)
		if !strings.HasPrefix(name, sessionID+"-") {
			continue
		}
		manifest, readErr := ReadManifest(path)
		if readErr != nil || manifest.SessionID != sessionID ||
			(latest.Revision != 0 && manifest.Revision <= latest.Revision) {
			return DatasetManifest{}, false, recorderError("resume_manifest_invalid")
		}
		latest = manifest
	}
	if latest.Revision == 0 {
		return DatasetManifest{}, false, nil
	}
	if VerifyManifestChain(root, latest) != nil {
		return DatasetManifest{}, false, recorderError("resume_manifest_chain_invalid")
	}
	return latest, true, nil
}

// ResumeCoherentMarketData restores one V2 recorder after all existing files,
// hashes, links, and collector-profile facts verify.
func ResumeCoherentMarketData(root, datasetID, sessionID, exchange string,
	ordinals *runtimecore.IngestOrdinals, commit segments.Committer, kill segments.KillPoint,
	profile CollectorProfile) (*Recorder, DatasetManifest, error) {
	value, err := NewCoherentMarketData(root, datasetID, sessionID, exchange, ordinals, commit, kill, profile)
	if err != nil {
		return nil, DatasetManifest{}, err
	}
	manifest, found, err := LatestManifest(root, sessionID)
	if err != nil {
		return nil, DatasetManifest{}, err
	}
	if !found {
		return value, DatasetManifest{}, nil
	}
	if manifest.DatasetID != datasetID || manifest.Exchange != exchange || manifest.SchemaVersion != datasetSchemaVersionV2 ||
		len(manifest.ExchangeCoverage) != 1 || manifest.ExchangeCoverage[0].CollectorInstance != profile.Instance ||
		manifest.ExchangeCoverage[0].CollectorRegion != profile.Region || manifest.Compatibility == nil ||
		manifest.Compatibility.MinimumReaderVersion != profile.MinimumReaderVersion {
		return nil, DatasetManifest{}, recorderError("resume_manifest_identity_invalid")
	}
	if _, err = VerifyDataset(root, manifest); err != nil {
		return nil, DatasetManifest{}, recorderError("resume_dataset_invalid")
	}
	value.revision, value.previous, value.latest = manifest.Revision, manifest.Hash, cloneManifest(manifest)
	value.segments, value.gaps = cloneReferences(manifest.Segments), append([]Gap(nil), manifest.Gaps...)
	value.rawCount, value.canonicalCount = manifest.RawRecordCount, manifest.CanonicalCount
	for _, generation := range manifest.ExchangeCoverage[0].GenerationHistory {
		value.generationCoverage[generation.ConnectionGeneration] = generation
	}
	return value, manifest, nil
}

// ManifestLastOrdinal returns the durable high-water ordinal.
func ManifestLastOrdinal(manifest DatasetManifest) (uint64, error) {
	if len(manifest.Segments) == 0 {
		return 0, fmt.Errorf("recorder_manifest_empty")
	}
	var maximum uint64
	for _, reference := range manifest.Segments {
		if reference.Manifest.Spec.LastOrdinal > maximum {
			maximum = reference.Manifest.Spec.LastOrdinal
		}
	}
	if maximum == 0 {
		return 0, fmt.Errorf("recorder_manifest_ordinal_invalid")
	}
	return maximum, nil
}

// ManifestLastGeneration returns the highest recorded connection generation.
func ManifestLastGeneration(manifest DatasetManifest) uint64 {
	var maximum uint64
	for _, coverage := range manifest.ExchangeCoverage {
		for _, generation := range coverage.GenerationHistory {
			if generation.ConnectionGeneration > maximum {
				maximum = generation.ConnectionGeneration
			}
		}
	}
	return maximum
}
