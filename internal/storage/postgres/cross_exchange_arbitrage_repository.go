package postgres

import (
	"context"
	"fmt"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CrossExchangeArbitrageCandidateWrite is one complete coherent two-leg decision plus exact
// before/after venue ownership snapshots.
type CrossExchangeArbitrageCandidateWrite struct {
	Candidate   generated.InsertCrossExchangeCandidateParams
	Members     []generated.InsertCrossExchangeCandidateMemberParams
	Legs        []generated.InsertCrossExchangeCandidateLegParams
	Inventories []generated.InsertCrossExchangeInventorySnapshotParams
}

// CrossExchangeArbitrageOutcomeWrite is one terminal concurrent saga, advisory restoration need,
// and all eleven independently balanced journal links.
type CrossExchangeArbitrageOutcomeWrite struct {
	Simulation  generated.InsertCrossExchangeSimulationOutcomeParams
	Legs        []generated.InsertCrossExchangeSimulationLegParams
	Rebalancing generated.InsertCrossExchangeRebalancingNeedParams
	Journals    []generated.InsertCrossExchangeJournalLinkParams
}

// CrossExchangeArbitrageRepository owns cross-exchange arbitrage durable evidence and atomic claim functions.
type CrossExchangeArbitrageRepository struct{ pool *pgxpool.Pool }

// NewCrossExchangeArbitrageRepository constructs the cross-exchange arbitrage persistence boundary.
func NewCrossExchangeArbitrageRepository(pool *pgxpool.Pool) (*CrossExchangeArbitrageRepository, error) {
	if pool == nil {
		return nil, fmt.Errorf("cross_exchange_arbitrage_repository_pool_missing")
	}
	return &CrossExchangeArbitrageRepository{pool: pool}, nil
}

// RecordCandidate commits the parent, exact coherent market data members, two legs, and two
// inventory snapshots in one deferred-constraint transaction.
func (repository *CrossExchangeArbitrageRepository) RecordCandidate(ctx context.Context, write CrossExchangeArbitrageCandidateWrite) error {
	if !validCrossExchangeArbitrageCandidateWrite(write) {
		return fmt.Errorf("cross_exchange_arbitrage_candidate_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_candidate_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.InsertCrossExchangeCandidate(ctx, write.Candidate); err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_candidate_insert_failed")
	}
	for _, member := range write.Members {
		if _, err = queries.InsertCrossExchangeCandidateMember(ctx, member); err != nil {
			return fmt.Errorf("cross_exchange_arbitrage_candidate_member_insert_failed")
		}
	}
	for _, leg := range write.Legs {
		if _, err = queries.InsertCrossExchangeCandidateLeg(ctx, leg); err != nil {
			return fmt.Errorf("cross_exchange_arbitrage_candidate_leg_insert_failed")
		}
	}
	for _, inventory := range write.Inventories {
		if _, err = queries.InsertCrossExchangeInventorySnapshot(ctx, inventory); err != nil {
			return fmt.Errorf("cross_exchange_arbitrage_inventory_insert_failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_candidate_commit_failed")
	}
	return nil
}

// RegisterClaimResource initializes or safely refreshes unheld capacity.
func (repository *CrossExchangeArbitrageRepository) RegisterClaimResource(
	ctx context.Context,
	write generated.RegisterCrossExchangeArbitrageClaimResourceParams,
) error {
	if write.PID == "" || write.PAccountID == "" || write.PExchangeID == "" ||
		write.PResourceKind == "" || write.PResourceKey == "" || write.PAvailable == nil ||
		!write.PRecordedAt.Valid {
		return fmt.Errorf("cross_exchange_arbitrage_claim_resource_invalid")
	}
	if err := generated.New(repository.pool).RegisterCrossExchangeArbitrageClaimResource(ctx, write); err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_claim_resource_failed: %w", err)
	}
	return nil
}

// Claim atomically acquires the exact seven-resource cross-exchange arbitrage shape.
func (repository *CrossExchangeArbitrageRepository) Claim(
	ctx context.Context,
	write generated.ClaimCrossExchangeArbitrageResourcesParams,
) error {
	if write.GroupID == "" || write.DecisionID == "" || write.FencingToken <= 0 ||
		write.CorrelationID == "" || write.CausationID == "" ||
		len(write.ResourceIds) != 7 || len(write.ResourceIds) != len(write.Quantities) ||
		!write.RecordedAt.Valid {
		return fmt.Errorf("cross_exchange_arbitrage_claim_invalid")
	}
	if err := generated.New(repository.pool).ClaimCrossExchangeArbitrageResources(ctx, write); err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_claim_failed: %w", err)
	}
	return nil
}

