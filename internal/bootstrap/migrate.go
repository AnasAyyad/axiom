package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"axiom/internal/config"
	postgresstore "axiom/internal/storage/postgres"
)

func runMigrate(ctx context.Context, runtimeConfig config.Runtime, product config.Configuration, output io.Writer) error {
	pool, err := postgresstore.Open(ctx, runtimeConfig.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	pingContext, cancel := context.WithTimeout(ctx, runtimeConfig.Database.ConnectionTimeout)
	defer cancel()
	if err := pool.Ping(pingContext); err != nil {
		return fmt.Errorf("migration_database_unavailable")
	}
	applied, err := postgresstore.ApplyMigrations(ctx, pool)
	if err != nil {
		return err
	}
	runtimeRole := environmentOr("POSTGRES_RUNTIME_USER", "axiom_app")
	recorderRole := environmentOr("POSTGRES_RECORDER_USER", "axiom_recorder")
	readOnlyRole := environmentOr("POSTGRES_READONLY_USER", "axiom_readonly")
	if err := postgresstore.ApplyRoleGrants(ctx, pool, runtimeRole, recorderRole, readOnlyRole); err != nil {
		return err
	}
	binanceRole := environmentOr("POSTGRES_BINANCE_ENGINE_USER", "axiom_binance_engine")
	bybitRole := environmentOr("POSTGRES_BYBIT_ENGINE_USER", "axiom_bybit_engine")
	if err := postgresstore.ApplyV1CEngineRoleGrants(ctx, pool, binanceRole, bybitRole); err != nil {
		return err
	}
	if err := postgresstore.EnsureV1AReferenceData(ctx, pool, product, time.Now().UTC()); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]any{
		"event_code": "migration_complete",
		"phase":      "A11",
		"applied":    applied,
		"checked_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func environmentOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
