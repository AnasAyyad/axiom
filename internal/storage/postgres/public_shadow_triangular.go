package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/reconciliation"
	"axiom/internal/risk"
	"axiom/internal/strategies/triangular"

	"github.com/jackc/pgx/v5"
)

type ownerConsoleTriangularReductionEvidence struct {
	Transactions   []accounting.Transaction `json:"transactions"`
	Reconciliation reconciliation.Case      `json:"reconciliation"`
}

type ownerConsoleTriangularEvaluationBalances struct {
	AvailableSettlement domain.Balance                        `json:"available_settlement"`
	StrategyBudget      domain.Balance                        `json:"strategy_budget"`
	GlobalReserveFloor  domain.Balance                        `json:"global_reserve_floor"`
	RecoveryAllowance   domain.Balance                        `json:"recovery_allowance"`
	FeeBalances         map[domain.AssetSymbol]domain.Balance `json:"fee_balances"`
}

// RecordTriangularShadowDecision atomically preserves one exact three-market
// evaluation. Accepted cycles use the dedicated multi-leg evidence record and
// exact virtual-balance projection; they never enter the one-order fill path.
func (store *PublicShadowStore) RecordTriangularShadowDecision(
	ctx context.Context,
	claim PublicShadowClaim,
	input triangular.Input,
	result backtest.EventResult,
) (map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	if store == nil || !validOwnerConsoleTriangularShadowInput(claim, input, result) {
		return nil, fmt.Errorf("owner_console_shadow_triangular_input_invalid")
	}
	inputPayload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("owner_console_shadow_triangular_input_invalid")
	}
	accepted, decisionPayload, reasonCode, projection, err := decodeOwnerConsoleTriangularShadowResult(claim, input, result)
	if err != nil {
		return nil, err
	}
	evidence := newOwnerConsoleTriangularDecisionEvidence(claim, input, accepted, reasonCode,
		inputPayload, decisionPayload)

	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = verifyPublicShadowEvidenceLease(ctx, tx, store.owner, claim.ID); err != nil {
		return nil, err
	}
	current, err := loadOwnerConsoleTriangularBalances(ctx, tx, claim.AccountID)
	if err != nil || !ownerConsoleTriangularInputBalancesMatch(input, current) {
		return nil, fmt.Errorf("owner_console_shadow_triangular_balance_input_stale")
	}
	if err = insertOwnerConsoleMultilegDecisionEvidence(ctx, tx, claim, evidence); err != nil {
		return nil, err
	}
	return finishOwnerConsoleTriangularShadowDecision(ctx, tx, store.owner, claim, evidence, result,
		projection, current, accepted)
}

func finishOwnerConsoleTriangularShadowDecision(ctx context.Context, tx pgx.Tx, owner string, claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence, result backtest.EventResult, projection *triangular.RecordedProjection,
	current map[domain.AssetSymbol]accounting.BalanceSnapshot, accepted bool,
) (map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	updated := current
	var err error
	if accepted {
		updated, err = insertOwnerConsoleTriangularExecutionEvidence(ctx, tx, claim, evidence, result, projection, current)
		if err != nil {
			return nil, err
		}
		if projection.Simulation.Recovery.Quarantined ||
			projection.Simulation.Saga.State == execution.PlanQuarantined {
			if err = failOwnerConsoleTriangularStrandedSession(ctx, tx, owner, claim, evidence); err != nil {
				return nil, err
			}
		}
	}
	if err = insertPublicShadowOutbox(ctx, tx, evidence); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return updated, nil
}

func validOwnerConsoleTriangularShadowInput(claim PublicShadowClaim, input triangular.Input,
	result backtest.EventResult,
) bool {
	configured, err := triangular.ConfigurationFromReviewed(claim.Configuration.Triangular)
	return claim.StrategyID == "triangular-arbitrage-1-0-0" && config.Validate(claim.Configuration) == nil && err == nil &&
		input.ConfigurationHash == claim.ConfigurationHash && reflect.DeepEqual(input.Configuration, configured) &&
		input.Exchange == claim.ExchangeID && input.ValidateEventBinding(result.Ordinal, input.LogicalTime) == nil &&
		(input.Reduction == nil || input.Reduction.Reconciliation.Scope == "shadow/triangular/"+claim.ID)
}