// Settle consumes exact claimed amounts under revision and fence.
func (repository *CrossExchangeArbitrageRepository) Settle(
	ctx context.Context,
	write generated.SettleCrossExchangeArbitrageClaimGroupParams,
) error {
	if write.GroupID == "" || write.ExpectedRevision <= 0 || write.FencingToken <= 0 ||
		len(write.ResourceIds) == 0 || len(write.ResourceIds) != len(write.Consumed) ||
		!write.RecordedAt.Valid {
		return fmt.Errorf("cross_exchange_arbitrage_claim_settlement_invalid")
	}
	if err := generated.New(repository.pool).SettleCrossExchangeArbitrageClaimGroup(ctx, write); err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_claim_settlement_failed: %w", err)
	}
	return nil
}

// Close releases, expires, or quarantines one active group.
func (repository *CrossExchangeArbitrageRepository) Close(
	ctx context.Context,
	write generated.CloseCrossExchangeArbitrageClaimGroupParams,
) error {
	if write.PGroupID == "" || write.PExpectedRevision <= 0 || write.PFencingToken <= 0 ||
		(write.PNextState != "released" && write.PNextState != "expired" &&
			write.PNextState != "quarantined") ||
		!write.PRecordedAt.Valid {
		return fmt.Errorf("cross_exchange_arbitrage_claim_close_invalid")
	}
	if err := generated.New(repository.pool).CloseCrossExchangeArbitrageClaimGroup(ctx, write); err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_claim_close_failed: %w", err)
	}
	return nil
}

