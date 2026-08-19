package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"axiom/internal/config"
	"axiom/internal/qualification/operationalreadiness"
	postgresstore "axiom/internal/storage/postgres"
)

type settings struct {
	healthcheck bool
	once        bool
	output      string
	status      string
	lifecycle   string
	drill       string
	interval    time.Duration
	window      time.Duration
	database    config.Database
	metrics     []operationalReadiness.RuntimeMetricTarget
	health      []operationalReadiness.RuntimeHealthTarget
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "operational_readiness_observer:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	configuration, err := loadSettings(arguments)
	if err != nil {
		return err
	}
	if configuration.healthcheck {
		if _, err = (&operationalReadiness.FileProbe{Path: configuration.output, StatusPath: configuration.status}).Observe(
			ctx, 0, time.Now().UTC(),
		); err != nil {
			return fmt.Errorf("observer_sample_unhealthy")
		}
		return nil
	}
	lifecycle, err := openObserverLifecycle(configuration.lifecycle)
	if err != nil {
		return err
	}
	defer lifecycle.close()
	startedAt := time.Now().UTC()
	if err = lifecycle.emit(observerLifecycleEvent{OccurredAt: startedAt, Event: "observer_started"}); err != nil {
		return err
	}
	defer func() {
		_ = lifecycle.emit(observerLifecycleEvent{OccurredAt: time.Now().UTC(), Event: "observer_stopped"})
	}()
	pool, err := postgresstore.Open(ctx, configuration.database)
	if err != nil {
		_ = lifecycle.emit(observerLifecycleEvent{OccurredAt: time.Now().UTC(), Event: "startup_failed",
			Source: "database", Stage: "connection_open", Reason: "unavailable", Retryable: true})
		return fmt.Errorf("observer_database_unavailable")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		_ = lifecycle.emit(observerLifecycleEvent{OccurredAt: time.Now().UTC(), Event: "startup_failed",
			Source: "database", Stage: "connection_ping", Reason: "unavailable", Retryable: true})
		return fmt.Errorf("observer_database_unavailable")
	}
	observer := operationalReadiness.LiveObserver{
		Database: operationalReadiness.PostgresTelemetrySource{Pool: pool},
		Runtime: operationalReadiness.HTTPRuntimeTelemetrySource{
			MetricTargets: configuration.metrics, HealthTargets: configuration.health,
		},
		Drill:  operationalReadiness.FileDrillObservation{Path: configuration.drill},
		Window: configuration.window,
	}
	revision, err := operationalReadiness.NextLiveSampleRevision(configuration.output)
	if err != nil {
		return err
	}
	var attempt, consecutiveFailures, failureCount, recoveryCount, lastOutageMillis uint64
	var lastSuccessAt time.Time
	var outageStartedAt time.Time
	var lastFailure operationalReadiness.SourceFailure
	for {
		attempt++
		observedAt := time.Now().UTC()
		attemptStarted := time.Now()
		if err = lifecycle.emit(observerLifecycleEvent{OccurredAt: observedAt, Event: "observation_started",
			Attempt: attempt, SourceRevision: revision, ConsecutiveFailures: consecutiveFailures,
			LastSuccessAt: lastSuccessAt}); err != nil {
			return err
		}
		sample, observeErr := observer.Observe(ctx, revision, observedAt)
		if observeErr == nil {
			if consecutiveFailures > 0 {
				recoveryCount++
				lastOutageMillis = uint64(time.Since(outageStartedAt).Milliseconds())
				if err = lifecycle.emit(observerLifecycleEvent{OccurredAt: time.Now().UTC(), Event: "source_recovered",
					Attempt: attempt, SourceRevision: revision, ConsecutiveFailures: consecutiveFailures,
					Source: lastFailure.Source, Stage: lastFailure.Stage, Role: lastFailure.Role, Reason: lastFailure.Reason,
					DurationMillis: lastOutageMillis, LastSuccessAt: lastSuccessAt}); err != nil {
					return err
				}
			}
			if err = lifecycle.emit(observerLifecycleEvent{OccurredAt: time.Now().UTC(), Event: "observation_succeeded",
				Attempt: attempt, SourceRevision: revision,
				DurationMillis: uint64(time.Since(attemptStarted).Milliseconds())}); err != nil {
				return err
			}
			sample.ObserverLifecycleHash = lifecycle.headHash
			sample.ObserverAttempt = attempt
			sample.ObserverFailureCount = failureCount
			sample.ObserverRecoveryCount = recoveryCount
			sample.ObserverLastOutageMillis = lastOutageMillis
			observeErr = operationalReadiness.WriteLiveSample(configuration.output, sample)
		}
		if observeErr == nil {
			lastSuccessAt = observedAt
			consecutiveFailures = 0
			if err = lifecycle.emit(observerLifecycleEvent{OccurredAt: time.Now().UTC(), Event: "sample_published",
				Attempt: attempt, SourceRevision: revision,
				DurationMillis: uint64(time.Since(attemptStarted).Milliseconds()), LastSuccessAt: lastSuccessAt}); err != nil {
				return err
			}
			if err = operationalReadiness.WriteObserverStatus(configuration.status, operationalReadiness.ObserverStatus{
				SchemaVersion: operationalReadiness.ObserverStatusSchema, UpdatedAt: time.Now().UTC(),
				LastAttemptAt: observedAt, LastSuccessAt: lastSuccessAt, PublishedRevision: revision,
				Attempt: attempt, FailureCount: failureCount, RecoveryCount: recoveryCount,
				LastOutageMillis: lastOutageMillis, LifecycleHeadHash: lifecycle.headHash,
			}); err != nil {
				return err
			}
			revision++
		} else {
			consecutiveFailures++
			failureCount++
			if consecutiveFailures == 1 {
				outageStartedAt = observedAt
			}
			failure, known := operationalReadiness.SourceFailureDetails(observeErr)
			if !known {
				failure = operationalReadiness.SourceFailure{Source: "observer", Stage: "sample_write",
					Reason: "write_failed", Retryable: false}
			}
			lastFailure = failure
			if err = lifecycle.emit(observerLifecycleEvent{OccurredAt: time.Now().UTC(), Event: "observation_failed",
				Attempt: attempt, SourceRevision: revision, Source: failure.Source, Stage: failure.Stage,
				Role: failure.Role, Reason: failure.Reason, Retryable: failure.Retryable,
				ConsecutiveFailures: consecutiveFailures,
				DurationMillis:      uint64(time.Since(attemptStarted).Milliseconds()), LastSuccessAt: lastSuccessAt}); err != nil {
				return err
			}
			if err = operationalReadiness.WriteObserverStatus(configuration.status, operationalReadiness.ObserverStatus{
				SchemaVersion: operationalReadiness.ObserverStatusSchema, UpdatedAt: time.Now().UTC(),
				LastAttemptAt: observedAt, LastSuccessAt: lastSuccessAt, PublishedRevision: revision - 1,
				Attempt: attempt, ConsecutiveFailures: consecutiveFailures,
				FailureCount: failureCount, RecoveryCount: recoveryCount, LastOutageMillis: lastOutageMillis, LastFailure: &failure,
				LifecycleHeadHash: lifecycle.headHash,
			}); err != nil {
				return err
			}
			if configuration.once || !failure.Retryable {
				return observeErr
			}
		}
		if configuration.once {
			return nil
		}
		timer := time.NewTimer(configuration.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func loadSettings(arguments []string) (settings, error) {
	flags := flag.NewFlagSet("operational-readiness-observer", flag.ContinueOnError)
	once := flags.Bool("once", false, "write exactly one fresh sample")
	healthcheck := flags.Bool("healthcheck", false, "validate the latest sample without changing it")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return settings{}, fmt.Errorf("observer_arguments_invalid")
	}
	interval, err := time.ParseDuration(environment("AXIOM_OPERATIONAL_READINESS_OBSERVER_INTERVAL", "15s"))
	if err != nil || interval < 5*time.Second || interval > time.Minute {
		return settings{}, fmt.Errorf("observer_interval_invalid")
	}
	window, err := time.ParseDuration(environment("AXIOM_OPERATIONAL_READINESS_OBSERVER_WINDOW", "1h"))
	if err != nil || window <= 0 || window > 24*time.Hour {
		return settings{}, fmt.Errorf("observer_window_invalid")
	}
	port, err := strconv.ParseUint(environment("DB_PORT", "5432"), 10, 16)
	if err != nil || port == 0 {
		return settings{}, fmt.Errorf("observer_database_configuration_invalid")
	}
	configuration := settings{
		healthcheck: *healthcheck, once: *once,
		output:    os.Getenv("AXIOM_OPERATIONAL_READINESS_SAMPLE_FILE"),
		status:    os.Getenv("AXIOM_OPERATIONAL_READINESS_OBSERVER_STATUS_FILE"),
		lifecycle: os.Getenv("AXIOM_OPERATIONAL_READINESS_OBSERVER_LIFECYCLE_FILE"),
		drill:     os.Getenv("AXIOM_OPERATIONAL_READINESS_DRILL_OBSERVATION_FILE"),
		interval:  interval, window: window,
		database: config.Database{
			Host: environment("DB_HOST", "postgres"), Port: uint16(port), Name: environment("DB_NAME", "axiom"),
			User:         environment("DB_USER", "axiom_operational_readiness"),
			PasswordFile: environment("DB_PASSWORD_FILE", "/run/secrets/postgres_operational_readiness_password"),
			SSLMode:      environment("DB_SSL_MODE", "disable"), MaxOpenConnections: 2,
			ConnectionMaxLifetime: 30 * time.Minute, ConnectionTimeout: 5 * time.Second,
			StatementTimeout: 10 * time.Second,
		},
	}
	if configuration.output == "" || configuration.status == "" || configuration.lifecycle == "" || configuration.drill == "" {
		return settings{}, fmt.Errorf("observer_file_configuration_invalid")
	}
	configuration.metrics, err = metricTargets()
	if err != nil {
		return settings{}, err
	}
	configuration.health = healthTargets()
	return configuration, nil
}

func metricTargets() ([]operationalReadiness.RuntimeMetricTarget, error) {
	definitions := []struct {
		role, host, limitEnvironment, defaultLimit string
	}{
		{"api", "api", "AXIOM_OPERATIONAL_READINESS_API_MEMORY_LIMIT", "1g"},
		{"engine-shadow", "engine-shadow", "AXIOM_OPERATIONAL_READINESS_ENGINE_MEMORY_LIMIT", "2g"},
		{"recorder", "recorder", "AXIOM_OPERATIONAL_READINESS_RECORDER_MEMORY_LIMIT", "2g"},
		{"backtest-worker", "backtest-worker", "AXIOM_OPERATIONAL_READINESS_WORKER_MEMORY_LIMIT", "4g"},
		{"binance-sandbox-engine", "binance-sandbox-engine", "AXIOM_OPERATIONAL_READINESS_BINANCE_MEMORY_LIMIT", "1g"},
		{"bybit-sandbox-engine", "bybit-sandbox-engine", "AXIOM_OPERATIONAL_READINESS_BYBIT_MEMORY_LIMIT", "1g"},
	}
	result := make([]operationalReadiness.RuntimeMetricTarget, 0, len(definitions))
	for _, definition := range definitions {
		limit, err := parseByteSize(environment(definition.limitEnvironment, definition.defaultLimit))
		if err != nil || limit == 0 {
			return nil, fmt.Errorf("observer_memory_limit_invalid")
		}
		result = append(result, operationalReadiness.RuntimeMetricTarget{
			Role: definition.role, URL: "http://" + definition.host + ":9091/metrics", MemoryLimitBytes: limit,
		})
	}
	return result, nil
}

func parseByteSize(value string) (uint64, error) {
	if value == "" {
		return 0, fmt.Errorf("byte_size_invalid")
	}
	multiplier := uint64(1)
	suffix := value[len(value)-1]
	switch suffix {
	case 'k', 'K':
		multiplier, value = 1<<10, value[:len(value)-1]
	case 'm', 'M':
		multiplier, value = 1<<20, value[:len(value)-1]
	case 'g', 'G':
		multiplier, value = 1<<30, value[:len(value)-1]
	}
	base, err := strconv.ParseUint(value, 10, 64)
	if err != nil || base == 0 || base > ^uint64(0)/multiplier {
		return 0, fmt.Errorf("byte_size_invalid")
	}
	return base * multiplier, nil
}

func healthTargets() []operationalReadiness.RuntimeHealthTarget {
	return []operationalReadiness.RuntimeHealthTarget{
		{Role: "api", URL: "http://api:8080/health/ready"},
		{Role: "engine-shadow", URL: "http://engine-shadow:9091/health/ready"},
		{Role: "recorder", URL: "http://recorder:9091/health/ready"},
		{Role: "backtest-worker", URL: "http://backtest-worker:9091/health/ready"},
		{Role: "binance-sandbox-engine", URL: "http://binance-sandbox-engine:9091/health/ready"},
		{Role: "bybit-sandbox-engine", URL: "http://bybit-sandbox-engine:9091/health/ready"},
		{Role: "prometheus", URL: "http://prometheus:9090/-/ready"},
		{Role: "grafana", URL: "http://grafana:3000/api/health"},
	}
}

func environment(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
