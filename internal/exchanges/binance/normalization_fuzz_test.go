package binance

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"axiom/internal/domain"
)

func FuzzNormalizePublicPayload(f *testing.F) {
	for _, name := range []string{
		"depth-snapshot.json", "depth-update.json", "trades.json", "candles.json",
		"candle-stream.json", "exchange-info.json", "exchange-info-unknown-status.json",
	} {
		payload, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "exchanges", "binance", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(payload)
	}
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		f.Fatal(err)
	}
	received := domain.EventTime{UTC: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC), Sequence: 1}
	f.Fuzz(func(_ *testing.T, payload []byte) {
		_, _ = NormalizeSnapshot(payload, instrument, received)
		_, _ = NormalizeDepth(payload, received)
		_, _ = NormalizeTrades(payload, instrument, received)
		_, _ = NormalizeCandle(payload, received)
		_, _ = NormalizeCandleHistory(payload, instrument, "4h", received)
		_, _ = NormalizeInstruments(payload, received.UTC, 1)
	})
}
