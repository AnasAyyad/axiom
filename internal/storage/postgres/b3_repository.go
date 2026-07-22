package postgres

import (
	"context"
	"fmt"

	"axiom/internal/config"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgxpool"
)

// B3RegistrationWrite reuses the strategy-neutral A10 research registration rows.
type B3RegistrationWrite A10RegistrationWrite

// B3Repository owns mean-reversion persistence and registered research use.
type B3Repository struct{ pool *pgxpool.Pool }

// NewB3Repository constructs the B3 persistence boundary.
func NewB3Repository(pool *pgxpool.Pool) (*B3Repository, error) {
	if pool == nil {
		return nil, fmt.Errorf("b3_repository_pool_missing")
	}
	return &B3Repository{pool: pool}, nil
}

// Register atomically persists one complete B3 strategy graph and generation.
func (repository *B3Repository) Register(ctx context.Context, write B3RegistrationWrite) error {
	return registerResearch(ctx, repository.pool, A10RegistrationWrite(write),
		config.MeanReversionParameterCount, "b3")
}

// ConsumeFinalTest records the one permitted final-window use for a generation.
func (repository *B3Repository) ConsumeFinalTest(ctx context.Context,
	write generated.ConsumeFinalTestGenerationParams) error {
	return (&A10Repository{pool: repository.pool}).ConsumeFinalTest(ctx, write)
}

// RecordDecision appends exact canonical dual-timeframe B3 explanation evidence.
func (repository *B3Repository) RecordDecision(ctx context.Context,
	write generated.InsertMeanReversionDecisionParams) error {
	if write.DecisionID == "" || write.StrategyVersionID == "" || write.ConfigurationID == "" ||
		write.ExplanationHash == nil || len(write.CanonicalExplanation) == 0 ||
		!matchesA10Hash(write.ExplanationHash, write.CanonicalExplanation) ||
		write.PrimaryCandleViewRevision <= 0 || write.HigherCandleViewRevision <= 0 ||
		write.MarketViewRevision <= 0 || write.AssetEligibilityVersion <= 0 ||
		write.PortfolioRevision <= 0 || write.PositionRevision <= 0 || write.RiskPolicyVersion <= 0 ||
		write.RiskPolicyID == "" ||
		!write.RecordedAt.Valid {
		return fmt.Errorf("b3_mean_reversion_decision_invalid")
	}
	if _, err := generated.New(repository.pool).InsertMeanReversionDecision(ctx, write); err != nil {
		return fmt.Errorf("b3_mean_reversion_decision_failed")
	}
	return nil
}

// LoadDecision returns the immutable stored B3 evidence for restart comparison.
func (repository *B3Repository) LoadDecision(ctx context.Context, decisionID string) (generated.MeanReversionDecision, error) {
	if decisionID == "" {
		return generated.MeanReversionDecision{}, fmt.Errorf("b3_mean_reversion_decision_invalid")
	}
	value, err := generated.New(repository.pool).GetMeanReversionDecision(ctx, decisionID)
	if err != nil {
		return generated.MeanReversionDecision{}, fmt.Errorf("b3_mean_reversion_decision_load_failed")
	}
	return *value, nil
}

// RecordReport persists a report already validated against the separate B3 contract.
func (repository *B3Repository) RecordReport(ctx context.Context,
	write generated.InsertResearchReportParams) error {
	return (&A10Repository{pool: repository.pool}).RecordReport(ctx, write)
}