func newOwnerConsoleTriangularDecisionEvidence(claim PublicShadowClaim, input triangular.Input, accepted bool,
	reason string, inputPayload, decisionPayload []byte,
) publicShadowDecisionEvidence {
	identity := ownerConsoleSHA256(append(append([]byte(nil), inputPayload...), decisionPayload...))
	return publicShadowDecisionEvidence{decisionID: "triangular-shadow-" + identity[:32], reasonCode: reason,
		inputKind: "triangular_input", inputID: input.InstrumentMetadataID,
		inputRevision: input.Ordinal, correlationID: claim.ID, causationID: "triangle-view-" + identity[:24],
		now: input.Now, ordinal: input.Ordinal, accepted: accepted, inputPayload: inputPayload,
		decisionPayload: decisionPayload, outboxTopic: "triangular.decision", outboxStream: "strategy",
		outboxEntity: "triangular_decision"}
}

func failOwnerConsoleTriangularStrandedSession(
	ctx context.Context,
	tx pgx.Tx,
	owner string,
	claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence,
) error {
	tag, err := tx.Exec(ctx, `UPDATE shadow_sessions SET state='FAILED',entries_enabled=false,
      revision=revision+1,failure_code='stranded_virtual_inventory',stopped_at=$1
      WHERE id=$2 AND claim_owner=$3 AND state IN ('PAUSED','RUNNING')`, evidence.now, claim.ID, owner)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_triangular_quarantine_failed")
	}
	tag, err = tx.Exec(ctx, `UPDATE runs SET state='failed',completed_at=$1
      WHERE id=$2 AND state='running'`, evidence.now, claim.RunID)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("owner_console_shadow_triangular_quarantine_failed")
	}
	return nil
}

func decodeOwnerConsoleTriangularShadowResult(
	claim PublicShadowClaim,
	input triangular.Input,
	result backtest.EventResult,
) (bool, []byte, string, *triangular.RecordedProjection, error) {
	if result.Ordinal != input.Ordinal || !json.Valid(result.Decision) || !json.Valid(result.Orders) ||
		!json.Valid(result.ExecutionEvents) || !json.Valid(result.Balances) {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_triangular_result_invalid")
	}
	if input.Reduction == nil {
		return decodeOwnerConsoleTriangularNoAction(input, result)
	}
	projection, err := triangular.ValidateCleanRecordedReduction(input)
	if err != nil {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_triangular_reduction_invalid")
	}
	var decision risk.Decision
	var plan execution.Saga
	var simulation triangular.SimulationResult
	var reduction ownerConsoleTriangularReductionEvidence
	if json.Unmarshal(result.Decision, &decision) != nil || decision.Action != risk.ActionApprove ||
		decision.ReasonCode == "" || json.Unmarshal(result.Orders, &plan) != nil ||
		!reflect.DeepEqual(plan, projection.Plan) ||
		json.Unmarshal(result.ExecutionEvents, &simulation) != nil ||
		!reflect.DeepEqual(simulation, projection.Simulation) || simulation.Saga.ID != plan.ID ||
		json.Unmarshal(result.Balances, &reduction) != nil ||
		reduction.Reconciliation.ID != "" || reduction.Reconciliation.State != "" {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_triangular_execution_invalid")
	}
	expectedTransactions, err := ownerConsoleTriangularTransactions(claim, input, projection)
	if err != nil || !reflect.DeepEqual(reduction.Transactions, expectedTransactions) {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_triangular_reduction_invalid")
	}
	return true, append([]byte(nil), result.Decision...), decision.ReasonCode, projection, nil
}

func decodeOwnerConsoleTriangularNoAction(input triangular.Input, result backtest.EventResult) (
	bool, []byte, string, *triangular.RecordedProjection, error,
) {
	prepared, projection, err := triangular.AttachCleanRecordedReduction(input,
		"shadow/triangular/"+input.InstrumentMetadataID)
	if err != nil || projection != nil || prepared.Reduction != nil ||
		string(result.Orders) != "[]" || string(result.ExecutionEvents) != "[]" {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_triangular_no_action_invalid")
	}
	var decision triangular.EvaluationDecision
	var balances ownerConsoleTriangularEvaluationBalances
	if json.Unmarshal(result.Decision, &decision) != nil || json.Unmarshal(result.Balances, &balances) != nil ||
		decision.Action != "no_action" || decision.ReasonCode != "no_eligible_cycle" ||
		decision.CandidateCount != 0 || decision.ConfigurationHash != input.ConfigurationHash ||
		decision.InstrumentMetadataID != input.InstrumentMetadataID ||
		balances.AvailableSettlement.Compare(input.AvailableSettlement) != 0 ||
		balances.StrategyBudget.Compare(input.StrategyBudget) != 0 ||
		balances.GlobalReserveFloor.Compare(input.GlobalReserveFloor) != 0 ||
		balances.RecoveryAllowance.Compare(input.RecoveryAllowance) != 0 ||
		!ownerConsoleBalanceMapsEqual(balances.FeeBalances, input.FeeBalances) {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_triangular_no_action_invalid")
	}
	return false, append([]byte(nil), result.Decision...), decision.ReasonCode, nil, nil
}

