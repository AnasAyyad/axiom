package bootstrap

import (
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

// decodeSandboxStrategyConfiguration validates the exact immutable payload
// attached to scheduled session work before any strategy runtime is built.
// A session can never silently follow a later active configuration.
func decodeSandboxStrategyConfiguration(
	work sandbox.StrategySessionWork,
	record sandbox.StrategySessionConfiguration,
	now time.Time,
) (config.Configuration, error) {
	if work.ValidAt(now) != nil || !record.ValidFor(work) {
		return config.Configuration{}, fmt.Errorf("sandbox_strategy_configuration_invalid")
	}
	var product config.Configuration
	if err := json.Unmarshal(record.Payload, &product); err != nil {
		return config.Configuration{}, fmt.Errorf("sandbox_strategy_configuration_invalid")
	}
	snapshot, err := config.NewSnapshot(product, config.SourceAdmin,
		"sandbox-engine", &domain.SystemClock{})
	if err != nil || snapshot.Hash() != record.Hash ||
		product.SchemaVersion != config.SchemaVersionSandboxRuntime ||
		!sandboxStrategyConfigurationModeMatches(work, product.Mode) {
		return config.Configuration{}, fmt.Errorf("sandbox_strategy_configuration_invalid")
	}
	return snapshot.Configuration(), nil
}

func sandboxStrategyConfigurationModeMatches(
	work sandbox.StrategySessionWork,
	mode config.ExecutionMode,
) bool {
	switch work.Account.Exchange {
	case sandbox.ExchangeBinance:
		return mode == config.ModeTestnet
	case sandbox.ExchangeBybit:
		return mode == config.ModeDemo
	default:
		return false
	}
}
