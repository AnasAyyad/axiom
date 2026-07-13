package config

import "fmt"

// ExecutionMode identifies one credential-free V1A research mode.
type ExecutionMode string

// V1A execution modes never submit an external exchange order.
const (
	ModeBacktest ExecutionMode = "backtest"
	ModeReplay   ExecutionMode = "replay"
	ModePaper    ExecutionMode = "paper"
	ModeShadow   ExecutionMode = "shadow"
)

// ParseExecutionMode accepts only exact, lower-case V1A mode names.
func ParseExecutionMode(value string) (ExecutionMode, error) {
	switch ExecutionMode(value) {
	case ModeBacktest, ModeReplay, ModePaper, ModeShadow:
		return ExecutionMode(value), nil
	default:
		return "", fmt.Errorf("execution_mode_rejected")
	}
}
