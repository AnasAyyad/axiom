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
	resume bool,
) (*marketrecorder.Recorder, error) {
	commit := segmentCommitter(pool, session, "binance")
	if exchangeCount == 2 {
		profile := marketrecorder.CollectorProfile{Instance: runtimeConfig.InstanceID,
			Region: runtimeConfig.Recorder.CollectorRegion, MinimumReaderVersion: "dataset-reader.v2"}
		if resume {
			recorder, _, err := marketrecorder.ResumeCoherentMarketData(root, recorderDatasetID(session), session,
				"binance", ordinals, commit, nil, profile)
			return recorder, err
		}
		return marketrecorder.NewCoherentMarketData(root, recorderDatasetID(session), session, "binance",
			ordinals, commit, nil, profile)
	}
	return marketrecorder.New(root, recorderDatasetID(session), session, "binance", ordinals, commit, nil)
}

func recorderResumeHighWater(binanceRoot, bybitRoot, session string, dual bool) (
	lastOrdinal, binanceGeneration, bybitGeneration uint64, binanceFound, bybitFound bool, err error) {
	binanceManifest, binanceFound, err := marketrecorder.LatestManifest(binanceRoot, session)
	if err != nil {
		return 0, 0, 0, false, false, err
	}
	if binanceFound {
		lastOrdinal, err = marketrecorder.ManifestLastOrdinal(binanceManifest)
		if err != nil {
			return 0, 0, 0, false, false, err
		}
		binanceGeneration = marketrecorder.ManifestLastGeneration(binanceManifest)
	}
	if !dual {
		return lastOrdinal, binanceGeneration, 0, binanceFound, false, nil
	}
	bybitManifest, bybitFound, err := marketrecorder.LatestManifest(bybitRoot, session+"-bybit")
	if err != nil {
		return 0, 0, 0, false, false, err
	}
	if bybitFound {
		bybitLast, lastErr := marketrecorder.ManifestLastOrdinal(bybitManifest)
		if lastErr != nil {
			return 0, 0, 0, false, false, lastErr
		}
		if bybitLast > lastOrdinal {
			lastOrdinal = bybitLast
		}
		bybitGeneration = marketrecorder.ManifestLastGeneration(bybitManifest)
	}
	return lastOrdinal, binanceGeneration, bybitGeneration, binanceFound, bybitFound, nil
}
