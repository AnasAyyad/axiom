package risk

import (
	"sort"
	"time"
)

func evaluatePolicies(request Request, engineState State) Decision {
	decision := Decision{Action: ActionApprove, ReasonCode: "approved", EffectiveState: engineState,
		EvaluatedAt: request.EvaluatedAt}
	policies := append([]Policy(nil), request.Policies...)
	sort.Slice(policies, func(left, right int) bool {
		leftRank, rightRank := scopeRank(policies[left].Scope.Kind), scopeRank(policies[right].Scope.Kind)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if policies[left].ID != policies[right].ID {
			return policies[left].ID < policies[right].ID
		}
		return policies[left].Version < policies[right].Version
	})
	for _, policy := range policies {
		decision.ContributingIDs = append(decision.ContributingIDs, policy.ID)
		decision.PolicyVersions = append(decision.PolicyVersions, policy.Version)
		if stateRank(policy.State) > stateRank(decision.EffectiveState) {
			decision.EffectiveState = policy.State
		}
	}
	violationReason, violationAction := "", Action("")
	for _, policy := range policies {
		if reason, action := thresholdViolation(request.Observations, policy.Limits); reason != "" {
			if actionRank(action) > actionRank(violationAction) ||
				(actionRank(action) == actionRank(violationAction) && (violationReason == "" || reason < violationReason)) {
				violationReason, violationAction = reason, action
			}
		}
	}
	if violationReason != "" {
		return withRejection(decision, violationReason, violationAction)
	}
	return applyPermission(request, decision)
}

func thresholdViolation(observation Observations, limits Limits) (string, Action) {
	if !completeObservations(observation) {
		return "risk_input_missing", ActionLockEngine
	}
	if reason, action := healthViolation(observation.Health); reason != "" {
		return reason, action
	}
	checks := []struct {
		failed bool
		reason string
		action Action
	}{
		{observation.AccountDrawdown.Compare(limits.AccountDrawdown) >= 0, "account_drawdown_limit", ActionLockEngine},
		{observation.UTCDayLoss.Compare(limits.DayLoss) >= 0, "utc_day_loss_limit", ActionLockEngine},
		{observation.Rolling24HourLoss.Compare(limits.RollingLoss) >= 0, "rolling_loss_limit", ActionLockEngine},
		{observation.StrategyLoss.Compare(limits.StrategyLoss) >= 0, "strategy_loss_limit", ActionPauseStrategy},
		{observation.AssetExposure.Compare(limits.AssetExposure) > 0, "asset_exposure_limit", ActionReject},
		{observation.CombinedExposure.Compare(limits.CombinedExposure) > 0, "combined_exposure_limit", ActionReject},
		{observation.ExchangeExposure.Compare(limits.ExchangeExposure) > 0, "exchange_exposure_limit", ActionPauseExchange},
		{observation.Reserve.Compare(limits.MinimumReserve) < 0, "minimum_reserve_limit", ActionReject},
		{observation.ReservedCapital.Compare(limits.ReservedCapital) > 0, "reserved_capital_limit", ActionReject},
		{*observation.OpenOrders > limits.MaximumOpenOrders, "open_order_limit", ActionReject},
		{observation.Spread.Compare(limits.MaximumSpread) > 0, "spread_limit", ActionPauseInstrument},
		{observation.Slippage.Compare(limits.MaximumSlippage) > 0, "slippage_limit", ActionPauseInstrument},
		{*observation.BookAge >= limits.MaximumBookAge, "book_age_limit", ActionPauseInstrument},
		{*observation.QueueLag > limits.MaximumQueueLag, "queue_lag_limit", ActionPauseExchange},
		{absoluteDuration(*observation.ClockDrift) > limits.MaximumClockDrift, "clock_drift_limit", ActionPauseExchange},
		{*observation.QualityScore < limits.MinimumQuality, "quality_score_limit", ActionPauseInstrument},
	}
	for _, check := range checks {
		if check.failed {
			return check.reason, check.action
		}
	}
	return "", ""
}

