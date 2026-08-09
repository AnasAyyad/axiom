package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

func sandboxRiskAdverseSlippage(side, fillText, limitText string) (domain.Percent, bool, error) {
	fill, fillErr := domain.ParseMoney(fillText)
	limit, limitErr := domain.ParseMoney(limitText)
	if fillErr != nil || limitErr != nil {
		return domain.Percent{}, false, fmt.Errorf("sandbox_strategy_risk_slippage_invalid")
	}
	var adverse domain.Money
	var err error
	switch side {
	case string(domain.SideBuy):
		if fill.Compare(limit) <= 0 {
			return domain.Percent{}, false, nil
		}
		adverse, err = fill.Subtract(limit)
	case string(domain.SideSell):
		if fill.Compare(limit) >= 0 {
			return domain.Percent{}, false, nil
		}
		adverse, err = limit.Subtract(fill)
	default:
		return domain.Percent{}, false, fmt.Errorf("sandbox_strategy_risk_slippage_invalid")
	}
	if err != nil {
		return domain.Percent{}, false, fmt.Errorf("sandbox_strategy_risk_slippage_invalid")
	}
	value, err := domain.CalculateConservativePercent(adverse, limit, 18)
	return value, true, err
}

func buildSandboxRiskCurrentValuation(
	work sandbox.StrategySessionWork,
	snapshot sandbox.AccountSnapshot,
	market sandbox.StrategyMarketInput,
	facts sandbox.StrategyRiskFacts,
	admission sandbox.StrategySessionAdmission,
	accounting sandboxRiskAccountingFacts,
	operational sandboxRiskOperationalFacts,
	now time.Time,
) (sandbox.StrategyRiskValuation, error) {
	base, quote, err := sandboxAccountingInstrumentAssets(work.Instrument)
	mark, err := sandboxRiskMarketMark(market, err == nil && quote == "USDT")
	if err != nil {
		return sandbox.StrategyRiskValuation{}, err
	}
	amounts, err := calculateSandboxRiskValuationAmounts(snapshot, base, quote, mark, accounting, operational)
	if err != nil {
		return sandbox.StrategyRiskValuation{}, err
	}
	return sandbox.StrategyRiskValuation{
		StrategySessionID: work.SessionID, StrategyRevision: work.StrategyRevision,
		AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch, Instrument: work.Instrument,
		SnapshotHash: snapshot.SnapshotHash, MarketHash: sandbox.StrategyMarketEvidenceHash(market),
		PolicyID: facts.PolicyID, PolicyVersion: facts.PolicyVersion, PolicyHash: facts.PolicyHash,
		AccountingState: accounting.State, AccountingEvidenceHash: accounting.EvidenceHash,
		AccountingProjectionHash: accounting.ProjectionHash, MarkPrice: mark, AccountEquity: amounts.equity,
		VolatileAssetValue: amounts.volatile, CombinedVolatileValue: amounts.volatile,
		CommittedBuyValue: operational.CommittedBuyValue, ExchangeRiskValue: amounts.exchangeRisk,
		ReserveValue: amounts.availableQuote, ReservedValue: amounts.reserved,
		StrategyPositionQuantity: accounting.Quantity, StrategyPositionValue: amounts.position,
		StrategyTotalCost: accounting.TotalCost, StrategyRealizedPnL: accounting.RealizedPnL,
		StrategyUnrealizedPnL: amounts.unrealized, StrategyTotalPnL: amounts.totalPnL,
		OpenOrders: operational.OpenOrders, Slippage: operational.Slippage,
		ReconciliationID: operational.ReconciliationID, ReconciliationHash: operational.ReconciliationHash,
		StorageRevision: operational.StorageRevision, StorageObservedAt: operational.StorageObservedAt,
		EngineStartupCycle: operational.EngineStartupCycle,
		AdmissionHash:      sandbox.StrategyRiskAdmissionHash(admission), ObservedAt: now,
	}, nil
}

func sandboxRiskMarketMark(market sandbox.StrategyMarketInput, instrumentValid bool) (domain.Price, error) {
	if !instrumentValid || len(market.Book.Bids) == 0 || len(market.Book.Asks) == 0 {
		return domain.Price{}, fmt.Errorf("sandbox_strategy_risk_valuation_invalid")
	}
	mark, bestAsk := market.Book.Bids[0].Price, market.Book.Asks[0].Price
	zero, _ := domain.ParsePrice("0")
	if mark.Compare(zero) <= 0 || bestAsk.Compare(mark) < 0 {
		return domain.Price{}, fmt.Errorf("sandbox_strategy_risk_market_invalid")
	}
	for _, level := range market.Book.Bids[1:] {
		if level.Price.Compare(mark) > 0 {
			return domain.Price{}, fmt.Errorf("sandbox_strategy_risk_market_invalid")
		}
	}
	for _, level := range market.Book.Asks[1:] {
		if level.Price.Compare(bestAsk) < 0 {
			return domain.Price{}, fmt.Errorf("sandbox_strategy_risk_market_invalid")
		}
	}
	return mark, nil
}

