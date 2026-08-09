package rebalancing

import (
	"strconv"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
)

// ConfigurationFromReviewed maps the validated immutable inventory rebalancing graph into the
// optimizer value object.
func ConfigurationFromReviewed(reviewed config.RebalancingConfiguration) (Configuration, error) {
	if err := config.ValidateRebalancingConfiguration(reviewed); err != nil {
		return Configuration{}, routeError("reviewed_configuration_invalid")
	}
	values := make(map[string]string, len(reviewed.Parameters))
	for _, parameter := range reviewed.Parameters {
		values[parameter.ID] = parameter.Value
	}
	hops, hopsErr := strconv.ParseUint(values["rebalancing.maximum_hops"], 10, 32)
	candidates, candidatesErr := strconv.ParseUint(values["rebalancing.maximum_candidates"], 10, 32)
	confidence, confidenceErr := domain.ParsePercent(values["rebalancing.minimum_confidence"])
	cost, costErr := domain.ParseMoney(values["rebalancing.maximum_total_cost"])
	durationMilliseconds, durationErr := strconv.ParseUint(values["rebalancing.maximum_duration"], 10, 64)
	risk, riskErr := domain.ParsePercent(values["rebalancing.maximum_risk_score"])
	checklist, checklistErr := strconv.ParseUint(values["rebalancing.minimum_checklist_steps"], 10, 32)
	if hopsErr != nil || candidatesErr != nil || confidenceErr != nil || costErr != nil ||
		durationErr != nil || riskErr != nil || checklistErr != nil {
		return Configuration{}, routeError("reviewed_configuration_invalid")
	}
	result := Configuration{
		OptimizerVersion: reviewed.OptimizerVersion, FactSchemaVersion: reviewed.FactSchemaVersion,
		CostModelVersion: reviewed.CostModelVersion, Mode: reviewed.Mode,
		NaturalReversalPolicy: reviewed.NaturalReversalPolicy,
		ApprovedAssets:        append([]string(nil), reviewed.ApprovedAssets...),
		Exchanges:             append([]string(nil), reviewed.Exchanges...),
		MaximumHops:           uint32(hops), MaximumCandidates: uint32(candidates),
		MinimumConfidence: confidence, MaximumTotalCost: cost,
		MaximumDuration:  time.Duration(durationMilliseconds) * time.Millisecond,
		MaximumRiskScore: risk, MinimumChecklistSteps: uint32(checklist),
	}
	if !validConfiguration(result) {
		return Configuration{}, routeError("reviewed_configuration_invalid")
	}
	return result, nil
}
