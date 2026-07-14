package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArtifactCreateAndVerifiedRestore(t *testing.T) {
	root := t.TempDir()
	spec := ArtifactSpec{
		Name: "axiom-20260714t100000z", Database: "axiom", SchemaVersion: "000003",
		ToolVersion: "pg_dump (PostgreSQL) 18.4", ValidatorVersion: "pg_restore (PostgreSQL) 18.4", WALBoundary: "0/16B6A50",
		StartedAt: time.Now().Add(-time.Second).UTC(),
	}
	source := bytes.Repeat([]byte("custom-format-dump"), 1000)
	manifest, err := CreateArtifact(root, spec, bytes.NewReader(source), testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Path == "" || manifest.SHA256 == "" || manifest.Size <= 0 || manifest.CompletedAt.IsZero() ||
		manifest.Spec.ToolVersion == "" || manifest.Spec.ValidatorVersion == "" || manifest.Spec.WALBoundary == "" {
		t.Fatalf("manifest = %#v", manifest)
	}
	loaded, err := ReadArtifactManifest(filepath.Join(root, spec.Name+".manifest.json"))
	if err != nil || loaded != manifest {
		t.Fatalf("loaded = %#v, %v", loaded, err)
	}
	var restored bytes.Buffer
	if err = RestoreArtifact(root, loaded, &restored, testKey(t)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Bytes(), source) {
		t.Fatal("restored artifact differs")
	}
}

func TestArtifactRestoreRejectsChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	spec := ArtifactSpec{
		Name: "axiom-20260714t100000z", Database: "axiom", SchemaVersion: "000003",
		ToolVersion: "pg_dump (PostgreSQL) 18.4", ValidatorVersion: "pg_restore (PostgreSQL) 18.4", WALBoundary: "0/16B6A50",
		StartedAt: time.Now().Add(-time.Second).UTC(),
	}
	manifest, _ := CreateArtifact(root, spec, bytes.NewReader([]byte("dump")), testKey(t))
	file, _ := os.OpenFile(filepath.Join(root, manifest.Path), os.O_WRONLY|os.O_APPEND, 0)
	_, _ = file.Write([]byte{1})
	_ = file.Close()
	if err := RestoreArtifact(root, manifest, new(bytes.Buffer), testKey(t)); err == nil {
		t.Fatal("modified artifact restored")
	}
}

func TestArtifactRestoreRejectsManifestMutation(t *testing.T) {
	root := t.TempDir()
	spec := ArtifactSpec{
		Name: "axiom-20260714t100000z", Database: "axiom", SchemaVersion: "000003",
		ToolVersion: "pg_dump (PostgreSQL) 18.4", ValidatorVersion: "pg_restore (PostgreSQL) 18.4", WALBoundary: "0/16B6A50",
		StartedAt: time.Now().Add(-time.Second).UTC(),
	}
	manifest, _ := CreateArtifact(root, spec, bytes.NewReader([]byte("dump")), testKey(t))
	manifest.Spec.SchemaVersion = "000002"
	if err := RestoreArtifact(root, manifest, new(bytes.Buffer), testKey(t)); err == nil {
		t.Fatal("mutated manifest restored")
	}
}

func TestArtifactRestoreRejectsSymlinkedArtifactAndManifest(t *testing.T) {
	root := t.TempDir()
	spec := ArtifactSpec{
		Name: "axiom-20260714t100000z", Database: "axiom", SchemaVersion: "000003",
		ToolVersion: "pg_dump (PostgreSQL) 18.4", ValidatorVersion: "pg_restore (PostgreSQL) 18.4", WALBoundary: "0/16B6A50",
		StartedAt: time.Now().Add(-time.Second).UTC(),
	}
	manifest, _ := CreateArtifact(root, spec, bytes.NewReader([]byte("dump")), testKey(t))
	artifact := filepath.Join(root, manifest.Path)
	realArtifact := artifact + ".real"
	if err := os.Rename(artifact, realArtifact); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realArtifact, artifact); err != nil {
		t.Fatal(err)
	}
	if err := RestoreArtifact(root, manifest, new(bytes.Buffer), testKey(t)); err == nil {
		t.Fatal("symlinked artifact restored")
	}
	manifestPath := filepath.Join(root, spec.Name+".manifest.json")
	realManifest := manifestPath + ".real"
	if err := os.Rename(manifestPath, realManifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realManifest, manifestPath); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadArtifactManifest(manifestPath); err == nil {
		t.Fatal("symlinked manifest read")
	}
}

func TestArtifactCreateRejectsFutureOrControlMetadata(t *testing.T) {
	base := ArtifactSpec{
		Name: "axiom-20260714t100000z", Database: "axiom", SchemaVersion: "000003",
		ToolVersion: "pg_dump (PostgreSQL) 18.4", ValidatorVersion: "pg_restore (PostgreSQL) 18.4", WALBoundary: "0/16B6A50",
		StartedAt: time.Now().Add(-time.Second).UTC(),
	}
	future := base
	future.StartedAt = time.Now().Add(time.Hour).UTC()
	if _, err := CreateArtifact(t.TempDir(), future, bytes.NewReader([]byte("dump")), testKey(t)); err == nil {
		t.Fatal("future backup start accepted")
	}
	control := base
	control.ToolVersion = "pg_dump\nsecret"
	if _, err := CreateArtifact(t.TempDir(), control, bytes.NewReader([]byte("dump")), testKey(t)); err == nil {
		t.Fatal("control character metadata accepted")
	}
}

func TestQuarantineArtifactRemovesInvalidDumpFromReadyInventory(t *testing.T) {
	root := t.TempDir()
	spec := ArtifactSpec{
		Name: "axiom-20260714t100000z", Database: "axiom", SchemaVersion: "000003",
		ToolVersion: "pg_dump (PostgreSQL) 18.4", ValidatorVersion: "pg_restore (PostgreSQL) 18.4",
		WALBoundary: "0/16B6A50", StartedAt: time.Now().Add(-time.Second).UTC(),
	}
	manifest, err := CreateArtifact(root, spec, bytes.NewReader([]byte("not-a-postgresql-archive")), testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if err = QuarantineArtifact(root, manifest, testKey(t)); err != nil {
		t.Fatal(err)
	}
	for _, ready := range []string{
		filepath.Join(root, spec.Name+".manifest.json"),
		filepath.Join(root, manifest.Path),
	} {
		if _, err = os.Stat(ready); !os.IsNotExist(err) {
			t.Fatalf("ready inventory survived quarantine: %s", ready)
		}
	}
	for _, quarantined := range []string{
		filepath.Join(root, spec.Name+".manifest.json.invalid"),
		filepath.Join(root, manifest.Path+".invalid"),
	} {
		if _, err = os.Stat(quarantined); err != nil {
			t.Fatalf("quarantine evidence missing: %s", quarantined)
		}
	}
}
