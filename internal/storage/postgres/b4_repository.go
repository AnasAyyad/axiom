package postgres

import (
	"context"
	"fmt"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// B4CandidateWrite is one complete three-leg immutable candidate record.
type B4CandidateWrite struct {
	Candidate generated.InsertTriangularCandidateParams
	Legs      []generated.InsertTriangularCandidateLegParams
}

// B4OutcomeWrite atomically records one terminal simulation, opportunity
// lifetime, and every already-balanced journal linkage.
type B4OutcomeWrite struct {
	Simulation generated.InsertTriangularSimulationOutcomeParams
	Lifetime   generated.InsertTriangularOpportunityLifetimeParams
	Journals   []generated.InsertTriangularJournalLinkParams
}

// B4Repository owns durable triangular decisions and atomic claim functions.
type B4Repository struct{ pool *pgxpool.Pool }

// NewB4Repository constructs the B4 persistence boundary.
func NewB4Repository(pool *pgxpool.Pool) (*B4Repository, error) {
	if pool == nil {
		return nil, fmt.Errorf("b4_repository_pool_missing")
	}
	return &B4Repository{pool: pool}, nil
}

// RecordCandidate commits the parent plus exactly three chained legs or none.
func (repository *B4Repository) RecordCandidate(ctx context.Context, write B4CandidateWrite) error {
	if !validB4CandidateWrite(write) {
		return fmt.Errorf("b4_candidate_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("b4_candidate_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.InsertTriangularCandidate(ctx, write.Candidate); err != nil {
		return fmt.Errorf("b4_candidate_insert_failed")
	}
	for _, leg := range write.Legs {
		if _, err = queries.InsertTriangularCandidateLeg(ctx, leg); err != nil {
			return fmt.Errorf("b4_candidate_leg_insert_failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("b4_candidate_commit_failed")
	}
	return nil
}

// RegisterClaimResource initializes or safely refreshes unheld exact capacity.
func (repository *B4Repository) RegisterClaimResource(
	ctx context.Context,
	write generated.RegisterB4ClaimResourceParams,
) error {
	if write.PID == "" || write.PAccountID == "" || write.PExchangeID == "" ||
		write.PResourceKind == "" || write.PResourceKey == "" || write.PAvailable == nil ||
		!write.PRecordedAt.Valid {
		return fmt.Errorf("b4_claim_resource_invalid")
	}
	if err := generated.New(repository.pool).RegisterB4ClaimResource(ctx, write); err != nil {
		return fmt.Errorf("b4_claim_resource_failed: %w", err)
	}
	return nil
}

// Claim atomically acquires every sorted multi-resource requirement.
func (repository *B4Repository) Claim(
	ctx context.Context,
	write generated.ClaimB4ResourcesParams,
) error {
	if write.GroupID == "" || write.DecisionID == "" || write.AccountID == "" ||
		write.FencingToken <= 0 || write.CorrelationID == "" || write.CausationID == "" ||
		len(write.ResourceIds) == 0 || len(write.ResourceIds) != len(write.Quantities) ||
		!write.RecordedAt.Valid {
		return fmt.Errorf("b4_claim_invalid")
	}
	if err := generated.New(repository.pool).ClaimB4Resources(ctx, write); err != nil {
		return fmt.Errorf("b4_claim_failed: %w", err)
	}
	return nil
}

// Settle applies exact consumption and optional final release atomically.
func (repository *B4Repository) Settle(
	ctx context.Context,
	write generated.SettleB4ClaimGroupParams,
) error {
	if write.GroupID == "" || write.ExpectedRevision <= 0 || write.FencingToken <= 0 ||
		len(write.ResourceIds) == 0 || len(write.ResourceIds) != len(write.Consumed) ||
		!write.RecordedAt.Valid {
		return fmt.Errorf("b4_claim_settlement_invalid")
	}
	if err := generated.New(repository.pool).SettleB4ClaimGroup(ctx, write); err != nil {
		return fmt.Errorf("b4_claim_settlement_failed: %w", err)
	}
	return nil
}

// Close releases, expires, or quarantines one active claim group.
func (repository *B4Repository) Close(
	ctx context.Context,
	write generated.CloseB4ClaimGroupParams,
) error {
	if write.PGroupID == "" || write.PExpectedRevision <= 0 || write.PFencingToken <= 0 ||
		(write.PNextState != "released" && write.PNextState != "expired" &&
			write.PNextState != "quarantined") ||
		!write.PRecordedAt.Valid {
		return fmt.Errorf("b4_claim_close_invalid")
	}
	if err := generated.New(repository.pool).CloseB4ClaimGroup(ctx, write); err != nil {
		return fmt.Errorf("b4_claim_close_failed: %w", err)
	}
	return nil
}

// RecordOutcome commits terminal execution, lifetime, and journal evidence.
func (repository *B4Repository) RecordOutcome(ctx context.Context, write B4OutcomeWrite) error {
	if !validB4OutcomeWrite(write) {
		return fmt.Errorf("b4_outcome_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("b4_outcome_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.InsertTriangularSimulationOutcome(ctx, write.Simulation); err != nil {
		return fmt.Errorf("b4_simulation_insert_failed")
	}
	if _, err = queries.InsertTriangularOpportunityLifetime(ctx, write.Lifetime); err != nil {
		return fmt.Errorf("b4_lifetime_insert_failed")
	}
	for _, journal := range write.Journals {
		if _, err = queries.InsertTriangularJournalLink(ctx, journal); err != nil {
			return fmt.Errorf("b4_journal_link_insert_failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("b4_outcome_commit_failed")
	}
	return nil
}

// LoadCandidate returns immutable parent and canonical leg order.
func (repository *B4Repository) LoadCandidate(
	ctx context.Context,
	decisionID string,
) (generated.TriangularCandidate, []generated.TriangularCandidateLeg, error) {
	if decisionID == "" {
		return generated.TriangularCandidate{}, nil, fmt.Errorf("b4_candidate_invalid")
	}
	queries := generated.New(repository.pool)
	candidate, err := queries.GetTriangularCandidate(ctx, decisionID)
	if err != nil {
		return generated.TriangularCandidate{}, nil, fmt.Errorf("b4_candidate_load_failed")
	}
	rows, err := queries.ListTriangularCandidateLegs(ctx, decisionID)
	if err != nil || len(rows) != 3 {
		return generated.TriangularCandidate{}, nil, fmt.Errorf("b4_candidate_load_failed")
	}
	legs := make([]generated.TriangularCandidateLeg, len(rows))
	for index, row := range rows {
		legs[index] = *row
	}
	return *candidate, legs, nil
}

func validB4CandidateWrite(write B4CandidateWrite) bool {
	candidate := write.Candidate
	if candidate.DecisionID == "" || candidate.StrategyVersionID == "" ||
		candidate.ConfigurationID == "" || candidate.PortfolioOwnershipAccountID == "" ||
		candidate.ExchangeID == "" || candidate.CorrelationID == "" || candidate.CausationID == "" ||
		candidate.ModelVersionID == "" || candidate.RiskEvaluationID == "" ||
		candidate.ClaimModelVersionID == "" || candidate.FeeModelVersionID == "" ||
		candidate.LatencyModelVersionID == "" || candidate.RecoveryModelVersionID == "" ||
		(candidate.Cycle != "USDT-BTC-ETH-USDT" && candidate.Cycle != "USDT-ETH-BTC-USDT") ||
		candidate.StartQuantity == nil || candidate.ExpectedFinalQuantity == nil ||
		candidate.WorstFinalQuantity == nil || candidate.ExpectedNet == nil ||
		candidate.WorstNet == nil || candidate.ExpectedEdge == nil ||
		candidate.WorstEdge == nil || candidate.AdditionalSafetyMargin == nil ||
		candidate.FirstDetectedOffsetNanos <= 0 ||
		candidate.DecisionOffsetNanos < candidate.FirstDetectedOffsetNanos ||
		candidate.ExpiresOffsetNanos-candidate.FirstDetectedOffsetNanos != 250_000_000 ||
		!candidate.RecordedAt.Valid || !validHashParameter(candidate.ConfigurationHash) ||
		!validHashParameter(candidate.InstrumentMetadataSetHash) ||
		!validHashParameter(candidate.CanonicalHash) || len(write.Legs) != 3 {
		return false
	}
	for index, leg := range write.Legs {
		if leg.DecisionID != candidate.DecisionID || leg.LegIndex != int32(index) ||
			leg.InstrumentID == "" || leg.InstrumentMetadataID == "" ||
			leg.SourceAsset == "" || leg.TargetAsset == "" ||
			(leg.Side != "buy" && leg.Side != "sell") ||
			leg.InputQuantity == nil || leg.TradeQuantity == nil ||
			leg.GrossOutput == nil || leg.NetOutput == nil ||
			leg.SourceDust == nil || leg.FeeAsset == "" || leg.FeeQuantity == nil ||
			leg.FeeQuoteEquivalent == nil || leg.Notional == nil || leg.Vwap == nil ||
			leg.SpreadDepthCost == nil ||
			leg.BookVersion <= 0 || leg.ConnectionGeneration <= 0 {
			return false
		}
	}
	return true
}

func validB4OutcomeWrite(write B4OutcomeWrite) bool {
	simulation := write.Simulation
	lifetime := write.Lifetime
	if simulation.DecisionID == "" || simulation.PlanID == "" ||
		simulation.LatencyModelVersionID == "" || simulation.CorrelationID == "" ||
		simulation.CausationID == "" || !simulation.RecordedAt.Valid ||
		!validHashParameter(simulation.CanonicalHash) ||
		lifetime.DecisionID != simulation.DecisionID ||
		lifetime.FirstDetectionNanos <= 0 ||
		lifetime.LastProfitableNanos < lifetime.FirstDetectionNanos ||
		lifetime.TotalLifetimeNanos != lifetime.LastProfitableNanos-lifetime.FirstDetectionNanos ||
		lifetime.MetricWindow != 1000 || !lifetime.RecordedAt.Valid {
		return false
	}
	for _, journal := range write.Journals {
		if journal.DecisionID != simulation.DecisionID ||
			journal.TransactionID == "" || journal.Category == "" {
			return false
		}
	}
	return true
}

func validHashParameter(value any) bool {
	switch hash := value.(type) {
	case string:
		return len(hash) == 64
	case []byte:
		return len(hash) == 64
	default:
		return false
	}
}
