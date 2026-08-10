package postgres

import (
	"context"
	"fmt"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5"
)

// PortfolioRiskFillSettlementWrite extends the strategy execution atomic execution boundary with real portfolio and risk ownership claims.
type PortfolioRiskFillSettlementWrite struct {
	Execution StrategyExecutionAtomicWrite
	Candidate generated.SettleAllocationCandidateFillParams
	Funds     generated.SettleReservationFillParams
	Liquidity generated.SettleLiquidityReservationFillParams
	Domain    generated.UpdateLiquidityDomainProjectionParams
}

// SettleFill atomically persists execution facts, journal, projections, and revised partial-fill claims.
func (repository *PortfolioRiskRepository) SettleFill(ctx context.Context, write PortfolioRiskFillSettlementWrite) error {
	if !validPortfolioRiskFillSettlement(write) {
		return fmt.Errorf("portfolio_risk_fill_settlement_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("portfolio_risk_fill_settlement_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if err = executeStrategyExecutionWrite(ctx, queries, write.Execution); err != nil {
		return err
	}
	if _, err = queries.SettleAllocationCandidateFill(ctx, write.Candidate); err != nil {
		return fmt.Errorf("portfolio_risk_fill_candidate_failed")
	}
	if _, err = queries.SettleReservationFill(ctx, write.Funds); err != nil {
		return fmt.Errorf("portfolio_risk_fill_funds_failed")
	}
	if _, err = queries.SettleLiquidityReservationFill(ctx, write.Liquidity); err != nil {
		return fmt.Errorf("portfolio_risk_fill_liquidity_failed")
	}
	if _, err = queries.UpdateLiquidityDomainProjection(ctx, write.Domain); err != nil {
		return fmt.Errorf("portfolio_risk_fill_liquidity_projection_failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("portfolio_risk_fill_settlement_commit_failed")
	}
	return nil
}

func validPortfolioRiskFillSettlement(write PortfolioRiskFillSettlementWrite) bool {
	final := write.Candidate.FinalFill
	return validStrategyExecutionWrite(write.Execution) && write.Candidate.ID != "" && write.Candidate.Revision > 0 &&
		write.Funds.ID != "" && write.Funds.Revision > 0 && write.Funds.FencingToken > 0 &&
		write.Funds.DebitQuantity != nil && write.Funds.FinalFill == final &&
		write.Liquidity.ID != "" && write.Liquidity.Revision > 0 &&
		write.Liquidity.FencingToken == write.Funds.FencingToken && write.Liquidity.FillQuantity != nil &&
		write.Liquidity.FinalFill == final && write.Domain.ID != "" && write.Domain.ExpectedRevision > 0 &&
		write.Domain.AvailableQuantity != nil
}

func insertPortfolioRiskJournal(
	ctx context.Context,
	queries *generated.Queries,
	journal generated.InsertJournalTransactionParams,
	entries []generated.InsertLedgerEntryParams,
) error {
	if _, err := queries.InsertJournalTransaction(ctx, journal); err != nil {
		return fmt.Errorf("portfolio_risk_reservation_journal_failed")
	}
	for _, entry := range entries {
		if _, err := queries.InsertLedgerEntry(ctx, entry); err != nil {
			return fmt.Errorf("portfolio_risk_reservation_journal_entry_failed")
		}
	}
	return nil
}

func validPortfolioRiskJournal(journal generated.InsertJournalTransactionParams, entries []generated.InsertLedgerEntryParams) bool {
	if journal.ID == "" || len(entries) < 2 {
		return false
	}
	for _, entry := range entries {
		if entry.TransactionID != journal.ID {
			return false
		}
	}
	return true
}
