package postgres

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
)

func TestPoolConfigurationKeepsPasswordOutOfConnectionString(t *testing.T) {
	database := config.Database{
		Host: "localhost", Port: 5432, Name: "axiom", User: "axiom_app",
		SSLMode: "disable", MaxOpenConnections: 4,
		ConnectionMaxLifetime: time.Minute, ConnectionTimeout: time.Second,
		StatementTimeout: time.Second,
	}
	const canary = "secret-canary-value"
	poolConfig, err := poolConfiguration(database, canary)
	if err != nil {
		t.Fatal(err)
	}
	if poolConfig.ConnConfig.Password != canary {
		t.Fatal("password was not assigned to the protected field")
	}
	if strings.Contains(poolConfig.ConnConfig.ConnString(), canary) {
		t.Fatal("password leaked into the connection string")
	}
}
