package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestMarketRecoveryVerifiesStableRootAndExchangeInventories(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "bybit"), 0o750); err != nil {
		t.Fatal(err)
	}
	rootPayload := []byte("root-segment")
	exchangePayload := []byte("exchange-segment")
	if err := os.WriteFile(filepath.Join(root, "binance-a.parquet"), rootPayload, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bybit", "bybit-a.parquet"), exchangePayload, 0o640); err != nil {
		t.Fatal(err)
	}
	references := []MarketSegmentReference{
		{ID: "segment-bybit", Exchange: "bybit", Path: "bybit-a.parquet", SHA256: testDigest(exchangePayload)},
		{ID: "segment-binance", Exchange: "binance", Path: "binance-a.parquet", SHA256: testDigest(rootPayload)},
	}
	first, err := VerifyMarketRecovery(root, references)
	if err != nil || first.VerifiedSegments != 2 || !validHash(first.InventoryHash) {
		t.Fatalf("evidence=%+v error=%v", first, err)
	}
	references[0], references[1] = references[1], references[0]
	second, err := VerifyMarketRecovery(root, references)
	if err != nil || second != first {
		t.Fatalf("unstable evidence first=%+v second=%+v error=%v", first, second, err)
	}
}

func TestMarketRecoveryFailsClosedOnMissingCorruptOrAmbiguousFiles(t *testing.T) {
	root := t.TempDir()
	payload := []byte("segment")
	reference := MarketSegmentReference{ID: "segment-a", Exchange: "binance",
		Path: "segment-a.parquet", SHA256: testDigest(payload)}
	if _, err := VerifyMarketRecovery(root, []MarketSegmentReference{reference}); err == nil {
		t.Fatal("missing segment accepted")
	}
	if err := os.WriteFile(filepath.Join(root, reference.Path), []byte("corrupt"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyMarketRecovery(root, []MarketSegmentReference{reference}); err == nil {
		t.Fatal("corrupt segment accepted")
	}
	if err := os.WriteFile(filepath.Join(root, reference.Path), payload, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, reference.Exchange), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, reference.Exchange, reference.Path), payload, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyMarketRecovery(root, []MarketSegmentReference{reference}); err == nil {
		t.Fatal("ambiguous segment location accepted")
	}
}

func TestMarketRecoveryRejectsTraversalDuplicateAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	digest := testDigest([]byte("segment"))
	for _, reference := range []MarketSegmentReference{
		{ID: "segment-a", Exchange: "binance", Path: "../escape", SHA256: digest},
		{ID: "segment-a", Exchange: "../escape", Path: "segment.parquet", SHA256: digest},
		{ID: "segment-a", Exchange: "binance", Path: `nested\segment.parquet`, SHA256: digest},
	} {
		if _, err := VerifyMarketRecovery(root, []MarketSegmentReference{reference}); err == nil {
			t.Fatalf("unsafe reference accepted: %+v", reference)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.parquet")
	if err := os.WriteFile(outside, []byte("segment"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.parquet")); err != nil {
		t.Fatal(err)
	}
	reference := MarketSegmentReference{ID: "segment-link", Exchange: "binance",
		Path: "linked.parquet", SHA256: digest}
	if _, err := VerifyMarketRecovery(root, []MarketSegmentReference{reference}); err == nil {
		t.Fatal("symlink escape accepted")
	}
	reference = MarketSegmentReference{ID: "segment-duplicate", Exchange: "binance",
		Path: "duplicate.parquet", SHA256: digest}
	if err := os.WriteFile(filepath.Join(root, reference.Path), []byte("segment"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyMarketRecovery(root, []MarketSegmentReference{reference, reference}); err == nil {
		t.Fatal("duplicate segment identity accepted")
	}
}

func TestMarketRecoverySealsEmptyInventoryWithoutClaimingSegments(t *testing.T) {
	evidence, err := VerifyMarketRecovery(t.TempDir(), nil)
	if err != nil || evidence.VerifiedSegments != 0 || !validHash(evidence.InventoryHash) {
		t.Fatalf("evidence=%+v error=%v", evidence, err)
	}
}

func testDigest(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
