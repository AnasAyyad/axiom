package config

// CrossExchangeParameterCount is the immutable B5 baseline graph size.
const CrossExchangeParameterCount = 20

func crossExchangeParameter(
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
		Scale: scale, Rounding: rounding, Cadence: "every_coherent_two_venue_view",
		WarmUp: "both_venues_healthy_with_owned_inventory", Mutability: "immutable_per_run",
		ModelDependencies: dependencies, AlgorithmVersion: algorithm, EvaluationTimezone: "UTC",
		ChangeBehavior:    "existing candidates retain their opening configuration; new decisions require a new snapshot",
		ApprovalActor:     "authoritative_specification",
		ApprovalReference: "AX-V1B-B05-FUN-001/AX-V1B-B05-SAF-001",
		ApprovedAt:        "2026-07-23T00:00:00Z",
		ChangeReason:      "initial immutable B5 closed-cycle cross-exchange arbitrage baseline",
	}
}

func defaultCrossExchangeConfiguration() CrossExchangeConfiguration {
	parameter := crossExchangeParameter
	parameters := []StrategyParameter{
		parameter("cross_exchange.instrument_count", "Exactly BTC-USDT and ETH-USDT are evaluated.", "cross-exchange-universe.v1", "2", "count", "2", "2", true, true, 0, "down", "instrument_metadata"),
		parameter("cross_exchange.venue_count", "Exactly Binance and Bybit public views are compared.", "cross-exchange-venues.v1", "2", "count", "2", "2", true, true, 0, "down", "market_view_model"),
		parameter("cross_exchange.direction_count", "Both buy/sell venue orderings are evaluated.", "cross-exchange-directions.v1", "2", "count", "2", "2", true, true, 0, "down", "cycle_model"),
		parameter("cross_exchange.maximum_book_age", "Each B2 member is at most 250 milliseconds old.", "coherent-book-age.v1", "250", "milliseconds", "0", "250", false, true, 0, "down", "market_view_model"),
		parameter("cross_exchange.maximum_inter_book_skew", "The B2 member receive skew is at most 250 milliseconds.", "coherent-book-skew.v1", "250", "milliseconds", "0", "250", false, true, 0, "down", "market_view_model"),
		parameter("cross_exchange.maximum_clock_uncertainty", "Each corrected receive interval has at most 100 milliseconds uncertainty.", "coherent-clock-uncertainty.v1", "100", "milliseconds", "0", "100", false, true, 0, "down", "market_view_model"),
		parameter("cross_exchange.candidate_lifetime", "Claim and virtual dispatch expire after 250 milliseconds.", "cross-exchange-candidate-lifetime.v1", "250", "milliseconds", "0", "250", false, true, 0, "down", "latency_model"),
		parameter("cross_exchange.maximum_notional", "One two-leg candidate uses at most 100 USDT.", "cross-exchange-notional-cap.v1", "100", "USDT", "0", "100", false, true, 18, "down", "portfolio_policy"),
		parameter("cross_exchange.reduced_direction_maximum_notional", "A depleted-direction candidate is clipped to 50 USDT.", "cross-exchange-reduced-cap.v1", "50", "USDT", "0", "50", false, true, 18, "down", "portfolio_policy"),
		parameter("cross_exchange.lower_inventory_band", "At or below thirty percent the depleted sell direction pauses.", "cross-exchange-inventory-band.v1", "0.30", "decimal_fraction", "0.30", "0.30", true, true, 18, "down", "portfolio_policy"),
		parameter("cross_exchange.target_inventory_band", "Fifty percent is the neutral inventory target.", "cross-exchange-inventory-band.v1", "0.50", "decimal_fraction", "0.50", "0.50", true, true, 18, "down", "portfolio_policy"),
		parameter("cross_exchange.upper_inventory_band", "Above seventy percent natural reversal is preferred.", "cross-exchange-inventory-band.v1", "0.70", "decimal_fraction", "0.70", "0.70", true, true, 18, "down", "portfolio_policy"),
		parameter("cross_exchange.minimum_closed_cycle_edge", "Expected and worst closed inventory cycle profit must be strictly positive.", "closed-cycle-profit.v1", "0", "decimal_fraction", "0", "1", true, true, 18, "down", "fee_model", "latency_model", "inventory_shadow_price"),
		parameter("cross_exchange.maximum_recovery_attempts", "Only one central-risk-approved retry may follow completed unknown verification.", "cross-exchange-recovery.v1", "1", "count", "1", "1", true, true, 0, "down", "recovery_model"),
		parameter("cross_exchange.unknown_verification_required", "Unknown is verified before it can be retried, unwound, or quarantined.", "unknown-verification.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "recovery_model"),
		parameter("cross_exchange.atomic_claim_required", "Both balances, fees, depth, and recovery capacity form one claim.", "atomic-multi-resource-claim.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "claim_model"),
		parameter("cross_exchange.concurrent_dispatch_required", "Both virtual legs use the deterministic concurrent scheduler.", "deterministic-concurrent-scheduler.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "latency_model"),
		parameter("cross_exchange.inventory_restoration_required", "Profit includes marginal inventory replacement and restoration.", "inventory-restoration.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "inventory_shadow_price"),
		parameter("cross_exchange.usdt_concentration_penalty_required", "USDT venue concentration is charged separately.", "usdt-concentration.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "concentration_model"),
		parameter("cross_exchange.rebalancing_advisory_only", "B5 emits read-only needs and has no transfer or B6 executor.", "rebalancing-advisory-only.v1", "1", "boolean_integer", "1", "1", true, true, 0, "down", "portfolio_policy"),
	}
	return CrossExchangeConfiguration{
		StrategyVersion: "cross-exchange.v1b.1", SettlementAsset: "USDT",
		Instruments: []string{"BTCUSDT", "ETHUSDT"}, Exchanges: []string{"binance", "bybit"},
		Directions:   []string{"buy_binance_sell_bybit", "buy_bybit_sell_binance"},
		DispatchMode: "concurrent", PricingModel: "cross-exchange-closed-cycle.v1",
		ClaimModel: "atomic-multi-resource.v1", RebalancingMode: "advisory_only",
		Parameters: parameters,
	}
}
