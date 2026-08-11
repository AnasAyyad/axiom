package evaluation

import (
	"context"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/recorder"
)

func TestAuditRecordedDatasetAcceptsVerifiedHistoricalArtifacts(t *testing.T) {
	root := t.TempDir()
	repository := &historicalSegmentMemory{}
	sink, _ := NewHistoricalFileSink(root, repository)
	start := time.Date(2023, 8, 1, 0, 0, 0, 0, time.UTC)
	spec := historicalAuditSpec(start)
	if _, err := sink.PersistHistoricalPage(context.Background(), spec, historicalSinkPage(t, spec, 2)); err != nil {
		t.Fatal(err)
	}
	stored, _ := repository.HistoricalImportSegments(context.Background(), spec.ID)
	references := make([]recorder.SegmentReference, len(stored))
	for index, item := range stored {
		references[index] = recorder.SegmentReference{Kind: item.Kind, Manifest: item.Manifest}
	}
	manifest, err := recorder.WriteImportedDatasetManifest(root, HistoricalImportDatasetID(spec.ID),
		HistoricalImportSessionID(spec.ID), spec.Exchange, references, start)
	if err != nil {
		t.Fatal(err)
	}
	finding := AuditRecordedDataset(root, RecordedDatasetExpectation{DatasetID: "catalog-dataset",
		RecorderDatasetID: manifest.DatasetID, ManifestHash: manifest.Hash, ManifestRevision: manifest.Revision,
		ManifestPath: manifest.SessionID + "-000001.dataset.json", ExpectedExchange: "binance"})
	if finding.Eligibility != "eligible" || finding.ReasonCode != "ELIGIBLE_VERIFIED" || finding.SegmentCount != 2 {
		t.Fatalf("finding=%#v", finding)
	}
}

func TestAuditRecordedDatasetMarksIdentityFailureIneligible(t *testing.T) {
	finding := AuditRecordedDataset(t.TempDir(), RecordedDatasetExpectation{DatasetID: "dataset",
		RecorderDatasetID: "recorder", ManifestHash: "not-a-hash", ManifestRevision: 1,
		ManifestPath: "session-000001.dataset.json"})
	if finding.Eligibility != "ineligible" || finding.ReasonCode != "MANIFEST_HASH_INVALID" {
		t.Fatalf("finding=%#v", finding)
	}
}

func historicalAuditSpec(start time.Time) HistoricalImportSpec {
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	return HistoricalImportSpec{ID: "import-audit", CampaignID: "campaign", Exchange: "binance",
		Instrument: instrument, Interval: "1h", WindowStart: start, WindowEnd: start.Add(2 * time.Hour), Checkpoint: start}
}
