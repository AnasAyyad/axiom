package buildinfo

import "testing"

func TestCurrentNormalizesEmptyValues(t *testing.T) {
	originalVersion, originalCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = originalVersion, originalCommit })
	Version, Commit = "", "  "

	info := Current()
	if info.Version != "unknown" || info.Commit != "unknown" {
		t.Fatalf("unexpected normalized identity: %#v", info)
	}
}
