package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

type sandboxAccountingStrategySessionRow struct {
	intent, strategySessionID, configurationID, strategy, primaryInstrument string
	exchange                                                                sandbox.Exchange
	environment                                                             sandbox.Environment
	automatic                                                               bool
}

func loadSandboxAccountingStrategySession(ctx context.Context, tx pgx.Tx, planID string,
	accountID sandbox.AccountID, accountEpoch uint64,
) (sandboxAccountingStrategySessionRow, error) {
	var row sandboxAccountingStrategySessionRow
	err := tx.QueryRow(ctx, `
SELECT plan.intent_kind,coalesce(decision.strategy_session_id,''),
       plan.configuration_id,account.exchange,account.environment,
       coalesce(strategy_session.strategy_id,''),
       coalesce(strategy_session.instrument,''),
       strategy_session.id IS NOT NULL
FROM sandbox_runtime_submission_plans plan
LEFT JOIN sandbox_strategy_decisions decision ON decision.plan_id=plan.id
LEFT JOIN sandbox_strategy_sessions strategy_session
  ON strategy_session.id=plan.sandbox_session_id
JOIN sandbox_runtime_exchange_accounts account
  ON account.id=$2 AND account.current_epoch=$3
WHERE plan.id=$1
FOR SHARE OF plan,account`, planID, accountID, accountEpoch).Scan(
		&row.intent, &row.strategySessionID, &row.configurationID, &row.exchange, &row.environment,
		&row.strategy, &row.primaryInstrument, &row.automatic)
	return row, err
}

func newSandboxAccountingTransaction(
	scope sandboxAccountingScope,
	submission sandbox.Submission,
	event sandbox.PrivateEvent,
	fill execution.FillFact,
) (sandboxAccountingTransaction, error) {
	if !validSandboxAccountingTransactionInput(scope, submission, event, fill) {
		return sandboxAccountingTransaction{}, fmt.Errorf("sandbox_accounting_fill_invalid")
	}
	notional, err := domain.CalculateNotional(fill.Price, fill.Quantity, 18)
	if err != nil || notional.String() == "0" {
		return sandboxAccountingTransaction{}, fmt.Errorf("sandbox_accounting_notional_invalid")
	}
	entries, err := sandboxAccountingEntries(submission, fill, notional)
	if err != nil {
		return sandboxAccountingTransaction{}, err
	}
	feeAsset := fill.FeeAsset
	zeroFee, _ := domain.ParseFee("0")
	if fill.Fee.Compare(zeroFee) == 0 && fill.Rebate.Compare(zeroFee) == 0 {
		feeAsset = ""
	} else if _, err = domain.ParseAssetSymbol(string(feeAsset)); err != nil {
		return sandboxAccountingTransaction{}, fmt.Errorf("sandbox_accounting_fee_asset_invalid")
	}
	fillID := fill.ID.String()
	idHash := stableSandboxRuntimeHash(string(submission.AccountID), strconv.FormatUint(submission.AccountEpoch, 10),
		submission.OrderID.String(), fillID)
	posting := sandboxAccountingTransaction{
		ID: "sandbox-accounting-" + idHash[:32], StrategySessionID: scope.StrategySessionID,
		PlanID: submission.PlanID.String(), TransactionType: "fill", SourceMode: "exchange_sandbox",
		AccountID:    submission.AccountID,
		AccountEpoch: submission.AccountEpoch, OrderID: submission.OrderID.String(),
		Exchange: scope.Exchange, Environment: scope.Environment,
		ConfigurationID: scope.ConfigurationID, PolicyHash: submission.PolicyHash,
		ClientOrderID: submission.ClientOrderID, Action: submission.Action,
		NativeFillHash: event.NativeFillHash, FillID: fillID,
		Instrument: submission.Instrument.Symbol(), Side: submission.Side,
		Quantity: fill.Quantity.String(), Price: fill.Price.String(), Notional: notional.String(),
		Fee: fill.Fee.String(), Rebate: fill.Rebate.String(), FeeAsset: feeAsset,
		FillOrdinal: fill.Ordinal, OccurredAt: event.OccurredAt,
		RecordedAt: event.ReceivedAt, Entries: entries,
	}
	posting.EvidenceHash = sandboxAccountingTransactionEvidenceHash(posting)
	return posting, nil
}

