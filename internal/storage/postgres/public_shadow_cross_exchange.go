package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/reconciliation"
	"axiom/internal/risk"
	"axiom/internal/strategies/crossarb"

	"github.com/jackc/pgx/v5"
)

type ownerConsoleCrossExchangeReductionEvidence struct {
	Transactions   []accounting.Transaction `json:"transactions"`
	Reconciliation reconciliation.Case      `json:"reconciliation"`
}

type ownerConsoleCrossExchangeEvaluationBalances struct {
	Inventory   []crossarb.VenueInventory `json:"inventory"`
	QuoteBudget domain.Balance            `json:"quote_budget"`
	FeeBalances map[string]domain.Balance `json:"fee_balances"`
}

// RecordCrossExchangeShadowDecision atomically persists one exact coherent
// two-venue evaluation and keeps every venue balance in its owning account.
func (store *PublicShadowStore) RecordCrossExchangeShadowDecision(
	ctx context.Context,
	claim PublicShadowClaim,
	input crossarb.Input,
	result backtest.EventResult,
) (map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	if store == nil || !validOwnerConsoleCrossExchangeShadowInput(claim, input, result) {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_input_invalid")
	}
	inputPayload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_input_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, publicShadowEvidenceTxOptions())
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = verifyPublicShadowEvidenceLease(ctx, tx, store.owner, claim.ID); err != nil {
		return nil, err
	}
	current, err := loadOwnerConsoleCrossExchangeBalances(ctx, tx, claim.VenueAccountIDs)
	if err != nil || !ownerConsoleCrossExchangeInputBalancesMatch(input, current) {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_balance_input_stale")
	}
	before := ownerConsoleCrossExchangeAvailableBalances(current)
	accepted, decisionPayload, reasonCode, projection, err := decodeOwnerConsoleCrossExchangeShadowResult(
		claim, input, result, before)
	if err != nil {
		return nil, err
	}
	evidence := newOwnerConsoleCrossExchangeDecisionEvidence(claim, input, accepted, reasonCode,
		inputPayload, decisionPayload)
	if err = insertOwnerConsoleMultilegDecisionEvidence(ctx, tx, claim, evidence); err != nil {
		return nil, err
	}
	return finishOwnerConsoleCrossExchangeShadowDecision(ctx, tx, claim, evidence, result, projection,
		current, accepted)
}

