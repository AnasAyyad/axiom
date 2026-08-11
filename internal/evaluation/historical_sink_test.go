package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/recorder"
	"axiom/internal/storage/segments"
)

type historicalSegmentMemory struct {
	mutex sync.Mutex
	items map[string]HistoricalStoredSegment
}

func (store *historicalSegmentMemory) CommitHistoricalSegment(_ context.Context, importID string,
	pageStart time.Time, kind string, manifest segments.Manifest) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.items == nil {
		store.items = make(map[string]HistoricalStoredSegment)
	}
	key := importID + ":" + pageStart.Format(time.RFC3339Nano) + ":" + kind
	wanted := HistoricalStoredSegment{PageStart: pageStart, Kind: kind, Manifest: manifest}
	if existing, ok := store.items[key]; ok && !reflect.DeepEqual(existing, wanted) {
		return ErrInvalidTransition
	}
	store.items[key] = wanted
	return nil
}

func (store *historicalSegmentMemory) HistoricalPageSegments(_ context.Context, importID string,
	pageStart time.Time) ([]HistoricalStoredSegment, error) {
	all, _ := store.HistoricalImportSegments(context.Background(), importID)
	result := make([]HistoricalStoredSegment, 0, 2)
	for _, item := range all {
		if item.PageStart.Equal(pageStart) {
			result = append(result, item)
		}
	}
	return result, nil
}

func (store *historicalSegmentMemory) HistoricalImportSegments(_ context.Context,
	importID string) ([]HistoricalStoredSegment, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	result := make([]HistoricalStoredSegment, 0)
	for key, item := range store.items {
		if len(key) >= len(importID)+1 && key[:len(importID)+1] == importID+":" {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].PageStart.Equal(result[right].PageStart) {
			return result[left].Kind == "wire"
		}
		return result[left].PageStart.Before(result[right].PageStart)
	})
	return result, nil
}

func TestHistoricalFileSinkPersistsLinkedPageAndBuildsDataset(t *testing.T) {
	root := t.TempDir()
	repository := &historicalSegmentMemory{}
	sink, err := NewHistoricalFileSink(root, repository)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2023, 8, 1, 0, 0, 0, 0, time.UTC)
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	spec := HistoricalImportSpec{ID: "import-file", CampaignID: "campaign", Exchange: "binance",
		Instrument: instrument, Interval: "1h", WindowStart: start, WindowEnd: start.Add(2 * time.Hour), Checkpoint: start}
	page := historicalSinkPage(t, spec, 2)
	artifact, err := sink.PersistHistoricalPage(context.Background(), spec, page)
	if err != nil || artifact.RowCount != 2 || !artifact.NextCheckpoint.Equal(spec.WindowEnd) {
		t.Fatalf("artifact=%#v err=%v", artifact, err)
	}
	recovered, found, err := sink.LoadHistoricalPage(context.Background(), spec)
	if err != nil || !found || !reflect.DeepEqual(recovered, artifact) {
		t.Fatalf("recovered=%#v found=%t err=%v", recovered, found, err)
	}
	stored, _ := repository.HistoricalImportSegments(context.Background(), spec.ID)
	refs := make([]recorder.SegmentReference, len(stored))
	for index, item := range stored {
		refs[index] = recorder.SegmentReference{Kind: item.Kind, Manifest: item.Manifest}
	}
	manifest, err := recorder.WriteImportedDatasetManifest(root, HistoricalImportDatasetID(spec.ID),
		HistoricalImportSessionID(spec.ID), spec.Exchange, refs, start)
	if err != nil || manifest.RawRecordCount != 1 || recorder.VerifyManifestChain(root, manifest) != nil {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
}

func historicalSinkPage(t *testing.T, spec HistoricalImportSpec,
	count int) exchangecontracts.HistoricalCandlePage {
	t.Helper()
	raw := []byte(`{"official":"page"}`)
	rawHash := sha256.Sum256(raw)
	received := domain.EventTime{UTC: spec.WindowEnd.Add(time.Hour), Sequence: 1}
	page := exchangecontracts.HistoricalCandlePage{Exchange: exchangecontracts.ExchangeID(spec.Exchange),
		Instrument: spec.Instrument, Interval: spec.Interval, Start: spec.Checkpoint,
		End: spec.Checkpoint.Add(time.Duration(count)*time.Hour - time.Millisecond), ReceivedAt: received,
		RawPayload: raw, RawPayloadHash: hex.EncodeToString(rawHash[:])}
	price, _ := domain.ParsePrice("100")
	quantity, _ := domain.ParseQuantity("1")
	for index := 0; index < count; index++ {
		open := spec.Checkpoint.Add(time.Duration(index) * time.Hour)
		page.Candles = append(page.Candles, exchangecontracts.Candle{Exchange: page.Exchange,
			Instrument: spec.Instrument, Interval: spec.Interval, OpenTime: open,
			CloseTime: open.Add(time.Hour - time.Millisecond), Open: price, High: price, Low: price,
			Close: price, Volume: quantity, Closed: true, ReceivedAt: received, RawPayloadHash: page.RawPayloadHash})
	}
	return page
}