func completeObservations(value Observations) bool {
	return value.AccountDrawdown != nil && value.UTCDayLoss != nil && value.Rolling24HourLoss != nil &&
		value.StrategyLoss != nil && value.AssetExposure != nil && value.CombinedExposure != nil &&
		value.ExchangeExposure != nil && value.Reserve != nil && value.ReservedCapital != nil &&
		value.Spread != nil && value.Slippage != nil && value.OpenOrders != nil && value.BookAge != nil &&
		value.QueueLag != nil && value.ClockDrift != nil && value.QualityScore != nil && completeHealth(value.Health)
}

func completeHealth(value HealthInputs) bool {
	return value.Gap != nil && value.StaleData != nil && value.ReconciliationFault != nil &&
		value.AccountingFault != nil && value.UnknownOrder != nil && value.PersistenceFault != nil &&
		value.DiskFault != nil && value.APIError != nil && value.LeaseLost != nil
}

func healthViolation(value HealthInputs) (string, Action) {
	checks := []struct {
		failed bool
		reason string
		action Action
	}{
		{*value.PersistenceFault, "persistence_failure", ActionLockEngine},
		{*value.DiskFault, "disk_failure", ActionLockEngine},
		{*value.LeaseLost, "lease_loss", ActionLockEngine},
		{*value.ReconciliationFault, "reconciliation_mismatch", ActionQuarantine},
		{*value.AccountingFault, "accounting_mismatch", ActionQuarantine},
		{*value.UnknownOrder, "unknown_order", ActionQuarantine},
		{*value.Gap, "sequence_gap", ActionPauseExchange},
		{*value.StaleData, "stale_data", ActionPauseExchange},
		{*value.APIError, "api_failure", ActionPauseExchange},
	}
	for _, check := range checks {
		if check.failed {
			return check.reason, check.action
		}
	}
	return "", ""
}

func applyPermission(request Request, decision Decision) Decision {
	state := decision.EffectiveState
	if request.Intent == IntentCancel || request.Intent == IntentReconciliation {
		return decision
	}
	if state == StateNormal || state == StateCautious {
		if state == StateCautious && request.Intent == IntentEntry &&
			(!request.Cautious.ReducedSize || !request.Cautious.StricterEdge || !request.Cautious.InstrumentEligible) {
			return withRejection(decision, "cautious_controls_missing", ActionReject)
		}
		return decision
	}
	if request.Intent == IntentEntry {
		return withRejection(decision, "entry_state_rejected", ActionReject)
	}
	if state == StatePaused && request.RiskReducing &&
		(request.Intent == IntentExit || request.Intent == IntentRecovery) {
		return decision
	}
	if state == StateLocked && request.RiskReducing && request.LockedPolicyApproved &&
		(request.Intent == IntentExit || request.Intent == IntentRecovery) {
		return decision
	}
	return withRejection(decision, "intent_state_rejected", ActionQuarantine)
}

func scopeRank(scope ScopeKind) int {
	switch scope {
	case ScopeGlobal:
		return 0
	case ScopeAccount:
		return 1
	case ScopeExchange:
		return 2
	case ScopeStrategy:
		return 3
	case ScopePortfolio:
		return 4
	case ScopeAsset:
		return 5
	case ScopeInstrument:
		return 6
	default:
		return 7
	}
}

func actionRank(action Action) int {
	switch action {
	case ActionLockEngine:
		return 7
	case ActionQuarantine:
		return 6
	case ActionPauseExchange:
		return 5
	case ActionPauseStrategy:
		return 4
	case ActionPauseInstrument:
		return 3
	case ActionReject:
		return 2
	case ActionApprove:
		return 1
	default:
		return 0
	}
}

func withRejection(decision Decision, reason string, action Action) Decision {
	decision.Action, decision.ReasonCode = action, reason
	if action == ActionLockEngine {
		decision.EffectiveState = StateLocked
	} else if stateRank(decision.EffectiveState) < stateRank(StatePaused) && action != ActionReject {
		decision.EffectiveState = StatePaused
	}
	return decision
}

func absoluteDuration(value time.Duration) time.Duration {
	if value == time.Duration(-1<<63) {
		return time.Duration(1<<63 - 1)
	}
	if value < 0 {
		return -value
	}
	return value
}
