package bybit

import (
	"fmt"
	"sort"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

func normalizeDemoBalances(body []byte) ([]sandbox.Balance, error) {
	result, err := decodeDemoResult[walletBalanceResult](body)
	if err != nil || len(result.List) != 1 ||
		result.List[0].AccountType != "UNIFIED" ||
		len(result.List[0].Coin) == 0 {
		return nil, fmt.Errorf(
			"bybit_demo_wallet_envelope_invalid: %w",
			ErrDemoPayload,
		)
	}
	balances := make([]sandbox.Balance, 0, len(result.List[0].Coin))
	for _, item := range result.List[0].Coin {
		balance, balanceErr := normalizeDemoBalance(item)
		if balanceErr != nil {
			return nil, balanceErr
		}
		balances = append(balances, balance)
	}
	sort.Slice(balances, func(left, right int) bool {
		return balances[left].Asset < balances[right].Asset
	})
	for index := 1; index < len(balances); index++ {
		if balances[index-1].Asset == balances[index].Asset {
			return nil, fmt.Errorf(
				"bybit_demo_wallet_duplicate_asset: %w",
				ErrDemoPayload,
			)
		}
	}
	return balances, nil
}

func normalizeDemoBalance(item walletCoinPayload) (sandbox.Balance, error) {
	if !validCollateralRestriction(item.CollateralRestriction) {
		return sandbox.Balance{}, fmt.Errorf(
			"bybit_demo_wallet_collateral_restriction_invalid: %w",
			ErrDemoPayload,
		)
	}
	if state := forbiddenDemoBalanceState(item); state != "" {
		return sandbox.Balance{}, fmt.Errorf(
			"bybit_demo_wallet_%s_nonzero: %w", state, ErrDemoPayload,
		)
	}
	asset, err := domain.ParseAssetSymbol(item.Coin)
	if err != nil {
		return sandbox.Balance{}, fmt.Errorf(
			"bybit_demo_wallet_asset_invalid: %w", ErrDemoPayload,
		)
	}
	available, reserved, err := demoBalanceValues(item)
	if err != nil {
		return sandbox.Balance{}, err
	}
	return sandbox.Balance{
		Asset: asset, Available: available, Reserved: reserved,
	}, nil
}

func forbiddenDemoBalanceState(item walletCoinPayload) string {
	states := []struct {
		name  string
		value string
	}{
		{"spot_borrow", item.SpotBorrow},
		{"borrow_amount", item.BorrowAmount},
		{"accrued_interest", item.AccruedInterest},
		{"total_order_im", item.TotalOrderIM},
		{"total_position_im", item.TotalPositionIM},
		{"total_position_mm", item.TotalPositionMM},
		{"unrealised_pnl", item.UnrealisedPNL},
		{"spot_hedging_quantity", item.SpotHedgingQuantity},
	}
	for _, state := range states {
		if !zeroOrEmpty(state.value) {
			return state.name
		}
	}
	return ""
}

func validCollateralRestriction(value string) bool {
	switch value {
	case "", "-1", "0", "1", "2":
		return true
	default:
		return false
	}
}

func demoBalanceValues(
	item walletCoinPayload,
) (domain.Balance, domain.Balance, error) {
	reserved, reservedErr := domain.ParseBalance(item.Locked)
	wallet, walletErr := domain.ParseBalance(item.WalletBalance)
	if reservedErr != nil {
		return domain.Balance{}, domain.Balance{},
			fmt.Errorf("bybit_demo_wallet_locked_invalid: %w", ErrDemoPayload)
	}
	if walletErr != nil {
		return domain.Balance{}, domain.Balance{},
			fmt.Errorf("bybit_demo_wallet_balance_invalid: %w", ErrDemoPayload)
	}
	if item.Free == "" {
		available, err := wallet.Subtract(reserved)
		if err != nil {
			return domain.Balance{}, domain.Balance{},
				fmt.Errorf("bybit_demo_wallet_locked_exceeds_balance: %w", ErrDemoPayload)
		}
		return available, reserved, err
	}
	available, err := domain.ParseBalance(item.Free)
	if err != nil {
		return domain.Balance{}, domain.Balance{},
			fmt.Errorf("bybit_demo_wallet_free_invalid: %w", ErrDemoPayload)
	}
	total, err := available.Add(reserved)
	if err != nil || total.Compare(wallet) != 0 {
		return domain.Balance{}, domain.Balance{},
			fmt.Errorf("bybit_demo_wallet_total_mismatch: %w", ErrDemoPayload)
	}
	return available, reserved, nil
}

func zeroOrEmpty(value string) bool {
	if value == "" {
		return true
	}
	decimal, err := domain.ParseBalance(value)
	if err != nil {
		return false
	}
	zero, _ := domain.ParseBalance("0")
	return decimal.Compare(zero) == 0
}
