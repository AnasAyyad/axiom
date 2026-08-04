package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/storage/pressure"

	"github.com/jackc/pgx/v5/pgxpool"
)

type storagePressureWriter interface {
	Observe(context.Context, pressure.Observation, pressure.Policy) (postgresstore.D5StoragePressureState, bool, error)
}

func configureRecorderPressure(work *recorderRoleWork, pool *pgxpool.Pool, instance string) error {
	work.pressureProbe = work.pressurePolicy.Probe
	if pool == nil {
		return nil
	}
	store, err := postgresstore.NewD5StoragePressureStore(pool, instance)
	if err != nil {
		return err
	}
	work.pressureStore = store
	return nil
}

func (work *recorderRoleWork) handleStoragePressure(ctx context.Context, logger *slog.Logger,
	cancel context.CancelFunc, group *sync.WaitGroup) error {
	critical, err := work.observeStoragePressure(ctx, logger)
	if err == nil && !critical {
		return nil
	}
	cancel()
	group.Wait()
	if flushErr := work.flushPending(logger, true); flushErr != nil {
		return fmt.Errorf("recorder_storage_pressure_flush_failed")
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("recorder_storage_pressure_critical")
}

func (work *recorderRoleWork) observeStoragePressure(ctx context.Context, logger *slog.Logger) (bool, error) {
	if work.pressureStore == nil || work.pressureProbe == nil || work.pressurePolicy.Validate() != nil {
		return true, fmt.Errorf("recorder_storage_pressure_monitor_unavailable")
	}
	observation, err := work.pressureProbe(work.root, time.Now().UTC())
	if err != nil {
		return true, fmt.Errorf("recorder_storage_pressure_probe_failed")
	}
	state, transitioned, err := work.pressureStore.Observe(ctx, observation, work.pressurePolicy)
	if err != nil {
		return true, fmt.Errorf("recorder_storage_pressure_persist_failed")
	}
	if transitioned || state.Level != pressure.LevelNormal {
		logger.Log(ctx, pressureLogLevel(state.Level), "recorder storage pressure observed",
			"event_code", "storage_pressure_"+stringLowerASCII(string(state.Level)),
			"level", state.Level, "available_bytes", state.AvailableBytes,
			"total_bytes", state.TotalBytes, "revision", state.Revision)
	}
	return state.Level == pressure.LevelCritical, nil
}

func pressureLogLevel(level pressure.Level) slog.Level {
	if level == pressure.LevelCritical {
		return slog.LevelError
	}
	if level == pressure.LevelHigh {
		return slog.LevelWarn
	}
	return slog.LevelInfo
}

func stringLowerASCII(value string) string {
	result := []byte(value)
	for index := range result {
		if result[index] >= 'A' && result[index] <= 'Z' {
			result[index] += 'a' - 'A'
		}
	}
	return string(result)
}
