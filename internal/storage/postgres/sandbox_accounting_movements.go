package postgres

import (
	"context"
	"fmt"
	"sort"

	"axiom/internal/domain"

	"github.com/jackc/pgx/v5"
)

// sandboxAccountingBaseMovement returns acquired quantity for a buy and total
// disposed quantity for a sell. Base-asset fees/rebates therefore change the
// owned lot exactly instead of being hidden in a functional-currency value.
func sandboxAccountingBaseMovement(
	gross domain.Balance,
	fee domain.Fee,
	rebate domain.Fee,
	applies bool,
	side domain.Side,
) (domain.Balance, error) {
	if !applies {
		return gross, nil
	}
	feeBalance, _ := domain.ParseBalance(fee.String())
	rebateBalance, _ := domain.ParseBalance(rebate.String())
	if feeBalance.Compare(rebateBalance) >= 0 {
		netFee, err := feeBalance.Subtract(rebateBalance)
		if err != nil {
			return domain.Balance{}, fmt.Errorf("sandbox_accounting_projection_invalid")
		}
		if side == domain.SideBuy {
			return gross.Subtract(netFee)
		}
		return gross.Add(netFee)
	}
	netRebate, err := rebateBalance.Subtract(feeBalance)
	if err != nil {
		return domain.Balance{}, fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	if side == domain.SideBuy {
		return gross.Add(netRebate)
	}
	return gross.Subtract(netRebate)
}

// sandboxAccountingQuoteValue returns acquisition cost for a buy and net
// proceeds for a sell. Quote fees and rebates remain separately accumulated
// while also participating in the documented weighted-average P&L policy.
func sandboxAccountingQuoteValue(
	notional domain.Notional,
	fee domain.Fee,
	rebate domain.Fee,
	applies bool,
	side domain.Side,
) (domain.Money, error) {
	value, err := domain.ParsePnL(notional.String())
	if err != nil {
		return domain.Money{}, fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	if applies {
		feeValue, feeErr := domain.ParsePnL(fee.String())
		rebateValue, rebateErr := domain.ParsePnL(rebate.String())
		if feeErr != nil || rebateErr != nil {
			return domain.Money{}, fmt.Errorf("sandbox_accounting_projection_invalid")
		}
		if side == domain.SideBuy {
			value, err = value.Add(feeValue)
			if err == nil {
				value, err = value.Subtract(rebateValue)
			}
		} else {
			value, err = value.Subtract(feeValue)
			if err == nil {
				value, err = value.Add(rebateValue)
			}
		}
	}
	result, parseErr := domain.ParseMoney(value.String())
	if err != nil || parseErr != nil {
		return domain.Money{}, fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	return result, nil
}

func applySandboxAccountingBuy(
	projection *sandboxAccountingPositionProjection,
	quantity domain.Balance,
	cost domain.Money,
) error {
	zeroBalance, _ := domain.ParseBalance("0")
	zeroMoney, _ := domain.ParseMoney("0")
	if quantity.Compare(zeroBalance) <= 0 || cost.Compare(zeroMoney) <= 0 {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	quantityTotal, err := projection.Quantity.Add(quantity)
	if err != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	costTotal, err := projection.TotalCost.Add(cost)
	if err != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	average, err := domain.CalculateAveragePrice(costTotal, quantityTotal, 18)
	if err != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	projection.Quantity, projection.TotalCost = quantityTotal, costTotal
	projection.WeightedAverageCost = average
	return nil
}

func applySandboxAccountingSell(
	projection *sandboxAccountingPositionProjection,
	disposed domain.Balance,
	proceeds domain.Money,
) error {
	if disposed.Compare(projection.Quantity) > 0 {
		return fmt.Errorf("sandbox_accounting_projection_oversell")
	}
	remaining, err := projection.Quantity.Subtract(disposed)
	if err != nil {
		return fmt.Errorf("sandbox_accounting_projection_oversell")
	}
	removedCost, err := domain.CalculateMoney(projection.WeightedAverageCost, disposed, 18)
	if err != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	if disposed.Compare(projection.Quantity) == 0 {
		removedCost = projection.TotalCost
	}
	zeroFee, _ := domain.ParseFee("0")
	pnl, err := domain.MoneyDifference(proceeds, removedCost, zeroFee)
	if err != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	realized, err := projection.RealizedPnL.Add(pnl)
	if err != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	remainingCost, err := projection.TotalCost.Subtract(removedCost)
	if err != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	zeroBalance, _ := domain.ParseBalance("0")
	if remaining.Compare(zeroBalance) == 0 {
		zeroMoney, _ := domain.ParseMoney("0")
		zeroPrice, _ := domain.ParsePrice("0")
		projection.TotalCost, projection.WeightedAverageCost = zeroMoney, zeroPrice
	} else {
		zeroMoney, _ := domain.ParseMoney("0")
		if remainingCost.Compare(zeroMoney) <= 0 {
			return fmt.Errorf("sandbox_accounting_projection_invalid")
		}
		projection.TotalCost = remainingCost
	}
	projection.Quantity, projection.RealizedPnL = remaining, realized
	return nil
}

func sandboxAccountingInstrumentAssets(
	instrument string,
) (domain.AssetSymbol, domain.AssetSymbol, error) {
	var base, quote string
	switch instrument {
	case "BTCUSDT":
		base, quote = "BTC", "USDT"
	case "ETHUSDT":
		base, quote = "ETH", "USDT"
	case "ETHBTC":
		base, quote = "ETH", "BTC"
	default:
		return "", "", fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	baseAsset, baseErr := domain.ParseAssetSymbol(base)
	quoteAsset, quoteErr := domain.ParseAssetSymbol(quote)
	if baseErr != nil || quoteErr != nil {
		return "", "", fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	return baseAsset, quoteAsset, nil
}

func storeSandboxAccountingPosition(
	ctx context.Context,
	tx pgx.Tx,
	projection sandboxAccountingPositionProjection,
) error {
	if projection.StrategySessionID == "" || projection.AccountID == "" ||
		projection.AccountEpoch == 0 || projection.SourceTransactionCount == 0 ||
		projection.LastTransactionID == "" || projection.ProjectionHash == "" {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO sandbox_accounting_positions(
 strategy_session_id,account_id,account_epoch,instrument,base_asset,quote_asset,
 quantity,total_cost,weighted_average_cost,realized_pnl,valuation_state,
 source_transaction_count,last_transaction_id,last_occurred_at,projection_hash,
 revision,updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$12,$16)
ON CONFLICT (strategy_session_id,account_id,account_epoch,instrument) DO UPDATE
SET base_asset=EXCLUDED.base_asset,quote_asset=EXCLUDED.quote_asset,
 quantity=EXCLUDED.quantity,total_cost=EXCLUDED.total_cost,
 weighted_average_cost=EXCLUDED.weighted_average_cost,
 realized_pnl=EXCLUDED.realized_pnl,valuation_state=EXCLUDED.valuation_state,
 source_transaction_count=EXCLUDED.source_transaction_count,
 last_transaction_id=EXCLUDED.last_transaction_id,
 last_occurred_at=EXCLUDED.last_occurred_at,
 projection_hash=EXCLUDED.projection_hash,revision=EXCLUDED.revision,
 updated_at=EXCLUDED.updated_at
WHERE sandbox_accounting_positions.source_transaction_count < EXCLUDED.source_transaction_count
   OR (sandbox_accounting_positions.source_transaction_count = EXCLUDED.source_transaction_count
       AND sandbox_accounting_positions.projection_hash = EXCLUDED.projection_hash)`,
		projection.StrategySessionID, projection.AccountID, projection.AccountEpoch,
		projection.Instrument, projection.BaseAsset, projection.QuoteAsset,
		projection.Quantity.String(), projection.TotalCost.String(),
		projection.WeightedAverageCost.String(), projection.RealizedPnL.String(),
		projection.ValuationState, projection.SourceTransactionCount,
		projection.LastTransactionID, projection.LastOccurredAt,
		projection.ProjectionHash, projection.UpdatedAt,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_accounting_projection_write_failed")
	}
	return storeSandboxAccountingPositionFees(ctx, tx, projection)
}

func storeSandboxAccountingPositionFees(ctx context.Context, tx pgx.Tx,
	projection sandboxAccountingPositionProjection,
) error {
	feeAssets := make([]string, 0, len(projection.Fees))
	for asset := range projection.Fees {
		feeAssets = append(feeAssets, string(asset))
	}
	sort.Strings(feeAssets)
	for _, asset := range feeAssets {
		total := projection.Fees[domain.AssetSymbol(asset)]
		tag, err := tx.Exec(ctx, `
INSERT INTO sandbox_accounting_position_fees(
 strategy_session_id,account_id,account_epoch,instrument,asset_symbol,
 fee_quantity,rebate_quantity,revision,updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT (strategy_session_id,account_id,account_epoch,instrument,asset_symbol)
DO UPDATE SET fee_quantity=EXCLUDED.fee_quantity,
 rebate_quantity=EXCLUDED.rebate_quantity,revision=EXCLUDED.revision,
 updated_at=EXCLUDED.updated_at
WHERE sandbox_accounting_position_fees.revision < EXCLUDED.revision
   OR (sandbox_accounting_position_fees.revision = EXCLUDED.revision
       AND sandbox_accounting_position_fees.fee_quantity = EXCLUDED.fee_quantity
       AND sandbox_accounting_position_fees.rebate_quantity = EXCLUDED.rebate_quantity)`,
			projection.StrategySessionID, projection.AccountID, projection.AccountEpoch,
			projection.Instrument, asset, total.Fee.String(), total.Rebate.String(),
			projection.SourceTransactionCount, projection.UpdatedAt,
		)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("sandbox_accounting_projection_fee_write_failed")
		}
	}
	return nil
}