func validSandboxAccountingTransactionInput(scope sandboxAccountingScope, submission sandbox.Submission,
	event sandbox.PrivateEvent, fill execution.FillFact,
) bool {
	return scope.StrategySessionID != "" && scope.ConfigurationID != "" &&
		(scope.Exchange == sandbox.ExchangeBinance || scope.Exchange == sandbox.ExchangeBybit) &&
		(scope.Environment == sandbox.EnvironmentBinanceSpotTestnet ||
			scope.Environment == sandbox.EnvironmentBybitDemo) &&
		submission.PolicyHash != "" && event.OrderEvent != nil &&
		event.OrderEvent.OrderID == event.OrderID && event.OrderEvent.ClientOrderID == event.ClientOrderID &&
		event.OrderEvent.OccurredAt.Equal(event.OccurredAt) && fill.ID.Value() != "" && fill.Ordinal != 0 &&
		fill.Ordinal <= event.OrderEvent.Ordinal && fill.Quantity.String() != "" && fill.Quantity.String() != "0" &&
		fill.Price.String() != "" && fill.Price.String() != "0" && event.NativeFillHash != "" &&
		!event.ReceivedAt.Before(event.OccurredAt)
}

func sandboxAccountingTransactionEvidenceHash(posting sandboxAccountingTransaction) string {
	return stableSandboxRuntimeHash(posting.StrategySessionID, posting.PlanID, posting.TransactionType, posting.SourceMode,
		string(posting.AccountID), strconv.FormatUint(posting.AccountEpoch, 10), posting.OrderID,
		string(posting.Exchange), string(posting.Environment), posting.ConfigurationID,
		posting.PolicyHash, posting.ClientOrderID, string(posting.Action),
		posting.NativeFillHash, posting.FillID, posting.Instrument, string(posting.Side),
		posting.Quantity, posting.Price, posting.Notional, posting.Fee, posting.Rebate,
		string(posting.FeeAsset), strconv.FormatUint(posting.FillOrdinal, 10),
		posting.OccurredAt.Format(time.RFC3339Nano))
}

func sandboxAccountingEntries(
	submission sandbox.Submission,
	fill execution.FillFact,
	notional domain.Notional,
) ([]sandboxAccountingEntry, error) {
	baseQuantity, baseErr := domain.ParseBalance(fill.Quantity.String())
	quoteQuantity, quoteErr := domain.ParseBalance(notional.String())
	feeQuantity, feeErr := domain.ParseBalance(fill.Fee.String())
	rebateQuantity, rebateErr := domain.ParseBalance(fill.Rebate.String())
	if baseErr != nil || quoteErr != nil || feeErr != nil || rebateErr != nil ||
		(submission.Side != domain.SideBuy && submission.Side != domain.SideSell) {
		return nil, fmt.Errorf("sandbox_accounting_fill_invalid")
	}
	entries := make([]sandboxAccountingEntry, 0, 8)
	if submission.Side == domain.SideBuy {
		entries = append(entries,
			sandboxAccountingLine("strategy_inventory", submission.Instrument.Base, "debit", baseQuantity),
			sandboxAccountingLine("exchange_inventory", submission.Instrument.Base, "credit", baseQuantity),
			sandboxAccountingLine("trade_cost_proceeds", submission.Instrument.Quote, "debit", quoteQuantity),
			sandboxAccountingLine("exchange_inventory", submission.Instrument.Quote, "credit", quoteQuantity),
		)
	} else {
		entries = append(entries,
			sandboxAccountingLine("exchange_inventory", submission.Instrument.Base, "debit", baseQuantity),
			sandboxAccountingLine("strategy_inventory", submission.Instrument.Base, "credit", baseQuantity),
			sandboxAccountingLine("exchange_inventory", submission.Instrument.Quote, "debit", quoteQuantity),
			sandboxAccountingLine("trade_cost_proceeds", submission.Instrument.Quote, "credit", quoteQuantity),
		)
	}
	zero, _ := domain.ParseBalance("0")
	if feeQuantity.Compare(zero) > 0 {
		if _, err := domain.ParseAssetSymbol(string(fill.FeeAsset)); err != nil {
			return nil, fmt.Errorf("sandbox_accounting_fee_asset_invalid")
		}
		entries = append(entries,
			sandboxAccountingLine("fee_expense", fill.FeeAsset, "debit", feeQuantity),
			sandboxAccountingLine("exchange_inventory", fill.FeeAsset, "credit", feeQuantity),
		)
	}
	if rebateQuantity.Compare(zero) > 0 {
		if _, err := domain.ParseAssetSymbol(string(fill.FeeAsset)); err != nil {
			return nil, fmt.Errorf("sandbox_accounting_fee_asset_invalid")
		}
		entries = append(entries,
			sandboxAccountingLine("exchange_inventory", fill.FeeAsset, "debit", rebateQuantity),
			sandboxAccountingLine("fee_expense", fill.FeeAsset, "credit", rebateQuantity),
		)
	}
	return entries, nil
}

