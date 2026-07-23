package config

import (
	"time"

	"axiom/internal/domain"
)

func validateMeanReversion(schema string, strategy MeanReversionConfiguration) error {
	if schema != SchemaVersionV1BB3 && schema != SchemaVersionV1BB4 {
		if strategy.StrategyVersion != "" || strategy.PrimaryTimeframe != "" ||
			strategy.HigherTimeframe != "" || len(strategy.Parameters) != 0 {
			return configError("invalid_configuration", "mean_reversion")
		}
		return nil
	}
	wanted := defaultMeanReversionConfiguration()
	if strategy.StrategyVersion != wanted.StrategyVersion || strategy.PrimaryTimeframe != wanted.PrimaryTimeframe ||
		strategy.HigherTimeframe != wanted.HigherTimeframe || len(strategy.Parameters) != MeanReversionParameterCount {
		return configError("invalid_mean_reversion_configuration", "mean_reversion")
	}
	contracts := make(map[string]StrategyParameter, len(wanted.Parameters))
	for _, parameter := range wanted.Parameters {
		contracts[parameter.ID] = parameter
	}
	seen := make(map[string]struct{}, len(strategy.Parameters))
	for _, parameter := range strategy.Parameters {
		contract, ok := contracts[parameter.ID]
		if !ok || !sameMeanReversionParameterContract(parameter, contract) {
			return configError("invalid_mean_reversion_parameter", "mean_reversion.parameters")
		}
		if _, duplicate := seen[parameter.ID]; duplicate {
			return configError("invalid_mean_reversion_parameter", "mean_reversion.parameters.id")
		}
		seen[parameter.ID] = struct{}{}
		if err := validateSignedStrategyValue(parameter); err != nil {
			return err
		}
	}
	return nil
}

// ValidateMeanReversionConfiguration validates one standalone B3 graph with
// the same contract used by the complete immutable configuration loader.
func ValidateMeanReversionConfiguration(strategy MeanReversionConfiguration) error {
	return validateMeanReversion(SchemaVersionV1BB3, strategy)
}

func sameMeanReversionParameterContract(parameter, contract StrategyParameter) bool {
	if parameter.Description != contract.Description || parameter.Unit != contract.Unit ||
		parameter.Minimum != contract.Minimum || parameter.Maximum != contract.Maximum ||
		parameter.MinimumInclusive != contract.MinimumInclusive || parameter.MaximumInclusive != contract.MaximumInclusive ||
		parameter.Scale != contract.Scale || parameter.Rounding != contract.Rounding ||
		parameter.Cadence != contract.Cadence || parameter.WarmUp != contract.WarmUp ||
		parameter.Mutability != contract.Mutability || !equalStrings(parameter.ModelDependencies, contract.ModelDependencies) ||
		parameter.AlgorithmVersion != contract.AlgorithmVersion || parameter.EvaluationTimezone != "UTC" ||
		parameter.ChangeBehavior != contract.ChangeBehavior || parameter.ApprovalActor != contract.ApprovalActor ||
		parameter.ApprovalReference != contract.ApprovalReference || parameter.ApprovedAt != contract.ApprovedAt ||
		parameter.ChangeReason != contract.ChangeReason {
		return false
	}
	approvedAt, err := time.Parse(time.RFC3339, parameter.ApprovedAt)
	return err == nil && approvedAt.Location() == time.UTC
}

func validateSignedStrategyValue(parameter StrategyParameter) error {
	if parameter.Scale > 18 || decimalScale(parameter.Value) > int(parameter.Scale) || !validRounding(parameter.Rounding) {
		return configError("invalid_mean_reversion_parameter", parameter.ID)
	}
	value, valueErr := domain.ParsePnL(parameter.Value)
	minimum, minimumErr := domain.ParsePnL(parameter.Minimum)
	maximum, maximumErr := domain.ParsePnL(parameter.Maximum)
	if valueErr != nil || minimumErr != nil || maximumErr != nil || maximum.Compare(minimum) < 0 {
		return configError("invalid_mean_reversion_parameter", parameter.ID)
	}
	if outsideRange(value.Compare(minimum), value.Compare(maximum), FinancialValue{
		MinimumInclusive: parameter.MinimumInclusive, MaximumInclusive: parameter.MaximumInclusive,
	}) {
		return configError("mean_reversion_parameter_out_of_range", parameter.ID)
	}
	return nil
}
