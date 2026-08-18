package bootstrap

import (
	"context"
	"fmt"
	"os"
	"time"

	marketrecorder "axiom/internal/recorder"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5/pgxpool"
)

type recorderRecoveryExchange struct {
	root, session, exchange string
	found                   bool
	manifest                marketrecorder.DatasetManifest
}

func reconcileRecorderRoleResume(ctx context.Context, pool *pgxpool.Pool, session string,
	state recorderRoleResumeState) (recorderRoleResumeState, error) {
	if err := ensureRecorderRecoveryRoots(state.binanceRoot, state.bybitRoot); err != nil {
		return recorderRoleResumeState{}, fmt.Errorf("recorder_recovery_root_unavailable")
	}
	catalog, catalogErr := postgresstore.NewRecordedDatasetCatalog(pool)
	committer, committerErr := postgresstore.NewRecordedSegmentCommitter(pool)
	if pool == nil {
		catalog, committer = nil, nil
		catalogErr, committerErr = nil, nil
	}
	exchanges := []recorderRecoveryExchange{
		{state.binanceRoot, session, "binance", state.binanceFound, state.binanceManifest},
		{state.bybitRoot, session + "-bybit", "bybit", state.bybitFound, state.bybitManifest},
	}
	for _, exchange := range exchanges {
		if exchange.exchange == "bybit" && !state.bybitFound && state.bybitRoot == state.binanceRoot {
			continue
		}
		result, recoveryErr := reconcileRecorderExchange(ctx, catalog, catalogErr, committer, committerErr, exchange)
		if recoveryErr != nil {
			return recorderRoleResumeState{}, recoveryErr
		}
		state.recoveredFiles += len(result.Moved)
	}
	return state, nil
}

func ensureRecorderRecoveryRoots(roots ...string) error {
	for _, root := range roots {
		if err := os.MkdirAll(root, 0o750); err != nil {
			return err
		}
	}
	return nil
}

func reconcileRecorderExchange(ctx context.Context, catalog *postgresstore.RecordedDatasetCatalog,
	catalogErr error, committer *postgresstore.RecordedSegmentCommitter, committerErr error,
	exchange recorderRecoveryExchange) (marketrecorder.ArtifactRecovery, error) {
	if exchange.found {
		if catalogErr != nil || catalog == nil {
			return marketrecorder.ArtifactRecovery{}, fmt.Errorf("recorder_recovery_catalog_unavailable")
		}
		catalogContext, cancel := context.WithTimeout(ctx, 30*time.Second)
		if exchange.manifest.SourceCommit != "" {
			_, catalogErr = catalog.Register(catalogContext, exchange.manifest, exchange.manifest.SourceCommit)
		} else {
			_, catalogErr = catalog.VerifyRegistered(catalogContext, exchange.manifest, "public_market")
		}
		cancel()
		if catalogErr != nil {
			return marketrecorder.ArtifactRecovery{}, fmt.Errorf("recorder_recovery_catalog_invalid")
		}
	}
	return marketrecorder.QuarantineUncommittedArtifacts(exchange.root, exchange.session,
		exchange.manifest, func(names []string, proven []segments.Manifest) error {
			if committerErr != nil || committer == nil {
				return fmt.Errorf("recorder_recovery_committer_unavailable")
			}
			commitContext, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return committer.QuarantineRecorderArtifacts(commitContext, exchange.session, exchange.exchange,
				names, proven, time.Now().UTC())
		})
}
