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
	commandAPI           CommandKind = "api"
	commandTrader        CommandKind = "trader"
	commandRecorder      CommandKind = "recorder"
	commandWorker        CommandKind = "worker"
	commandMigrate       CommandKind = "admin_migrate"
	commandHealthcheck   CommandKind = "healthcheck"
	commandEgressProxy   CommandKind = "egress_proxy"
	commandSandboxEngine CommandKind = "sandbox_engine"
	commandSandboxCanary CommandKind = "sandbox_canary"
)

// Command is validated local intent; it owns no business behavior.
type Command struct {
	Kind              CommandKind
	Mode              config.ExecutionMode
	URL               string
	Exchange          string
	Phase             string
	InputFile         string
	CanaryID          string
	EvidenceDirectory string
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
	case "egress-proxy":
		return parseEgressProxy(arguments[1:])
	case "sandbox-engine":
		return parseSandboxEngine(arguments[1:])
	case "sandbox-canary":
		return parseSandboxCanary(arguments[1:])
	case "help", "--help", "-h":
		return Command{}, flag.ErrHelp
	default:
		return Command{}, errUsage
	}
}

func parseSandboxCanary(arguments []string) (Command, error) {
	flags := flag.NewFlagSet("sandbox-canary", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	exchange := flags.String("exchange", "", "closed sandbox exchange")
	phase := flags.String("phase", "", "prepare, recover, verify, or abort")
	inputFile := flags.String("input-file", "", "protected prepare request file")
	canaryID := flags.String("canary-id", "", "prepared canary identity")
	evidenceDirectory := flags.String(
		"evidence-dir",
		"",
		"absolute immutable evidence output directory",
	)
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return Command{}, errUsage
	}
	mode := config.ModeTestnet
	if *exchange == "bybit" {
		mode = config.ModeDemo
	} else if *exchange != "binance" {
		return Command{}, errUsage
	}
	command := Command{
		Kind: commandSandboxCanary, Mode: mode, Exchange: *exchange,
		Phase: *phase, InputFile: *inputFile, CanaryID: *canaryID,
		EvidenceDirectory: *evidenceDirectory,
	}
	if (*phase == "prepare" && *inputFile != "" && *canaryID == "" &&
		*evidenceDirectory == "") ||
		(*phase == "recover" && *inputFile == "" && *canaryID != "" &&
			*evidenceDirectory == "") ||
		(*phase == "verify" && *inputFile == "" && *canaryID != "" &&
			*evidenceDirectory != "") ||
		(*phase == "abort" && *inputFile == "" && *canaryID != "" &&
			*evidenceDirectory == "") {
		return command, nil
	}
	return Command{}, errUsage
}

func parseSandboxEngine(arguments []string) (Command, error) {
	flags := flag.NewFlagSet("sandbox-engine", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	exchange := flags.String(
		"exchange",
		"",
		"closed authenticated sandbox exchange",
	)
	if err := flags.Parse(arguments); err != nil ||
		flags.NArg() != 0 {
		return Command{}, errUsage
	}
	switch *exchange {
	case "binance":
		return Command{
			Kind:     commandSandboxEngine,
			Mode:     config.ModeTestnet,
			Exchange: "binance",
		}, nil
	case "bybit":
		return Command{
			Kind:     commandSandboxEngine,
			Mode:     config.ModeDemo,
			Exchange: "bybit",
		}, nil
	default:
		return Command{}, errUsage
	}
}

func parseEgressProxy(arguments []string) (Command, error) {
	flags := flag.NewFlagSet("egress-proxy", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	exchange := flags.String("exchange", "", "closed sandbox exchange policy")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 ||
		(*exchange != "binance" && *exchange != "bybit") {
		return Command{}, errUsage
	}
	return Command{Kind: commandEgressProxy, Exchange: *exchange}, nil
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
  platform egress-proxy --exchange binance|bybit
  platform sandbox-engine --exchange binance|bybit
  platform sandbox-canary --exchange binance|bybit --phase prepare --input-file /absolute/request.json
  platform sandbox-canary --exchange binance|bybit --phase recover --canary-id ID
  platform sandbox-canary --exchange binance|bybit --phase verify --canary-id ID --evidence-dir /absolute/directory
  platform sandbox-canary --exchange binance|bybit --phase abort --canary-id ID

The egress proxy is CONNECT-only and has a compile-time destination policy.
`)
}
