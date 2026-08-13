package config

func validateSandbox(schema string, policy SandboxConfiguration) error {
	if schema != SchemaVersionSandboxRuntime {
		if !emptySandbox(policy) {
			return configError("invalid_configuration", "sandbox")
		}
		return nil
	}
	if policy.ArmDurationSeconds != 900 || policy.ReauthorizationSeconds != 120 ||
		policy.MaximumOpenPerAccount != 1 || policy.MaximumOpenGlobal != 2 ||
		policy.RebalancingMode != "advisory_only" || policy.SandboxProfitabilityProof ||
		!equalStrings(policy.OrderStyles, []string{"LIMIT_GTC", "LIMIT_IOC", "POST_ONLY"}) ||
		!equalStrings(policy.EligibleStrategies,
			[]string{"cross-exchange-arbitrage", "mean-reversion", "trend", "triangular"}) {
		return configError("unsafe_configuration", "sandbox")
	}
	if err := validateFinancial("sandbox.maximum_order_notional", policy.MaximumOrderNotional); err != nil {
		return err
	}
	if err := validateFinancial("sandbox.maximum_daily_notional", policy.MaximumDailyNotional); err != nil {
		return err
	}
	if policy.MaximumOrderNotional.Value != "10" || policy.MaximumOrderNotional.Maximum != "10" ||
		policy.MaximumDailyNotional.Value != "50" || policy.MaximumDailyNotional.Maximum != "50" {
		return configError("unsafe_configuration", "sandbox.caps")
	}
	if len(policy.Exchanges) != 2 ||
		policy.Exchanges[0].ID != "binance" || policy.Exchanges[0].Environment != "spot_testnet" ||
		policy.Exchanges[1].ID != "bybit" || policy.Exchanges[1].Environment != "demo" {
		return configError("invalid_configuration", "sandbox.exchanges")
	}
	for _, exchange := range policy.Exchanges {
		if exchange.SubmissionEnabled && (!exchange.IntegrationEnabled || !policy.IntegrationsEnabled ||
			!policy.SubmissionEnabled) {
			return configError("unsafe_configuration", "sandbox.enablement")
		}
	}
	if policy.SubmissionEnabled && !policy.IntegrationsEnabled {
		return configError("unsafe_configuration", "sandbox.enablement")
	}
	environment := policy.SecretFileEnvironment
	if environment.BinanceAPIKeyFile != binanceKeyFileEnvironment ||
		environment.BinanceAPISecretFile != binanceSecretFileEnvironment ||
		environment.BybitAPIKeyFile != bybitKeyFileEnvironment ||
		environment.BybitAPISecretFile != bybitSecretFileEnvironment ||
		environment.TOTPSeedFile != totpSeedFileEnvironment {
		return configError("secret_reference_rejected", "sandbox.secret_file_environment")
	}
	return nil
}

func emptySandbox(policy SandboxConfiguration) bool {
	return !policy.IntegrationsEnabled && !policy.SubmissionEnabled &&
		policy.ArmDurationSeconds == 0 && policy.ReauthorizationSeconds == 0 &&
		policy.MaximumOrderNotional == (FinancialValue{}) &&
		policy.MaximumDailyNotional == (FinancialValue{}) &&
		policy.MaximumOpenPerAccount == 0 && policy.MaximumOpenGlobal == 0 &&
		len(policy.OrderStyles) == 0 && len(policy.EligibleStrategies) == 0 &&
		policy.RebalancingMode == "" && !policy.SandboxProfitabilityProof &&
		len(policy.Exchanges) == 0 && policy.SecretFileEnvironment == (SandboxSecretEnvironment{})
}
