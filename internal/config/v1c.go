package config

import "fmt"

const (
	binanceKeyFileEnvironment    = "AXIOM_BINANCE_TESTNET_API_KEY_FILE"
	binanceSecretFileEnvironment = "AXIOM_BINANCE_TESTNET_API_SECRET_FILE"
	bybitKeyFileEnvironment      = "AXIOM_BYBIT_DEMO_API_KEY_FILE"
	bybitSecretFileEnvironment   = "AXIOM_BYBIT_DEMO_API_SECRET_FILE"
	totpSeedFileEnvironment      = "AXIOM_TOTP_SEED_FILE"
)

// DefaultV1CConfiguration returns the reviewed C1-C6 graph with every
// authenticated integration and submission gate disabled.
func DefaultV1CConfiguration(mode ExecutionMode) (Configuration, error) {
	if mode != ModeTestnet && mode != ModeDemo {
		return Configuration{}, fmt.Errorf("v1c_mode_rejected")
	}
	configuration := DefaultV1BConfiguration()
	configuration.SchemaVersion = SchemaVersionV1C
	configuration.Environment = EnvironmentSandbox
	configuration.Mode = mode
	configuration.Sandbox = SandboxConfiguration{
		IntegrationsEnabled:       false,
		SubmissionEnabled:         false,
		ArmDurationSeconds:        15 * 60,
		ReauthorizationSeconds:    2 * 60,
		MaximumOrderNotional:      sandboxMoneyValue("10"),
		MaximumDailyNotional:      sandboxMoneyValue("50"),
		MaximumOpenPerAccount:     1,
		MaximumOpenGlobal:         2,
		OrderStyles:               []string{"LIMIT_GTC", "LIMIT_IOC", "POST_ONLY"},
		EligibleStrategies:        []string{"cross-exchange-arbitrage", "mean-reversion", "trend", "triangular"},
		RebalancingMode:           "advisory_only",
		SandboxProfitabilityProof: false,
		Exchanges: []SandboxExchangeConfiguration{
			{ID: "binance", Environment: "spot_testnet"},
			{ID: "bybit", Environment: "demo"},
		},
		SecretFileEnvironment: SandboxSecretEnvironment{
			BinanceAPIKeyFile:    binanceKeyFileEnvironment,
			BinanceAPISecretFile: binanceSecretFileEnvironment,
			BybitAPIKeyFile:      bybitKeyFileEnvironment,
			BybitAPISecretFile:   bybitSecretFileEnvironment,
			TOTPSeedFile:         totpSeedFileEnvironment,
		},
	}
	configuration.Capabilities = V1CCapabilities()
	return configuration, nil
}

func sandboxMoneyValue(value string) FinancialValue {
	return FinancialValue{
		Value: value, Unit: "USDT", Minimum: "0", Maximum: value,
		MinimumInclusive: false, MaximumInclusive: true, Scale: 8, Rounding: "down",
	}
}
