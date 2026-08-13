package postgres

import (
	"context"
	"fmt"
	"strconv"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

type sandboxTriangularAccountingLot struct {
	Quantity    domain.Balance
	TotalCost   domain.Money
	AverageCost domain.Price
}

type sandboxTriangularAccountingState struct {
	PrimaryAsset   domain.AssetSymbol
	Lots           map[domain.AssetSymbol]sandboxTriangularAccountingLot
	RealizedPnL    domain.PnL
	ValuationState string
	Fees           map[domain.AssetSymbol]sandboxAccountingProjectionFee
}

// rebuildSandboxTriangularAccountingPosition replays the entire three-market
// fill chain as one USDT-numeraire portfolio. Cost follows the owned asset when
// BTC is converted into ETH, so the closing ETH/USDT leg realizes the complete
// cycle result instead of looking like an unrelated oversell.
func rebuildSandboxTriangularAccountingPosition(
	ctx context.Context,
	tx pgx.Tx,
	scope sandboxAccountingScope,
	account sandbox.AccountID,
	epoch uint64,
) (sandboxAccountingPositionProjection, error) {
	base, quote, err := sandboxAccountingInstrumentAssets(scope.PrimaryInstrument)
	if ctx == nil || tx == nil || scope.StrategySessionID == "" ||
		scope.Strategy != sandbox.StrategyTriangular || account == "" || epoch == 0 ||
		err != nil || quote != "USDT" {
		return sandboxAccountingPositionProjection{}, fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	state := newSandboxTriangularAccountingState(base)
	projection := newSandboxTriangularPositionProjection(scope, account, epoch, base, quote)
	rows, err := tx.Query(ctx, `
SELECT id,instrument,side,quantity::text,price::text,notional::text,
       fee::text,rebate::text,coalesce(fee_asset,''),fill_ordinal,
       occurred_at,recorded_at,evidence_hash::text
FROM sandbox_accounting_transactions
WHERE strategy_session_id=$1 AND account_id=$2 AND account_epoch=$3
ORDER BY occurred_at,fill_ordinal,id
FOR SHARE`, scope.StrategySessionID, account, epoch)
	if err != nil {
		return sandboxAccountingPositionProjection{}, fmt.Errorf("sandbox_accounting_projection_read_failed")
	}
	defer rows.Close()
	hashParts := []string{scope.StrategySessionID, string(account), strconv.FormatUint(epoch, 10),
		scope.PrimaryInstrument, "triangular-usdt-portfolio-v1"}
	if err = scanSandboxTriangularAccountingRows(rows, &state, &projection, &hashParts); err != nil {
		return sandboxAccountingPositionProjection{}, err
	}
	if projection.SourceTransactionCount == 0 || projection.LastTransactionID == "" ||
		projection.UpdatedAt.IsZero() || projection.UpdatedAt.Before(projection.LastOccurredAt) {
		return sandboxAccountingPositionProjection{}, fmt.Errorf("sandbox_accounting_projection_unavailable")
	}
	finalizeSandboxTriangularAccountingState(&state)
	primary := state.Lots[state.PrimaryAsset]
	projection.Quantity, projection.TotalCost = primary.Quantity, primary.TotalCost
	projection.WeightedAverageCost, projection.RealizedPnL = primary.AverageCost, state.RealizedPnL
	projection.ValuationState, projection.Fees = state.ValuationState, state.Fees
	appendSandboxTriangularProjectionHash(&hashParts, state, projection)
	projection.ProjectionHash = stableSandboxRuntimeHash(hashParts...)
	return projection, nil
}

func scanSandboxTriangularAccountingRows(rows pgx.Rows, state *sandboxTriangularAccountingState,
	projection *sandboxAccountingPositionProjection, hashParts *[]string,
) error {
	for rows.Next() {
		var fill sandboxAccountingProjectionFill
		var ordinal int64
		if err := rows.Scan(&fill.ID, &fill.Instrument, &fill.Side, &fill.Quantity,
			&fill.Price, &fill.Notional, &fill.Fee, &fill.Rebate, &fill.FeeAsset,
			&ordinal, &fill.OccurredAt, &fill.RecordedAt, &fill.EvidenceHash); err != nil || ordinal <= 0 {
			return fmt.Errorf("sandbox_accounting_projection_read_failed")
		}
		fill.FillOrdinal = uint64(ordinal)
		fill.OccurredAt, fill.RecordedAt = fill.OccurredAt.UTC(), fill.RecordedAt.UTC()
		if fill.ID == "" || fill.Instrument == "" || fill.OccurredAt.IsZero() ||
			fill.RecordedAt.Before(fill.OccurredAt) || fill.EvidenceHash == "" ||
			applySandboxTriangularAccountingFill(state, fill) != nil {
			return fmt.Errorf("sandbox_accounting_projection_invalid")
		}
		projection.SourceTransactionCount++
		projection.LastTransactionID = fill.ID
		projection.LastOccurredAt = fill.OccurredAt
		if fill.RecordedAt.After(projection.UpdatedAt) {
			projection.UpdatedAt = fill.RecordedAt
		}
		*hashParts = append(*hashParts, fill.ID, fill.Instrument, fill.EvidenceHash)
	}
	if rows.Err() != nil {
		return fmt.Errorf("sandbox_accounting_projection_read_failed")
	}
	return nil
}

func newSandboxTriangularAccountingState(
	primary domain.AssetSymbol,
) sandboxTriangularAccountingState {
	zeroPnL, _ := domain.ParsePnL("0")
	return sandboxTriangularAccountingState{
		PrimaryAsset: primary, Lots: make(map[domain.AssetSymbol]sandboxTriangularAccountingLot),
		RealizedPnL: zeroPnL, ValuationState: sandboxAccountingValuationComplete,
		Fees: make(map[domain.AssetSymbol]sandboxAccountingProjectionFee),
	}
}

func newSandboxTriangularPositionProjection(
	scope sandboxAccountingScope,
	account sandbox.AccountID,
	epoch uint64,
	base, quote domain.AssetSymbol,
) sandboxAccountingPositionProjection {
	zeroBalance, _ := domain.ParseBalance("0")
	zeroMoney, _ := domain.ParseMoney("0")
	zeroPrice, _ := domain.ParsePrice("0")
	zeroPnL, _ := domain.ParsePnL("0")
	return sandboxAccountingPositionProjection{
		StrategySessionID: scope.StrategySessionID, AccountID: account, AccountEpoch: epoch,
		Instrument: scope.PrimaryInstrument, BaseAsset: base, QuoteAsset: quote,
		Quantity: zeroBalance, TotalCost: zeroMoney, WeightedAverageCost: zeroPrice,
		RealizedPnL: zeroPnL, ValuationState: sandboxAccountingValuationComplete,
		Fees: make(map[domain.AssetSymbol]sandboxAccountingProjectionFee),
	}
}

func applySandboxTriangularAccountingFill(
	state *sandboxTriangularAccountingState,
	fill sandboxAccountingProjectionFill,
) error {
	if state == nil || state.PrimaryAsset == "" ||
		(fill.Side != domain.SideBuy && fill.Side != domain.SideSell) {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	base, quote, err := sandboxAccountingInstrumentAssets(fill.Instrument)
	quantity, quantityErr := domain.ParseBalance(fill.Quantity)
	tradeQuantity, tradeErr := domain.ParseQuantity(fill.Quantity)
	price, priceErr := domain.ParsePrice(fill.Price)
	notional, notionalErr := domain.ParseNotional(fill.Notional)
	fee, feeErr := domain.ParseFee(fill.Fee)
	rebate, rebateErr := domain.ParseFee(fill.Rebate)
	calculated, calculatedErr := domain.CalculateNotional(price, tradeQuantity, 18)
	zeroBalance, _ := domain.ParseBalance("0")
	zeroFee, _ := domain.ParseFee("0")
	if err != nil || quantityErr != nil || tradeErr != nil || priceErr != nil ||
		notionalErr != nil || feeErr != nil || rebateErr != nil || calculatedErr != nil ||
		quantity.Compare(zeroBalance) <= 0 || calculated.Compare(notional) != 0 {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	if err = applySandboxTriangularAccountingFees(state, fill.FeeAsset, base, quote,
		fee, rebate, zeroFee); err != nil {
		return err
	}
	// Once ownership or valuation is unresolved, retain and hash all later
	// immutable fills but never manufacture a recoverable cost basis.
	if state.ValuationState == sandboxAccountingValuationUnresolved {
		return nil
	}
	baseMovement, movementErr := sandboxAccountingBaseMovement(
		quantity, fee, rebate, fill.FeeAsset == base, fill.Side,
	)
	quoteMovement, quoteErr := sandboxTriangularQuoteMovement(
		notional, fee, rebate, fill.FeeAsset == quote, fill.Side,
	)
	if movementErr != nil || quoteErr != nil || baseMovement.Compare(zeroBalance) <= 0 ||
		quoteMovement.Compare(zeroBalance) <= 0 {
		markSandboxTriangularValuation(state, sandboxAccountingValuationUnresolved)
		return nil
	}
	if fill.Side == domain.SideBuy {
		return applySandboxTriangularBuy(state, base, quote, baseMovement, quoteMovement)
	}
	return applySandboxTriangularSell(state, base, quote, baseMovement, quoteMovement)
}

func applySandboxTriangularAccountingFees(state *sandboxTriangularAccountingState,
	asset, base, quote domain.AssetSymbol, fee, rebate, zero domain.Fee,
) error {
	if fee.Compare(zero) == 0 && rebate.Compare(zero) == 0 {
		if asset != "" {
			return fmt.Errorf("sandbox_accounting_projection_invalid")
		}
		return nil
	}
	if _, err := domain.ParseAssetSymbol(string(asset)); err != nil ||
		addSandboxAccountingProjectionFeeForTriangular(state, asset, fee, rebate) != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	if asset != base && asset != quote {
		markSandboxTriangularValuation(state, sandboxAccountingValuationUnvaluedFee)
	}
	return nil
}

func addSandboxAccountingProjectionFeeForTriangular(
	state *sandboxTriangularAccountingState,
	asset domain.AssetSymbol,
	fee, rebate domain.Fee,
) error {
	projection := sandboxAccountingPositionProjection{Fees: state.Fees}
	if err := addSandboxAccountingProjectionFee(&projection, asset, fee, rebate); err != nil {
		return err
	}
	state.Fees = projection.Fees
	return nil
}
