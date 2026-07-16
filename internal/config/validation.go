package config

import (
	"os"
	"path/filepath"
	"strings"

	"axiom/internal/domain"
	"axiom/internal/security"
)

const publicEndpointSet = "market-data-only-v1"

// Validate checks the complete configuration graph before resources or workers open.
func Validate(configuration Configuration) error {
	if err := validateIdentity(configuration); err != nil {
		return err
	}
	if err := validateSafety(configuration.Safety, configuration.Capabilities); err != nil {
		return err
	}
	if err := validateEndpoint(configuration.Endpoint); err != nil {
		return err
	}
	approved, err := validateAssets(configuration.Assets)
	if err != nil {
		return err
	}
	if err := validateInstruments(configuration.Instruments, approved); err != nil {
		return err
	}
	if err := validateRisk(configuration.Risk); err != nil {
		return err
	}
	if err := validatePortfolio(configuration.Portfolio, approved); err != nil {
		return err
	}
	if err := validateModels(configuration.Models); err != nil {
		return err
	}
	if err := validateTrend(configuration.Trend); err != nil {
		return err
	}
	return validateSecrets(configuration.Secrets)
}

func validateIdentity(configuration Configuration) error {
	if configuration.SchemaVersion != SchemaVersion || configuration.Revision == 0 {
		return configError("invalid_configuration", "schema")
	}
	switch configuration.Environment {
	case EnvironmentLocal, EnvironmentTest, EnvironmentShadow:
	default:
		return configError("prohibited_environment", "environment")
	}
	if _, err := ParseExecutionMode(string(configuration.Mode)); err != nil {
		return configError("prohibited_mode", "mode")
	}
	if configuration.Environment == EnvironmentShadow && configuration.Mode != ModeShadow {
		return configError("invalid_configuration", "environment_mode")
	}
	if configuration.Product != domain.ProductSpot {
		return configError("prohibited_product", "product")
	}
	return nil
}

func validateSafety(safety SafetyConfiguration, capabilities []CapabilityDisposition) error {
	if !safety.FailClosed || safety.RiskInitialState != "PAUSED" || safety.AutoUnpause {
		return configError("unsafe_configuration", "safety")
	}
	if !capabilitiesExactlyUnsupported(capabilities) {
		return configError("prohibited_capability", "capabilities")
	}
	return nil
}

func validateEndpoint(endpoint EndpointConfiguration) error {
	if endpoint.Set != publicEndpointSet {
		return configError("endpoint_rejected", "endpoint.set")
	}
	if endpoint.REST != "https://data-api.binance.vision" {
		return configError("endpoint_rejected", "endpoint.rest")
	}
	if endpoint.WebSocket != "wss://data-stream.binance.vision" {
		return configError("endpoint_rejected", "endpoint.websocket")
	}
	return nil
}

func validateAssets(assets []domain.Asset) (map[domain.AssetSymbol]struct{}, error) {
	if len(assets) == 0 {
		return nil, configError("invalid_configuration", "assets")
	}
	if err := domain.ValidateAssetRegistry(assets); err != nil {
		return nil, configError("invalid_configuration", "assets")
	}
	approved := make(map[domain.AssetSymbol]struct{})
	for _, asset := range assets {
		if asset.Status == domain.AssetApproved {
			approved[asset.Symbol] = struct{}{}
		}
	}
	return approved, nil
}

func validateInstruments(instruments []Instrument, approved map[domain.AssetSymbol]struct{}) error {
	if len(instruments) == 0 {
		return configError("invalid_configuration", "instruments")
	}
	seen := make(map[string]struct{}, len(instruments))
	for _, configured := range instruments {
		base, baseError := domain.ParseAssetSymbol(configured.Base)
		quote, quoteError := domain.ParseAssetSymbol(configured.Quote)
		instrument, instrumentError := domain.NewSpotInstrument(base, quote)
		if baseError != nil || quoteError != nil || instrumentError != nil || configured.Product != "spot" {
			return configError("prohibited_instrument", "instruments")
		}
		if _, ok := approved[base]; !ok {
			return configError("unapproved_asset", "instruments.base")
		}
		if _, ok := approved[quote]; !ok {
			return configError("unapproved_asset", "instruments.quote")
		}
		if _, duplicate := seen[instrument.Symbol()]; duplicate {
			return configError("invalid_configuration", "instruments")
		}
		seen[instrument.Symbol()] = struct{}{}
	}
	return nil
}

