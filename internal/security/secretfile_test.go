package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSecretFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "secret")
	if err := os.WriteFile(path, []byte("fixture-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := ReadSecretFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if value != "fixture-value" {
		t.Fatal("secret newline was not normalized")
	}
}

func TestReadSecretFileRejectsUnsafeInputsWithoutPath(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "unsafe-secret")
	if err := os.WriteFile(path, []byte("fixture-value"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadSecretFile(path)
	if err == nil || strings.Contains(err.Error(), path) {
		t.Fatalf("expected redacted permission error, got %v", err)
	}
}

func TestReadSecretFileRejectsSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	link := filepath.Join(directory, "link")
	if err := os.WriteFile(target, []byte("fixture-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecretFile(link); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestReadSecretFileGroupPolicy(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "group-secret")
	if err := os.WriteFile(path, []byte("fixture-value"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecretFile(path); err != nil {
		t.Fatalf("current-group 0640 secret rejected: %v", err)
	}
	for _, mode := range []os.FileMode{0o660, 0o644, 0o740} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadSecretFile(path); err == nil {
			t.Fatalf("unsafe mode %04o accepted", mode)
		}
	}
}
