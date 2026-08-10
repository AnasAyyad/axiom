package crossarb

import (
	"sort"

	"axiom/internal/domain"
	"axiom/internal/strategies/arbitrage"
)

// SingleInstrumentInventoryModel identifies the reviewed venue-prefunding rule.
const SingleInstrumentInventoryModel = "cross-exchange-single-instrument-prefund.v1"

// VenueInitialization records the exact public-book conversion used to
// prefund one isolated single-instrument Cross-Exchange experiment. The
// selected-instrument variant intentionally retains the unselected volatile
// allocation as USDT and identifies that policy explicitly.
type VenueInitialization struct {
	Exchange            string             `json:"exchange"`
	BaseAsset           domain.AssetSymbol `json:"base_asset"`
	VenueCapital        domain.Balance     `json:"venue_capital"`
	TargetBaseValue     domain.Balance     `json:"target_base_value"`
	ReferencePrice      domain.Price       `json:"reference_price"`
	BaseQuantity        domain.Balance     `json:"base_quantity"`
	AvailableUSDT       domain.Balance     `json:"available_usdt"`
	ModelVersion        string             `json:"model_version"`
	UnselectedAssetRule string             `json:"unselected_asset_rule"`
}

// InitializeSingleInstrumentInventory creates equal venue ownership from the
// exact coherent books. BTC receives the reviewed 25% venue allocation and ETH
// 15%; the unselected volatile allocation stays as USDT for this explicitly
// versioned single-instrument experiment.
func InitializeSingleInstrumentInventory(
	markets []MarketInput,
	venueCapital domain.Balance,
) (VenueBalances, []VenueInitialization, error) {
	parameters, err := resolveSingleInstrumentInventoryParameters(markets, venueCapital)
	if err != nil {
		return nil, nil, err
	}
	result := make(VenueBalances, 2)
	evidence := make([]VenueInitialization, 0, 2)
	seen := make(map[string]bool, 2)
	for _, recorded := range markets {
		exchange, balances, initialization, venueErr := initializeSingleVenue(recorded, parameters)
		if venueErr != nil || seen[exchange] {
			return nil, nil, strategyError("inventory_initialization_invalid")
		}
		seen[exchange], result[exchange] = true, balances
		evidence = append(evidence, initialization)
	}
	if !seen["binance"] || !seen["bybit"] {
		return nil, nil, strategyError("inventory_initialization_invalid")
	}
	sort.Slice(evidence, func(left, right int) bool { return evidence[left].Exchange < evidence[right].Exchange })
	return result, evidence, nil
}

type singleInstrumentInventoryParameters struct {
	base, settlement, other domain.AssetSymbol
	zero, capital, target   domain.Balance
}

func resolveSingleInstrumentInventoryParameters(markets []MarketInput,
	venueCapital domain.Balance,
) (singleInstrumentInventoryParameters, error) {
	if len(markets) != 2 {
		return singleInstrumentInventoryParameters{}, strategyError("inventory_initialization_invalid")
	}
	zero, _ := domain.ParseBalance("0")
	if venueCapital.Compare(zero) <= 0 {
		return singleInstrumentInventoryParameters{}, strategyError("inventory_initialization_invalid")
	}
	base := markets[0].Snapshot.Instrument.Base
	if markets[1].Snapshot.Instrument.Base != base ||
		markets[0].Snapshot.Instrument != markets[1].Snapshot.Instrument {
		return singleInstrumentInventoryParameters{}, strategyError("inventory_initialization_invalid")
	}
	fractionText := "0.25"
	if base == domain.AssetSymbol("ETH") {
		fractionText = "0.15"
	} else if base != domain.AssetSymbol("BTC") {
		return singleInstrumentInventoryParameters{}, strategyError("inventory_initialization_invalid")
	}
	fraction, err := domain.ParsePercent(fractionText)
	target, targetErr := domain.ScaleBalanceFloor(venueCapital, fraction, 18)
	if err != nil || targetErr != nil || target.Compare(zero) <= 0 {
		return singleInstrumentInventoryParameters{}, strategyError("inventory_initialization_invalid")
	}
	settlement, _ := domain.ParseAssetSymbol("USDT")
	other, _ := domain.ParseAssetSymbol("ETH")
	if base == other {
		other, _ = domain.ParseAssetSymbol("BTC")
	}
	return singleInstrumentInventoryParameters{base, settlement, other, zero, venueCapital, target}, nil
}

func initializeSingleVenue(recorded MarketInput, parameters singleInstrumentInventoryParameters,
) (string, map[domain.AssetSymbol]domain.Balance, VenueInitialization, error) {
	market, err := recorded.market()
	if err != nil {
		return "", nil, VenueInitialization{}, err
	}
	conversion, conversionErr := arbitrage.Convert(arbitrage.Request{Source: parameters.settlement,
		Target: parameters.base, Input: quantity(parameters.target.String()), Book: market.Book, Rules: market.Rules})
	consumed, consumedErr := conversion.Input.Subtract(conversion.SourceDust)
	consumedBalance, consumedBalanceErr := domain.ParseBalance(consumed.String())
	baseBalance, baseErr := domain.ParseBalance(conversion.NetOutput.String())
	availableUSDT, usdtErr := parameters.capital.Subtract(consumedBalance)
	if conversionErr != nil || consumedErr != nil || consumedBalanceErr != nil || baseErr != nil ||
		usdtErr != nil || baseBalance.Compare(parameters.zero) <= 0 || availableUSDT.Compare(parameters.zero) <= 0 {
		return "", nil, VenueInitialization{}, strategyError("inventory_initialization_invalid")
	}
	exchange := market.Book.Exchange()
	balances := map[domain.AssetSymbol]domain.Balance{parameters.settlement: availableUSDT,
		parameters.base: baseBalance, parameters.other: parameters.zero}
	evidence := VenueInitialization{Exchange: exchange, BaseAsset: parameters.base,
		VenueCapital: parameters.capital, TargetBaseValue: parameters.target, ReferencePrice: conversion.VWAP,
		BaseQuantity: baseBalance, AvailableUSDT: availableUSDT, ModelVersion: SingleInstrumentInventoryModel,
		UnselectedAssetRule: "retain_unselected_volatile_allocation_as_usdt"}
	return exchange, balances, evidence, nil
}
