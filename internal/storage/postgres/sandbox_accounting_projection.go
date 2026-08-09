package postgres

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

const (
	sandboxAccountingValuationComplete    = "complete"
	sandboxAccountingValuationUnvaluedFee = "unvalued_fee_asset"
	sandboxAccountingValuationCrossAsset  = "cross_asset_open"
	sandboxAccountingValuationUnresolved  = "inventory_unresolved"
)

type sandboxAccountingProjectionFill struct {
	ID           string
	Instrument   string
	Side         domain.Side
	Quantity     string
	Price        string
	Notional     string
	Fee          string
	Rebate       string
	FeeAsset     domain.AssetSymbol
	FillOrdinal  uint64
	OccurredAt   time.Time
	RecordedAt   time.Time
	EvidenceHash string
}

type sandboxAccountingProjectionFee struct {
	Fee    domain.Balance
	Rebate domain.Balance
}

type sandboxAccountingPositionProjection struct {
	StrategySessionID      string
	AccountID              sandbox.AccountID
	AccountEpoch           uint64
	Instrument             string
	BaseAsset              domain.AssetSymbol
	QuoteAsset             domain.AssetSymbol
	Quantity               domain.Balance
	TotalCost              domain.Money
	WeightedAverageCost    domain.Price
	RealizedPnL            domain.PnL
	ValuationState         string
	SourceTransactionCount uint64
	LastTransactionID      string
	LastOccurredAt         time.Time
	ProjectionHash         string
	UpdatedAt              time.Time
	Fees                   map[domain.AssetSymbol]sandboxAccountingProjectionFee
}