func validateRisk(risk RiskConfiguration) error {
	if err := validateFinancial("risk.maximum_asset_allocation", risk.MaximumAssetAllocation); err != nil {
		return err
	}
	if err := validateFinancial("risk.maximum_order_notional", risk.MaximumOrderNotional); err != nil {
		return err
	}
	return validateFinancial("risk.maximum_daily_loss", risk.MaximumDailyLoss)
}

func validatePortfolio(portfolio PortfolioConfiguration, approved map[domain.AssetSymbol]struct{}) error {
	asset, err := domain.ParseAssetSymbol(portfolio.SettlementAsset)
	if err != nil {
		return configError("invalid_configuration", "portfolio.settlement_asset")
	}
	if _, ok := approved[asset]; !ok {
		return configError("unapproved_asset", "portfolio.settlement_asset")
	}
	return validateFinancial("portfolio.starting_capital", portfolio.StartingCapital)
}

func validateFinancial(field string, value FinancialValue) error {
	if value.Scale > 18 || !validRounding(value.Rounding) || decimalScale(value.Value) > int(value.Scale) {
		return configError("invalid_financial_value", field)
	}
	switch value.Unit {
	case "decimal_fraction":
		return validatePercentRange(field, value)
	case "USDT":
		return validateMoneyRange(field, value)
	default:
		return configError("invalid_unit", field)
	}
}

func validatePercentRange(field string, spec FinancialValue) error {
	value, valueError := domain.ParsePercent(spec.Value)
	minimum, minimumError := domain.ParsePercent(spec.Minimum)
	maximum, maximumError := domain.ParsePercent(spec.Maximum)
	if valueError != nil || minimumError != nil || maximumError != nil || maximum.Compare(minimum) < 0 {
		return configError("invalid_financial_value", field)
	}
	if outsideRange(value.Compare(minimum), value.Compare(maximum), spec) {
		return configError("financial_value_out_of_range", field)
	}
	return nil
}

func validateMoneyRange(field string, spec FinancialValue) error {
	value, valueError := domain.ParseMoney(spec.Value)
	minimum, minimumError := domain.ParseMoney(spec.Minimum)
	maximum, maximumError := domain.ParseMoney(spec.Maximum)
	if valueError != nil || minimumError != nil || maximumError != nil || maximum.Compare(minimum) < 0 {
		return configError("invalid_financial_value", field)
	}
	if outsideRange(value.Compare(minimum), value.Compare(maximum), spec) {
		return configError("financial_value_out_of_range", field)
	}
	return nil
}

func outsideRange(minimumComparison, maximumComparison int, spec FinancialValue) bool {
	below := minimumComparison < 0 || (minimumComparison == 0 && !spec.MinimumInclusive)
	above := maximumComparison > 0 || (maximumComparison == 0 && !spec.MaximumInclusive)
	return below || above
}

func validRounding(value string) bool {
	switch value {
	case "down", "ceiling", "floor", "half_even":
		return true
	default:
		return false
	}
}

func decimalScale(value string) int {
	point := strings.IndexByte(value, '.')
	if point < 0 {
		return 0
	}
	return len(value) - point - 1
}

func validateModels(models ModelConfiguration) error {
	if models.Fee != "fixed-bps-v1" && models.Fee != "historical-fee-v1" {
		return configError("model_rejected", "models.fee")
	}
	if models.Latency != "fixed-zero-v1" && models.Latency != "historical-latency-v1" {
		return configError("model_rejected", "models.latency")
	}
	return nil
}

