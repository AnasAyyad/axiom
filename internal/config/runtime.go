package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const maximumShutdown = 60 * time.Second

// Database is the least-privilege PostgreSQL connection contract.
type Database struct {
	Host                  string
	Port                  uint16
	Name                  string
	User                  string
	PasswordFile          string
	SSLMode               string
	MaxOpenConnections    int32
	ConnectionMaxLifetime time.Duration
	ConnectionTimeout     time.Duration
	StatementTimeout      time.Duration
}

// Runtime is the immutable A1 process configuration.
type Runtime struct {
	DeploymentEnvironment string
	InstanceID            string
	HTTPBindAddress       string
	MetricsBindAddress    string
	ShutdownTimeout       time.Duration
	Database              Database
}

// LoadRuntime validates the narrow A1 runtime configuration before resources open.
func LoadRuntime() (Runtime, error) {
	if err := ValidateEnvironment(os.Environ()); err != nil {
		return Runtime{}, err
	}
	if err := validateSafetyFlags(); err != nil {
		return Runtime{}, err
	}
	shutdown, err := durationValue("APP_SHUTDOWN_TIMEOUT", "60s")
	if err != nil || shutdown <= 0 || shutdown > maximumShutdown {
		return Runtime{}, fmt.Errorf("invalid_configuration:APP_SHUTDOWN_TIMEOUT")
	}
	database, err := loadDatabase()
	if err != nil {
		return Runtime{}, err
	}
	runtimeConfig := Runtime{
		DeploymentEnvironment: value("DEPLOYMENT_ENV", "local"),
		InstanceID:            value("APP_INSTANCE_ID", "axiom-local-01"),
		HTTPBindAddress:       value("HTTP_BIND_ADDRESS", "127.0.0.1:8080"),
		MetricsBindAddress:    value("METRICS_BIND_ADDRESS", "127.0.0.1:9091"),
		ShutdownTimeout:       shutdown,
		Database:              database,
	}
	if err := validateRuntime(runtimeConfig); err != nil {
		return Runtime{}, err
	}
	return runtimeConfig, nil
}

func loadDatabase() (Database, error) {
	port, err := integerValue("DB_PORT", "5432", 1, 65535)
	if err != nil {
		return Database{}, err
	}
	maxOpen, err := integerValue("DB_MAX_OPEN_CONNECTIONS", "30", 1, 1024)
	if err != nil {
		return Database{}, err
	}
	return databaseValues(uint16(port), int32(maxOpen))
}

func databaseValues(port uint16, maxOpen int32) (Database, error) {
	var err error
	database := Database{
		Host: value("DB_HOST", "127.0.0.1"), Port: port,
		Name: value("DB_NAME", "axiom"), User: value("DB_USER", "axiom_app"),
		PasswordFile: value("DB_PASSWORD_FILE", "/run/secrets/postgres_runtime_password"),
		SSLMode:      value("DB_SSL_MODE", "disable"), MaxOpenConnections: maxOpen,
		ConnectionMaxLifetime: mustDuration("DB_CONNECTION_MAX_LIFETIME", "30m", &err),
		ConnectionTimeout:     mustDuration("DB_CONNECTION_TIMEOUT", "5s", &err),
		StatementTimeout:      mustDuration("DB_STATEMENT_TIMEOUT", "10s", &err),
	}
	if err != nil {
		return Database{}, err
	}
	return database, nil
}

func validateRuntime(runtimeConfig Runtime) error {
	if runtimeConfig.InstanceID == "" || strings.ContainsAny(runtimeConfig.InstanceID, "\r\n") {
		return fmt.Errorf("invalid_configuration:APP_INSTANCE_ID")
	}
	addresses := []struct{ key, value string }{
		{key: "HTTP_BIND_ADDRESS", value: runtimeConfig.HTTPBindAddress},
		{key: "METRICS_BIND_ADDRESS", value: runtimeConfig.MetricsBindAddress},
	}
	for _, address := range addresses {
		if _, _, err := net.SplitHostPort(address.value); err != nil {
			return fmt.Errorf("invalid_configuration:%s", address.key)
		}
	}
	if net.ParseIP(runtimeConfig.Database.Host) == nil && strings.ContainsAny(runtimeConfig.Database.Host, "/:@?#") {
		return fmt.Errorf("invalid_configuration:DB_HOST")
	}
	if runtimeConfig.Database.Name == "" || runtimeConfig.Database.User == "" {
		return fmt.Errorf("invalid_configuration:DB_IDENTITY")
	}
	if !strings.HasPrefix(runtimeConfig.Database.PasswordFile, "/") {
		return fmt.Errorf("invalid_configuration:DB_PASSWORD_FILE")
	}
	switch runtimeConfig.Database.SSLMode {
	case "disable", "require", "verify-ca", "verify-full":
		return nil
	default:
		return fmt.Errorf("invalid_configuration:DB_SSL_MODE")
	}
}

func validateSafetyFlags() error {
	exact := []struct{ key, expected string }{
		{key: "APP_FAIL_CLOSED", expected: "true"},
		{key: "RISK_INITIAL_STATE", expected: "PAUSED"},
		{key: "RISK_AUTO_UNPAUSE", expected: "false"},
		{key: "RISK_FAIL_CLOSED", expected: "true"},
		{key: "BINANCE_PUBLIC_ENDPOINT_SET", expected: "market-data-only-v1"},
	}
	for _, item := range exact {
		if actual, found := os.LookupEnv(item.key); found && actual != item.expected {
			return fmt.Errorf("unsafe_configuration:%s", item.key)
		}
	}
	if mode, found := os.LookupEnv("EXECUTION_MODE"); found {
		if _, err := ParseExecutionMode(mode); err != nil {
			return err
		}
	}
	return nil
}

func value(key, fallback string) string {
	if current, found := os.LookupEnv(key); found {
		return current
	}
	return fallback
}

func durationValue(key, fallback string) (time.Duration, error) {
	parsed, err := time.ParseDuration(value(key, fallback))
	if err != nil {
		return 0, fmt.Errorf("invalid_configuration:%s", key)
	}
	return parsed, nil
}

func mustDuration(key, fallback string, prior *error) time.Duration {
	if *prior != nil {
		return 0
	}
	parsed, err := durationValue(key, fallback)
	if err != nil || parsed <= 0 {
		*prior = fmt.Errorf("invalid_configuration:%s", key)
	}
	return parsed
}

func integerValue(key, fallback string, minimum, maximum int) (int, error) {
	parsed, err := strconv.Atoi(value(key, fallback))
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("invalid_configuration:%s", key)
	}
	return parsed, nil
}
