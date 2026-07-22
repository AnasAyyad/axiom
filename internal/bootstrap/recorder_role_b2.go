package bootstrap

import (
	"path/filepath"

	"axiom/internal/config"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"

	"github.com/jackc/pgx/v5/pgxpool"
)

func recorderExchangeRoot(root, exchange string, exchangeCount int) string {
	if exchangeCount > 1 {
		return filepath.Join(root, exchange)
	}
	return root
}

func newBinanceStreamRecorder(
	root, session string,
	runtimeConfig config.Runtime,
	exchangeCount int,
	ordinals *runtimecore.IngestOrdinals,
	pool *pgxpool.Pool,
) (*marketrecorder.Recorder, error) {
	commit := segmentCommitter(pool, session, "binance")
	if exchangeCount == 2 {
		return marketrecorder.NewB2(root, recorderDatasetID(session), session, "binance", ordinals, commit, nil,
			marketrecorder.CollectorProfile{Instance: runtimeConfig.InstanceID,
				Region: runtimeConfig.Recorder.CollectorRegion, MinimumReaderVersion: "dataset-reader.v2"})
	}
	return marketrecorder.New(root, recorderDatasetID(session), session, "binance", ordinals, commit, nil)
}
