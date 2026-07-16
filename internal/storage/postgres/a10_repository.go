package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A10RegistrationWrite atomically registers the immutable strategy and experiment generation.
type A10RegistrationWrite struct {
	Definition generated.InsertA10StrategyDefinitionParams
	Version    generated.InsertA10StrategyVersionParams
	Parameters []generated.InsertA10StrategyParameterParams
	Experiment generated.InsertA10ExperimentRegistrationParams
	Generation generated.InsertResearchGenerationParams
}

// A10Repository owns Trend research persistence boundaries.
type A10Repository struct{ pool *pgxpool.Pool }

// NewA10Repository constructs the A10 persistence boundary.
func NewA10Repository(pool *pgxpool.Pool) (*A10Repository, error) {
	if pool == nil {
		return nil, fmt.Errorf("a10_repository_pool_missing")
	}
	return &A10Repository{pool: pool}, nil
}

// Register atomically persists one strategy version, complete parameters, experiment, and generation.
func (repository *A10Repository) Register(ctx context.Context, write A10RegistrationWrite) error {
	if !validA10Registration(write) {
		return fmt.Errorf("a10_registration_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("a10_registration_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.InsertA10StrategyDefinition(ctx, write.Definition); err != nil {
		return fmt.Errorf("a10_strategy_definition_failed")
	}
	if _, err = queries.InsertA10StrategyVersion(ctx, write.Version); err != nil {
		return fmt.Errorf("a10_strategy_version_failed")
	}
	for _, parameter := range write.Parameters {
		if _, err = queries.InsertA10StrategyParameter(ctx, parameter); err != nil {
			return fmt.Errorf("a10_strategy_parameter_failed")
		}
	}
	if _, err = queries.InsertA10ExperimentRegistration(ctx, write.Experiment); err != nil {
		return fmt.Errorf("a10_experiment_registration_failed")
	}
	if _, err = queries.InsertResearchGeneration(ctx, write.Generation); err != nil {
		return fmt.Errorf("a10_research_generation_failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("a10_registration_commit_failed")
	}
	return nil
}

// ConsumeFinalTest records the one permitted final-test use for a generation.
func (repository *A10Repository) ConsumeFinalTest(ctx context.Context, write generated.ConsumeFinalTestGenerationParams) error {
	if write.ResearchGenerationID == "" || write.ConsumedByRunID == "" || write.ConsumptionHash == nil || !write.ConsumedAt.Valid {
		return fmt.Errorf("a10_final_test_consumption_invalid")
	}
	if _, err := generated.New(repository.pool).ConsumeFinalTestGeneration(ctx, write); err != nil {
		return fmt.Errorf("a10_final_test_already_consumed")
	}
	return nil
}

// RecordDecision appends exact canonical Trend explanation evidence.
func (repository *A10Repository) RecordDecision(ctx context.Context, write generated.InsertTrendDecisionParams) error {
	if write.DecisionID == "" || write.ExplanationHash == nil || len(write.CanonicalExplanation) == 0 ||
		!matchesA10Hash(write.ExplanationHash, write.CanonicalExplanation) || write.CandleViewRevision <= 0 ||
		write.MarketViewRevision <= 0 || !write.RecordedAt.Valid {
		return fmt.Errorf("a10_trend_decision_invalid")
	}
	if _, err := generated.New(repository.pool).InsertTrendDecision(ctx, write); err != nil {
		return fmt.Errorf("a10_trend_decision_failed")
	}
	return nil
}

// RecordReport appends one immutable research report manifest.
func (repository *A10Repository) RecordReport(ctx context.Context, write generated.InsertResearchReportParams) error {
	if write.ID == "" || write.ResearchGenerationID == "" || write.ManifestHash == nil || write.ArtifactHash == nil ||
		len(write.CanonicalManifest) == 0 || !matchesA10Hash(write.ManifestHash, write.CanonicalManifest) ||
		!validA10Hash(write.ArtifactHash) || len(write.RunReferences) == 0 || !write.CreatedAt.Valid ||
		write.DisclaimerPolicy != "no_production_profitability_claim" {
		return fmt.Errorf("a10_research_report_invalid")
	}
	if _, err := generated.New(repository.pool).InsertResearchReport(ctx, write); err != nil {
		return fmt.Errorf("a10_research_report_failed")
	}
	return nil
}

func validA10Registration(write A10RegistrationWrite) bool {
	if write.Definition.ID == "" || write.Version.ID == "" || write.Version.StrategyID != write.Definition.ID ||
		write.Version.ManifestHash == nil || len(write.Version.CanonicalManifest) == 0 || len(write.Parameters) != 16 ||
		!matchesA10Hash(write.Version.ManifestHash, write.Version.CanonicalManifest) ||
		write.Experiment.ID == "" || write.Experiment.StrategyVersionID != write.Version.ID ||
		write.Generation.ID == "" || write.Generation.ExperimentID != write.Experiment.ID ||
		write.Generation.FinalWindowHash == nil || write.Generation.RegistrationHash == nil {
		return false
	}
	seen := make(map[string]struct{}, len(write.Parameters))
	for _, parameter := range write.Parameters {
		if parameter.StrategyVersionID != write.Version.ID || parameter.ParameterName == "" ||
			parameter.Description == nil || parameter.AlgorithmVersion == nil || parameter.ModelDependencies == nil {
			return false
		}
		if _, duplicate := seen[parameter.ParameterName]; duplicate {
			return false
		}
		seen[parameter.ParameterName] = struct{}{}
	}
	return true
}

func matchesA10Hash(value any, payload []byte) bool {
	digest := sha256.Sum256(payload)
	return hashText(value) == hex.EncodeToString(digest[:])
}

func validA10Hash(value any) bool {
	text := hashText(value)
	decoded, err := hex.DecodeString(text)
	return err == nil && len(decoded) == sha256.Size
}

func hashText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}