// RecordOutcome commits terminal saga, two leg facts, advisory need, and
// complete categorized journal linkage.
func (repository *CrossExchangeArbitrageRepository) RecordOutcome(ctx context.Context, write CrossExchangeArbitrageOutcomeWrite) error {
	if !validCrossExchangeArbitrageOutcomeWrite(write) {
		return fmt.Errorf("cross_exchange_arbitrage_outcome_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_outcome_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.InsertCrossExchangeSimulationOutcome(ctx, write.Simulation); err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_simulation_insert_failed")
	}
	for _, leg := range write.Legs {
		if _, err = queries.InsertCrossExchangeSimulationLeg(ctx, leg); err != nil {
			return fmt.Errorf("cross_exchange_arbitrage_simulation_leg_insert_failed")
		}
	}
	if _, err = queries.InsertCrossExchangeRebalancingNeed(ctx, write.Rebalancing); err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_rebalancing_insert_failed")
	}
	for _, journal := range write.Journals {
		if _, err = queries.InsertCrossExchangeJournalLink(ctx, journal); err != nil {
			return fmt.Errorf("cross_exchange_arbitrage_journal_link_insert_failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("cross_exchange_arbitrage_outcome_commit_failed")
	}
	return nil
}

// LoadCandidate returns a complete immutable candidate aggregate.
func (repository *CrossExchangeArbitrageRepository) LoadCandidate(
	ctx context.Context,
	decisionID string,
) (
	generated.CrossExchangeCandidate,
	[]generated.CrossExchangeCandidateMember,
	[]generated.CrossExchangeCandidateLeg,
	[]generated.CrossExchangeInventorySnapshot,
	error,
) {
	if decisionID == "" {
		return generated.CrossExchangeCandidate{}, nil, nil, nil, fmt.Errorf("cross_exchange_arbitrage_candidate_invalid")
	}
	queries := generated.New(repository.pool)
	candidate, err := queries.GetCrossExchangeCandidate(ctx, decisionID)
	if err != nil {
		return generated.CrossExchangeCandidate{}, nil, nil, nil, fmt.Errorf("cross_exchange_arbitrage_candidate_load_failed")
	}
	memberRows, memberErr := queries.ListCrossExchangeCandidateMembers(ctx, decisionID)
	legRows, legErr := queries.ListCrossExchangeCandidateLegs(ctx, decisionID)
	inventoryRows, inventoryErr := queries.ListCrossExchangeInventorySnapshots(ctx, decisionID)
	if memberErr != nil || legErr != nil || inventoryErr != nil ||
		len(memberRows) != 2 || len(legRows) != 2 || len(inventoryRows) != 2 {
		return generated.CrossExchangeCandidate{}, nil, nil, nil, fmt.Errorf("cross_exchange_arbitrage_candidate_load_failed")
	}
	members := dereferenceMembers(memberRows)
	legs := dereferenceCrossExchangeArbitrageLegs(legRows)
	inventories := dereferenceInventories(inventoryRows)
	return *candidate, members, legs, inventories, nil
}

func validCrossExchangeArbitrageCandidateWrite(write CrossExchangeArbitrageCandidateWrite) bool {
	candidate := write.Candidate
	if candidate.DecisionID == "" || candidate.StrategyVersionID == "" ||
		candidate.ConfigurationID == "" || candidate.InstrumentID == "" ||
		candidate.BuyExchangeID == candidate.SellExchangeID ||
		candidate.BuyOwnershipAccountID == candidate.SellOwnershipAccountID ||
		candidate.QuoteBudget == nil || candidate.BaseQuantity == nil ||
		candidate.GrossSpread == nil || candidate.BuyFee == nil || candidate.SellFee == nil ||
		candidate.SpreadDepthCost == nil || candidate.LatencyDeterioration == nil ||
		candidate.RecoveryAllowance == nil || candidate.ExpectedExecutionPnl == nil ||
		candidate.MaximumOneLegLoss == nil || candidate.MarginalInventoryReplacement == nil ||
		candidate.NaturalReversalCost == nil || candidate.AdvisoryRebalancingCost == nil ||
		candidate.ExchangeConcentrationPenalty == nil ||
		candidate.UsdtVenueConcentrationPenalty == nil ||
		candidate.ExpectedClosedCycleProfit == nil || candidate.WorstClosedCycleProfit == nil ||
		candidate.FirstDetectedOffsetNanos <= 0 ||
		candidate.ExpiresOffsetNanos-candidate.FirstDetectedOffsetNanos != 250_000_000 ||
		!validCrossExchangeArbitrageCandidateReferences(candidate) || len(write.Members) != 2 ||
		len(write.Legs) != 2 || len(write.Inventories) != 2 {
		return false
	}
	for index := range 2 {
		if !validCrossExchangeArbitrageMember(write.Members[index], candidate, index) ||
			!validCrossExchangeArbitrageLeg(write.Legs[index], candidate, index) ||
			!validCrossExchangeArbitrageInventory(write.Inventories[index], candidate, index) {
			return false
		}
	}
	return true
}

func validCrossExchangeArbitrageCandidateReferences(candidate generated.InsertCrossExchangeCandidateParams) bool {
	return candidate.RiskEvaluationID != "" && candidate.PricingModelVersionID != "" &&
		candidate.ClaimModelVersionID != "" && candidate.FeeModelVersionID != "" &&
		candidate.LatencyModelVersionID != "" && candidate.RecoveryModelVersionID != "" &&
		candidate.InventoryShadowModelVersionID != "" && candidate.ConcentrationModelVersionID != "" &&
		candidate.CorrelationID != "" && candidate.CausationID != "" && candidate.RecordedAt.Valid &&
		validHashParameter(candidate.CoherentViewID) &&
		validHashParameter(candidate.ConfigurationHash) &&
		validHashParameter(candidate.InstrumentMetadataSetHash) &&
		validHashParameter(candidate.CanonicalHash) &&
		(candidate.Direction == "buy_binance_sell_bybit" ||
			candidate.Direction == "buy_bybit_sell_binance")
}

func validCrossExchangeArbitrageMember(
	member generated.InsertCrossExchangeCandidateMemberParams,
	candidate generated.InsertCrossExchangeCandidateParams,
	index int,
) bool {
	return member.DecisionID == candidate.DecisionID && member.MemberOrdinal == int32(index) &&
		member.ExchangeID != "" && member.InstrumentID == candidate.InstrumentID &&
		member.BookVersion > 0 && member.ConnectionGeneration > 0 &&
		member.ReceiveMonotonicNanos > 0 && member.ReceiveUtc.Valid &&
		member.IngestOrdinal > 0 && member.ClockUncertaintyNanos >= 0 &&
		member.ClockIntervalStart.Valid && member.ClockIntervalEnd.Valid &&
		member.CollectorInstance != "" && member.CollectorRegion != "" &&
		validHashParameter(member.CoherentViewID) && validHashParameter(member.StateHash)
}

func validCrossExchangeArbitrageLeg(
	leg generated.InsertCrossExchangeCandidateLegParams,
	candidate generated.InsertCrossExchangeCandidateParams,
	index int,
) bool {
	wantExchange, wantAccount, wantSide := candidate.BuyExchangeID,
		candidate.BuyOwnershipAccountID, "buy"
	if index == 1 {
		wantExchange, wantAccount, wantSide = candidate.SellExchangeID,
			candidate.SellOwnershipAccountID, "sell"
	}
	return leg.DecisionID == candidate.DecisionID && leg.LegIndex == int32(index) &&
		leg.ExchangeID == wantExchange && leg.OwnershipAccountID == wantAccount &&
		leg.InstrumentID == candidate.InstrumentID && leg.InstrumentMetadataID != "" &&
		leg.Side == wantSide && leg.InputQuantity != nil && leg.TradeQuantity != nil &&
		leg.GrossOutput != nil && leg.NetOutput != nil && leg.SourceDust != nil &&
		leg.FeeAsset != "" && leg.FeeQuantity != nil && leg.FeeQuoteEquivalent != nil &&
		leg.Notional != nil && leg.Vwap != nil && leg.SpreadDepthCost != nil &&
		leg.BookVersion > 0 && leg.ConnectionGeneration > 0
}

func validCrossExchangeArbitrageInventory(
	inventory generated.InsertCrossExchangeInventorySnapshotParams,
	candidate generated.InsertCrossExchangeCandidateParams,
	index int,
) bool {
	wantRole, wantExchange, wantAccount := "buy_venue",
		candidate.BuyExchangeID, candidate.BuyOwnershipAccountID
	if index == 1 {
		wantRole, wantExchange, wantAccount = "sell_venue",
			candidate.SellExchangeID, candidate.SellOwnershipAccountID
	}
	return inventory.DecisionID == candidate.DecisionID &&
		inventory.SnapshotRole == wantRole && inventory.ExchangeID == wantExchange &&
		inventory.OwnershipAccountID == wantAccount && inventory.BaseAsset != "" &&
		inventory.OwnerLabel != "" && inventory.OwnershipRevision > 0 &&
		inventory.BaseBefore != nil && inventory.BaseAfter != nil &&
		inventory.TotalEligibleBase != nil && inventory.BaseShareBefore != nil &&
		inventory.UsdtBefore != nil && inventory.UsdtAfter != nil &&
		inventory.TotalEligibleUsdt != nil && inventory.UsdtShareBefore != nil &&
		(inventory.BandState == "paused_depleted" || inventory.BandState == "reduced" ||
			inventory.BandState == "normal" || inventory.BandState == "preferred_natural_reverse")
}

func validCrossExchangeArbitrageOutcomeWrite(write CrossExchangeArbitrageOutcomeWrite) bool {
	simulation := write.Simulation
	if simulation.DecisionID == "" || simulation.PlanID == "" || simulation.Outcome == "" ||
		simulation.ActualUsdtNet == nil || simulation.FinalDisposition == "" ||
		simulation.RecoveryLoss == nil || simulation.LatencyModelVersionID == "" ||
		!simulation.RecordedAt.Valid || !validHashParameter(simulation.CanonicalHash) ||
		simulation.CorrelationID == "" || simulation.CausationID == "" ||
		len(write.Legs) != 2 || len(write.Journals) != 11 ||
		write.Rebalancing.DecisionID != simulation.DecisionID ||
		!write.Rebalancing.AdvisoryOnly || !write.Rebalancing.RecordedAt.Valid {
		return false
	}
	categories := make(map[string]struct{}, 11)
	for index, leg := range write.Legs {
		if leg.DecisionID != simulation.DecisionID || leg.LegIndex != int32(index) ||
			leg.ExchangeID == "" || leg.ArrivalOffsetNanos <= 0 ||
			leg.InitialState == "" || leg.VerifiedState == "" || leg.FinalState == "" ||
			leg.InputQuantity == nil || leg.FilledQuantity == nil ||
			leg.VerificationCount < 0 || leg.RetryCount < 0 {
			return false
		}
	}
	for _, journal := range write.Journals {
		if journal.DecisionID != simulation.DecisionID || journal.TransactionID == "" ||
			!validCrossExchangeArbitrageJournalCategory(journal.Category) {
			return false
		}
		categories[journal.Category] = struct{}{}
	}
	return len(categories) == 11
}

func validCrossExchangeArbitrageJournalCategory(category string) bool {
	switch category {
	case "execution_pnl", "btc_inventory_market_pnl", "eth_inventory_market_pnl",
		"stablecoin_valuation", "fees", "spread", "slippage", "latency", "recovery",
		"inventory_restoration", "combined_pnl":
		return true
	default:
		return false
	}
}

func dereferenceMembers(rows []*generated.CrossExchangeCandidateMember) []generated.CrossExchangeCandidateMember {
	result := make([]generated.CrossExchangeCandidateMember, len(rows))
	for index, row := range rows {
		result[index] = *row
	}
	return result
}

func dereferenceCrossExchangeArbitrageLegs(rows []*generated.CrossExchangeCandidateLeg) []generated.CrossExchangeCandidateLeg {
	result := make([]generated.CrossExchangeCandidateLeg, len(rows))
	for index, row := range rows {
		result[index] = *row
	}
	return result
}

func dereferenceInventories(rows []*generated.CrossExchangeInventorySnapshot) []generated.CrossExchangeInventorySnapshot {
	result := make([]generated.CrossExchangeInventorySnapshot, len(rows))
	for index, row := range rows {
		result[index] = *row
	}
	return result
}