type sandboxRiskValuationAmounts struct {
	availableQuote, equity, volatile, reserved, exchangeRisk, position domain.Money
	unrealized, totalPnL                                               domain.PnL
}

func calculateSandboxRiskValuationAmounts(snapshot sandbox.AccountSnapshot, base, quote domain.AssetSymbol,
	mark domain.Price, accounting sandboxRiskAccountingFacts, operational sandboxRiskOperationalFacts,
) (sandboxRiskValuationAmounts, error) {
	availableQuote, reservedQuote, availableBase, reservedBase, err := sandboxRiskBalances(snapshot, base, quote)
	if err != nil {
		return sandboxRiskValuationAmounts{}, err
	}
	baseTotal, err := availableBase.Add(reservedBase)
	if err != nil || accounting.Quantity.Compare(baseTotal) > 0 {
		return sandboxRiskValuationAmounts{}, fmt.Errorf("sandbox_strategy_risk_inventory_mismatch")
	}
	volatile, volatileErr := domain.CalculateMoney(mark, baseTotal, 18)
	reservedBaseValue, baseErr := domain.CalculateMoney(mark, reservedBase, 18)
	availableMoney, availableErr := domain.ParseMoney(availableQuote.String())
	reservedMoney, reservedErr := domain.ParseMoney(reservedQuote.String())
	if volatileErr != nil || baseErr != nil || availableErr != nil || reservedErr != nil {
		return sandboxRiskValuationAmounts{}, fmt.Errorf("sandbox_strategy_risk_valuation_invalid")
	}
	quoteTotal, quoteErr := availableMoney.Add(reservedMoney)
	equity, equityErr := quoteTotal.Add(volatile)
	reserved, reserveErr := reservedMoney.Add(reservedBaseValue)
	exchangeRisk, exchangeErr := volatile.Add(operational.CommittedBuyValue)
	position, positionErr := domain.CalculateMoney(mark, accounting.Quantity, 18)
	if quoteErr != nil || equityErr != nil || reserveErr != nil || exchangeErr != nil || positionErr != nil {
		return sandboxRiskValuationAmounts{}, fmt.Errorf("sandbox_strategy_risk_valuation_invalid")
	}
	zeroFee, _ := domain.ParseFee("0")
	unrealized, pnlErr := domain.MoneyDifference(position, accounting.TotalCost, zeroFee)
	if pnlErr != nil {
		return sandboxRiskValuationAmounts{}, fmt.Errorf("sandbox_strategy_risk_valuation_invalid")
	}
	totalPnL, totalErr := accounting.RealizedPnL.Add(unrealized)
	if totalErr != nil {
		return sandboxRiskValuationAmounts{}, fmt.Errorf("sandbox_strategy_risk_valuation_invalid")
	}
	return sandboxRiskValuationAmounts{availableQuote: availableMoney, equity: equity, volatile: volatile,
		reserved: reserved, exchangeRisk: exchangeRisk, position: position,
		unrealized: unrealized, totalPnL: totalPnL}, nil
}

func sandboxRiskBalances(
	snapshot sandbox.AccountSnapshot,
	base domain.AssetSymbol,
	quote domain.AssetSymbol,
) (domain.Balance, domain.Balance, domain.Balance, domain.Balance, error) {
	zero, _ := domain.ParseBalance("0")
	availableQuote, reservedQuote := zero, zero
	availableBase, reservedBase := zero, zero
	quoteFound := false
	for _, balance := range snapshot.Balances {
		switch balance.Asset {
		case quote:
			availableQuote, reservedQuote, quoteFound = balance.Available, balance.Reserved, true
		case base:
			availableBase, reservedBase = balance.Available, balance.Reserved
		default:
			if balance.Available.Compare(zero) != 0 || balance.Reserved.Compare(zero) != 0 {
				return domain.Balance{}, domain.Balance{}, domain.Balance{}, domain.Balance{},
					fmt.Errorf("sandbox_strategy_risk_asset_valuation_unavailable")
			}
		}
	}
	if !quoteFound {
		return domain.Balance{}, domain.Balance{}, domain.Balance{}, domain.Balance{},
			fmt.Errorf("sandbox_strategy_risk_settlement_balance_unavailable")
	}
	return availableQuote, reservedQuote, availableBase, reservedBase, nil
}

