package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
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

// AlertWebhook is the optional code-owned HTTPS alert sink wiring.
type AlertWebhook struct {
	Enabled     bool
	URL         string
	AllowedHost string
	TokenFile   string
}

// Tracing controls the optional bounded OTLP/HTTP exporter. Disabled tracing
// is a true no-op and enabling it requires an explicit HTTPS collector URL.
type Tracing struct {
	Enabled  bool
	Endpoint string
}

// RecorderRuntime is the bounded production-public recorder process contract.
type RecorderRuntime struct {
	Root          string
	FlushInterval time.Duration
	QueueCapacity int
	BookDepth     int
}

// Runtime is the immutable A1 process configuration.
type Runtime struct {
	DeploymentEnvironment string
	InstanceID            string
	HTTPBindAddress       string
	MetricsBindAddress    string
	HealthDetailTokenFile string
	AlertWebhook          AlertWebhook
	Tracing               Tracing
	ShutdownTimeout       time.Duration
	Database              Database
	Recorder              RecorderRuntime
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
	webhook, err := loadAlertWebhook()
	if err != nil {
		return Runtime{}, err
	}
	tracing, err := loadTracing()
	if err != nil {
		return Runtime{}, err
	}
	recorderRuntime, err := loadRecorderRuntime()
	if err != nil {
		return Runtime{}, err
	}
	runtimeConfig := Runtime{
		DeploymentEnvironment: value("DEPLOYMENT_ENV", "local"),
		InstanceID:            value("APP_INSTANCE_ID", "axiom-local-01"),
		HTTPBindAddress:       value("HTTP_BIND_ADDRESS", "127.0.0.1:8080"),
		MetricsBindAddress:    value("METRICS_BIND_ADDRESS", "127.0.0.1:9091"),
		HealthDetailTokenFile: value("HEALTH_DETAIL_TOKEN_FILE", "/run/secrets/health_detail_token"),
		AlertWebhook:          webhook,
		Tracing:               tracing,
		ShutdownTimeout:       shutdown,
		Database:              database,
		Recorder:              recorderRuntime,
	}
	if err := validateRuntime(runtimeConfig); err != nil {
		return Runtime{}, err
	}
	return runtimeConfig, nil
}

func loadRecorderRuntime() (RecorderRuntime, error) {
	flush, err := durationValue("RECORDER_FLUSH_INTERVAL", "5m")
	if err != nil || flush < time.Second || flush > time.Hour {
		return RecorderRuntime{}, fmt.Errorf("invalid_configuration:RECORDER_FLUSH_INTERVAL")
	}
	queue, err := integerValue("MARKET_EVENT_QUEUE_CAPACITY", "16384", 1000, 1<<20)
	if err != nil {
		return RecorderRuntime{}, err
	}
	depth, err := integerValue("ORDER_BOOK_RETAINED_DEPTH", "1000", 1, 5000)
	if err != nil || queue < depth {
		return RecorderRuntime{}, fmt.Errorf("invalid_configuration:ORDER_BOOK_RETAINED_DEPTH")
	}
	root := value("RECORDER_ROOT", "/var/lib/axiom/market-data")
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		return RecorderRuntime{}, fmt.Errorf("invalid_configuration:RECORDER_ROOT")
	}
	return RecorderRuntime{Root: filepath.Clean(root), FlushInterval: flush,
		QueueCapacity: queue, BookDepth: depth}, nil
}

func loadTracing() (Tracing, error) {
	enabled, err := booleanValue("OTEL_TRACING_ENABLED", false)
	if err != nil {
		return Tracing{}, err
	}
	tracing := Tracing{Enabled: enabled, Endpoint: value("OTEL_EXPORTER_OTLP_ENDPOINT", "")}
	if !enabled {
		return tracing, nil
	}
	endpoint, err := url.Parse(tracing.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return Tracing{}, fmt.Errorf("invalid_configuration:OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	return tracing, nil
}

func loadAlertWebhook() (AlertWebhook, error) {
	enabled, err := booleanValue("ALERT_WEBHOOK_ENABLED", false)
	if err != nil {
		return AlertWebhook{}, err
	}
	webhook := AlertWebhook{
		Enabled: enabled, URL: value("ALERT_WEBHOOK_URL", ""),
		AllowedHost: value("ALERT_WEBHOOK_ALLOWED_HOST", ""),
		TokenFile:   value("ALERT_WEBHOOK_TOKEN_FILE", ""),
	}
	if enabled && (webhook.URL == "" || webhook.AllowedHost == "") {
		return AlertWebhook{}, fmt.Errorf("invalid_configuration:ALERT_WEBHOOK")
	}
	if webhook.TokenFile != "" && !strings.HasPrefix(webhook.TokenFile, "/") {
		return AlertWebhook{}, fmt.Errorf("invalid_configuration:ALERT_WEBHOOK_TOKEN_FILE")
	}
	return webhook, nil
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
	if !strings.HasPrefix(runtimeConfig.HealthDetailTokenFile, "/") {
		return fmt.Errorf("invalid_configuration:HEALTH_DETAIL_TOKEN_FILE")
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

func booleanValue(key string, fallback bool) (bool, error) {
	raw, found := os.LookupEnv(key)
	if !found || raw == "" {
		return fallback, nil
	}
	if raw == "true" {
		return true, nil
	}
	if raw == "false" {
		return false, nil
	}
	return false, fmt.Errorf("invalid_configuration:%s", key)
}
