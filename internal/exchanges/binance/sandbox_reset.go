package binance

import (
	"fmt"
	"sort"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/cockroachdb/apd/v3"
)

// DetectSandboxReset recognizes only a coherent Testnet history wipe plus an
// exchange-authoritative balance discontinuity. Ordinary balance changes or
// an empty new account are not enough.
func DetectSandboxReset(
	previous sandbox.AccountSnapshot,
	current sandbox.AccountSnapshot,
) (sandbox.AccountResetIncident, bool, error) {
	if previous.Validate() != nil || current.Validate() != nil ||
		previous.AccountID != current.AccountID ||
		previous.Epoch != current.Epoch ||
		!current.ObservedAt.After(previous.ObservedAt) {
		return sandbox.AccountResetIncident{}, false, ErrSandboxPayload
	}
	emptyOrders := canonicalSandboxOrdersHash(nil)
	emptyFills := canonicalSandboxFillsHash(nil)
	historyWiped := current.OrdersHash == emptyOrders &&
		current.FillsHash == emptyFills &&
		(previous.OrdersHash != emptyOrders || previous.FillsHash != emptyFills)
	if !historyWiped {
		return sandbox.AccountResetIncident{}, false, nil
	}
	adjustments, err := sandboxBalanceAdjustments(previous, current)
	if err != nil {
		return sandbox.AccountResetIncident{}, false, err
	}
	if len(adjustments) == 0 {
		return sandbox.AccountResetIncident{}, false, nil
	}
	evidenceHash := canonicalHash(struct {
		Exchange string                  `json:"exchange"`
		Previous sandbox.AccountSnapshot `json:"previous"`
		Current  sandbox.AccountSnapshot `json:"current"`
	}{
		Exchange: "binance_testnet",
		Previous: previous,
		Current:  current,
	})
	incident := sandbox.AccountResetIncident{
		ID:           "binance-testnet-reset-" + evidenceHash[:20],
		AccountID:    previous.AccountID,
		PriorEpoch:   previous.Epoch,
		EvidenceHash: evidenceHash,
		DetectedAt:   current.ObservedAt,
		Adjustments:  adjustments,
	}
	if incident.Validate() != nil {
		return sandbox.AccountResetIncident{}, false, ErrSandboxPayload
	}
	return incident, true, nil
}

func sandboxBalanceAdjustments(
	previous sandbox.AccountSnapshot,
	current sandbox.AccountSnapshot,
) ([]sandbox.ExternalAdjustment, error) {
	prior, err := sandboxBalanceTotals(previous.Balances)
	if err != nil {
		return nil, err
	}
	next, err := sandboxBalanceTotals(current.Balances)
	if err != nil {
		return nil, err
	}
	sortedAssets := sortedSandboxBalanceAssets(prior, next)
	result := make([]sandbox.ExternalAdjustment, 0, len(sortedAssets))
	for _, text := range sortedAssets {
		adjustment, changed, adjustmentErr := sandboxBalanceAdjustment(
			previous, domain.AssetSymbol(text), prior, next,
		)
		if adjustmentErr != nil {
			return nil, adjustmentErr
		}
		if changed {
			result = append(result, adjustment)
		}
	}
	return result, nil
}

func sortedSandboxBalanceAssets(
	prior, next map[domain.AssetSymbol]string,
) []string {
	assets := make(map[domain.AssetSymbol]struct{}, len(prior)+len(next))
	for asset := range prior {
		assets[asset] = struct{}{}
	}
	for asset := range next {
		assets[asset] = struct{}{}
	}
	sortedAssets := make([]string, 0, len(assets))
	for asset := range assets {
		sortedAssets = append(sortedAssets, string(asset))
	}
	sort.Strings(sortedAssets)
	return sortedAssets
}

func sandboxBalanceAdjustment(
	previous sandbox.AccountSnapshot,
	asset domain.AssetSymbol,
	prior, next map[domain.AssetSymbol]string,
) (sandbox.ExternalAdjustment, bool, error) {
	left, right := prior[asset], next[asset]
	if left == "" {
		left = "0"
	}
	if right == "" {
		right = "0"
	}
	quantity, err := signedDecimalDifference(right, left)
	if err != nil || quantity == "0" {
		return sandbox.ExternalAdjustment{}, false, err
	}
	hash := canonicalHash([]string{
		string(previous.AccountID), fmt.Sprintf("%d", previous.Epoch),
		string(asset), left, right, quantity,
	})
	return sandbox.ExternalAdjustment{
		ID:    "binance-reset-adjustment-" + hash[:20],
		Asset: asset, Quantity: quantity, AdjustmentHash: hash,
	}, true, nil
}

func sandboxBalanceTotals(
	balances []sandbox.Balance,
) (map[domain.AssetSymbol]string, error) {
	result := make(map[domain.AssetSymbol]string, len(balances))
	for _, balance := range balances {
		total, err := balance.Available.Add(balance.Reserved)
		if err != nil {
			return nil, ErrSandboxPayload
		}
		result[balance.Asset] = total.String()
	}
	return result, nil
}

func signedDecimalDifference(left, right string) (string, error) {
	leftDecimal, _, leftErr := apd.NewFromString(left)
	rightDecimal, _, rightErr := apd.NewFromString(right)
	if leftErr != nil || rightErr != nil {
		return "", ErrSandboxPayload
	}
	context := apd.BaseContext.WithPrecision(38)
	var difference apd.Decimal
	if _, err := context.Sub(&difference, leftDecimal, rightDecimal); err != nil {
		return "", ErrSandboxPayload
	}
	quantity := difference.Text('f')
	if _, err := domain.ParsePnL(quantity); err != nil {
		return "", ErrSandboxPayload
	}
	return quantity, nil
}