// rebuildSandboxAccountingPosition replays the complete immutable fill chain
// in canonical order. The stored current-state row is disposable; this result
// is the authority used to replace or verify it inside the fill transaction.
func rebuildSandboxAccountingPosition(
	ctx context.Context,
	tx pgx.Tx,
	scope sandboxAccountingScope,
	account sandbox.AccountID,
	epoch uint64,
	instrument string,
) (sandboxAccountingPositionProjection, error) {
	base, quote, err := sandboxAccountingInstrumentAssets(instrument)
	if ctx == nil || tx == nil || scope.StrategySessionID == "" || account == "" || epoch == 0 || err != nil {
		return sandboxAccountingPositionProjection{}, fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	projection := newSandboxAccountingPositionProjection(scope, account, epoch, instrument, base, quote)
	rows, err := tx.Query(ctx, `
SELECT id,side,quantity::text,price::text,notional::text,
       fee::text,rebate::text,coalesce(fee_asset,''),fill_ordinal,
       occurred_at,recorded_at,evidence_hash::text
FROM sandbox_accounting_transactions
WHERE strategy_session_id=$1 AND account_id=$2 AND account_epoch=$3
  AND instrument=$4
ORDER BY occurred_at,fill_ordinal,id
FOR SHARE`, scope.StrategySessionID, account, epoch, instrument)
	if err != nil {
		return sandboxAccountingPositionProjection{}, fmt.Errorf("sandbox_accounting_projection_read_failed")
	}
	defer rows.Close()
	hashParts := []string{scope.StrategySessionID, string(account), strconv.FormatUint(epoch, 10), instrument}
	if err = scanSandboxAccountingProjectionRows(rows, &projection, &hashParts); err != nil {
		return sandboxAccountingPositionProjection{}, err
	}
	if err = finalizeSandboxAccountingProjection(&projection, &hashParts); err != nil {
		return sandboxAccountingPositionProjection{}, err
	}
	return projection, nil
}

func finalizeSandboxAccountingProjection(projection *sandboxAccountingPositionProjection,
	hashParts *[]string,
) error {
	if projection.SourceTransactionCount == 0 || projection.LastTransactionID == "" ||
		projection.UpdatedAt.IsZero() || projection.UpdatedAt.Before(projection.LastOccurredAt) {
		return fmt.Errorf("sandbox_accounting_projection_unavailable")
	}
	feeAssets := make([]string, 0, len(projection.Fees))
	for asset := range projection.Fees {
		feeAssets = append(feeAssets, string(asset))
	}
	sort.Strings(feeAssets)
	*hashParts = append(*hashParts, projection.Quantity.String(), projection.TotalCost.String(),
		projection.WeightedAverageCost.String(), projection.RealizedPnL.String(),
		projection.ValuationState, strconv.FormatUint(projection.SourceTransactionCount, 10),
		projection.LastTransactionID, projection.LastOccurredAt.Format(time.RFC3339Nano),
		projection.UpdatedAt.Format(time.RFC3339Nano))
	for _, asset := range feeAssets {
		total := projection.Fees[domain.AssetSymbol(asset)]
		*hashParts = append(*hashParts, asset, total.Fee.String(), total.Rebate.String())
	}
	projection.ProjectionHash = stableSandboxRuntimeHash((*hashParts)...)
	return nil
}

func newSandboxAccountingPositionProjection(scope sandboxAccountingScope, account sandbox.AccountID,
	epoch uint64, instrument string, base, quote domain.AssetSymbol,
) sandboxAccountingPositionProjection {
	zeroBalance, _ := domain.ParseBalance("0")
	zeroMoney, _ := domain.ParseMoney("0")
	zeroPrice, _ := domain.ParsePrice("0")
	zeroPnL, _ := domain.ParsePnL("0")
	return sandboxAccountingPositionProjection{StrategySessionID: scope.StrategySessionID,
		AccountID: account, AccountEpoch: epoch, Instrument: instrument, BaseAsset: base, QuoteAsset: quote,
		Quantity: zeroBalance, TotalCost: zeroMoney, WeightedAverageCost: zeroPrice,
		RealizedPnL: zeroPnL, ValuationState: sandboxAccountingValuationComplete,
		Fees: make(map[domain.AssetSymbol]sandboxAccountingProjectionFee)}
}

func scanSandboxAccountingProjectionRows(rows pgx.Rows, projection *sandboxAccountingPositionProjection,
	hashParts *[]string,
) error {
	for rows.Next() {
		var fill sandboxAccountingProjectionFill
		var ordinal int64
		if err := rows.Scan(&fill.ID, &fill.Side, &fill.Quantity, &fill.Price, &fill.Notional,
			&fill.Fee, &fill.Rebate, &fill.FeeAsset, &ordinal, &fill.OccurredAt,
			&fill.RecordedAt, &fill.EvidenceHash); err != nil || ordinal <= 0 {
			return fmt.Errorf("sandbox_accounting_projection_read_failed")
		}
		fill.FillOrdinal = uint64(ordinal)
		fill.OccurredAt, fill.RecordedAt = fill.OccurredAt.UTC(), fill.RecordedAt.UTC()
		if fill.ID == "" || fill.OccurredAt.IsZero() || fill.RecordedAt.Before(fill.OccurredAt) ||
			fill.EvidenceHash == "" || applySandboxAccountingProjectionFill(projection, fill) != nil {
			return fmt.Errorf("sandbox_accounting_projection_invalid")
		}
		projection.SourceTransactionCount++
		projection.LastTransactionID = fill.ID
		projection.LastOccurredAt = fill.OccurredAt
		if fill.RecordedAt.After(projection.UpdatedAt) {
			projection.UpdatedAt = fill.RecordedAt
		}
		*hashParts = append(*hashParts, fill.ID, fill.EvidenceHash)
	}
	if rows.Err() != nil {
		return fmt.Errorf("sandbox_accounting_projection_read_failed")
	}
	return nil
}

func applySandboxAccountingProjectionFill(
	projection *sandboxAccountingPositionProjection,
	fill sandboxAccountingProjectionFill,
) error {
	if projection == nil || projection.BaseAsset == "" || projection.QuoteAsset == "" ||
		(fill.Side != domain.SideBuy && fill.Side != domain.SideSell) {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	quantity, quantityErr := domain.ParseBalance(fill.Quantity)
	tradeQuantity, tradeErr := domain.ParseQuantity(fill.Quantity)
	price, priceErr := domain.ParsePrice(fill.Price)
	notional, notionalErr := domain.ParseNotional(fill.Notional)
	fee, feeErr := domain.ParseFee(fill.Fee)
	rebate, rebateErr := domain.ParseFee(fill.Rebate)
	calculated, calculatedErr := domain.CalculateNotional(price, tradeQuantity, 18)
	zeroBalance, _ := domain.ParseBalance("0")
	zeroFee, _ := domain.ParseFee("0")
	if quantityErr != nil || tradeErr != nil || priceErr != nil || notionalErr != nil ||
		feeErr != nil || rebateErr != nil || calculatedErr != nil ||
		quantity.Compare(zeroBalance) <= 0 || calculated.Compare(notional) != 0 {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	if err := applySandboxAccountingProjectionFees(projection, fill.FeeAsset, fee, rebate, zeroFee); err != nil {
		return err
	}
	baseMovement, err := sandboxAccountingBaseMovement(
		quantity, fee, rebate, fill.FeeAsset == projection.BaseAsset, fill.Side,
	)
	if err != nil {
		return err
	}
	quoteValue, err := sandboxAccountingQuoteValue(
		notional, fee, rebate, fill.FeeAsset == projection.QuoteAsset, fill.Side,
	)
	if err != nil {
		return err
	}
	if fill.Side == domain.SideBuy {
		return applySandboxAccountingBuy(projection, baseMovement, quoteValue)
	}
	return applySandboxAccountingSell(projection, baseMovement, quoteValue)
}

func applySandboxAccountingProjectionFees(projection *sandboxAccountingPositionProjection,
	asset domain.AssetSymbol, fee, rebate, zero domain.Fee,
) error {
	if fee.Compare(zero) == 0 && rebate.Compare(zero) == 0 {
		if asset != "" {
			return fmt.Errorf("sandbox_accounting_projection_invalid")
		}
		return nil
	}
	if _, err := domain.ParseAssetSymbol(string(asset)); err != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	if err := addSandboxAccountingProjectionFee(projection, asset, fee, rebate); err != nil {
		return err
	}
	if asset != projection.BaseAsset && asset != projection.QuoteAsset {
		projection.ValuationState = sandboxAccountingValuationUnvaluedFee
	}
	return nil
}

func addSandboxAccountingProjectionFee(
	projection *sandboxAccountingPositionProjection,
	asset domain.AssetSymbol,
	fee domain.Fee,
	rebate domain.Fee,
) error {
	feeBalance, feeErr := domain.ParseBalance(fee.String())
	rebateBalance, rebateErr := domain.ParseBalance(rebate.String())
	if feeErr != nil || rebateErr != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	total := projection.Fees[asset]
	var err error
	total.Fee, err = total.Fee.Add(feeBalance)
	if err != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	total.Rebate, err = total.Rebate.Add(rebateBalance)
	if err != nil {
		return fmt.Errorf("sandbox_accounting_projection_invalid")
	}
	projection.Fees[asset] = total
	return nil
}
