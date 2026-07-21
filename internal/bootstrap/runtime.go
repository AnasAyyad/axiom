package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"axiom/internal/alerting"
	"axiom/internal/api"
	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/api/health"
	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/observability"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/security"
	postgresstore "axiom/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func runHTTPRole(ctx context.Context, runtimeConfig config.Runtime, product config.Configuration, role string, includeUI bool, logger *slog.Logger) error {
	observability.ConfigureTraceErrorHandler(logger)
	pool, err := postgresstore.Open(ctx, runtimeConfig.Database)
	if err != nil {
		return err
	}
	defer pool.Close()

	state := &lifecycleState{}
	services, err := newRoleServices(ctx, pool, runtimeConfig, product, role, state)
	if err != nil {
		return err
	}
	work, err := workForRole(ctx, pool, runtimeConfig, product, role)
	if err != nil {
		return err
	}
	servers, serveErrors, err := startRoleServers(runtimeConfig, includeUI, services)
	if err != nil {
		return err
	}
	tracedContext, span := services.tracing.Tracer("axiom/bootstrap").Start(
		ctx, "role.lifecycle", trace.WithAttributes(attribute.String("axiom.role", role)),
	)
	roleContext, cancelRole := context.WithCancel(tracedContext)
	defer cancelRole()
	workState, serveErrors := attachRoleWork(roleContext, work, serveErrors, logger)
	go markReady(roleContext, services.health.Dependency, state, services.metrics, services.alerts, logger, role, workState.ready)
	if services.sink != nil {
		go retryAlertDeliveries(roleContext, services.alerts, logger)
	}
	logger.Info("service_started", "event_code", "service_started", "role", role)
	runErr := awaitShutdown(roleContext, servers, state, serveErrors, runtimeConfig.ShutdownTimeout, logger, role)
	cancelRole()
	runErr = workState.wait(runErr)
	span.End()
	traceContext, cancel := context.WithTimeout(context.Background(), traceExportTimeout)
	defer cancel()
	if err := services.tracing.Shutdown(traceContext); err != nil {
		logger.Warn("trace shutdown incomplete", "event_code", "trace_shutdown_incomplete", "cause", err)
	}
	return runErr
}

type roleWork interface {
	Run(context.Context, *slog.Logger) error
	Ready() bool
}

func workForRole(
	ctx context.Context,
	pool *pgxpool.Pool,
	runtimeConfig config.Runtime,
	product config.Configuration,
	role string,
) (roleWork, error) {
	switch role {
	case "recorder":
		return newRecorderRoleWork(ctx, pool, runtimeConfig, product, &domain.SystemClock{})
	case "worker":
		return newA11WorkerRoleWork(pool, runtimeConfig)
	case "engine-shadow":
		return newA11LiveShadowRoleWork(pool, runtimeConfig)
	default:
		return nil, nil
	}
}

type roleWorkState struct {
	ready    func() bool
	finished <-chan struct{}
	result   *error
}

func attachRoleWork(
	ctx context.Context,
	work roleWork,
	serveErrors <-chan error,
	logger *slog.Logger,
) (roleWorkState, <-chan error) {
	if work == nil {
		return roleWorkState{}, serveErrors
	}
	workErrors, finished := make(chan error, 1), make(chan struct{})
	var result error
	go func() {
		result = work.Run(ctx, logger)
		if result != nil {
			logger.Error("role work failed", "event_code", "role_work_failed", "cause", result)
		}
		workErrors <- result
		close(finished)
	}()
	return roleWorkState{ready: work.Ready, finished: finished, result: &result}, mergeRoleErrors(serveErrors, workErrors)
}

func (state roleWorkState) wait(runErr error) error {
	if state.finished == nil {
		return runErr
	}
	<-state.finished
	if *state.result != nil && runErr == nil {
		return fmt.Errorf("role_work_failed")
	}
	return runErr
}

func mergeRoleErrors(left, right <-chan error) <-chan error {
	merged := make(chan error, 2)
	go func() { merged <- <-left }()
	go func() { merged <- <-right }()
	return merged
}

const traceExportTimeout = 5 * time.Second

type roleServices struct {
	health  health.Options
	metrics *observability.Metrics
	alerts  *alerting.Service
	sink    alerting.Sink
	tracing *observability.Tracing
	console *console.Options
}

func newRoleServices(ctx context.Context, pool *pgxpool.Pool, runtimeConfig config.Runtime, product config.Configuration, role string, state *lifecycleState) (roleServices, error) {
	healthToken, err := security.ReadSecretFile(runtimeConfig.HealthDetailTokenFile)
	if err != nil {
		return roleServices{}, fmt.Errorf("health_detail_secret_unavailable: %w", err)
	}
	authorize, err := health.NewBearerAuthorizer(healthToken)
	if err != nil {
		return roleServices{}, err
	}
	options := health.Options{
		Role: role, Build: buildinfo.Current(), Dependency: pool, Lifecycle: state.current, Authorize: authorize,
	}
	var consoleOptions *console.Options
	if role == "api" {
		setup := setupA11Console(ctx, pool, runtimeConfig)
		options.Dependency = setup.dependency
		options.Phase = generated.HealthResponsePhaseA11
		consoleOptions = setup.options
	}
	metrics, err := observability.NewMetrics(role, metricCatalog(product))
	if err != nil {
		return roleServices{}, fmt.Errorf("metrics_configuration_invalid")
	}
	alertStore, err := postgresstore.NewAlertStore(pool)
	if err != nil {
		return roleServices{}, err
	}
	sink, err := newAlertSink(runtimeConfig.AlertWebhook)
	if err != nil {
		return roleServices{}, err
	}
	gate := runtimecore.NewSafetyGate()
	alertService, err := alerting.NewService(alertStore, sink, gate, nil)
	if err != nil {
		return roleServices{}, err
	}
	tracing, err := observability.NewTracing(ctx, observability.TracingConfiguration{
		Enabled: runtimeConfig.Tracing.Enabled, Endpoint: runtimeConfig.Tracing.Endpoint,
		Service: role, InstanceID: runtimeConfig.InstanceID,
	})
	if err != nil {
		return roleServices{}, err
	}
	return roleServices{health: options, metrics: metrics, alerts: alertService, sink: sink, tracing: tracing, console: consoleOptions}, nil
}

