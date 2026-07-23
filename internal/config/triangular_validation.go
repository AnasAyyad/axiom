package config

import (
	"time"

	"axiom/internal/domain"
)

func validateTriangular(schema string, strategy TriangularConfiguration) error {
	if schema != SchemaVersionV1BB4 && schema != SchemaVersionV1BB5 {
		if strategy.StrategyVersion != "" || strategy.SettlementAsset != "" ||
			len(strategy.Cycles) != 0 || strategy.DispatchMode != "" ||
			strategy.PricingModel != "" || strategy.ClaimModel != "" ||
			len(strategy.Parameters) != 0 {
			return configError("invalid_configuration", "triangular")
		}
		return nil
	}
	wanted := defaultTriangularConfiguration()
	if strategy.StrategyVersion != wanted.StrategyVersion ||
		strategy.SettlementAsset != wanted.SettlementAsset ||
		!equalStrings(strategy.Cycles, wanted.Cycles) ||
		strategy.DispatchMode != wanted.DispatchMode ||
		strategy.PricingModel != wanted.PricingModel ||
		strategy.ClaimModel != wanted.ClaimModel ||
		len(strategy.Parameters) != TriangularParameterCount {
		return configError("invalid_triangular_configuration", "triangular")
	}
	contracts := make(map[string]StrategyParameter, len(wanted.Parameters))
	for _, parameter := range wanted.Parameters {
		contracts[parameter.ID] = parameter
	}
	seen := make(map[string]struct{}, len(strategy.Parameters))
	for _, parameter := range strategy.Parameters {
		contract, ok := contracts[parameter.ID]
		if !ok || !sameTriangularParameterContract(parameter, contract) {
			return configError("invalid_triangular_parameter", "triangular.parameters")
		}
		if _, duplicate := seen[parameter.ID]; duplicate {
			return configError("invalid_triangular_parameter", "triangular.parameters.id")
		}
		seen[parameter.ID] = struct{}{}
		if err := validateTriangularValue(parameter); err != nil {
			return err
		}
	}
	return nil
}

// ValidateTriangularConfiguration validates one standalone B4 graph.
func ValidateTriangularConfiguration(strategy TriangularConfiguration) error {
	return validateTriangular(SchemaVersionV1BB4, strategy)
}

func sameTriangularParameterContract(parameter, contract StrategyParameter) bool {
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

func validateTriangularValue(parameter StrategyParameter) error {
	if parameter.Scale > 18 || decimalScale(parameter.Value) > int(parameter.Scale) ||
		!validRounding(parameter.Rounding) {
		return configError("invalid_triangular_parameter", parameter.ID)
	}
	value, valueErr := domain.ParseRate(parameter.Value)
	minimum, minimumErr := domain.ParseRate(parameter.Minimum)
	maximum, maximumErr := domain.ParseRate(parameter.Maximum)
	if valueErr != nil || minimumErr != nil || maximumErr != nil ||
		maximum.Compare(minimum) < 0 {
		return configError("invalid_triangular_parameter", parameter.ID)
	}
	if outsideRange(value.Compare(minimum), value.Compare(maximum), FinancialValue{
		MinimumInclusive: parameter.MinimumInclusive,
		MaximumInclusive: parameter.MaximumInclusive,
	}) {
		return configError("triangular_parameter_out_of_range", parameter.ID)
	}
	return nil
}
