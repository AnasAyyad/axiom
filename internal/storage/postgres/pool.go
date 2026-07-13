package postgres

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"axiom/internal/config"
	"axiom/internal/security"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open constructs a lazy pool from the validated config and secret file.
// Connectivity is reported separately through Ping so liveness remains useful
// while readiness is false.
func Open(ctx context.Context, database config.Database) (*pgxpool.Pool, error) {
	password, err := security.ReadSecretFile(database.PasswordFile)
	if err != nil {
		return nil, fmt.Errorf("postgres_secret_unavailable: %w", err)
	}
	poolConfig, err := poolConfiguration(database, password)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("postgres_pool_invalid")
	}
	return pool, nil
}

func poolConfiguration(database config.Database, password string) (*pgxpool.Config, error) {
	origin := &url.URL{
		Scheme: "postgres",
		User:   url.User(database.User),
		Host:   net.JoinHostPort(database.Host, strconv.Itoa(int(database.Port))),
		Path:   database.Name,
	}
	query := origin.Query()
	query.Set("sslmode", database.SSLMode)
	query.Set("connect_timeout", wholeSeconds(database.ConnectionTimeout))
	origin.RawQuery = query.Encode()

	poolConfig, err := pgxpool.ParseConfig(origin.String())
	if err != nil {
		return nil, fmt.Errorf("postgres_configuration_invalid")
	}
	poolConfig.ConnConfig.Password = password
	poolConfig.MaxConns = database.MaxOpenConnections
	poolConfig.MinConns = 0
	poolConfig.MaxConnLifetime = database.ConnectionMaxLifetime
	poolConfig.ConnConfig.RuntimeParams["statement_timeout"] = durationMilliseconds(database.StatementTimeout)
	return poolConfig, nil
}

func wholeSeconds(duration time.Duration) string {
	seconds := duration / time.Second
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(int64(seconds), 10)
}

func durationMilliseconds(duration time.Duration) string {
	return strconv.FormatInt(duration.Milliseconds(), 10)
}
