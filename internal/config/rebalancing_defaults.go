package config

// RebalancingParameterCount is the immutable inventory rebalancing advisory graph size.
const RebalancingParameterCount = 12

func rebalancingParameter(
	id, description, algorithm, value, unit, minimum, maximum string,
	minimumInclusive, maximumInclusive bool,
	scale uint8,
	rounding string,
	dependencies ...string,
) StrategyParameter {
	return StrategyParameter{
		ID: id, Description: description, Value: value, Unit: unit,
		Minimum: minimum, Maximum: maximum,
		MinimumInclusive: minimumInclusive, MaximumInclusive: maximumInclusive,
		Scale: scale, Rounding: rounding, Cadence: "every_advisory_rebalancing_request",
		WarmUp: "approved_inventory_snapshot_and_complete_reviewed_facts", Mutability: "immutable_per_run",
		ModelDependencies: dependencies, AlgorithmVersion: algorithm, EvaluationTimezone: "UTC",
		ChangeBehavior:    "existing recommendations retain their fact set and configuration; new requests require a new snapshot",
		ApprovalActor:     "authoritative_specification",
		ApprovalReference: "AX-V1B-B06-FUN-001/AX-V1B-B06-SAF-001",
		ApprovedAt:        "2026-07-23T00:00:00Z",
		ChangeReason:      "initial immutable inventory rebalancing advisory inventory and rebalancing optimizer",
	}
}

func defaultRebalancingConfiguration() RebalancingConfiguration {
	parameter := rebalancingParameter
	return RebalancingConfiguration{
		OptimizerVersion:      "inventory-rebalancing@1.0.0",
		FactSchemaVersion:     "rebalancing-fact.v1",
		CostModelVersion:      "rebalancing-cost.v1",
		Mode:                  "advisory_only",
		NaturalReversalPolicy: "prefer_eligible_before_transfer",
		ApprovedAssets:        []string{"BTC", "ETH", "USDT"},
		Exchanges:             []string{"binance", "bybit"},
		Parameters: []StrategyParameter{
			parameter("rebalancing.maximum_hops", "A graph route uses at most six reviewed facts.", "deterministic-simple-path.v1", "6", "count", "1", "6", true, true, 0, "down", "route_graph"),
			parameter("rebalancing.maximum_candidates", "A request considers at most 1024 complete simple paths.", "bounded-route-search.v1", "1024", "count", "1", "1024", true, true, 0, "down", "route_graph"),
			parameter("rebalancing.minimum_confidence", "Every selected fact has confidence of at least 0.80.", "reviewed-fact-confidence.v1", "0.80", "decimal_fraction", "0.80", "1", true, true, 18, "down", "fact_schema"),
			parameter("rebalancing.maximum_total_cost", "One recommendation costs at most 25 USDT after every component.", "exact-route-cost-cap.v1", "25", "USDT", "0", "25", false, true, 18, "down", "cost_model"),
			parameter("rebalancing.maximum_duration", "The upper duration bound is at most seven days.", "route-duration-cap.v1", "604800000", "milliseconds", "1", "604800000", true, true, 0, "down", "delay_model"),
			parameter("rebalancing.maximum_risk_score", "The additive operational risk score cannot exceed one.", "route-risk-cap.v1", "1", "decimal_fraction", "0", "1", true, true, 18, "down", "risk_model"),
			parameter("rebalancing.exact_cost_scale", "Every authoritative route cost supports scale eighteen.", "exact-fixed-point-cost.v1", "18", "decimal_places", "18", "18", true, true, 0, "down", "cost_model"),
			parameter("rebalancing.minimum_checklist_steps", "Every recommendation includes at least four explicit manual checks.", "manual-checklist.v1", "4", "count", "4", "4", true, true, 0, "down", "advisory_policy"),
			parameter("rebalancing.prefer_natural_reversal", "An eligible natural reverse plan is selected before a transfer route.", "natural-reversal-preference.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "inventory_policy"),
			parameter("rebalancing.transfer_execution_disabled", "External asset movement execution remains absent.", "advisory-only-boundary.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "compiled_safety_policy"),
			parameter("rebalancing.provenance_required", "Every selected fact has immutable approved provenance.", "immutable-provenance.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "fact_schema"),
			parameter("rebalancing.compatibility_fail_closed", "Missing, ambiguous, or incompatible network evidence is unavailable.", "network-compatibility.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "fact_schema"),
		},
	}
}