func loadSandboxRiskValuationHistory(
	ctx context.Context,
	tx pgx.Tx,
	current sandbox.StrategyRiskValuation,
	now time.Time,
) (sandboxRiskValuationHistory, error) {
	rows, err := tx.Query(ctx, sandboxRiskValuationHistorySQL, current.AccountID, current.AccountEpoch, now)
	if err != nil {
		return sandboxRiskValuationHistory{}, fmt.Errorf("sandbox_strategy_risk_history_unavailable")
	}
	defer rows.Close()
	history := newSandboxRiskValuationHistory(current)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	rollingStart := now.Add(-24 * time.Hour)
	seenAny, seenDay, seenRolling := false, false, false
	for rows.Next() {
		var sessionID, equityText, pnlText string
		var observedAt time.Time
		if err = rows.Scan(&sessionID, &equityText, &pnlText, &observedAt); err != nil {
			return sandboxRiskValuationHistory{}, fmt.Errorf("sandbox_strategy_risk_history_invalid")
		}
		equity, equityErr := domain.ParseMoney(equityText)
		pnl, pnlErr := domain.ParsePnL(pnlText)
		observedAt = observedAt.UTC()
		if equityErr != nil || pnlErr != nil || observedAt.After(now) {
			return sandboxRiskValuationHistory{}, fmt.Errorf("sandbox_strategy_risk_history_invalid")
		}
		seenAny = true
		if equity.Compare(history.AccountPeakEquity) > 0 {
			history.AccountPeakEquity = equity
		}
		if !seenDay && !observedAt.Before(dayStart) {
			history.UTCDayBaselineEquity, seenDay = equity, true
		}
		if !seenRolling && !observedAt.Before(rollingStart) {
			history.Rolling24HourBaselineEquity, seenRolling = equity, true
		}
		if sessionID == string(current.StrategySessionID) && pnl.Compare(history.StrategyPeakPnL) > 0 {
			history.StrategyPeakPnL = pnl
		}
	}
	if rows.Err() != nil {
		return sandboxRiskValuationHistory{}, fmt.Errorf("sandbox_strategy_risk_history_unavailable")
	}
	history.Ready = seenAny && seenDay && seenRolling
	return history, nil
}

func newSandboxRiskValuationHistory(current sandbox.StrategyRiskValuation) sandboxRiskValuationHistory {
	zero, _ := domain.ParsePnL("0")
	history := sandboxRiskValuationHistory{AccountPeakEquity: current.AccountEquity,
		UTCDayBaselineEquity: current.AccountEquity, Rolling24HourBaselineEquity: current.AccountEquity,
		StrategyPeakPnL: zero}
	if current.StrategyTotalPnL.Compare(zero) > 0 {
		history.StrategyPeakPnL = current.StrategyTotalPnL
	}
	return history
}

func insertSandboxRiskValuation(
	ctx context.Context,
	tx pgx.Tx,
	valuation sandbox.StrategyRiskValuation,
	riskObservationID string,
	recordedAt time.Time,
) error {
	evidenceHash := valuation.EvidenceHash()
	if evidenceHash == "" {
		return fmt.Errorf("sandbox_strategy_risk_valuation_invalid")
	}
	var projectionHash any
	if valuation.AccountingProjectionHash != "" {
		projectionHash = valuation.AccountingProjectionHash
	}
	var observationID any
	if riskObservationID != "" {
		observationID = riskObservationID
	}
	id := "strategy-risk-valuation-" + evidenceHash[:32]
	tag, err := tx.Exec(ctx, insertSandboxRiskValuationSQL, id, valuation.Purpose, valuation.StrategySessionID,
		valuation.AccountID, valuation.AccountEpoch, valuation.StrategyRevision, valuation.Instrument,
		valuation.SnapshotHash, valuation.MarketHash, valuation.PolicyID, valuation.PolicyVersion,
		valuation.PolicyHash, valuation.AccountingState, valuation.AccountingEvidenceHash,
		projectionHash, valuation.MarkPrice.String(), valuation.AccountEquity.String(),
		valuation.VolatileAssetValue.String(), valuation.CombinedVolatileValue.String(),
		valuation.CommittedBuyValue.String(), valuation.ExchangeRiskValue.String(),
		valuation.ReserveValue.String(), valuation.ReservedValue.String(),
		valuation.StrategyPositionQuantity.String(), valuation.StrategyPositionValue.String(),
		valuation.StrategyTotalCost.String(), valuation.StrategyRealizedPnL.String(),
		valuation.StrategyUnrealizedPnL.String(), valuation.StrategyTotalPnL.String(),
		valuation.AccountPeakEquity.String(), valuation.UTCDayBaselineEquity.String(),
		valuation.Rolling24HourBaselineEquity.String(), valuation.StrategyPeakPnL.String(),
		valuation.OpenOrders, valuation.Slippage.String(), valuation.ReconciliationID,
		valuation.ReconciliationHash, valuation.StorageRevision, valuation.StorageObservedAt,
		valuation.EngineStartupCycle, valuation.AdmissionHash, observationID,
		valuation.ObservedAt, recordedAt, evidenceHash)
	if err != nil {
		return fmt.Errorf("sandbox_strategy_risk_valuation_write_failed")
	}
	if tag.RowsAffected() == 0 {
		var recordedHash string
		if err = tx.QueryRow(ctx, `SELECT evidence_hash::text FROM sandbox_strategy_risk_valuations WHERE id=$1`, id).Scan(&recordedHash); err != nil || recordedHash != evidenceHash {
			return fmt.Errorf("sandbox_strategy_risk_valuation_conflict")
		}
	}
	return nil
}

var _ sandbox.StrategyRiskObservationProjector = (*SandboxRuntimeDispatcherStore)(nil)
