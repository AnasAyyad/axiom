package config

import "fmt"

// ExecutionMode identifies one closed V1 execution mode.
type ExecutionMode string

// V1 execution modes. Testnet and demo remain schema- and gate-constrained;
// live is absent in every release.
const (
	ModeBacktest ExecutionMode = "backtest"
	ModeReplay   ExecutionMode = "replay"
	ModePaper    ExecutionMode = "paper"
	ModeShadow   ExecutionMode = "shadow"
	ModeTestnet  ExecutionMode = "testnet"
	ModeDemo     ExecutionMode = "demo"
)

// ParseExecutionMode accepts only exact, lower-case V1 mode names.
func ParseExecutionMode(value string) (ExecutionMode, error) {
	switch ExecutionMode(value) {
	case ModeBacktest, ModeReplay, ModePaper, ModeShadow, ModeTestnet, ModeDemo:
		return ExecutionMode(value), nil
	default:
		return "", fmt.Errorf("execution_mode_rejected")
	}
}
