package buildinfo

import "testing"

func TestCurrentNormalizesEmptyValues(t *testing.T) {
	originalVersion, originalCommit := Version, Commit
	originalGoSum, originalPNPM := GoSumHash, PNPMLockHash
	t.Cleanup(func() {
		Version, Commit = originalVersion, originalCommit
		GoSumHash, PNPMLockHash = originalGoSum, originalPNPM
	})
	Version, Commit, GoSumHash, PNPMLockHash = "", "  ", "", " "

	info := Current()
	if info.Version != "unknown" || info.Commit != "unknown" || info.GoSumHash != "unknown" || info.PNPMLockHash != "unknown" {
		t.Fatalf("unexpected normalized identity: %#v", info)
	}
}