func ownerConsoleTriangularTransactions(
	claim PublicShadowClaim,
	input triangular.Input,
	projection *triangular.RecordedProjection,
) ([]accounting.Transaction, error) {
	const stride uint64 = 32
	if projection == nil || input.Ordinal == 0 || input.Ordinal > (^uint64(0)-1)/stride+1 {
		return nil, fmt.Errorf("owner_console_shadow_triangular_reduction_invalid")
	}
	runID, runErr := domain.NewRunID(claim.RunID)
	portfolioID, portfolioErr := domain.NewPortfolioID(claim.PortfolioID)
	if runErr != nil || portfolioErr != nil {
		return nil, fmt.Errorf("owner_console_shadow_triangular_reduction_invalid")
	}
	journal, err := triangular.NewCycleJournal(accounting.NewMemoryJournal(), triangular.JournalContext{
		RunID: runID, PortfolioID: portfolioID, Owner: "triangular",
		ConfigurationHash: input.ConfigurationHash,
		RecordedAt:        domain.EventTime{UTC: input.Now, Sequence: input.Ordinal},
		FirstOrdinal:      (input.Ordinal-1)*stride + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("owner_console_shadow_triangular_reduction_invalid")
	}
	return journal.Transactions(projection.Candidate, projection.Simulation)
}

func loadOwnerConsoleTriangularBalances(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
) (map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	rows, err := tx.Query(ctx, `SELECT asset_symbol,available::text,reserved::text,revision
      FROM virtual_balances WHERE account_id=$1 ORDER BY asset_symbol FOR UPDATE`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[domain.AssetSymbol]accounting.BalanceSnapshot, 3)
	for rows.Next() {
		var assetText, availableText, reservedText string
		var revision uint64
		if err = rows.Scan(&assetText, &availableText, &reservedText, &revision); err != nil {
			return nil, err
		}
		asset, assetErr := domain.ParseAssetSymbol(assetText)
		available, availableErr := domain.ParseBalance(availableText)
		reserved, reservedErr := domain.ParseBalance(reservedText)
		if assetErr != nil || availableErr != nil || reservedErr != nil || revision == 0 ||
			result[asset].Revision != 0 {
			return nil, fmt.Errorf("owner_console_shadow_triangular_balance_invalid")
		}
		result[asset] = accounting.BalanceSnapshot{Available: available, Reserved: reserved, Revision: revision}
	}
	if err = rows.Err(); err != nil || len(result) != 3 {
		return nil, fmt.Errorf("owner_console_shadow_triangular_balance_invalid")
	}
	for _, value := range []string{"USDT", "BTC", "ETH"} {
		asset, _ := domain.ParseAssetSymbol(value)
		if result[asset].Revision == 0 {
			return nil, fmt.Errorf("owner_console_shadow_triangular_balance_invalid")
		}
	}
	return result, nil
}

func ownerConsoleTriangularInputBalancesMatch(
	input triangular.Input,
	current map[domain.AssetSymbol]accounting.BalanceSnapshot,
) bool {
	zero, _ := domain.ParseBalance("0")
	positive := make(map[domain.AssetSymbol]domain.Balance)
	for asset, snapshot := range current {
		if snapshot.Reserved.Compare(zero) != 0 {
			return false
		}
		if snapshot.Available.Compare(zero) > 0 {
			positive[asset] = snapshot.Available
		}
	}
	settlement, _ := domain.ParseAssetSymbol("USDT")
	return current[settlement].Available.Compare(input.AvailableSettlement) == 0 &&
		ownerConsoleBalanceMapsEqual(positive, input.FeeBalances)
}

func ownerConsoleBalanceMapsEqual(
	left, right map[domain.AssetSymbol]domain.Balance,
) bool {
	if len(left) != len(right) {
		return false
	}
	for asset, value := range left {
		other, exists := right[asset]
		if !exists || value.Compare(other) != 0 {
			return false
		}
	}
	return true
}

func insertOwnerConsoleTriangularExecutionEvidence(
	ctx context.Context,
	tx pgx.Tx,
	claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence,
	result backtest.EventResult,
	projection *triangular.RecordedProjection,
	current map[domain.AssetSymbol]accounting.BalanceSnapshot,
) (map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	if projection == nil || projection.Candidate.ID == "" || len(projection.AvailableBalances) != len(current) {
		return nil, fmt.Errorf("owner_console_shadow_triangular_projection_invalid")
	}
	projectedPayload, err := json.Marshal(projection.AvailableBalances)
	if err != nil {
		return nil, fmt.Errorf("owner_console_shadow_triangular_projection_invalid")
	}
	var reduction ownerConsoleTriangularReductionEvidence
	if json.Unmarshal(result.Balances, &reduction) != nil ||
		insertOwnerConsoleTriangularJournal(ctx, tx, claim, evidence, reduction.Transactions) != nil {
		return nil, fmt.Errorf("owner_console_shadow_triangular_journal_failed")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO shadow_multileg_execution_evidence(decision_id,strategy_version_id,
      candidate_id,outcome,execution_plan_hash,canonical_execution_plan,simulation_hash,canonical_simulation,
      reduction_hash,canonical_reduction,projected_balances_hash,canonical_projected_balances,recorded_at)
      VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, evidence.decisionID, claim.StrategyID,
		projection.Candidate.ID, string(projection.Simulation.Outcome), ownerConsoleSHA256(result.Orders), result.Orders,
		ownerConsoleSHA256(result.ExecutionEvents), result.ExecutionEvents, ownerConsoleSHA256(result.Balances), result.Balances,
		ownerConsoleSHA256(projectedPayload), projectedPayload, evidence.now); err != nil {
		return nil, err
	}
	updated := make(map[domain.AssetSymbol]accounting.BalanceSnapshot, len(current))
	for asset, before := range current {
		after, exists := projection.AvailableBalances[asset]
		if !exists {
			return nil, fmt.Errorf("owner_console_shadow_triangular_projection_invalid")
		}
		tag, updateErr := tx.Exec(ctx, `UPDATE virtual_balances SET available=$1,reserved=0,
        revision=revision+1,updated_at=$2 WHERE account_id=$3 AND asset_symbol=$4 AND revision=$5
        AND available=$6 AND reserved=0`, after.String(), evidence.now, claim.AccountID, asset,
			before.Revision, before.Available.String())
		if updateErr != nil || tag.RowsAffected() != 1 {
			return nil, fmt.Errorf("owner_console_shadow_triangular_balance_projection_failed")
		}
		updated[asset] = accounting.BalanceSnapshot{Available: after, Reserved: before.Reserved,
			Revision: before.Revision + 1}
	}
	return updated, nil
}

func insertOwnerConsoleTriangularJournal(
	ctx context.Context,
	tx pgx.Tx,
	claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence,
	transactions []accounting.Transaction,
) error {
	if len(transactions) == 0 {
		return fmt.Errorf("owner_console_shadow_triangular_journal_invalid")
	}
	for _, transaction := range transactions {
		if accounting.ValidateTransaction(transaction) != nil || transaction.RunID.Value() != claim.RunID ||
			transaction.PortfolioID.Value() != claim.PortfolioID ||
			transaction.ConfigurationHash != claim.ConfigurationHash {
			return fmt.Errorf("owner_console_shadow_triangular_journal_invalid")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO journal_transactions(id,transaction_type,run_id,portfolio_id,
        configuration_id,causation_id,correlation_id,recorded_at,ingest_ordinal)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, transaction.ID.Value(), transaction.Type,
			claim.RunID, claim.PortfolioID, claim.ConfigurationID, transaction.CausationID.Value(),
			evidence.correlationID, transaction.RecordedAt.UTC, transaction.IngestOrdinal); err != nil {
			return err
		}
		for index, line := range transaction.Lines {
			if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries(transaction_id,line_number,account_class,
          account_owner,asset_symbol,direction,quantity,lot_reference,rounding_metadata)
          VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, transaction.ID.Value(), index+1,
				string(line.Account.Class), line.Account.Owner, line.Account.Asset, string(line.Direction),
				line.Quantity.String(), nullableOwnerConsoleText(line.Lot), nullableOwnerConsoleText(line.Rounding)); err != nil {
				return err
			}
		}
	}
	return nil
}
