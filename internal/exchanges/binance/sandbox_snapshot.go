package binance

import (
	"context"
	"sort"
	"strconv"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

func (adapter *SandboxAdapter) resolveSubmission(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID string,
) (sandbox.Submission, bool, error) {
	if account != adapter.identity.AccountID || epoch != adapter.epoch ||
		clientOrderID == "" {
		return sandbox.Submission{}, false, ErrSandboxRequest
	}
	submission, found, err := adapter.lookup.SubmissionByClientOrderID(
		ctx,
		account,
		epoch,
		clientOrderID,
	)
	if err != nil || !found {
		return sandbox.Submission{}, found, err
	}
	if submission.AccountID != account || submission.AccountEpoch != epoch ||
		submission.ClientOrderID != clientOrderID {
		return sandbox.Submission{}, false, ErrSandboxRequest
	}
	return submission, true, nil
}

func (adapter *SandboxAdapter) normalizeOrderWithFills(
	ctx context.Context,
	body []byte,
	submission sandbox.Submission,
) (sandbox.PrivateEvent, error) {
	var native sandboxOrderPayload
	if strictDecode(body, &native) != nil ||
		native.OrderID.String() == "" {
		return sandbox.PrivateEvent{}, ErrSandboxPayload
	}
	fillsBody := []byte("[]")
	executed, err := domain.ParseQuantity(native.ExecutedQuantity)
	if err != nil {
		return sandbox.PrivateEvent{}, ErrSandboxPayload
	}
	zero, _ := domain.ParseQuantity("0")
	if executed.Compare(zero) > 0 {
		fillsBody, err = adapter.client.fillsForOrder(
			ctx,
			submission,
			native.OrderID.String(),
		)
		if err != nil {
			return sandbox.PrivateEvent{}, err
		}
	}
	return normalizeSandboxOrder(
		body,
		fillsBody,
		submission,
		adapter.client.now().UTC(),
	)
}

func (adapter *SandboxAdapter) availableBalance(
	ctx context.Context,
	asset domain.AssetSymbol,
) (domain.Balance, error) {
	body, err := adapter.client.account(ctx)
	if err != nil {
		return domain.Balance{}, err
	}
	balances, err := normalizeSandboxBalances(body)
	if err != nil {
		return domain.Balance{}, err
	}
	for _, balance := range balances {
		if balance.Asset == asset {
			return balance.Available, nil
		}
	}
	return domain.Balance{}, ErrSandboxPayload
}

func (adapter *SandboxAdapter) completeOrderHistory(
	ctx context.Context,
	symbol string,
) ([]sandboxOrderPayload, error) {
	result := make([]sandboxOrderPayload, 0)
	var next uint64
	for page := 0; page < sandboxHistoryPageLimit; page++ {
		var body []byte
		var err error
		if next == 0 {
			body, err = adapter.client.orderHistory(ctx, symbol)
		} else {
			if err = adapter.rateBudget.acquire(
				adapter.client.now().UTC(),
				sandboxHistoryPageWeight,
				sandboxRequestReconcile,
			); err != nil {
				return nil, err
			}
			body, err = adapter.client.orderHistoryFrom(ctx, symbol, next)
		}
		if err != nil {
			return nil, err
		}
		orders, err := decodeSandboxOrders(body)
		if err != nil {
			return nil, err
		}
		result = append(result, orders...)
		if len(orders) < 1000 {
			return result, nil
		}
		last, parseErr := strconv.ParseUint(
			orders[len(orders)-1].OrderID.String(),
			10,
			64,
		)
		if parseErr != nil || last == ^uint64(0) {
			return nil, ErrSandboxPayload
		}
		next = last + 1
	}
	return nil, ErrSandboxPayload
}

func (adapter *SandboxAdapter) completeFillHistory(
	ctx context.Context,
	symbol string,
) ([]sandboxFillPayload, error) {
	result := make([]sandboxFillPayload, 0)
	var next uint64
	for page := 0; page < sandboxHistoryPageLimit; page++ {
		var body []byte
		var err error
		if next == 0 {
			body, err = adapter.client.fills(ctx, symbol)
		} else {
			if err = adapter.rateBudget.acquire(
				adapter.client.now().UTC(),
				sandboxHistoryPageWeight,
				sandboxRequestReconcile,
			); err != nil {
				return nil, err
			}
			body, err = adapter.client.fillsFrom(ctx, symbol, next)
		}
		if err != nil {
			return nil, err
		}
		var fills []sandboxFillPayload
		if strictDecode(body, &fills) != nil {
			return nil, ErrSandboxPayload
		}
		result = append(result, fills...)
		if len(fills) < 1000 {
			return result, nil
		}
		last, parseErr := strconv.ParseUint(
			fills[len(fills)-1].ID.String(),
			10,
			64,
		)
		if parseErr != nil || last == ^uint64(0) {
			return nil, ErrSandboxPayload
		}
		next = last + 1
	}
	return nil, ErrSandboxPayload
}

func normalizeSandboxBalances(body []byte) ([]sandbox.Balance, error) {
	var native sandboxAccountPayload
	if strictDecode(body, &native) != nil || !native.CanTrade ||
		native.AccountType != "SPOT" || !containsOnlyExact(native.Permissions, "SPOT") ||
		len(native.Balances) == 0 {
		return nil, ErrSandboxPayload
	}
	balances := make([]sandbox.Balance, 0, len(native.Balances))
	for _, item := range native.Balances {
		if !approvedSandboxBalanceAsset(item.Asset) {
			continue
		}
		asset, assetErr := domain.ParseAssetSymbol(item.Asset)
		available, availableErr := domain.ParseBalance(item.Free)
		reserved, reservedErr := domain.ParseBalance(item.Locked)
		if assetErr != nil || availableErr != nil || reservedErr != nil {
			return nil, ErrSandboxPayload
		}
		balances = append(balances, sandbox.Balance{
			Asset: asset, Available: available, Reserved: reserved,
		})
	}
	if len(balances) != 3 {
		return nil, ErrSandboxPayload
	}
	sort.Slice(balances, func(left, right int) bool {
		return balances[left].Asset < balances[right].Asset
	})
	for index := 1; index < len(balances); index++ {
		if balances[index-1].Asset == balances[index].Asset {
			return nil, ErrSandboxPayload
		}
	}
	return balances, nil
}

func approvedSandboxBalanceAsset(asset string) bool {
	for _, instrument := range approvedSandboxInstruments() {
		if asset == string(instrument.Base) || asset == string(instrument.Quote) {
			return true
		}
	}
	return false
}

func decodeSandboxOrders(body []byte) ([]sandboxOrderPayload, error) {
	var orders []sandboxOrderPayload
	if strictDecode(body, &orders) != nil {
		return nil, ErrSandboxPayload
	}
	return orders, nil
}

func canonicalSandboxOrdersHash(orders []sandboxOrderPayload) string {
	type canonicalOrder struct {
		Symbol          string `json:"symbol"`
		OrderID         string `json:"order_id"`
		ClientOrderID   string `json:"client_order_id"`
		Status          string `json:"status"`
		Executed        string `json:"executed"`
		CumulativeQuote string `json:"cumulative_quote"`
		UpdateTime      int64  `json:"update_time"`
	}
	unique := make(map[string]canonicalOrder, len(orders))
	for _, order := range orders {
		key := order.Symbol + "|" + order.OrderID.String()
		unique[key] = canonicalOrder{
			Symbol: order.Symbol, OrderID: order.OrderID.String(),
			ClientOrderID: order.ClientOrderID, Status: order.Status,
			Executed:        order.ExecutedQuantity,
			CumulativeQuote: order.CumulativeQuoteQuantity,
			UpdateTime:      order.UpdateTime,
		}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	canonical := make([]canonicalOrder, 0, len(keys))
	for _, key := range keys {
		canonical = append(canonical, unique[key])
	}
	return canonicalHash(canonical)
}

func canonicalSandboxFillsHash(fills []sandboxFillPayload) string {
	type canonicalFill struct {
		Symbol     string `json:"symbol"`
		ID         string `json:"id"`
		OrderID    string `json:"order_id"`
		Price      string `json:"price"`
		Quantity   string `json:"quantity"`
		Commission string `json:"commission"`
		Asset      string `json:"asset"`
		Time       int64  `json:"time"`
	}
	canonical := make([]canonicalFill, 0, len(fills))
	for _, fill := range fills {
		canonical = append(canonical, canonicalFill{
			Symbol: fill.Symbol, ID: fill.ID.String(), OrderID: fill.OrderID.String(),
			Price: fill.Price, Quantity: fill.Quantity,
			Commission: fill.Commission, Asset: fill.CommissionAsset, Time: fill.Time,
		})
	}
	sort.Slice(canonical, func(left, right int) bool {
		if canonical[left].Symbol != canonical[right].Symbol {
			return canonical[left].Symbol < canonical[right].Symbol
		}
		return canonical[left].ID < canonical[right].ID
	})
	return canonicalHash(canonical)
}

func sandboxSnapshotDifferences(
	expected sandbox.SnapshotExpectation,
	found bool,
	actual sandbox.AccountSnapshot,
) []sandbox.Difference {
	if !found {
		missing := canonicalHash("missing_local_expectation")
		return []sandbox.Difference{{
			Category: "snapshot", Classification: "local_expectation_missing",
			ExpectedHash: missing, ActualHash: actual.SnapshotHash, Critical: true,
		}}
	}
	differences := make([]sandbox.Difference, 0, 3)
	appendDifference := func(category, expectedHash, actualHash string) {
		if expectedHash == actualHash {
			return
		}
		differences = append(differences, sandbox.Difference{
			Category: category, Classification: "authoritative_mismatch",
			ExpectedHash: expectedHash, ActualHash: actualHash, Critical: true,
		})
	}
	appendDifference("snapshot", expected.SnapshotHash, actual.SnapshotHash)
	appendDifference("orders", expected.OrdersHash, actual.OrdersHash)
	appendDifference("fills", expected.FillsHash, actual.FillsHash)
	return differences
}

func approvedSandboxInstruments() []domain.Instrument {
	result := make([]domain.Instrument, 0, 3)
	for _, pair := range [][2]domain.AssetSymbol{
		{"BTC", "USDT"},
		{"ETH", "USDT"},
		{"ETH", "BTC"},
	} {
		instrument, _ := domain.NewSpotInstrument(pair[0], pair[1])
		result = append(result, instrument)
	}
	return result
}
