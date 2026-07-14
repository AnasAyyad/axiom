package bootstrap

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"axiom/internal/config"
	"axiom/internal/domain"
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
		_, _ = fmt.Fprintf(errorOutput, "axiom_start_failed:%s\n", err.Error())
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
	logger := slog.New(slog.NewJSONHandler(errorOutput, &slog.HandlerOptions{Level: slog.LevelInfo}))
	switch command.Kind {
	case commandAPI:
		return runHTTPRole(ctx, runtimeConfig, "api", true, logger)
	case commandTrader:
		return runHTTPRole(ctx, runtimeConfig, "engine-shadow", false, logger)
	case commandRecorder:
		return runHTTPRole(ctx, runtimeConfig, "recorder", false, logger)
	case commandWorker:
		return runHTTPRole(ctx, runtimeConfig, "worker", false, logger)
	case commandMigrate:
		return runMigrate(ctx, runtimeConfig, output)
	default:
		return errUsage
	}
}
