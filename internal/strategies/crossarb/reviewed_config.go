package crossarb

import (
	"strconv"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
)

// ConfigurationFromReviewed maps the fully validated immutable config graph
// into the hot-path B5 value object.
func ConfigurationFromReviewed(reviewed config.CrossExchangeConfiguration) (Configuration, error) {
	if err := config.ValidateCrossExchangeConfiguration(reviewed); err != nil {
		return Configuration{}, strategyError("reviewed_configuration_invalid")
	}
	values := make(map[string]string, len(reviewed.Parameters))
	for _, parameter := range reviewed.Parameters {
		values[parameter.ID] = parameter.Value
	}
	bookAge, bookErr := reviewedMilliseconds(values["cross_exchange.maximum_book_age"])
	skew, skewErr := reviewedMilliseconds(values["cross_exchange.maximum_inter_book_skew"])
	uncertainty, uncertaintyErr := reviewedMilliseconds(values["cross_exchange.maximum_clock_uncertainty"])
	lifetime, lifetimeErr := reviewedMilliseconds(values["cross_exchange.candidate_lifetime"])
	maximum, maximumErr := domain.ParseBalance(values["cross_exchange.maximum_notional"])
	reduced, reducedErr := domain.ParseBalance(values["cross_exchange.reduced_direction_maximum_notional"])
	minimumEdge, edgeErr := domain.ParsePercent(values["cross_exchange.minimum_closed_cycle_edge"])
	lower, lowerErr := domain.ParsePercent(values["cross_exchange.lower_inventory_band"])
	target, targetErr := domain.ParsePercent(values["cross_exchange.target_inventory_band"])
	upper, upperErr := domain.ParsePercent(values["cross_exchange.upper_inventory_band"])
	recovery, recoveryErr := strconv.ParseUint(values["cross_exchange.maximum_recovery_attempts"], 10, 32)
	if bookErr != nil || skewErr != nil || uncertaintyErr != nil || lifetimeErr != nil ||
		maximumErr != nil || reducedErr != nil || edgeErr != nil || lowerErr != nil ||
		targetErr != nil || upperErr != nil || recoveryErr != nil {
		return Configuration{}, strategyError("reviewed_configuration_invalid")
	}
	result := Configuration{
		StrategyVersion: reviewed.StrategyVersion, ModelVersion: reviewed.PricingModel,
		ApprovedInstruments: append([]string(nil), reviewed.Instruments...),
		MaximumBookAge:      bookAge, MaximumInterBookSkew: skew,
		MaximumClockUncertainty: uncertainty, CandidateLifetime: lifetime,
		MaximumNotional: maximum, ReducedDirectionMaximumNotional: reduced,
		MinimumClosedCycleEdge: minimumEdge, LowerBand: lower, TargetBand: target, UpperBand: upper,
		MaximumRecoveryAttempts: uint32(recovery), DispatchMode: reviewed.DispatchMode,
		ClaimModel: reviewed.ClaimModel,
	}
	if !validConfiguration(result) {
		return Configuration{}, strategyError("reviewed_configuration_invalid")
	}
	return result, nil
}

func reviewedMilliseconds(value string) (time.Duration, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(parsed) * time.Millisecond, nil
}
