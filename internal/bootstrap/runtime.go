package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"axiom/internal/api"
	"axiom/internal/api/health"
	"axiom/internal/buildinfo"
	"axiom/internal/config"
	postgresstore "axiom/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

func runHTTPRole(ctx context.Context, runtimeConfig config.Runtime, role string, includeUI bool, logger *slog.Logger) error {
	pool, err := postgresstore.Open(ctx, runtimeConfig.Database)
	if err != nil {
		return err
	}
	defer pool.Close()

	state := &lifecycleState{}
	options := health.Options{
		Role: role, Build: buildinfo.Current(), Dependency: pool, Lifecycle: state.current,
	}
	handler := api.NewHealthRouter(options)
	address := runtimeConfig.MetricsBindAddress
	if includeUI {
		handler = api.NewRouter(options)
		address = runtimeConfig.HTTPBindAddress
	}
	server := newHTTPServer(address, handler)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("http_listener_unavailable")
	}

	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	go markReady(ctx, pool, state)
	logger.Info("service_started", "event_code", "service_started", "role", role)
	return awaitShutdown(ctx, server, state, serveErrors, runtimeConfig.ShutdownTimeout, logger, role)
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: address, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
}

func markReady(ctx context.Context, pool *pgxpool.Pool, state *lifecycleState) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		pingContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := pool.Ping(pingContext)
		cancel()
		if err == nil {
			state.ready()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func awaitShutdown(
	ctx context.Context,
	server *http.Server,
	state *lifecycleState,
	serveErrors <-chan error,
	timeout time.Duration,
	logger *slog.Logger,
	role string,
) error {
	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("http_server_failed")
	case <-ctx.Done():
	}
	state.stopping()
	shutdownContext, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		_ = server.Close()
		return fmt.Errorf("graceful_shutdown_failed")
	}
	logger.Info("service_stopped", "event_code", "service_stopped", "role", role)
	return nil
}
