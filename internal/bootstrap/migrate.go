package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"axiom/internal/config"
	postgresstore "axiom/internal/storage/postgres"
)

func runMigrate(ctx context.Context, runtimeConfig config.Runtime, output io.Writer) error {
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
	return json.NewEncoder(output).Encode(map[string]any{
		"event_code": "migration_complete",
		"phase":      "A1",
		"applied":    0,
		"checked_at": time.Now().UTC().Format(time.RFC3339),
	})
}