func newAlertSink(configuration config.AlertWebhook) (alerting.Sink, error) {
	if !configuration.Enabled {
		return nil, nil
	}
	token := ""
	var err error
	if configuration.TokenFile != "" {
		token, err = security.ReadSecretFile(configuration.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("alert_webhook_secret_unavailable: %w", err)
		}
	}
	return alerting.NewWebhookSink(configuration.URL, token, []string{configuration.AllowedHost}, nil)
}

func startRoleServers(runtimeConfig config.Runtime, includeUI bool, services roleServices) ([]*http.Server, <-chan error, error) {
	operationalServer := newHTTPServer(runtimeConfig.MetricsBindAddress, api.NewOperationalRouter(services.health, services.metrics.Handler()))
	servers := []*http.Server{operationalServer}
	if includeUI {
		if services.console != nil {
			servers = append(servers, newHTTPServer(runtimeConfig.HTTPBindAddress, api.NewRouter(services.health, *services.console)))
		} else {
			servers = append(servers, newHTTPServer(runtimeConfig.HTTPBindAddress, api.NewRouter(services.health)))
		}
	}
	listeners := make([]net.Listener, 0, len(servers))
	for _, server := range servers {
		listener, listenErr := net.Listen("tcp", server.Addr)
		if listenErr != nil {
			for _, opened := range listeners {
				_ = opened.Close()
			}
			return nil, nil, fmt.Errorf("http_listener_unavailable")
		}
		listeners = append(listeners, listener)
	}

	serveErrors := make(chan error, len(servers))
	for index := range servers {
		server, listener := servers[index], listeners[index]
		go func() { serveErrors <- server.Serve(listener) }()
	}
	return servers, serveErrors, nil
}

func retryAlertDeliveries(ctx context.Context, alerts *alerting.Service, logger *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := alerts.RetryDue(ctx, 25); err != nil {
				logger.Error("alert delivery retry failed", "event_code", "alert_delivery_retry_failed", "cause", err)
			}
		}
	}
}

func metricCatalog(product config.Configuration) observability.MetricCatalog {
	instruments := make([]string, 0, len(product.Instruments))
	for _, instrument := range product.Instruments {
		instruments = append(instruments, instrument.Base+instrument.Quote)
	}
	return observability.MetricCatalog{
		Exchanges: []string{"binance"}, Instruments: instruments,
		Strategies: []string{"trend"}, Modes: []string{string(product.Mode)},
	}
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: address, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
}

func markReady(
	ctx context.Context,
	dependency health.Dependency,
	state *lifecycleState,
	metrics *observability.Metrics,
	alerts *alerting.Service,
	logger *slog.Logger,
	role string,
	additionalReady func() bool,
) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	alertRecorded := false
	faultPending := false
	for {
		pingContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := dependency.Ping(pingContext)
		cancel()
		ready := err == nil && (additionalReady == nil || additionalReady())
		_ = metrics.SetDependencyReady("postgres", ready)
		if ready {
			state.ready()
			if faultPending && !alertRecorded {
				alertRecorded = recordPersistenceAlert(ctx, alerts, logger, role)
			}
			if faultPending && alertRecorded {
				faultPending, alertRecorded = false, false
			}
		} else {
			state.notReady()
			faultPending = true
			if !alertRecorded {
				alertRecorded = recordPersistenceAlert(ctx, alerts, logger, role)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func recordPersistenceAlert(ctx context.Context, alerts *alerting.Service, logger *slog.Logger, role string) bool {
	now := time.Now().UTC()
	stored, err := alerts.Trigger(ctx, alerting.Fault{
		Severity: alerting.SeverityCritical, Reason: alerting.ReasonPersistenceFailure,
		Component: role, CorrelationID: fmt.Sprintf("health-ping-%d", now.Unix()), OccurredAt: now,
	})
	if err != nil {
		logger.Error("critical dependency alert failed", "event_code", "alert_persistence_failed", "cause", err)
	}
	return stored.ID != ""
}

func awaitShutdown(
	ctx context.Context,
	servers []*http.Server,
	state *lifecycleState,
	serveErrors <-chan error,
	timeout time.Duration,
	logger *slog.Logger,
	role string,
) error {
	var serveErr error
	select {
	case serveErr = <-serveErrors:
	case <-ctx.Done():
	}
	state.stopping()
	shutdownContext, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for _, server := range servers {
		if err := server.Shutdown(shutdownContext); err != nil {
			for _, openServer := range servers {
				_ = openServer.Close()
			}
			return fmt.Errorf("graceful_shutdown_failed")
		}
	}
	logger.Info("service_stopped", "event_code", "service_stopped", "role", role)
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("http_server_failed")
	}
	return nil
}
