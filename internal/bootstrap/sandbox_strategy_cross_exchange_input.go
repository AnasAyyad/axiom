package bootstrap

import (
	"fmt"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/crossarb"
)

// BuildCrossExchangeSandboxInput assembles one canonical paired decision from
// synchronized public books, independently owned venue inventory, account
// reserve facts, and the atomic conservative risk projection. It receives no
// adapter, credential, peer engine owner, fence, or submission capability.
func BuildCrossExchangeSandboxInput(
	work sandbox.StrategySessionWork,
	product config.Configuration,
	market SandboxCrossExchangeMarketInput,
	facts SandboxSagaPlanFacts,
	riskInputs *sandbox.StrategySagaRiskInputs,
) (crossarb.Input, error) {
	now := market.Trigger.UTC
	if work.Strategy != sandbox.StrategyCrossExchangeArbitrage ||
		work.Account.Exchange != sandbox.ExchangeBinance || config.Validate(product) != nil ||
		product.SchemaVersion != config.SchemaVersionSandboxRuntime || work.ValidAt(now) != nil ||
		facts.Coordinator.Work != work || validateSandboxSagaFacts(facts, work.Strategy, 2) != nil ||
		len(market.Markets) != 2 || len(market.Coherent.Members) != 2 ||
		market.Trigger.MonotonicNanos == 0 || market.Trigger.IngestOrdinal == 0 ||
		market.InstrumentMetadataSetHash == "" || riskInputs == nil {
		return crossarb.Input{}, fmt.Errorf("sandbox_cross_exchange_input_invalid")
	}
	configuration, err := crossarb.ConfigurationFromReviewed(product.CrossExchange)
	if err != nil {
		return crossarb.Input{}, fmt.Errorf("sandbox_cross_exchange_input_invalid")
	}
	base, err := sandboxSagaBaseAsset(work.Instrument)
	if err != nil {
		return crossarb.Input{}, fmt.Errorf("sandbox_cross_exchange_inventory_invalid")
	}
	inventories, feeBalances, err := sandboxCrossExchangeInventories(work, facts, market, base)
	if err != nil {
		return crossarb.Input{}, err
	}
	budget, err := sandboxCrossExchangeBudget(product, configuration, market)
	if err != nil {
		return crossarb.Input{}, err
	}
	riskInput, err := sandboxCrossExchangeRiskInput(riskInputs, facts, now)
	if err != nil {
		return crossarb.Input{}, err
	}
	restoration, err := sandboxCrossExchangeRestoration(product, market, inventories,
		budget, *riskInput.Observations.Spread, *riskInput.Observations.Slippage)
	if err != nil {
		return crossarb.Input{}, fmt.Errorf("sandbox_cross_exchange_restoration_unavailable")
	}
	input := newCrossExchangeSandboxInput(work, market, inventories, feeBalances, budget,
		configuration, restoration, riskInput)
	if _, err = input.EvaluationInput(); err != nil ||
		input.ValidateEventBinding(input.Ordinal, input.LogicalTime) != nil {
		return crossarb.Input{}, fmt.Errorf("sandbox_cross_exchange_input_invalid")
	}
	return input, nil
}

func newCrossExchangeSandboxInput(work sandbox.StrategySessionWork,
	market SandboxCrossExchangeMarketInput, inventories []crossarb.VenueInventory,
	feeBalances map[string]domain.Balance, budget domain.Balance,
	configuration crossarb.Configuration, restoration crossarb.RestorationEconomics,
	riskInput crossarb.RiskInput,
) crossarb.Input {
	return crossarb.Input{Ordinal: market.Trigger.IngestOrdinal,
		LogicalTime: market.Trigger.MonotonicNanos, Now: market.Trigger.UTC,
		Markets: append([]crossarb.MarketInput(nil), market.Markets...),
		Coherent: crossarb.CoherentViewInput{Identity: market.Coherent.Identity,
			Policy: market.Coherent.Policy, Trigger: market.Coherent.Trigger,
			Members: append([]runtimecore.ViewReference(nil), market.Coherent.Members...)},
		Inventory: inventories, QuoteBudget: budget, FeeBalances: feeBalances,
		Configuration: configuration, ConfigurationHash: work.ConfigurationHash,
		InstrumentMetadataSetHash: market.InstrumentMetadataSetHash, Restoration: restoration,
		CentralRisk: &riskInput,
	}
}

