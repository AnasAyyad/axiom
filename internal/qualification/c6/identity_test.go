package c6

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSHA256ReturnsExactDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observer")
	content := []byte("exact-c6-observer")
	if err := os.WriteFile(path, content, 0o500); err != nil {
		t.Fatal(err)
	}
	wantBytes := sha256.Sum256(content)
	want := hex.EncodeToString(wantBytes[:])
	got, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("hash mismatch: got %s want %s", got, want)
	}
}

func TestVerifyCurrentExecutableHashRejectsInvalidAndMismatch(t *testing.T) {
	if err := VerifyCurrentExecutableHash("not-a-hash"); err == nil {
		t.Fatal("invalid executable hash accepted")
	}
	if err := VerifyCurrentExecutableHash(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	); err == nil {
		t.Fatal("mismatched executable hash accepted")
	}
}