func sandboxAccountingLine(
	class string,
	asset domain.AssetSymbol,
	direction string,
	quantity domain.Balance,
) sandboxAccountingEntry {
	return sandboxAccountingEntry{AccountClass: class, Asset: asset,
		Direction: direction, Quantity: quantity.String()}
}

func insertSandboxAccountingTransaction(
	ctx context.Context,
	tx pgx.Tx,
	posting sandboxAccountingTransaction,
) (bool, error) {
	var feeAsset any
	if posting.FeeAsset != "" {
		feeAsset = posting.FeeAsset
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO sandbox_accounting_transactions(
 id,strategy_session_id,plan_id,transaction_type,source_mode,
 account_id,account_epoch,exchange,environment,configuration_id,policy_hash,
 order_id,client_order_id,intent_action,
 native_fill_id_hash,fill_id,instrument,side,quantity,price,notional,
 fee,rebate,fee_asset,fill_ordinal,occurred_at,recorded_at,evidence_hash
) VALUES (
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
 $17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28
)
ON CONFLICT (account_id,account_epoch,fill_id) DO NOTHING`,
		posting.ID, posting.StrategySessionID, posting.PlanID,
		posting.TransactionType, posting.SourceMode, posting.AccountID, posting.AccountEpoch,
		posting.Exchange, posting.Environment, posting.ConfigurationID, posting.PolicyHash,
		posting.OrderID, posting.ClientOrderID, posting.Action,
		posting.NativeFillHash, posting.FillID, posting.Instrument, posting.Side,
		posting.Quantity, posting.Price, posting.Notional, posting.Fee, posting.Rebate,
		feeAsset, posting.FillOrdinal, posting.OccurredAt, posting.RecordedAt,
		posting.EvidenceHash,
	)
	if err != nil {
		return false, fmt.Errorf("sandbox_accounting_transaction_write_failed")
	}
	return tag.RowsAffected() == 1, nil
}

func sameSandboxAccountingTransaction(
	ctx context.Context,
	tx pgx.Tx,
	posting sandboxAccountingTransaction,
) bool {
	var same bool
	err := tx.QueryRow(ctx, `
SELECT id=$4 AND strategy_session_id=$5 AND plan_id=$6
 AND transaction_type=$7 AND source_mode=$8
 AND exchange=$9 AND environment=$10 AND configuration_id=$11 AND policy_hash=$12
 AND order_id=$13 AND client_order_id=$14 AND intent_action=$15
 AND native_fill_id_hash=$16 AND instrument=$17 AND side=$18
 AND quantity=$19::numeric AND price=$20::numeric AND notional=$21::numeric
 AND fee=$22::numeric AND rebate=$23::numeric AND coalesce(fee_asset,'')=$24
 AND fill_ordinal=$25 AND occurred_at=$26 AND evidence_hash=$27
FROM sandbox_accounting_transactions
WHERE account_id=$1 AND account_epoch=$2 AND fill_id=$3`,
		posting.AccountID, posting.AccountEpoch, posting.FillID, posting.ID,
		posting.StrategySessionID, posting.PlanID, posting.TransactionType, posting.SourceMode,
		posting.Exchange, posting.Environment, posting.ConfigurationID, posting.PolicyHash,
		posting.OrderID, posting.ClientOrderID, posting.Action, posting.NativeFillHash,
		posting.Instrument, posting.Side, posting.Quantity, posting.Price, posting.Notional,
		posting.Fee, posting.Rebate, posting.FeeAsset, posting.FillOrdinal,
		posting.OccurredAt, posting.EvidenceHash,
	).Scan(&same)
	return err == nil && same
}
