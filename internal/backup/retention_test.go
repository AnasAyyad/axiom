package backup

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneArtifactsRetainsNewestVerifiedFourteen(t *testing.T) {
	root := t.TempDir()
	key := testKey(t)
	for index := 0; index < MinimumRetainedGenerations+2; index++ {
		createRetentionFixture(t, root, key, index)
	}
	removed, err := PruneArtifacts(root, key, MinimumRetainedGenerations)
	if err != nil || len(removed) != 2 || removed[0] != "axiom-generation-00" || removed[1] != "axiom-generation-01" {
		t.Fatalf("removed = %v, %v", removed, err)
	}
	manifests, _ := filepath.Glob(filepath.Join(root, "*.manifest.json"))
	artifacts, _ := filepath.Glob(filepath.Join(root, "*.dump.aesgcm"))
	if len(manifests) != MinimumRetainedGenerations || len(artifacts) != MinimumRetainedGenerations {
		t.Fatalf("retained manifests=%d artifacts=%d", len(manifests), len(artifacts))
	}
}

func TestPruneArtifactsFailsClosedBeforeDeletingCorruptInventory(t *testing.T) {
	root := t.TempDir()
	key := testKey(t)
	for index := 0; index < MinimumRetainedGenerations+1; index++ {
		createRetentionFixture(t, root, key, index)
	}
	path := filepath.Join(root, "axiom-generation-10.dump.aesgcm")
	file, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	_, _ = file.Write([]byte("corrupt"))
	_ = file.Close()
	if removed, err := PruneArtifacts(root, key, MinimumRetainedGenerations); err == nil || len(removed) != 0 {
		t.Fatalf("corrupt inventory pruned: %v, %v", removed, err)
	}
	manifests, _ := filepath.Glob(filepath.Join(root, "*.manifest.json"))
	if len(manifests) != MinimumRetainedGenerations+1 {
		t.Fatalf("inventory changed before validation completed: %d", len(manifests))
	}
}

func TestPruneArtifactsResumesAuthenticatedDeletionTombstone(t *testing.T) {
	root := t.TempDir()
	key := testKey(t)
	manifest := createRetentionFixture(t, root, key, 0)
	original := filepath.Join(root, manifest.Spec.Name+".manifest.json")
	tombstone := original + ".deleting"
	if err := os.Rename(original, tombstone); err != nil {
		t.Fatal(err)
	}
	if _, err := PruneArtifacts(root, key, MinimumRetainedGenerations); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{tombstone, filepath.Join(root, manifest.Path)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("interrupted deletion survived: %s", path)
		}
	}
}

func TestPruneArtifactsRejectsTombstoneWithWrongKey(t *testing.T) {
	root := t.TempDir()
	key := testKey(t)
	manifest := createRetentionFixture(t, root, key, 0)
	original := filepath.Join(root, manifest.Spec.Name+".manifest.json")
	tombstone := original + ".deleting"
	if err := os.Rename(original, tombstone); err != nil {
		t.Fatal(err)
	}
	wrongKey := key
	wrongKey[0] ^= 0xff
	if _, err := PruneArtifacts(root, wrongKey, MinimumRetainedGenerations); err == nil {
		t.Fatal("tombstone authenticated with wrong key")
	}
	if _, err := os.Stat(filepath.Join(root, manifest.Path)); err != nil {
		t.Fatal("artifact removed after tombstone authentication failure")
	}
}

func TestPruneArtifactsRejectsWeakenedRetention(t *testing.T) {
	if _, err := PruneArtifacts(t.TempDir(), testKey(t), MinimumRetainedGenerations-1); err == nil {
		t.Fatal("retention below fourteen accepted")
	}
}

func createRetentionFixture(t *testing.T, root string, key [32]byte, index int) ArtifactManifest {
	t.Helper()
	spec := ArtifactSpec{
		Name: fmt.Sprintf("axiom-generation-%02d", index), Database: "axiom", SchemaVersion: "000003",
		ToolVersion: "pg_dump (PostgreSQL) 18.4", ValidatorVersion: "pg_restore (PostgreSQL) 18.4",
		WALBoundary: fmt.Sprintf("0/%08X", index+1),
		StartedAt:   time.Date(2026, 6, 1+index, 0, 0, 0, 0, time.UTC),
	}
	manifest, err := CreateArtifact(root, spec, bytes.NewReader([]byte(fmt.Sprintf("dump-%02d", index))), key)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
