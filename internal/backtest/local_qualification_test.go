package backtest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const a7SourceCommit = "a641cd46694a1975bacdd1597f6bdf1cfed753f6"

func TestA8IgnoredLocalDatasetQualification(t *testing.T) {
	firstRoot := os.Getenv("AXIOM_A8_DATASET_43_ROOT")
	secondRoot := os.Getenv("AXIOM_A8_DATASET_R2_ROOT")
	if firstRoot == "" || secondRoot == "" {
		t.Skip("A8 local recording roots are not configured")
	}
	firstManifest := selectedManifest(t, firstRoot, 43)
	secondManifest := selectedManifest(t, secondRoot, 62)
	degradedManifest := selectedManifest(t, secondRoot, 84)
	qualifyRepeatedDataset(t, firstRoot, firstManifest)
	qualifySecondDataset(t, secondRoot, secondManifest)
	qualifyDegradedDataset(t, secondRoot, degradedManifest)
}

func qualifyRepeatedDataset(t *testing.T, root, manifest string) {
	t.Helper()
	var expected string
	for pass := 0; pass < 10; pass++ {
		reader := openLocalReader(t, root, manifest)
		if reader.Descriptor().Confidence != ConfidenceB || reader.Descriptor().RecordCount != 1_083_956 {
			t.Fatal("revision 43 identity or confidence mismatch")
		}
		hash, count, maximumRows, elapsed, logicalSpan := streamDataset(t, reader)
		if count != 1_083_956 || maximumRows > 100_000 || elapsed >= logicalSpan {
			t.Fatal("revision 43 bounded maximum-speed replay requirement failed")
		}
		if pass == 0 {
			expected = hash
		} else if hash != expected {
			t.Fatal("revision 43 streaming pass was not deterministic")
		}
	}
}

func qualifySecondDataset(t *testing.T, root, manifest string) {
	t.Helper()
	reader := openLocalReader(t, root, manifest)
	if reader.Descriptor().Confidence != ConfidenceB || reader.Descriptor().RecordCount != 1_240_370 {
		t.Fatal("revision 62 identity or confidence mismatch")
	}
	_, count, maximumRows, _, _ := streamDataset(t, reader)
	if count != 1_240_370 || maximumRows > 100_000 {
		t.Fatal("revision 62 bounded streaming requirement failed")
	}
}

func qualifyDegradedDataset(t *testing.T, root, manifest string) {
	t.Helper()
	reader := openLocalReader(t, root, manifest)
	descriptor := reader.Descriptor()
	if descriptor.Confidence != ConfidenceC || descriptor.LowDensitySegments != 22 ||
		descriptor.RecordCount != 1_241_218 || descriptor.RequireDecisionGrade() == nil {
		t.Fatal("revision 84 degradation did not fail closed")
	}
}

func openLocalReader(t *testing.T, root, manifest string) *DatasetReader {
	t.Helper()
	reader, err := OpenDataset(root, manifest, DatasetCompatibility{SourceCommit: a7SourceCommit,
		ParserVersion: "binance-public-parser.v1", NormalizationVersion: "binance-public-normalizer.v1",
		MinimumRecordsPerPair: 1_000, MaximumLowDensityPairs: 0})
	if err != nil {
		t.Fatal("local dataset verification failed")
	}
	return reader
}

func streamDataset(t *testing.T, reader *DatasetReader) (string, uint64, int, time.Duration, time.Duration) {
	t.Helper()
	digest := sha256.New()
	started := time.Now()
	var count, first, last uint64
	maximumRows := 0
	for {
		event, ok, err := reader.Next()
		if err != nil {
			t.Fatal("local dataset streaming failed")
		}
		if !ok {
			break
		}
		if count == 0 {
			first = event.Record.RecordedLogicalTime
		}
		last = event.Record.RecordedLogicalTime
		writeRecordDigest(digest, event.Record.IngestOrdinal, last, event.Record.Canonical)
		count++
		if len(reader.rows) > maximumRows {
			maximumRows = len(reader.rows)
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), count, maximumRows, time.Since(started), durationBetween(first, last)
}

func writeRecordDigest(digest interface{ Write([]byte) (int, error) }, ordinal, logical uint64, canonical []byte) {
	framing := make([]byte, 16)
	binary.BigEndian.PutUint64(framing[:8], ordinal)
	binary.BigEndian.PutUint64(framing[8:], logical)
	_, _ = digest.Write(framing)
	_, _ = digest.Write(canonical)
}

func durationBetween(first, last uint64) time.Duration {
	if last <= first || last-first > uint64(1<<63-1) {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(last - first)
}

func selectedManifest(t *testing.T, root string, revision uint64) string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal("local dataset root unavailable")
	}
	suffix := strings.Repeat("0", 6-len(decimalRevision(revision))) + decimalRevision(revision) + ".dataset.json"
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), suffix) {
			return filepath.Join(root, entry.Name())
		}
	}
	t.Fatal("selected local manifest unavailable")
	return ""
}

func decimalRevision(revision uint64) string {
	const digits = "0123456789"
	if revision == 0 {
		return "0"
	}
	result := make([]byte, 0, 6)
	for revision > 0 {
		result = append([]byte{digits[revision%10]}, result...)
		revision /= 10
	}
	return string(result)
}
