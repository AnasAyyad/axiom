package config

import (
	"time"

	"axiom/internal/domain"
)

func validateRebalancing(schema string, optimizer RebalancingConfiguration) error {
	if schema != SchemaVersionV1BB6 && schema != SchemaVersionV1C {
		if !emptyRebalancing(optimizer) {
			return configError("invalid_configuration", "rebalancing")
		}
		return nil
	}
	wanted := defaultRebalancingConfiguration()
	if optimizer.OptimizerVersion != wanted.OptimizerVersion ||
		optimizer.FactSchemaVersion != wanted.FactSchemaVersion ||
		optimizer.CostModelVersion != wanted.CostModelVersion ||
		optimizer.Mode != wanted.Mode ||
		optimizer.NaturalReversalPolicy != wanted.NaturalReversalPolicy ||
		!equalStrings(optimizer.ApprovedAssets, wanted.ApprovedAssets) ||
		!equalStrings(optimizer.Exchanges, wanted.Exchanges) ||
		len(optimizer.Parameters) != RebalancingParameterCount {
		return configError("invalid_rebalancing_configuration", "rebalancing")
	}
	return validateRebalancingParameters(optimizer.Parameters, wanted.Parameters)
}

func emptyRebalancing(optimizer RebalancingConfiguration) bool {
	return optimizer.OptimizerVersion == "" && optimizer.FactSchemaVersion == "" &&
		optimizer.CostModelVersion == "" && optimizer.Mode == "" &&
		optimizer.NaturalReversalPolicy == "" && len(optimizer.ApprovedAssets) == 0 &&
		len(optimizer.Exchanges) == 0 && len(optimizer.Parameters) == 0
}

func validateRebalancingParameters(parameters, wanted []StrategyParameter) error {
	contracts := make(map[string]StrategyParameter, len(wanted))
	for _, parameter := range wanted {
		contracts[parameter.ID] = parameter
	}
	seen := make(map[string]struct{}, len(parameters))
	for _, parameter := range parameters {
		contract, ok := contracts[parameter.ID]
		if !ok || !sameRebalancingParameterContract(parameter, contract) {
			return configError("invalid_rebalancing_parameter", "rebalancing.parameters")
		}
		if _, duplicate := seen[parameter.ID]; duplicate {
			return configError("invalid_rebalancing_parameter", "rebalancing.parameters.id")
		}
		seen[parameter.ID] = struct{}{}
		if err := validateRebalancingValue(parameter); err != nil {
			return err
		}
	}
	return nil
}

func sameRebalancingParameterContract(parameter, contract StrategyParameter) bool {
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

func validateRebalancingValue(parameter StrategyParameter) error {
	if parameter.Scale > 18 || decimalScale(parameter.Value) > int(parameter.Scale) ||
		!validRounding(parameter.Rounding) {
		return configError("invalid_rebalancing_parameter", parameter.ID)
	}
	value, valueErr := domain.ParseRate(parameter.Value)
	minimum, minimumErr := domain.ParseRate(parameter.Minimum)
	maximum, maximumErr := domain.ParseRate(parameter.Maximum)
	if valueErr != nil || minimumErr != nil || maximumErr != nil ||
		maximum.Compare(minimum) < 0 {
		return configError("invalid_rebalancing_parameter", parameter.ID)
	}
	if outsideRange(value.Compare(minimum), value.Compare(maximum), FinancialValue{
		MinimumInclusive: parameter.MinimumInclusive,
		MaximumInclusive: parameter.MaximumInclusive,
	}) {
		return configError("rebalancing_parameter_out_of_range", parameter.ID)
	}
	return nil
}

// ValidateRebalancingConfiguration validates one standalone B6 graph.
func ValidateRebalancingConfiguration(optimizer RebalancingConfiguration) error {
	return validateRebalancing(SchemaVersionV1BB6, optimizer)
}
