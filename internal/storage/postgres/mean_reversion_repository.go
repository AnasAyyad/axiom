package postgres

import (
	"context"
	"fmt"

	"axiom/internal/config"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MeanReversionRegistrationWrite reuses the strategy-neutral research registry research registration rows.
type MeanReversionRegistrationWrite ResearchRegistryRegistrationWrite

// MeanReversionRepository owns mean-reversion persistence and registered research use.
type MeanReversionRepository struct{ pool *pgxpool.Pool }

// NewMeanReversionRepository constructs the mean reversion persistence boundary.
func NewMeanReversionRepository(pool *pgxpool.Pool) (*MeanReversionRepository, error) {
	if pool == nil {
		return nil, fmt.Errorf("mean_reversion_repository_pool_missing")
	}
	return &MeanReversionRepository{pool: pool}, nil
}

// Register atomically persists one complete mean reversion strategy graph and generation.
func (repository *MeanReversionRepository) Register(ctx context.Context, write MeanReversionRegistrationWrite) error {
	return registerResearch(ctx, repository.pool, ResearchRegistryRegistrationWrite(write),
		config.MeanReversionParameterCount, "mean_reversion")
}

// ConsumeFinalTest records the one permitted final-window use for a generation.
func (repository *MeanReversionRepository) ConsumeFinalTest(ctx context.Context,
	write generated.ConsumeFinalTestGenerationParams) error {
	return (&ResearchRegistryRepository{pool: repository.pool}).ConsumeFinalTest(ctx, write)
}

// RecordDecision appends exact canonical dual-timeframe mean reversion explanation evidence.
func (repository *MeanReversionRepository) RecordDecision(ctx context.Context,
	write generated.InsertMeanReversionDecisionParams) error {
	if write.DecisionID == "" || write.StrategyVersionID == "" || write.ConfigurationID == "" ||
		write.ExplanationHash == nil || len(write.CanonicalExplanation) == 0 ||
		!matchesResearchRegistryHash(write.ExplanationHash, write.CanonicalExplanation) ||
		write.PrimaryCandleViewRevision <= 0 || write.HigherCandleViewRevision <= 0 ||
		write.MarketViewRevision <= 0 || write.AssetEligibilityVersion <= 0 ||
		write.PortfolioRevision <= 0 || write.PositionRevision <= 0 || write.RiskPolicyVersion <= 0 ||
		write.RiskPolicyID == "" ||
		!write.RecordedAt.Valid {
		return fmt.Errorf("mean_reversion_mean_reversion_decision_invalid")
	}
	if _, err := generated.New(repository.pool).InsertMeanReversionDecision(ctx, write); err != nil {
		return fmt.Errorf("mean_reversion_mean_reversion_decision_failed")
	}
	return nil
}

// LoadDecision returns the immutable stored mean reversion evidence for restart comparison.
func (repository *MeanReversionRepository) LoadDecision(ctx context.Context, decisionID string) (generated.MeanReversionDecision, error) {
	if decisionID == "" {
		return generated.MeanReversionDecision{}, fmt.Errorf("mean_reversion_mean_reversion_decision_invalid")
	}
	value, err := generated.New(repository.pool).GetMeanReversionDecision(ctx, decisionID)
	if err != nil {
		return generated.MeanReversionDecision{}, fmt.Errorf("mean_reversion_mean_reversion_decision_load_failed")
	}
	return *value, nil
}

// RecordReport persists a report already validated against the separate mean reversion contract.
func (repository *MeanReversionRepository) RecordReport(ctx context.Context,
	write generated.InsertResearchReportParams) error {
	return (&ResearchRegistryRepository{pool: repository.pool}).RecordReport(ctx, write)
}
