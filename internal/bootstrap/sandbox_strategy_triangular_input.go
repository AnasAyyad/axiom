package bootstrap

import (
	"fmt"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/risk"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/triangular"
)

// BuildTriangularSandboxInput assembles one canonical automatic sandbox
// decision from the synchronized three-book capture, reviewed configuration,
// exact account capacity, and already-projected central-risk evidence. It has
// no adapter, credential, dispatcher, or database access.
func BuildTriangularSandboxInput(
	work sandbox.StrategySessionWork,
	product config.Configuration,
	market SandboxTriangularMarketInput,
	facts SandboxSagaPlanFacts,
	riskInputs *sandbox.StrategySagaRiskInputs,
) (triangular.Input, error) {
	now := market.Trigger.UTC
	if work.Strategy != sandbox.StrategyTriangular || config.Validate(product) != nil ||
		product.SchemaVersion != config.SchemaVersionSandboxRuntime || work.ValidAt(now) != nil ||
		facts.Coordinator.Work != work || validateSandboxSagaFacts(facts, work.Strategy, 1) != nil ||
		len(market.Markets) != 3 || market.Trigger.MonotonicNanos == 0 ||
		market.Trigger.IngestOrdinal == 0 || market.FirstDetectedOffset == 0 ||
		market.FirstDetectedOffset > market.Trigger.MonotonicNanos ||
		market.CoherentViewID == "" || market.InstrumentMetadataID == "" || riskInputs == nil {
		return triangular.Input{}, fmt.Errorf("sandbox_triangular_input_invalid")
	}
	configuration, err := triangular.ConfigurationFromReviewed(product.Triangular)
	if err != nil {
		return triangular.Input{}, fmt.Errorf("sandbox_triangular_input_invalid")
	}
	snapshot := facts.Snapshots[work.Account.ID]
	riskFacts := facts.RiskFacts[work.Account.ID]
	capital, err := triangularSandboxCapital(product, configuration, snapshot, riskFacts)
	if err != nil {
		return triangular.Input{}, err
	}
	riskInput, err := triangularSandboxRiskInput(riskInputs, riskFacts, now)
	if err != nil {
		return triangular.Input{}, err
	}
	input := triangular.Input{
		Ordinal: market.Trigger.IngestOrdinal, LogicalTime: market.Trigger.MonotonicNanos,
		Now: now, Exchange: string(work.Account.Exchange),
		Markets:             append([]triangular.MarketInput(nil), market.Markets...),
		FirstDetectedOffset: market.FirstDetectedOffset, AvailableSettlement: capital.available,
		StrategyBudget: capital.budget, GlobalReserveFloor: capital.reserve,
		RecoveryAllowance: capital.recovery, FeeBalances: capital.balances,
		Configuration: configuration, ConfigurationHash: work.ConfigurationHash,
		InstrumentMetadataID: market.InstrumentMetadataID, CentralRisk: &riskInput,
	}
	if _, err = input.EvaluationInput(); err != nil ||
		input.ValidateEventBinding(input.Ordinal, input.LogicalTime) != nil {
		return triangular.Input{}, fmt.Errorf("sandbox_triangular_input_invalid")
	}
	return input, nil
}

type triangularSandboxCapitalFacts struct {
	available, budget, reserve, recovery domain.Balance
	balances                             map[domain.AssetSymbol]domain.Balance
}

