package bootstrap

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"axiom/internal/config"
)

// CommandKind is one exact platform subcommand surface.
type CommandKind string

const (
	commandAPI         CommandKind = "api"
	commandTrader      CommandKind = "trader"
	commandRecorder    CommandKind = "recorder"
	commandWorker      CommandKind = "worker"
	commandMigrate     CommandKind = "admin_migrate"
	commandHealthcheck CommandKind = "healthcheck"
)

// Command is validated local intent; it owns no business behavior.
type Command struct {
	Kind CommandKind
	Mode config.ExecutionMode
	URL  string
}

var errUsage = errors.New("invalid_command")

func parseCommand(arguments []string) (Command, error) {
	if len(arguments) == 0 {
		return Command{}, errUsage
	}
	switch arguments[0] {
	case "api":
		return commandWithoutArguments(commandAPI, arguments[1:])
	case "recorder":
		return commandWithoutArguments(commandRecorder, arguments[1:])
	case "worker":
		return commandWithoutArguments(commandWorker, arguments[1:])
	case "trader":
		return parseTrader(arguments[1:])
	case "admin":
		if len(arguments) == 2 && arguments[1] == "migrate" {
			return Command{Kind: commandMigrate}, nil
		}
		return Command{}, errUsage
	case "healthcheck":
		return parseHealthcheck(arguments[1:])
	case "help", "--help", "-h":
		return Command{}, flag.ErrHelp
	default:
		return Command{}, errUsage
	}
}

func commandWithoutArguments(kind CommandKind, arguments []string) (Command, error) {
	if len(arguments) != 0 {
		return Command{}, errUsage
	}
	return Command{Kind: kind}, nil
}

func parseTrader(arguments []string) (Command, error) {
	flags := flag.NewFlagSet("trader", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	modeValue := flags.String("mode", "", "credential-free V1A mode")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return Command{}, errUsage
	}
	mode, err := config.ParseExecutionMode(*modeValue)
	if err != nil || mode != config.ModeShadow {
		return Command{}, fmt.Errorf("trader_mode_rejected")
	}
	return Command{Kind: commandTrader, Mode: mode}, nil
}

func parseHealthcheck(arguments []string) (Command, error) {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	urlValue := flags.String("url", "http://127.0.0.1:8080/health/live", "local health URL")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return Command{}, errUsage
	}
	if err := validateHealthURL(*urlValue); err != nil {
		return Command{}, err
	}
	return Command{Kind: commandHealthcheck, URL: *urlValue}, nil
}

func writeUsage(writer io.Writer) {
	_, _ = io.WriteString(writer, `Axiom platform

Usage:
  platform api
  platform trader --mode shadow
  platform recorder
  platform worker
  platform admin migrate
  platform healthcheck [--url http://127.0.0.1:8080/health/live]

V1A contains no authenticated exchange or external-order command.
`)
}
