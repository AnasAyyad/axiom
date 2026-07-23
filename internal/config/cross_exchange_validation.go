package config

import (
	"time"

	"axiom/internal/domain"
)

func validateCrossExchange(schema string, strategy CrossExchangeConfiguration) error {
	if schema != SchemaVersionV1BB5 {
		if !emptyCrossExchange(strategy) {
			return configError("invalid_configuration", "cross_exchange")
		}
		return nil
	}
	wanted := defaultCrossExchangeConfiguration()
	if strategy.StrategyVersion != wanted.StrategyVersion ||
		strategy.SettlementAsset != wanted.SettlementAsset ||
		!equalStrings(strategy.Instruments, wanted.Instruments) ||
		!equalStrings(strategy.Exchanges, wanted.Exchanges) ||
		!equalStrings(strategy.Directions, wanted.Directions) ||
		strategy.DispatchMode != wanted.DispatchMode ||
		strategy.PricingModel != wanted.PricingModel ||
		strategy.ClaimModel != wanted.ClaimModel ||
		strategy.RebalancingMode != wanted.RebalancingMode ||
		len(strategy.Parameters) != CrossExchangeParameterCount {
		return configError("invalid_cross_exchange_configuration", "cross_exchange")
	}
	return validateCrossExchangeParameters(strategy.Parameters, wanted.Parameters)
}

func emptyCrossExchange(strategy CrossExchangeConfiguration) bool {
	return strategy.StrategyVersion == "" && strategy.SettlementAsset == "" &&
		len(strategy.Instruments) == 0 && len(strategy.Exchanges) == 0 &&
		len(strategy.Directions) == 0 && strategy.DispatchMode == "" &&
		strategy.PricingModel == "" && strategy.ClaimModel == "" &&
		strategy.RebalancingMode == "" && len(strategy.Parameters) == 0
}

func validateCrossExchangeParameters(parameters, wanted []StrategyParameter) error {
	contracts := make(map[string]StrategyParameter, len(wanted))
	for _, parameter := range wanted {
		contracts[parameter.ID] = parameter
	}
	seen := make(map[string]struct{}, len(parameters))
	for _, parameter := range parameters {
		contract, ok := contracts[parameter.ID]
		if !ok || !sameCrossExchangeParameterContract(parameter, contract) {
			return configError("invalid_cross_exchange_parameter", "cross_exchange.parameters")
		}
		if _, duplicate := seen[parameter.ID]; duplicate {
			return configError("invalid_cross_exchange_parameter", "cross_exchange.parameters.id")
		}
		seen[parameter.ID] = struct{}{}
		if err := validateCrossExchangeValue(parameter); err != nil {
			return err
		}
	}
	return nil
}

func sameCrossExchangeParameterContract(parameter, contract StrategyParameter) bool {
	if parameter.Description != contract.Description || parameter.Unit != contract.Unit ||
		parameter.Minimum != contract.Minimum || parameter.Maximum != contract.Maximum ||
		parameter.MinimumInclusive != contract.MinimumInclusive ||
		parameter.MaximumInclusive != contract.MaximumInclusive ||
		parameter.Scale != contract.Scale || parameter.Rounding != contract.Rounding ||
		parameter.Cadence != contract.Cadence || parameter.WarmUp != contract.WarmUp ||
		parameter.Mutability != contract.Mutability ||
		!equalStrings(parameter.ModelDependencies, contract.ModelDependencies) ||
		parameter.AlgorithmVersion != contract.AlgorithmVersion ||
		parameter.EvaluationTimezone != "UTC" ||
		parameter.ChangeBehavior != contract.ChangeBehavior ||
		parameter.ApprovalActor != contract.ApprovalActor ||
		parameter.ApprovalReference != contract.ApprovalReference ||
		parameter.ApprovedAt != contract.ApprovedAt ||
		parameter.ChangeReason != contract.ChangeReason {
		return false
	}
	approvedAt, err := time.Parse(time.RFC3339, parameter.ApprovedAt)
	return err == nil && approvedAt.Location() == time.UTC
}

func validateCrossExchangeValue(parameter StrategyParameter) error {
	if parameter.Scale > 18 || decimalScale(parameter.Value) > int(parameter.Scale) ||
		!validRounding(parameter.Rounding) {
		return configError("invalid_cross_exchange_parameter", parameter.ID)
	}
	value, valueErr := domain.ParseRate(parameter.Value)
	minimum, minimumErr := domain.ParseRate(parameter.Minimum)
	maximum, maximumErr := domain.ParseRate(parameter.Maximum)
	if valueErr != nil || minimumErr != nil || maximumErr != nil ||
		maximum.Compare(minimum) < 0 {
		return configError("invalid_cross_exchange_parameter", parameter.ID)
	}
	if outsideRange(value.Compare(minimum), value.Compare(maximum), FinancialValue{
		MinimumInclusive: parameter.MinimumInclusive,
		MaximumInclusive: parameter.MaximumInclusive,
	}) {
		return configError("cross_exchange_parameter_out_of_range", parameter.ID)
	}
	return nil
}

// ValidateCrossExchangeConfiguration validates one standalone B5 graph.
func ValidateCrossExchangeConfiguration(strategy CrossExchangeConfiguration) error {
	return validateCrossExchange(SchemaVersionV1BB5, strategy)
}