func validateTrend(trend TrendConfiguration) error {
	if trend.StrategyVersion == "" || len(trend.StrategyVersion) > 120 || trend.Timeframe != "4h" {
		return configError("invalid_trend_configuration", "trend")
	}
	wanted := defaultTrendConfiguration()
	if len(trend.Parameters) != len(wanted.Parameters) {
		return configError("invalid_trend_configuration", "trend.parameters")
	}
	contracts := make(map[string]StrategyParameter, len(wanted.Parameters))
	for _, parameter := range wanted.Parameters {
		contracts[parameter.ID] = parameter
	}
	seen := make(map[string]struct{}, len(trend.Parameters))
	for _, parameter := range trend.Parameters {
		contract, ok := contracts[parameter.ID]
		if !ok || parameter.Description != contract.Description || parameter.Unit != contract.Unit ||
			parameter.Minimum != contract.Minimum || parameter.Maximum != contract.Maximum ||
			parameter.MinimumInclusive != contract.MinimumInclusive || parameter.MaximumInclusive != contract.MaximumInclusive ||
			parameter.Scale != contract.Scale || parameter.Rounding != contract.Rounding ||
			parameter.Cadence != contract.Cadence || parameter.WarmUp != contract.WarmUp ||
			parameter.Mutability != contract.Mutability || !equalStrings(parameter.ModelDependencies, contract.ModelDependencies) {
			return configError("invalid_trend_parameter", "trend.parameters")
		}
		if _, duplicate := seen[parameter.ID]; duplicate {
			return configError("invalid_trend_parameter", "trend.parameters.id")
		}
		seen[parameter.ID] = struct{}{}
		if err := validateTrendValue(parameter); err != nil {
			return err
		}
	}
	return nil
}

func validateTrendValue(parameter StrategyParameter) error {
	if parameter.Scale > 18 || decimalScale(parameter.Value) > int(parameter.Scale) || !validRounding(parameter.Rounding) {
		return configError("invalid_trend_parameter", parameter.ID)
	}
	value, valueErr := domain.ParseRate(parameter.Value)
	minimum, minimumErr := domain.ParseRate(parameter.Minimum)
	maximum, maximumErr := domain.ParseRate(parameter.Maximum)
	if valueErr != nil || minimumErr != nil || maximumErr != nil || maximum.Compare(minimum) < 0 {
		return configError("invalid_trend_parameter", parameter.ID)
	}
	if outsideRange(value.Compare(minimum), value.Compare(maximum), FinancialValue{
		MinimumInclusive: parameter.MinimumInclusive, MaximumInclusive: parameter.MaximumInclusive,
	}) {
		return configError("trend_parameter_out_of_range", parameter.ID)
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateSecrets(references []SecretReference) error {
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if reference.Name == "" || placeholder(reference.Name) {
			return configError("secret_reference_rejected", "secrets.name")
		}
		if _, duplicate := seen[reference.Name]; duplicate {
			return configError("secret_reference_rejected", "secrets.name")
		}
		seen[reference.Name] = struct{}{}
		if err := validateSecretFile(reference); err != nil {
			return err
		}
	}
	return nil
}

func validateSecretFile(reference SecretReference) error {
	if !filepath.IsAbs(reference.File) || placeholder(filepath.Base(reference.File)) {
		return configError("secret_reference_rejected", "secrets.file")
	}
	information, err := os.Lstat(reference.File)
	if err != nil {
		if reference.Required {
			return configError("required_secret_missing", "secrets.file")
		}
		return nil
	}
	if information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() {
		return configError("secret_reference_rejected", "secrets.file")
	}
	if _, err := security.ReadSecretFile(reference.File); err != nil {
		return configError("secret_reference_rejected", "secrets.file")
	}
	return nil
}

func placeholder(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "placeholder") || strings.Contains(lower, "changeme") ||
		strings.Contains(lower, "<") || strings.Contains(lower, ">")
}
