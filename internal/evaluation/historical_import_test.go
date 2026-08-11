package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

type historicalSourceStub struct {
	page exchangecontracts.HistoricalCandlePage
}

func (source historicalSourceStub) CandlePage(_ context.Context,
	request exchangecontracts.CandleRequest) (exchangecontracts.HistoricalCandlePage, error) {
	page := source.page
	page.Start, page.End = request.Start, request.End
	return page, nil
}

type historicalSinkStub struct {
	page exchangecontracts.HistoricalCandlePage
}

func (sink *historicalSinkStub) PersistHistoricalPage(_ context.Context, _ HistoricalImportSpec,
	page exchangecontracts.HistoricalCandlePage) (HistoricalPageArtifact, error) {
	sink.page = page
	normalized, _ := json.Marshal(page.Candles)
	return HistoricalPageArtifact{ID: "artifact", ByteCount: int64(len(page.RawPayload) + len(normalized)),
		RawPayloadHash: sha256.Sum256(page.RawPayload), NormalizedPageHash: sha256.Sum256(normalized)}, nil
}

func TestHistoricalImporterAdvancesOnlyExactContiguousPage(t *testing.T) {
	start := time.Date(2023, 8, 1, 0, 0, 0, 0, time.UTC)
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	raw := []byte(`[["official"]]`)
	rawHash := sha256.Sum256(raw)
	page := exchangecontracts.HistoricalCandlePage{Exchange: "binance", Instrument: instrument,
		Interval: "1h", ReceivedAt: domain.EventTime{UTC: start.Add(3 * time.Hour), Sequence: 1},
		RawPayload: raw, RawPayloadHash: hex.EncodeToString(rawHash[:])}
	for index := 0; index < 2; index++ {
		open := start.Add(time.Duration(index) * time.Hour)
		price, _ := domain.ParsePrice("100")
		quantity, _ := domain.ParseQuantity("1")
		page.Candles = append(page.Candles, exchangecontracts.Candle{Exchange: "binance", Instrument: instrument,
			Interval: "1h", OpenTime: open, CloseTime: open.Add(time.Hour - time.Millisecond), Open: price,
			High: price, Low: price, Close: price, Volume: quantity, Closed: true,
			ReceivedAt: page.ReceivedAt, RawPayloadHash: page.RawPayloadHash})
	}
	sink := &historicalSinkStub{}
	importer, err := NewHistoricalImporter(historicalSourceStub{page}, historicalSourceStub{page}, sink)
	if err != nil {
		t.Fatal(err)
	}
	spec := HistoricalImportSpec{ID: "import", CampaignID: "campaign", Exchange: "binance",
		Instrument: instrument, Interval: "1h", WindowStart: start, WindowEnd: start.Add(2 * time.Hour), Checkpoint: start}
	progress, err := importer.ImportPage(context.Background(), spec)
	if err != nil || !progress.Complete || !progress.NextCheckpoint.Equal(spec.WindowEnd) || progress.RowCount != 2 {
		t.Fatalf("progress=%#v err=%v", progress, err)
	}
}

func TestHistoricalImporterBlocksMissingCandleWithoutPersistence(t *testing.T) {
	start := time.Date(2023, 8, 1, 0, 0, 0, 0, time.UTC)
	instrument, _ := domain.NewSpotInstrument("ETH", "USDT")
	raw := []byte(`[]`)
	hash := sha256.Sum256(raw)
	page := exchangecontracts.HistoricalCandlePage{Exchange: "bybit", Instrument: instrument, Interval: "4h",
		ReceivedAt: domain.EventTime{UTC: start.Add(9 * time.Hour), Sequence: 1}, RawPayload: raw,
		RawPayloadHash: hex.EncodeToString(hash[:])}
	price, _ := domain.ParsePrice("100")
	quantity, _ := domain.ParseQuantity("1")
	page.Candles = []exchangecontracts.Candle{{Exchange: "bybit", Instrument: instrument, Interval: "4h",
		OpenTime: start, CloseTime: start.Add(4*time.Hour - time.Millisecond), Open: price, High: price, Low: price,
		Close: price, Volume: quantity, Closed: true, ReceivedAt: page.ReceivedAt, RawPayloadHash: page.RawPayloadHash}}
	sink := &historicalSinkStub{}
	importer, _ := NewHistoricalImporter(historicalSourceStub{page}, historicalSourceStub{page}, sink)
	_, err := importer.ImportPage(context.Background(), HistoricalImportSpec{ID: "import", CampaignID: "campaign",
		Exchange: "bybit", Instrument: instrument, Interval: "4h", WindowStart: start,
		WindowEnd: start.Add(8 * time.Hour), Checkpoint: start})
	failure, ok := HistoricalFailure(err)
	if !ok || failure.Reason != ReasonDataCorrupt || failure.Recoverable || len(sink.page.Candles) != 0 {
		t.Fatalf("failure=%#v ok=%t sink=%#v", failure, ok, sink.page)
	}
}
