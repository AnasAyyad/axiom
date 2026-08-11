package evaluation

// EvidenceMetrics are exact selection inputs derived from the virtual ledger.
// Financial values use USDT micro-units and drawdown uses basis points.
type EvidenceMetrics struct {
	NetResultMicros       int64 `json:"net_result_micros"`
	Stress15NetMicros     int64 `json:"stress_1_5_net_micros"`
	GrossProfitMicros     int64 `json:"gross_profit_micros"`
	LargestWinMicros      int64 `json:"largest_win_micros"`
	MaximumDrawdownBPS    int64 `json:"maximum_drawdown_bps"`
	TradeCount            int64 `json:"trade_count"`
	RouteEvidenceCount    int64 `json:"route_evidence_count"`
	SnapshotEvidenceCount int64 `json:"snapshot_evidence_count"`
	DatasetCorrect        bool  `json:"dataset_correct"`
	RuntimeCorrect        bool  `json:"runtime_correct"`
	AccountingReconciled  bool  `json:"accounting_reconciled"`
	NoNegativeInventory   bool  `json:"no_negative_inventory"`
	NoDuplicateFill       bool  `json:"no_duplicate_fill"`
	NoUnsupportedSale     bool  `json:"no_unsupported_sale"`
	DeterministicRepeat   bool  `json:"deterministic_repeat"`
}

// SelectionPolicy is fixed and versioned by the preset.
type SelectionPolicy struct {
	MaximumDrawdownBPS        int64
	MinimumTrades             int64
	MinimumRoutes             int64
	MaximumLargestWinShareBPS int64
}

// BalancedSelectionPolicy implements the plan's pre-shadow gate.
func BalancedSelectionPolicy() SelectionPolicy {
	return SelectionPolicy{MaximumDrawdownBPS: 300, MinimumTrades: 20,
		MinimumRoutes: 20, MaximumLargestWinShareBPS: 5_000}
}

// EvaluateCandidate returns a deterministic verdict and stable reason. A
// correctness or safety failure is BLOCKED; an unprofitable but trustworthy
// result is REJECT or IMPROVE rather than hidden.
func EvaluateCandidate(strategy Strategy, metrics EvidenceMetrics, policy SelectionPolicy) (Verdict, ReasonCode) {
	if !metrics.DatasetCorrect {
		return VerdictBlocked, ReasonDataCorrupt
	}
	if !metrics.RuntimeCorrect || !metrics.DeterministicRepeat {
		return VerdictBlocked, ReasonSafetyFailed
	}
	if !metrics.AccountingReconciled || !metrics.NoNegativeInventory ||
		!metrics.NoDuplicateFill || !metrics.NoUnsupportedSale {
		return VerdictBlocked, ReasonAccountingFailed
	}
	if metrics.MaximumDrawdownBPS > policy.MaximumDrawdownBPS {
		return VerdictReject, "DRAWDOWN_LIMIT_EXCEEDED"
	}
	if strategy == StrategyInventory {
		if metrics.RouteEvidenceCount < policy.MinimumRoutes || metrics.SnapshotEvidenceCount == 0 {
			return VerdictImprove, "ADVISORY_SAMPLE_INSUFFICIENT"
		}
		return VerdictContinue, "ADVISORY_EVIDENCE_ACCEPTED"
	}
	if metrics.TradeCount < policy.MinimumTrades {
		return VerdictImprove, "TRADE_SAMPLE_INSUFFICIENT"
	}
	if metrics.NetResultMicros <= 0 {
		return VerdictReject, "FINAL_TEST_NOT_POSITIVE"
	}
	if metrics.Stress15NetMicros <= 0 {
		return VerdictImprove, "COST_STRESS_NOT_POSITIVE"
	}
	if metrics.GrossProfitMicros <= 0 || metrics.LargestWinMicros < 0 ||
		metrics.LargestWinMicros > metrics.GrossProfitMicros {
		return VerdictBlocked, ReasonAccountingFailed
	}
	if metrics.LargestWinMicros*10_000 > metrics.GrossProfitMicros*policy.MaximumLargestWinShareBPS {
		return VerdictImprove, "EXCEPTIONAL_TRADE_DEPENDENCY"
	}
	return VerdictContinue, "PRE_SHADOW_GATE_PASSED"
}
