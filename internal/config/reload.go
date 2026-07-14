package config

import (
	"reflect"

	"axiom/internal/domain"
)

func validateReload(current, next Configuration) error {
	if current.Environment != next.Environment || current.Mode != next.Mode || current.Product != next.Product {
		return configError("restart_required", "identity")
	}
	if !reflect.DeepEqual(current.Safety, next.Safety) || !reflect.DeepEqual(current.Endpoint, next.Endpoint) {
		return configError("restart_required", "wiring")
	}
	if !reflect.DeepEqual(current.Secrets, next.Secrets) || !reflect.DeepEqual(current.Capabilities, next.Capabilities) {
		return configError("restart_required", "security")
	}
	if !reflect.DeepEqual(current.Portfolio, next.Portfolio) {
		return configError("restart_required", "portfolio")
	}
	if !instrumentSubset(current.Instruments, next.Instruments) {
		return configError("reload_rejected", "instruments")
	}
	if !assetTransitionSafe(current.Assets, next.Assets) {
		return configError("reload_rejected", "assets")
	}
	return validateRiskTightening(current.Risk, next.Risk)
}

func instrumentSubset(current, next []Instrument) bool {
	allowed := make(map[Instrument]struct{}, len(current))
	for _, instrument := range current {
		allowed[instrument] = struct{}{}
	}
	for _, instrument := range next {
		if _, ok := allowed[instrument]; !ok {
			return false
		}
	}
	return true
}

func assetTransitionSafe(current, next []domain.Asset) bool {
	prior := make(map[domain.AssetSymbol]domain.AssetStatus, len(current))
	for _, asset := range current {
		prior[asset.Symbol] = asset.Status
	}
	if len(prior) != len(next) {
		return false
	}
	for _, asset := range next {
		status, ok := prior[asset.Symbol]
		if !ok || (status != domain.AssetApproved && asset.Status == domain.AssetApproved) {
			return false
		}
	}
	return true
}

func validateRiskTightening(current, next RiskConfiguration) error {
	pairs := []struct {
		field string
		old   FinancialValue
		new   FinancialValue
	}{
		{field: "maximum_asset_allocation", old: current.MaximumAssetAllocation, new: next.MaximumAssetAllocation},
		{field: "maximum_order_notional", old: current.MaximumOrderNotional, new: next.MaximumOrderNotional},
		{field: "maximum_daily_loss", old: current.MaximumDailyLoss, new: next.MaximumDailyLoss},
	}
	for _, pair := range pairs {
		if !sameFinancialContract(pair.old, pair.new) || financialValueIncreased(pair.old, pair.new) {
			return configError("risk_loosening_rejected", "risk."+pair.field)
		}
	}
	return nil
}

func sameFinancialContract(left, right FinancialValue) bool {
	left.Value, right.Value = "", ""
	return left == right
}

func financialValueIncreased(current, next FinancialValue) bool {
	if current.Unit == "decimal_fraction" {
		oldValue, oldError := domain.ParsePercent(current.Value)
		newValue, newError := domain.ParsePercent(next.Value)
		return oldError != nil || newError != nil || newValue.Compare(oldValue) > 0
	}
	oldValue, oldError := domain.ParseMoney(current.Value)
	newValue, newError := domain.ParseMoney(next.Value)
	return oldError != nil || newError != nil || newValue.Compare(oldValue) > 0
}

func configurationChanges(current *Snapshot, next Configuration) []string {
	if current == nil {
		return []string{"initial"}
	}
	prior := current.configuration
	changes := []string{"revision"}
	fields := []struct {
		name     string
		oldValue any
		newValue any
	}{
		{name: "assets", oldValue: prior.Assets, newValue: next.Assets},
		{name: "instruments", oldValue: prior.Instruments, newValue: next.Instruments},
		{name: "risk", oldValue: prior.Risk, newValue: next.Risk},
		{name: "models", oldValue: prior.Models, newValue: next.Models},
	}
	for _, field := range fields {
		if !reflect.DeepEqual(field.oldValue, field.newValue) {
			changes = append(changes, field.name)
		}
	}
	return changes
}
