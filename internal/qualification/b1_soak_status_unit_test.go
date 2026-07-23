package qualification

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"axiom/internal/exchanges/bybit"
)

func TestB1SoakStatusWriteIsAtomicAndComplete(t *testing.T) {
	root := t.TempDir()
	observed := time.Unix(1_800_000_000, 0).UTC()
	status := b1SoakStatus{
		SchemaVersion:        "axiom.b1-soak-status.v1",
		SourceCommit:         testSourceCommit,
		StartedAt:            observed.Add(-time.Hour),
		ObservedAt:           observed,
		Elapsed:              time.Hour,
		RequiredDuration:     b1FormalSoakDuration,
		ProvisionalQualified: true,
		ProvisionalSLOs:      map[string]b1ProvisionalSLO{},
		Collectors:           map[string]bybit.CollectorStats{},
		Books:                map[string]bookSample{},
	}
	if err := writeB1SoakStatus(root, status); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "b1-soak-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded b1SoakStatus
	if err = json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SourceCommit != testSourceCommit || !decoded.ProvisionalQualified ||
		decoded.Elapsed != time.Hour {
		t.Fatalf("unexpected status: %#v", decoded)
	}
	temporary, err := filepath.Glob(filepath.Join(root, ".b1-soak-status.json.*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("temporary files remain: %v", temporary)
	}
}

func TestB1QualificationSourceCommitFailsClosed(t *testing.T) {
	t.Setenv("AXIOM_B1_SOURCE_COMMIT", "")
	if _, err := b1QualificationSourceCommit(); err == nil {
		t.Fatal("missing source commit accepted")
	}
	t.Setenv("AXIOM_B1_SOURCE_COMMIT", "not-a-commit")
	if _, err := b1QualificationSourceCommit(); err == nil {
		t.Fatal("invalid source commit accepted")
	}
	t.Setenv("AXIOM_B1_SOURCE_COMMIT", testSourceCommit)
	if commit, err := b1QualificationSourceCommit(); err != nil || commit != testSourceCommit {
		t.Fatalf("commit=%q err=%v", commit, err)
	}
}
