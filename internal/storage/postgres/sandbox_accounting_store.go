package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

type sandboxAccountingEntry struct {
	AccountClass string
	Asset        domain.AssetSymbol
	Direction    string
	Quantity     string
}

type sandboxAccountingTransaction struct {
	ID                string
	StrategySessionID string
	PlanID            string
	TransactionType   string
	SourceMode        string
	AccountID         sandbox.AccountID
	AccountEpoch      uint64
	Exchange          sandbox.Exchange
	Environment       sandbox.Environment
	ConfigurationID   string
	PolicyHash        string
	OrderID           string
	ClientOrderID     string
	Action            sandbox.IntentAction
	NativeFillHash    string
	FillID            string
	Instrument        string
	Side              domain.Side
	Quantity          string
	Price             string
	Notional          string
	Fee               string
	Rebate            string
	FeeAsset          domain.AssetSymbol
	FillOrdinal       uint64
	OccurredAt        time.Time
	RecordedAt        time.Time
	EvidenceHash      string
	Entries           []sandboxAccountingEntry
}

type sandboxAccountingScope struct {
	StrategySessionID string
	ConfigurationID   string
	Exchange          sandbox.Exchange
	Environment       sandbox.Environment
	Strategy          string
	PrimaryInstrument string
}

// postSandboxStrategyAccounting appends every previously unseen fill fact to
// the strategy-owned sandbox journal in the same serializable transaction as
// the canonical fill and order-state reduction. Connection-check/canary fills
// remain outside strategy accounting and are deliberately ignored.
func postSandboxStrategyAccounting(
	ctx context.Context,
	tx pgx.Tx,
	record sandbox.SubmissionOutbox,
	event sandbox.PrivateEvent,
) error {
	if ctx == nil || tx == nil || event.OrderEvent == nil ||
		event.Kind != sandbox.PrivateFillEvent || len(event.OrderEvent.Fills) == 0 ||
		record.Submission.PlanID.Value() == "" ||
		record.Submission.OrderID != event.OrderID ||
		record.Submission.ClientOrderID != event.ClientOrderID ||
		record.Submission.AccountID != event.AccountID ||
		record.Submission.AccountEpoch != event.AccountEpoch {
		return fmt.Errorf("sandbox_accounting_fill_invalid")
	}
	scope, strategyOwned, err := sandboxAccountingStrategySession(
		ctx, tx, record.Submission.PlanID.String(), record.Submission.AccountID,
		record.Submission.AccountEpoch,
	)
	if err != nil || !strategyOwned {
		return err
	}
	if err = postSandboxAccountingTransactions(ctx, tx, scope, record.Submission, event); err != nil {
		return err
	}
	projection, err := rebuildSandboxAccountingProjection(ctx, tx, scope, record.Submission)
	if err != nil {
		return err
	}
	return storeSandboxAccountingPosition(ctx, tx, projection)
}

func postSandboxAccountingTransactions(ctx context.Context, tx pgx.Tx, scope sandboxAccountingScope,
	submission sandbox.Submission, event sandbox.PrivateEvent,
) error {
	for _, fill := range event.OrderEvent.Fills {
		posting, err := newSandboxAccountingTransaction(scope, submission, event, fill)
		if err != nil {
			return err
		}
		inserted, err := insertSandboxAccountingTransaction(ctx, tx, posting)
		if err != nil {
			return err
		}
		if !inserted {
			if !sameSandboxAccountingTransaction(ctx, tx, posting) {
				return fmt.Errorf("sandbox_accounting_fill_identity_conflict")
			}
			continue
		}
		for index, entry := range posting.Entries {
			if _, err = tx.Exec(ctx, `
INSERT INTO sandbox_accounting_entries(
 transaction_id,line_number,account_class,account_owner,asset_symbol,
 direction,quantity
) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
				posting.ID, index+1, entry.AccountClass, posting.StrategySessionID,
				entry.Asset, entry.Direction, entry.Quantity); err != nil {
				return fmt.Errorf("sandbox_accounting_entry_write_failed")
			}
		}
	}
	return nil
}

func rebuildSandboxAccountingProjection(ctx context.Context, tx pgx.Tx, scope sandboxAccountingScope,
	submission sandbox.Submission,
) (sandboxAccountingPositionProjection, error) {
	if scope.Strategy == sandbox.StrategyTriangular {
		return rebuildSandboxTriangularAccountingPosition(ctx, tx, scope,
			submission.AccountID, submission.AccountEpoch)
	}
	return rebuildSandboxAccountingPosition(ctx, tx, scope, submission.AccountID,
		submission.AccountEpoch, submission.Instrument.Symbol())
}

func sandboxAccountingStrategySession(
	ctx context.Context,
	tx pgx.Tx,
	planID string,
	accountID sandbox.AccountID,
	accountEpoch uint64,
) (sandboxAccountingScope, bool, error) {
	row, err := loadSandboxAccountingStrategySession(ctx, tx, planID, accountID, accountEpoch)
	if err != nil {
		return sandboxAccountingScope{}, false, fmt.Errorf("sandbox_accounting_plan_unavailable")
	}
	if row.intent == "CANARY" {
		return sandboxAccountingScope{}, false, nil
	}
	if row.intent != "STRATEGY" {
		return sandboxAccountingScope{}, false, fmt.Errorf("sandbox_accounting_plan_intent_invalid")
	}
	// A retained manual connection check uses STRATEGY but has no automatic
	// child session and therefore no strategy-owned sub-ledger.
	if !row.automatic {
		if row.strategySessionID != "" {
			return sandboxAccountingScope{}, false, fmt.Errorf("sandbox_accounting_strategy_scope_invalid")
		}
		return sandboxAccountingScope{}, false, nil
	}
	if row.strategySessionID == "" || row.strategy == "" || row.primaryInstrument == "" {
		return sandboxAccountingScope{}, false, fmt.Errorf("sandbox_accounting_strategy_decision_unavailable")
	}
	return sandboxAccountingScope{StrategySessionID: row.strategySessionID,
		ConfigurationID: row.configurationID, Exchange: row.exchange, Environment: row.environment,
		Strategy: row.strategy, PrimaryInstrument: row.primaryInstrument}, true, nil
}
