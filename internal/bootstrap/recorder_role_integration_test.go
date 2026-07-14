package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	postgresstore "axiom/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestA7RecorderRolePublicIntegration(t *testing.T) {
	dsn := os.Getenv("AXIOM_A7_RECORDER_ROLE_DSN")
	if dsn == "" {
		t.Skip("AXIOM_A7_RECORDER_ROLE_DSN is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = postgresstore.ApplyMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	runtimeConfig := config.Runtime{InstanceID: "a7-role-integration", Recorder: config.RecorderRuntime{
		Root: t.TempDir(), FlushInterval: 5 * time.Second, QueueCapacity: 8192, BookDepth: 1000}}
	clock := &domain.SystemClock{}
	work, err := newRecorderRoleWork(ctx, pool, runtimeConfig, config.DefaultConfiguration(), clock)
	if err != nil {
		t.Fatal(err)
	}
	runContext, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { done <- work.Run(runContext, logger) }()
	deadline := time.NewTimer(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for !work.Ready() {
		select {
		case err = <-done:
			t.Fatalf("recorder stopped before readiness: %v", err)
		case <-deadline.C:
			t.Fatal("recorder did not become ready")
		case <-ticker.C:
		}
	}
	time.Sleep(6 * time.Second)
	stop()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM market_data_segments WHERE recorder_session LIKE 'recorder-%'").Scan(&count); err != nil || count < 2 {
		t.Fatalf("registered segment count=%d err=%v", count, err)
	}
}