func sandboxCrossExchangeBudget(product config.Configuration, configuration crossarb.Configuration,
	market SandboxCrossExchangeMarketInput,
) (domain.Balance, error) {
	maximum, err := domain.ParseBalance(product.Sandbox.MaximumOrderNotional.Value)
	if err != nil {
		return domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_invalid")
	}
	budget := configuration.MaximumNotional
	if maximum.Compare(budget) < 0 {
		budget = maximum
	}
	budget, err = sandboxCrossSafeQuoteBudget(market, budget)
	if err != nil {
		return domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_invalid")
	}
	zero, _ := domain.ParseBalance("0")
	if budget.Compare(zero) <= 0 {
		return domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_unavailable")
	}
	return budget, nil
}

func sandboxCrossExchangeRiskInput(riskInputs *sandbox.StrategySagaRiskInputs,
	facts SandboxSagaPlanFacts, now time.Time,
) (crossarb.RiskInput, error) {
	observations, policies, evaluatedAt, err := riskInputs.Current()
	if err != nil || len(policies) != 1 || !evaluatedAt.Equal(now) ||
		policies[0].State != risk.StateNormal || observations.Spread == nil || observations.Slippage == nil {
		return crossarb.RiskInput{}, fmt.Errorf("sandbox_cross_exchange_risk_unavailable")
	}
	for _, riskFacts := range facts.RiskFacts {
		if policies[0].ID != riskFacts.PolicyID || policies[0].Version != riskFacts.PolicyVersion {
			return crossarb.RiskInput{}, fmt.Errorf("sandbox_cross_exchange_risk_unavailable")
		}
	}
	return crossarb.RiskInput{Policies: policies, Observations: observations, EvaluatedAt: evaluatedAt,
		Cautious: risk.CautiousControls{ReducedSize: true, StricterEdge: false,
			InstrumentEligible: true}}, nil
}

func sandboxCrossSafeQuoteBudget(
	market SandboxCrossExchangeMarketInput,
	maximum domain.Balance,
) (domain.Balance, error) {
	if len(market.Markets) != 2 {
		return domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_invalid")
	}
	byExchange := make(map[string]crossarb.MarketInput, 2)
	for _, member := range market.Markets {
		if len(member.Snapshot.Bids) == 0 || len(member.Snapshot.Asks) == 0 {
			return domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_invalid")
		}
		byExchange[string(member.Snapshot.Exchange)] = member
	}
	for _, direction := range [][2]string{{"binance", "bybit"}, {"bybit", "binance"}} {
		buy, buyOK := byExchange[direction[0]]
		sell, sellOK := byExchange[direction[1]]
		if !buyOK || !sellOK {
			return domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_invalid")
		}
		ask := buy.Snapshot.Asks[0].Price
		bid := sell.Snapshot.Bids[0].Price
		if bid.Compare(ask) <= 0 {
			continue
		}
		askMoney, firstErr := domain.ParseMoney(ask.String())
		bidMoney, secondErr := domain.ParseMoney(bid.String())
		if firstErr != nil || secondErr != nil {
			return domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_invalid")
		}
		fraction, fractionErr := domain.CalculatePercent(askMoney, bidMoney, 18)
		if fractionErr != nil {
			return domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_invalid")
		}
		safe, safeErr := domain.ScaleBalanceFloor(maximum, fraction, 18)
		if safeErr != nil {
			return domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_invalid")
		}
		if safe.Compare(maximum) < 0 {
			maximum = safe
		}
	}
	return maximum, nil
}

func sandboxCrossExchangeInventories(
	coordinator sandbox.StrategySessionWork,
	facts SandboxSagaPlanFacts,
	market SandboxCrossExchangeMarketInput,
	base domain.AssetSymbol,
) ([]crossarb.VenueInventory, map[string]domain.Balance, error) {
	members, totalBase, totalUSDT, err := sandboxCrossInventoryMembers(coordinator, facts, base)
	if err != nil {
		return nil, nil, err
	}
	owner := "strategy-session-" + string(coordinator.SessionID)
	inventories := make([]crossarb.VenueInventory, 0, 2)
	for _, exchange := range []sandbox.Exchange{sandbox.ExchangeBinance, sandbox.ExchangeBybit} {
		item := members[exchange]
		inventories = append(inventories, crossarb.VenueInventory{Owner: owner,
			Exchange: string(exchange), BaseAsset: base, OwnedBase: item.owned.Available,
			TotalEligibleBase: totalBase, OwnedUSDT: item.spendable,
			TotalEligibleUSDT: totalUSDT, Revision: coordinator.StrategyRevision})
	}
	feeBalances, err := sandboxCrossFeeBalances(market, facts, members)
	if err != nil {
		return nil, nil, err
	}
	return inventories, feeBalances, nil
}

