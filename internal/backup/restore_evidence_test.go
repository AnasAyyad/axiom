package backup

import (
	"bytes"
	"testing"
	"time"
)

func TestRestoreEvidenceIsAuthenticatedAndNoReplace(t *testing.T) {
	root := t.TempDir()
	key := testKey(t)
	started := time.Now().Add(-time.Second).UTC()
	manifest, err := CreateArtifact(root, ArtifactSpec{Name: "axiom", Database: "axiom", SchemaVersion: "000027", ToolVersion: "pg_dump 18.4", ValidatorVersion: "pg_restore 18.4", WALBoundary: "0/16B6C50", StartedAt: started}, bytes.NewBufferString("dump"), key)
	if err != nil {
		t.Fatal(err)
	}
	market := MarketRecoveryEvidence{VerifiedSegments: 2,
		InventoryHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	path, evidence, err := WriteRestoreEvidence(root, manifest, started, started.Add(3*time.Minute), market, key)
	if err != nil || path == "" || !VerifyRestoreEvidence(evidence, key) {
		t.Fatalf("path=%s evidence=%+v error=%v", path, evidence, err)
	}
	if !evidence.MarketDataVerified || evidence.MarketSegmentsVerified != 2 ||
		evidence.MarketInventoryHash != market.InventoryHash {
		t.Fatalf("market recovery evidence missing: %+v", evidence)
	}
	if _, _, err = WriteRestoreEvidence(root, manifest, started, started.Add(3*time.Minute), market, key); err == nil {
		t.Fatal("restore evidence overwritten")
	}
	evidence.DurationSeconds++
	if VerifyRestoreEvidence(evidence, key) {
		t.Fatal("mutated restore evidence authenticated")
	}
}
