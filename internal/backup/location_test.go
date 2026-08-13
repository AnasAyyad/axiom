package backup

import "testing"

func TestContainingMountUsesLongestBoundaryMatch(t *testing.T) {
	mounts := []mountRecord{{id: "8:1", point: "/"}, {id: "8:2", point: "/srv/backups"}}
	mount, found := containingMount("/srv/backups/axiom", mounts)
	if !found || mount.id != "8:2" {
		t.Fatalf("mount=%+v found=%t", mount, found)
	}
	if pathContains("/srv/backups", "/srv/backups-other") {
		t.Fatal("path prefix without a directory boundary was accepted")
	}
}

func TestValidateIndependentDestinationRejectsRootFilesystem(t *testing.T) {
	root := t.TempDir()
	if _, err := ValidateIndependentDestination(root, []string{t.TempDir(), t.TempDir()}); err == nil {
		t.Fatal("ordinary root-filesystem directory accepted as independent backup mount")
	}
}