type sandboxCrossInventoryMember struct {
	admission sandbox.StrategySessionAdmission
	owned     sandbox.StrategyOwnedInventory
	spendable domain.Balance
}

func sandboxCrossInventoryMembers(coordinator sandbox.StrategySessionWork,
	facts SandboxSagaPlanFacts, base domain.AssetSymbol,
) (map[sandbox.Exchange]sandboxCrossInventoryMember, domain.Balance, domain.Balance, error) {
	members := make(map[sandbox.Exchange]sandboxCrossInventoryMember, 2)
	totalBase, _ := domain.ParseBalance("0")
	totalUSDT, _ := domain.ParseBalance("0")
	zero, _ := domain.ParseBalance("0")
	for _, exchange := range []sandbox.Exchange{sandbox.ExchangeBinance, sandbox.ExchangeBybit} {
		admission, exists := facts.Admissions[exchange]
		if !exists || admission.Work.SessionID != coordinator.SessionID {
			return nil, domain.Balance{}, domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_inventory_invalid")
		}
		snapshot := facts.Snapshots[admission.Work.Account.ID]
		settlement, _, err := sandboxSagaAvailableBalances(snapshot)
		if err != nil {
			return nil, domain.Balance{}, domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_unavailable")
		}
		owned := facts.OwnedInventory[admission.Work.Account.ID]
		if owned.ValidFor(admission, base) != nil {
			return nil, domain.Balance{}, domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_inventory_invalid")
		}
		reserve, err := domain.ParseBalance(facts.RiskFacts[admission.Work.Account.ID].MinimumReserve.String())
		if err != nil {
			return nil, domain.Balance{}, domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_capital_invalid")
		}
		spendable, err := settlement.Subtract(reserve)
		if err != nil {
			spendable = zero
		}
		totalBase, err = totalBase.Add(owned.Available)
		if err == nil {
			totalUSDT, err = totalUSDT.Add(spendable)
		}
		if err != nil {
			return nil, domain.Balance{}, domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_inventory_invalid")
		}
		members[exchange] = sandboxCrossInventoryMember{admission: admission, owned: owned, spendable: spendable}
	}
	if totalBase.Compare(zero) <= 0 || totalUSDT.Compare(zero) <= 0 {
		return nil, domain.Balance{}, domain.Balance{}, fmt.Errorf("sandbox_cross_exchange_inventory_unavailable")
	}
	return members, totalBase, totalUSDT, nil
}

func sandboxCrossFeeBalances(market SandboxCrossExchangeMarketInput, facts SandboxSagaPlanFacts,
	members map[sandbox.Exchange]sandboxCrossInventoryMember,
) (map[string]domain.Balance, error) {
	zero, _ := domain.ParseBalance("0")
	feeBalances := make(map[string]domain.Balance, len(market.Markets))
	for _, marketMember := range market.Markets {
		exchange := sandbox.Exchange(marketMember.Snapshot.Exchange)
		item, exists := members[exchange]
		if !exists || marketMember.Rules.Fee.Asset == "" {
			return nil, fmt.Errorf("sandbox_cross_exchange_fee_facts_invalid")
		}
		snapshot := facts.Snapshots[item.admission.Work.Account.ID]
		balance, found := sandboxSnapshotAvailableBalance(snapshot, marketMember.Rules.Fee.Asset)
		if !found || balance.Compare(zero) <= 0 {
			return nil, fmt.Errorf("sandbox_cross_exchange_fee_facts_unavailable")
		}
		feeBalances[string(exchange)+":"+string(marketMember.Rules.Fee.Asset)] = balance
	}
	return feeBalances, nil
}

func sandboxSnapshotAvailableBalance(
	snapshot sandbox.AccountSnapshot,
	asset domain.AssetSymbol,
) (domain.Balance, bool) {
	for _, balance := range snapshot.Balances {
		if balance.Asset == asset {
			return balance.Available, true
		}
	}
	return domain.Balance{}, false
}