func triangularSandboxCapital(product config.Configuration, configuration triangular.Configuration,
	snapshot sandbox.AccountSnapshot, riskFacts sandbox.StrategyRiskFacts,
) (triangularSandboxCapitalFacts, error) {
	available, balances, err := sandboxSagaAvailableBalances(snapshot)
	if err != nil {
		return triangularSandboxCapitalFacts{}, fmt.Errorf("sandbox_triangular_capital_unavailable")
	}
	reserve, err := domain.ParseBalance(riskFacts.MinimumReserve.String())
	if err != nil {
		return triangularSandboxCapitalFacts{}, fmt.Errorf("sandbox_triangular_capital_invalid")
	}
	spendable, err := available.Subtract(reserve)
	if err != nil {
		return triangularSandboxCapitalFacts{}, fmt.Errorf("sandbox_triangular_capital_unavailable")
	}
	maximum, err := domain.ParseBalance(product.Sandbox.MaximumOrderNotional.Value)
	if err != nil {
		return triangularSandboxCapitalFacts{}, fmt.Errorf("sandbox_triangular_capital_invalid")
	}
	configuredMaximum, err := domain.ParseBalance(configuration.MaximumCycleNotional.String())
	if err != nil {
		return triangularSandboxCapitalFacts{}, fmt.Errorf("sandbox_triangular_capital_invalid")
	}
	budget := maximum
	if configuredMaximum.Compare(budget) < 0 {
		budget = configuredMaximum
	}
	capacity := spendable
	if budget.Compare(capacity) < 0 {
		capacity = budget
	}
	fraction, err := triangularRecoveryReserveFraction(product.Triangular)
	if err != nil {
		return triangularSandboxCapitalFacts{}, fmt.Errorf("sandbox_triangular_capital_invalid")
	}
	recovery, err := domain.ScaleBalanceCeiling(capacity, fraction, 18)
	zero, zeroErr := domain.ParseBalance("0")
	if err != nil || zeroErr != nil || capacity.Compare(recovery) <= 0 || recovery.Compare(zero) <= 0 {
		return triangularSandboxCapitalFacts{}, fmt.Errorf("sandbox_triangular_capital_unavailable")
	}
	return triangularSandboxCapitalFacts{available: available, budget: budget, reserve: reserve,
		recovery: recovery, balances: balances}, nil
}

func triangularSandboxRiskInput(riskInputs *sandbox.StrategySagaRiskInputs,
	riskFacts sandbox.StrategyRiskFacts, now time.Time,
) (triangular.RiskInput, error) {
	observations, policies, evaluatedAt, err := riskInputs.Current()
	if err != nil || len(policies) != 1 || !evaluatedAt.Equal(now) ||
		policies[0].ID != riskFacts.PolicyID || policies[0].Version != riskFacts.PolicyVersion ||
		policies[0].State != risk.StateNormal {
		return triangular.RiskInput{}, fmt.Errorf("sandbox_triangular_risk_unavailable")
	}
	return triangular.RiskInput{Policies: policies, Observations: observations,
		EvaluatedAt: evaluatedAt, Cautious: risk.CautiousControls{ReducedSize: true,
			StricterEdge: false, InstrumentEligible: true}}, nil
}

func sandboxSagaAvailableBalances(
	snapshot sandbox.AccountSnapshot,
) (domain.Balance, map[domain.AssetSymbol]domain.Balance, error) {
	if snapshot.Validate() != nil {
		return domain.Balance{}, nil, fmt.Errorf("sandbox_saga_balances_invalid")
	}
	zero, _ := domain.ParseBalance("0")
	values := make(map[domain.AssetSymbol]domain.Balance, len(snapshot.Balances))
	var settlement domain.Balance
	foundSettlement := false
	for _, balance := range snapshot.Balances {
		if _, duplicate := values[balance.Asset]; duplicate {
			return domain.Balance{}, nil, fmt.Errorf("sandbox_saga_balances_invalid")
		}
		if balance.Available.Compare(zero) > 0 {
			values[balance.Asset] = balance.Available
		}
		if balance.Asset == "USDT" {
			settlement = balance.Available
			foundSettlement = true
		}
	}
	if !foundSettlement || settlement.Compare(zero) <= 0 {
		return domain.Balance{}, nil, fmt.Errorf("sandbox_saga_balances_unavailable")
	}
	return settlement, values, nil
}

func triangularRecoveryReserveFraction(
	reviewed config.TriangularConfiguration,
) (domain.Percent, error) {
	if config.ValidateTriangularConfiguration(reviewed) != nil {
		return domain.Percent{}, fmt.Errorf("sandbox_triangular_recovery_fraction_invalid")
	}
	for _, parameter := range reviewed.Parameters {
		if parameter.ID != "triangular.recovery_reserve_fraction" {
			continue
		}
		if parameter.Rounding != "ceiling" {
			return domain.Percent{}, fmt.Errorf("sandbox_triangular_recovery_fraction_invalid")
		}
		return domain.ParsePercent(parameter.Value)
	}
	return domain.Percent{}, fmt.Errorf("sandbox_triangular_recovery_fraction_unavailable")
}
