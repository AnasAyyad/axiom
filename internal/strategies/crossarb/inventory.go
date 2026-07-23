package crossarb

import (
	"axiom/internal/domain"
)

// BandState is the deterministic 30/50/70 inventory control state.
type BandState string

// Inventory control band values implement the reviewed 30/50/70 policy.
const (
	// BandPaused blocks a sell direction at or below the lower band.
	BandPaused BandState = "paused_depleted"
	// BandReduced clips a sell direction between the lower and target bands.
	BandReduced BandState = "reduced"
	// BandNormal permits the reviewed cap from the target through upper band.
	BandNormal BandState = "normal"
	// BandPreferred prioritizes natural reversal above the upper band.
	BandPreferred BandState = "preferred_natural_reverse"
)

func inventoryControl(buy, sell VenueInventory, configuration Configuration) (InventoryControl, error) {
	if buy.Owner == "" || sell.Owner == "" || buy.Owner != sell.Owner ||
		buy.Exchange == sell.Exchange || buy.BaseAsset != sell.BaseAsset ||
		buy.Revision == 0 || sell.Revision == 0 {
		return InventoryControl{}, strategyError("inventory_identity_invalid")
	}
	sellShare, err := inventoryShare(sell.OwnedBase, sell.TotalEligibleBase)
	if err != nil {
		return InventoryControl{}, err
	}
	buyShare, err := inventoryShare(buy.OwnedBase, buy.TotalEligibleBase)
	if err != nil {
		return InventoryControl{}, err
	}
	state := BandNormal
	switch {
	case sellShare.Compare(configuration.LowerBand) <= 0:
		state = BandPaused
	case sellShare.Compare(configuration.TargetBand) < 0:
		state = BandReduced
	case sellShare.Compare(configuration.UpperBand) > 0:
		state = BandPreferred
	}
	return InventoryControl{
		SellVenueShare: sellShare, BuyVenueShare: buyShare, State: state,
		NaturalReverse: state == BandPreferred || buyShare.Compare(configuration.LowerBand) <= 0,
	}, nil
}

func inventoryShare(owned, total domain.Balance) (domain.Percent, error) {
	zero := balance("0")
	if total.Compare(zero) <= 0 || owned.Compare(total) > 0 {
		return domain.Percent{}, strategyError("inventory_share_invalid")
	}
	numerator, numeratorErr := domain.ParseMoney(owned.String())
	denominator, denominatorErr := domain.ParseMoney(total.String())
	if numeratorErr != nil || denominatorErr != nil {
		return domain.Percent{}, strategyError("inventory_share_invalid")
	}
	share, err := domain.CalculatePercent(numerator, denominator, 18)
	if err != nil {
		return domain.Percent{}, strategyError("inventory_share_invalid")
	}
	return share, nil
}

func rebalancingNeed(
	control InventoryControl,
	buy, sell VenueInventory,
	restoration RestorationEconomics,
) RebalancingNeed {
	required := control.State == BandPaused || control.State == BandReduced ||
		restoration.AdvisoryRebalancingRequired
	action := "none"
	if control.NaturalReverse {
		action = "prefer_natural_reverse_candidate"
	} else if required {
		action = "operator_review_only"
	}
	return RebalancingNeed{
		Required: required, Asset: sell.BaseAsset, DepletedExchange: sell.Exchange,
		OverweightExchange: buy.Exchange, PreferredAction: action,
		EstimatedCost:       restoration.AdvisoryRebalancingCost,
		EstimatedDelayNanos: restoration.EstimatedRestorationDelayNanos,
	}
}
