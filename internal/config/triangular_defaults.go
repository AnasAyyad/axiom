package config

// TriangularParameterCount is the immutable size of the triangular arbitrage baseline graph.
const TriangularParameterCount = 18

func triangularParameter(
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
		Scale: scale, Rounding: rounding, Cadence: "every_approved_book_view",
		WarmUp: "three_healthy_approved_books", Mutability: "immutable_per_run",
		ModelDependencies: dependencies, AlgorithmVersion: algorithm,
		EvaluationTimezone: "UTC",
		ChangeBehavior:     "existing candidates retain their opening configuration; new decisions require a new snapshot",
		ApprovalActor:      "authoritative_specification",
		ApprovalReference:  "AX-V1B-B04-FUN-001/AX-V1B-B04-SAF-001",
		ApprovedAt:         "2026-07-23T00:00:00Z",
		ChangeReason:       "initial immutable triangular arbitrage exact triangular arbitrage baseline",
	}
}

func defaultTriangularConfiguration() TriangularConfiguration {
	parameters := []StrategyParameter{
		triangularParameter("triangular.leg_count", "Every approved cycle has exactly three sequential conversions.", "triangular-cycle-enumeration.v1", "3", "count", "3", "3", true, true, 0, "down", "cycle_model"),
		triangularParameter("triangular.size_ladder_10", "First reviewed USDT cycle size.", "triangular-size-ladder.v1", "10", "USDT", "10", "10", true, true, 18, "down", "portfolio_policy", "instrument_metadata"),
		triangularParameter("triangular.size_ladder_25", "Second reviewed USDT cycle size.", "triangular-size-ladder.v1", "25", "USDT", "25", "25", true, true, 18, "down", "portfolio_policy", "instrument_metadata"),
		triangularParameter("triangular.size_ladder_50", "Third reviewed USDT cycle size.", "triangular-size-ladder.v1", "50", "USDT", "50", "50", true, true, 18, "down", "portfolio_policy", "instrument_metadata"),
		triangularParameter("triangular.size_ladder_100", "Fourth reviewed USDT cycle size.", "triangular-size-ladder.v1", "100", "USDT", "100", "100", true, true, 18, "down", "portfolio_policy", "instrument_metadata"),
		triangularParameter("triangular.dynamic_size_enabled", "One dynamically clipped size is evaluated with the reviewed ladder.", "triangular-dynamic-clip.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "portfolio_policy", "depth_model"),
		triangularParameter("triangular.maximum_cycle_notional", "Maximum settlement amount entering one cycle.", "triangular-size-cap.v1", "100", "USDT", "0", "100", false, true, 18, "down", "portfolio_policy"),
		triangularParameter("triangular.candidate_lifetime", "Candidate claim and dispatch must occur at or before this lifetime.", "triangular-opportunity-lifetime.v1", "250", "milliseconds", "0", "250", false, true, 0, "down", "latency_model"),
		triangularParameter("triangular.arrival_book_max_age", "Every leg book age must remain at or below this threshold.", "triangular-book-freshness.v2", "100", "milliseconds", "0", "250", false, true, 0, "down", "market_view_model"),
		triangularParameter("triangular.minimum_net_edge", "Expected and worst-case cycle economics must be strictly positive.", "triangular-net-edge.v1", "0", "decimal_fraction", "0", "1", true, true, 18, "down", "fee_model", "depth_model"),
		triangularParameter("triangular.additional_safety_margin", "Worst-case net edge must be strictly above fifteen basis points.", "triangular-safety-margin.v1", "0.0015", "decimal_fraction", "0.0015", "0.0015", true, true, 18, "ceiling", "risk_policy", "latency_model"),
		triangularParameter("triangular.latency_deterioration", "Per-leg conservative output haircut used by worst-case qualification.", "triangular-latency-haircut.v1", "0.0005", "decimal_fraction", "0", "0.01", true, true, 18, "ceiling", "latency_model"),
		triangularParameter("triangular.maximum_recovery_attempts", "A failed cycle permits one immediate bounded conversion-to-USDT attempt.", "triangular-recovery.v1", "1", "count", "1", "1", true, true, 0, "down", "recovery_model"),
		triangularParameter("triangular.recovery_reserve_fraction", "Minimum separately claimed recovery capacity as a fraction of cycle start.", "triangular-recovery-reserve.v1", "0.02", "decimal_fraction", "0", "0.10", false, true, 18, "ceiling", "portfolio_policy", "recovery_model"),
		triangularParameter("triangular.atomic_claim_required", "Funds, fee buffers, recovery capacity, and depth must be one atomic claim group.", "atomic-multi-resource-claim.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "claim_model"),
		triangularParameter("triangular.approved_book_required", "Only healthy approved active-generation books are eligible.", "approved-book-view.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "market_view_model"),
		triangularParameter("triangular.opportunity_metric_window", "Bounded survivor samples retained for p50, p95, and p99 lifetime evidence.", "opportunity-lifetime-window.v1", "1000", "samples", "100", "10000", true, true, 0, "down", "observability_model"),
		triangularParameter("triangular.maximum_concurrent_claims", "One portfolio resource can have only one active or quarantined owner.", "exclusive-claim-ownership.v1", "1", "count", "1", "1", true, true, 0, "down", "claim_model"),
	}
	for index := range parameters {
		if parameters[index].ID == "triangular.arrival_book_max_age" {
			parameters[index].ApprovalActor = "owner_review"
			parameters[index].ApprovalReference = "ADR-0028"
			parameters[index].ApprovedAt = "2026-08-13T00:00:00Z"
			parameters[index].ChangeReason = "reviewed 100ms same-exchange as-of freshness ceiling"
		}
	}
	return TriangularConfiguration{
		StrategyVersion: "triangular-arbitrage@1.0.0", SettlementAsset: "USDT",
		Cycles:       []string{"USDT-BTC-ETH-USDT", "USDT-ETH-BTC-USDT"},
		DispatchMode: "sequential", PricingModel: "triangular-exact-depth.v1",
		ClaimModel: "atomic-multi-resource.v1", Parameters: parameters,
	}
}
