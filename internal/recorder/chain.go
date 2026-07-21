package recorder

import (
	"fmt"
	"path/filepath"
)

// VerifyManifestChain validates every cumulative revision through selected and
// rejects missing, forked, regressed, or non-cumulative history.
func VerifyManifestChain(root string, selected DatasetManifest) error {
	if err := validateManifest(selected); err != nil {
		return err
	}
	var previous DatasetManifest
	for revision := uint64(1); revision <= selected.Revision; revision++ {
		path := filepath.Join(root, fmt.Sprintf("%s-%06d.dataset.json", selected.SessionID, revision))
		current, err := ReadManifest(path)
		if err != nil || validateManifest(current) != nil {
			return recorderError("manifest_chain_invalid")
		}
		if !sameDatasetIdentity(current, selected) || current.Revision != revision {
			return recorderError("manifest_chain_invalid")
		}
		if revision > 1 && !validSuccessor(previous, current) {
			return recorderError("manifest_chain_invalid")
		}
		previous = current
	}
	if previous.Hash != selected.Hash {
		return recorderError("manifest_chain_invalid")
	}
	return nil
}

func sameDatasetIdentity(left, right DatasetManifest) bool {
	return left.SchemaVersion == right.SchemaVersion && left.DatasetID == right.DatasetID &&
		left.SessionID == right.SessionID && left.Exchange == right.Exchange
}

func validSuccessor(previous, current DatasetManifest) bool {
	if current.PreviousHash != previous.Hash || len(current.Segments) != len(previous.Segments)+2 ||
		current.RawRecordCount < previous.RawRecordCount || current.CanonicalCount < previous.CanonicalCount ||
		len(current.Gaps) < len(previous.Gaps) {
		return false
	}
	for index := range previous.Segments {
		if current.Segments[index] != previous.Segments[index] {
			return false
		}
	}
	for index := range previous.Gaps {
		if current.Gaps[index] != previous.Gaps[index] {
			return false
		}
	}
	return true
}
