package bootstrap

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/egressproxy"
	"axiom/internal/observability"
)

// Main installs process-wide signal cancellation and returns a shell exit code.
func Main(arguments []string, output, errorOutput io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := Run(ctx, arguments, output, errorOutput); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			writeUsage(output)
			return 0
		}
		observability.NewLogger(errorOutput, "platform").Error(
			"startup failed", "event_code", "startup_failed", "cause", err,
		)
		return 1
	}
	return 0
}

// Run validates safety and dispatches exactly one platform subcommand.
func Run(ctx context.Context, arguments []string, output, errorOutput io.Writer) error {
	if err := config.ValidateEnvironment(os.Environ()); err != nil {
		return err
	}
	command, err := parseCommand(arguments)
	if err != nil {
		if errors.Is(err, errUsage) {
			writeUsage(errorOutput)
		}
		return err
	}
	if command.Kind == commandHealthcheck {
		return runHealthcheck(ctx, command.URL)
	}
	if command.Kind == commandEgressProxy {
		return runEgressProxy(ctx, command.Exchange)
	}
	productConfiguration, source, err := config.LoadProductConfiguration(command.Mode)
	if err != nil {
		return err
	}
	productClock := &domain.SystemClock{}
	if _, err := config.NewSnapshot(productConfiguration, source, "process-startup", productClock); err != nil {
		return err
	}
	runtimeConfig, err := config.LoadRuntime()
	if err != nil {
		return err
	}
	switch command.Kind {
	case commandAPI:
		return runHTTPRole(ctx, runtimeConfig, productConfiguration, "api", true, observability.NewLogger(errorOutput, "api"))
	case commandTrader:
		return runHTTPRole(ctx, runtimeConfig, productConfiguration, "engine-shadow", false, observability.NewLogger(errorOutput, "engine-shadow"))
	case commandRecorder:
		return runHTTPRole(ctx, runtimeConfig, productConfiguration, "recorder", false, observability.NewLogger(errorOutput, "recorder"))
	case commandWorker:
		return runHTTPRole(ctx, runtimeConfig, productConfiguration, "worker", false, observability.NewLogger(errorOutput, "worker"))
	case commandMigrate:
		return runMigrate(ctx, runtimeConfig, productConfiguration, output)
	default:
		return errUsage
	}
}

func runEgressProxy(ctx context.Context, exchange string) error {
	policy := egressproxy.PolicyBinanceTestnet
	if exchange == "bybit" {
		policy = egressproxy.PolicyBybitDemo
	}
	handler, err := egressproxy.New(policy)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              "0.0.0.0:8080",
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	stopped := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		stopped <- err
	}()
	select {
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return fmt.Errorf("egress_proxy_shutdown_failed")
		}
		return <-stopped
	case err := <-stopped:
		return err
	}
}
