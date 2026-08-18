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
	sourceCommit string,
) (*marketrecorder.Recorder, error) {
	commit := segmentCommitter(pool, session, "binance")
	if exchangeCount == 2 {
		profile := marketrecorder.CollectorProfile{Instance: runtimeConfig.InstanceID,
			Region: runtimeConfig.Recorder.CollectorRegion, MinimumReaderVersion: "dataset-reader.v2",
			SourceCommit: sourceCommit}
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

type recorderResumeHighWaterResult struct {
	lastOrdinal, binanceGeneration, bybitGeneration uint64
	binanceFound, bybitFound                        bool
	binanceManifest, bybitManifest                  marketrecorder.DatasetManifest
}

func recorderResumeHighWater(binanceRoot, bybitRoot, session string, dual bool) (
	result recorderResumeHighWaterResult, err error) {
	result.binanceManifest, result.binanceFound, err = marketrecorder.LatestManifest(binanceRoot, session)
	if err != nil {
		return recorderResumeHighWaterResult{}, err
	}
	if result.binanceFound {
		result.lastOrdinal, err = marketrecorder.ManifestLastOrdinal(result.binanceManifest)
		if err != nil {
			return recorderResumeHighWaterResult{}, err
		}
		result.binanceGeneration = marketrecorder.ManifestLastGeneration(result.binanceManifest)
	}
	if !dual {
		return result, nil
	}
	result.bybitManifest, result.bybitFound, err = marketrecorder.LatestManifest(bybitRoot, session+"-bybit")
	if err != nil {
		return recorderResumeHighWaterResult{}, err
	}
	if result.bybitFound {
		bybitLast, lastErr := marketrecorder.ManifestLastOrdinal(result.bybitManifest)
		if lastErr != nil {
			return recorderResumeHighWaterResult{}, lastErr
		}
		if bybitLast > result.lastOrdinal {
			result.lastOrdinal = bybitLast
		}
		result.bybitGeneration = marketrecorder.ManifestLastGeneration(result.bybitManifest)
	}
	return result, nil
}