func finishOwnerConsoleCrossExchangeShadowDecision(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence, result backtest.EventResult, projection *crossarb.RecordedProjection,
	current map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, accepted bool,
) (map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	updated := current
	var err error
	if accepted {
		updated, err = insertOwnerConsoleCrossExchangeExecutionEvidence(ctx, tx, claim, evidence, result, projection, current)
		if err != nil {
			return nil, err
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

func validOwnerConsoleCrossExchangeShadowInput(claim PublicShadowClaim, input crossarb.Input,
	result backtest.EventResult,
) bool {
	configured, err := crossarb.ConfigurationFromReviewed(claim.Configuration.CrossExchange)
	return claim.StrategyID == "cross-exchange-arbitrage-1-0-0" && len(claim.VenueAccountIDs) == 2 &&
		config.Validate(claim.Configuration) == nil && err == nil &&
		input.ConfigurationHash == claim.ConfigurationHash && reflect.DeepEqual(input.Configuration, configured) &&
		input.ValidateEventBinding(result.Ordinal, input.LogicalTime) == nil &&
		(input.Reduction == nil || input.Reduction.Reconciliation.Scope == "shadow/cross-exchange/"+claim.ID)
}

func newOwnerConsoleCrossExchangeDecisionEvidence(claim PublicShadowClaim, input crossarb.Input, accepted bool,
	reason string, inputPayload, decisionPayload []byte,
) publicShadowDecisionEvidence {
	identity := ownerConsoleSHA256(append(append([]byte(nil), inputPayload...), decisionPayload...))
	return publicShadowDecisionEvidence{decisionID: "cross-shadow-" + identity[:32], reasonCode: reason,
		inputKind: "cross_exchange_input", inputID: input.InstrumentMetadataSetHash,
		inputRevision: input.Ordinal, correlationID: claim.ID, causationID: "cross-view-" + identity[:24],
		now: input.Now, ordinal: input.Ordinal, accepted: accepted, inputPayload: inputPayload,
		decisionPayload: decisionPayload, outboxTopic: "cross_exchange.decision", outboxStream: "strategy",
		outboxEntity: "cross_exchange_decision"}
}

func decodeOwnerConsoleCrossExchangeShadowResult(
	claim PublicShadowClaim,
	input crossarb.Input,
	result backtest.EventResult,
	before crossarb.VenueBalances,
) (bool, []byte, string, *crossarb.RecordedProjection, error) {
	if result.Ordinal != input.Ordinal || !json.Valid(result.Decision) || !json.Valid(result.Orders) ||
		!json.Valid(result.ExecutionEvents) || !json.Valid(result.Balances) {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_cross_exchange_result_invalid")
	}
	if input.Reduction == nil {
		return decodeOwnerConsoleCrossExchangeNoAction(claim, input, result, before)
	}
	projection, err := crossarb.ValidateCleanRecordedReduction(input, before)
	if err != nil {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_cross_exchange_reduction_invalid")
	}
	var decision risk.Decision
	var plan execution.Saga
	var simulation crossarb.SimulationResult
	var reduction ownerConsoleCrossExchangeReductionEvidence
	if json.Unmarshal(result.Decision, &decision) != nil || decision.Action != risk.ActionApprove ||
		decision.ReasonCode == "" || json.Unmarshal(result.Orders, &plan) != nil ||
		!reflect.DeepEqual(plan, projection.Plan) || json.Unmarshal(result.ExecutionEvents, &simulation) != nil ||
		!reflect.DeepEqual(simulation, projection.Simulation) || simulation.Saga.ID != plan.ID ||
		json.Unmarshal(result.Balances, &reduction) != nil || reduction.Reconciliation.ID != "" ||
		reduction.Reconciliation.State != "" {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_cross_exchange_execution_invalid")
	}
	expectedTransactions, err := ownerConsoleCrossExchangeTransactions(claim, input, projection)
	if err != nil || !reflect.DeepEqual(reduction.Transactions, expectedTransactions) {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_cross_exchange_reduction_invalid")
	}
	return true, append([]byte(nil), result.Decision...), decision.ReasonCode, projection, nil
}

func decodeOwnerConsoleCrossExchangeNoAction(claim PublicShadowClaim, input crossarb.Input,
	result backtest.EventResult, before crossarb.VenueBalances,
) (bool, []byte, string, *crossarb.RecordedProjection, error) {
	prepared, projection, err := crossarb.AttachCleanRecordedReduction(input,
		"shadow/cross-exchange/"+claim.ID, before)
	if err != nil || projection != nil || prepared.Reduction != nil ||
		string(result.Orders) != "[]" || string(result.ExecutionEvents) != "[]" {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_cross_exchange_no_action_invalid")
	}
	var decision crossarb.EvaluationDecision
	var balances ownerConsoleCrossExchangeEvaluationBalances
	if json.Unmarshal(result.Decision, &decision) != nil || json.Unmarshal(result.Balances, &balances) != nil ||
		decision.Action != "no_action" || decision.ReasonCode != "no_eligible_direction" ||
		decision.CandidateCount != 0 || decision.ConfigurationHash != input.ConfigurationHash ||
		decision.InstrumentMetadataSetHash != input.InstrumentMetadataSetHash ||
		!reflect.DeepEqual(balances.Inventory, input.Inventory) ||
		balances.QuoteBudget.Compare(input.QuoteBudget) != 0 ||
		!ownerConsoleStringBalanceMapsEqual(balances.FeeBalances, input.FeeBalances) {
		return false, nil, "", nil, fmt.Errorf("owner_console_shadow_cross_exchange_no_action_invalid")
	}
	return false, append([]byte(nil), result.Decision...), decision.ReasonCode, nil, nil
}

func ownerConsoleCrossExchangeTransactions(claim PublicShadowClaim, input crossarb.Input,
	projection *crossarb.RecordedProjection,
) ([]accounting.Transaction, error) {
	const stride uint64 = 32
	if projection == nil || input.Ordinal == 0 || input.Ordinal > (^uint64(0)-1)/stride+1 ||
		input.Reduction == nil {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_reduction_invalid")
	}
	runID, runErr := domain.NewRunID(claim.RunID)
	portfolioID, portfolioErr := domain.NewPortfolioID(claim.PortfolioID)
	if runErr != nil || portfolioErr != nil {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_reduction_invalid")
	}
	journal, err := crossarb.NewCrossExchangeJournal(accounting.NewMemoryJournal(), crossarb.JournalContext{
		RunID: runID, PortfolioID: portfolioID, Owner: "cross_exchange",
		ConfigurationHash: input.ConfigurationHash,
		RecordedAt:        domain.EventTime{UTC: input.Now, Sequence: input.Ordinal},
		FirstOrdinal:      (input.Ordinal-1)*stride + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_reduction_invalid")
	}
	return journal.Transactions(projection.Candidate, projection.Simulation, input.Reduction.Attribution)
}

func loadOwnerConsoleCrossExchangeBalances(ctx context.Context, tx pgx.Tx, accountIDs map[string]string,
) (map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	if len(accountIDs) != 2 {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_balance_invalid")
	}
	result := make(map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, 2)
	for _, exchange := range []string{"binance", "bybit"} {
		accountID := accountIDs[exchange]
		balances, err := loadOwnerConsoleCrossExchangeAccountBalances(ctx, tx, accountID)
		if err != nil {
			return nil, err
		}
		result[exchange] = balances
	}
	return result, nil
}

func ownerConsoleCrossExchangeAvailableBalances(
	values map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot,
) crossarb.VenueBalances {
	result := make(crossarb.VenueBalances, len(values))
	for exchange, balances := range values {
		result[exchange] = make(map[domain.AssetSymbol]domain.Balance, len(balances))
		for asset, balance := range balances {
			result[exchange][asset] = balance.Available
		}
	}
	return result
}

func ownerConsoleCrossExchangeInputBalancesMatch(input crossarb.Input,
	current map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot,
) bool {
	if len(input.Inventory) != 2 || len(current) != 2 {
		return false
	}
	base := input.Markets[0].Snapshot.Instrument.Base
	totalBase, totalUSDT, valid := ownerConsoleCrossExchangeBalanceTotals(current, base)
	if !valid {
		return false
	}
	owner := "cross-exchange:" + input.ConfigurationHash
	for _, inventory := range input.Inventory {
		balances, exists := current[inventory.Exchange]
		if !exists || inventory.Owner != owner || inventory.BaseAsset != base ||
			inventory.OwnedBase.Compare(balances[base].Available) != 0 ||
			inventory.OwnedUSDT.Compare(balances[domain.AssetSymbol("USDT")].Available) != 0 ||
			inventory.TotalEligibleBase.Compare(totalBase) != 0 ||
			inventory.TotalEligibleUSDT.Compare(totalUSDT) != 0 ||
			inventory.Revision != balances[base].Revision {
			return false
		}
	}
	for key, expected := range input.FeeBalances {
		exchange, asset, found := strings.Cut(key, ":")
		if !found || exchange == "" || asset == "" {
			return false
		}
		balance, exists := current[exchange][domain.AssetSymbol(asset)]
		if !exists || balance.Available.Compare(expected) != 0 {
			return false
		}
	}
	return len(input.FeeBalances) == 2
}

func ownerConsoleCrossExchangeBalanceTotals(current map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot,
	base domain.AssetSymbol,
) (domain.Balance, domain.Balance, bool) {
	zero, _ := domain.ParseBalance("0")
	totalBase, _ := domain.ParseBalance("0")
	totalUSDT, _ := domain.ParseBalance("0")
	for _, exchange := range []string{"binance", "bybit"} {
		balances := current[exchange]
		if len(balances) != 3 {
			return domain.Balance{}, domain.Balance{}, false
		}
		for _, balance := range balances {
			if balance.Reserved.Compare(zero) != 0 || balance.Revision < 2 {
				return domain.Balance{}, domain.Balance{}, false
			}
		}
		var err error
		totalBase, err = totalBase.Add(balances[base].Available)
		if err == nil {
			totalUSDT, err = totalUSDT.Add(balances[domain.AssetSymbol("USDT")].Available)
		}
		if err != nil {
			return domain.Balance{}, domain.Balance{}, false
		}
	}
	return totalBase, totalUSDT, true
}

func ownerConsoleStringBalanceMapsEqual(left, right map[string]domain.Balance) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		other, exists := right[key]
		if !exists || value.Compare(other) != 0 {
			return false
		}
	}
	return true
}

func insertOwnerConsoleCrossExchangeExecutionEvidence(
	ctx context.Context,
	tx pgx.Tx,
	claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence,
	result backtest.EventResult,
	projection *crossarb.RecordedProjection,
	current map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot,
) (map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	if projection == nil || projection.Candidate.ID == "" || len(projection.VenueBalances) != 2 {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_projection_invalid")
	}
	projectedPayload, err := json.Marshal(projection.VenueBalances)
	if err != nil {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_projection_invalid")
	}
	var reduction ownerConsoleCrossExchangeReductionEvidence
	if json.Unmarshal(result.Balances, &reduction) != nil ||
		insertOwnerConsoleTriangularJournal(ctx, tx, claim, evidence, reduction.Transactions) != nil {
		return nil, fmt.Errorf("owner_console_shadow_cross_exchange_journal_failed")
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
	return projectOwnerConsoleCrossExchangeBalances(ctx, tx, claim, evidence, projection, current)
}

func projectOwnerConsoleCrossExchangeBalances(ctx context.Context, tx pgx.Tx, claim PublicShadowClaim,
	evidence publicShadowDecisionEvidence, projection *crossarb.RecordedProjection,
	current map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot,
) (map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, error) {
	updated := make(map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, 2)
	for _, exchange := range []string{"binance", "bybit"} {
		accountID := claim.VenueAccountIDs[exchange]
		updated[exchange] = make(map[domain.AssetSymbol]accounting.BalanceSnapshot, 3)
		for asset, before := range current[exchange] {
			after, exists := projection.VenueBalances[exchange][asset]
			if !exists {
				return nil, fmt.Errorf("owner_console_shadow_cross_exchange_projection_invalid")
			}
			tag, updateErr := tx.Exec(ctx, `UPDATE virtual_balances SET available=$1,reserved=0,
            revision=revision+1,updated_at=$2 WHERE account_id=$3 AND asset_symbol=$4 AND revision=$5
            AND available=$6 AND reserved=0`, after.String(), evidence.now, accountID, asset,
				before.Revision, before.Available.String())
			if updateErr != nil || tag.RowsAffected() != 1 {
				return nil, fmt.Errorf("owner_console_shadow_cross_exchange_balance_projection_failed")
			}
			updated[exchange][asset] = accounting.BalanceSnapshot{Available: after,
				Reserved: before.Reserved, Revision: before.Revision + 1}
		}
	}
	return updated, nil
}
