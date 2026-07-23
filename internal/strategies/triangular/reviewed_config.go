package triangular

import (
	"strconv"
	"time"

	platformconfig "axiom/internal/config"
	"axiom/internal/domain"
)

// ConfigurationFromReviewed converts the validated immutable platform graph
// into the strategy's typed runtime contract.
func ConfigurationFromReviewed(reviewed platformconfig.TriangularConfiguration) (Configuration, error) {
	if err := platformconfig.ValidateTriangularConfiguration(reviewed); err != nil {
		return Configuration{}, strategyError("reviewed_configuration_invalid")
	}
	values := make(map[string]string, len(reviewed.Parameters))
	for _, parameter := range reviewed.Parameters {
		values[parameter.ID] = parameter.Value
	}
	sizeIDs := []string{
		"triangular.size_ladder_10", "triangular.size_ladder_25",
		"triangular.size_ladder_50", "triangular.size_ladder_100",
	}
	sizes := make([]domain.Quantity, len(sizeIDs))
	for index, id := range sizeIDs {
		var err error
		sizes[index], err = domain.ParseQuantity(values[id])
		if err != nil {
			return Configuration{}, strategyError("reviewed_configuration_invalid")
		}
	}
	maximum, maximumErr := domain.ParseQuantity(values["triangular.maximum_cycle_notional"])
	margin, marginErr := domain.ParsePercent(values["triangular.additional_safety_margin"])
	deterioration, deteriorationErr := domain.ParseRate(values["triangular.latency_deterioration"])
	bookAge, bookErr := milliseconds(values["triangular.arrival_book_max_age"])
	lifetime, lifetimeErr := milliseconds(values["triangular.candidate_lifetime"])
	recoveryAttempts, recoveryErr := parseUint32(values["triangular.maximum_recovery_attempts"])
	window, windowErr := parseUint32(values["triangular.opportunity_metric_window"])
	if maximumErr != nil || marginErr != nil || deteriorationErr != nil ||
		bookErr != nil || lifetimeErr != nil || recoveryErr != nil || windowErr != nil {
		return Configuration{}, strategyError("reviewed_configuration_invalid")
	}
	return Configuration{
		StrategyVersion: reviewed.StrategyVersion, ModelVersion: reviewed.PricingModel,
		SizeLadder: sizes, MaximumCycleNotional: maximum,
		DynamicSizing:  values["triangular.dynamic_size_enabled"] == "1",
		MaximumBookAge: bookAge, CandidateLifetime: lifetime,
		AdditionalSafetyMargin: margin, LatencyDeterioration: deterioration,
		MaximumRecoveryAttempts: recoveryAttempts, OpportunityMetricWindow: window,
		ClaimModel: reviewed.ClaimModel,
	}, nil
}

func milliseconds(value string) (time.Duration, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return 0, strategyError("reviewed_configuration_invalid")
	}
	return time.Duration(parsed) * time.Millisecond, nil
}

func parseUint32(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return 0, strategyError("reviewed_configuration_invalid")
	}
	return uint32(parsed), nil
}
