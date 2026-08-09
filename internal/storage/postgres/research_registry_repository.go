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

// ResearchRegistryRegistrationWrite atomically registers the immutable strategy and experiment generation.
type ResearchRegistryRegistrationWrite struct {
	Definition generated.InsertResearchRegistryStrategyDefinitionParams
	Version    generated.InsertResearchRegistryStrategyVersionParams
	Parameters []generated.InsertResearchRegistryStrategyParameterParams
	Experiment generated.InsertResearchRegistryExperimentRegistrationParams
	Generation generated.InsertResearchGenerationParams
}

// ResearchRegistryRepository owns Trend research persistence boundaries.
type ResearchRegistryRepository struct{ pool *pgxpool.Pool }

// NewResearchRegistryRepository constructs the research registry persistence boundary.
func NewResearchRegistryRepository(pool *pgxpool.Pool) (*ResearchRegistryRepository, error) {
	if pool == nil {
		return nil, fmt.Errorf("research_registry_repository_pool_missing")
	}
	return &ResearchRegistryRepository{pool: pool}, nil
}

// Register atomically persists one strategy version, complete parameters, experiment, and generation.
func (repository *ResearchRegistryRepository) Register(ctx context.Context, write ResearchRegistryRegistrationWrite) error {
	return registerResearch(ctx, repository.pool, write, 16, "research_registry")
}

func registerResearch(ctx context.Context, pool *pgxpool.Pool, write ResearchRegistryRegistrationWrite,
	expectedParameters int, prefix string) error {
	if !validResearchRegistration(write, expectedParameters) {
		return fmt.Errorf("%s_registration_invalid", prefix)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("%s_registration_begin_failed", prefix)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.InsertResearchRegistryStrategyDefinition(ctx, write.Definition); err != nil {
		return fmt.Errorf("%s_strategy_definition_failed", prefix)
	}
	if _, err = queries.InsertResearchRegistryStrategyVersion(ctx, write.Version); err != nil {
		return fmt.Errorf("%s_strategy_version_failed", prefix)
	}
	for _, parameter := range write.Parameters {
		if _, err = queries.InsertResearchRegistryStrategyParameter(ctx, parameter); err != nil {
			return fmt.Errorf("%s_strategy_parameter_failed", prefix)
		}
	}
	if _, err = queries.InsertResearchRegistryExperimentRegistration(ctx, write.Experiment); err != nil {
		return fmt.Errorf("%s_experiment_registration_failed", prefix)
	}
	if _, err = queries.InsertResearchGeneration(ctx, write.Generation); err != nil {
		return fmt.Errorf("%s_research_generation_failed", prefix)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s_registration_commit_failed", prefix)
	}
	return nil
}

// ConsumeFinalTest records the one permitted final-test use for a generation.
func (repository *ResearchRegistryRepository) ConsumeFinalTest(ctx context.Context, write generated.ConsumeFinalTestGenerationParams) error {
	if write.ResearchGenerationID == "" || write.ConsumedByRunID == "" || write.ConsumptionHash == nil || !write.ConsumedAt.Valid {
		return fmt.Errorf("research_registry_final_test_consumption_invalid")
	}
	if _, err := generated.New(repository.pool).ConsumeFinalTestGeneration(ctx, write); err != nil {
		return fmt.Errorf("research_registry_final_test_already_consumed")
	}
	return nil
}

// RecordDecision appends exact canonical Trend explanation evidence.
func (repository *ResearchRegistryRepository) RecordDecision(ctx context.Context, write generated.InsertTrendDecisionParams) error {
	if write.DecisionID == "" || write.ExplanationHash == nil || len(write.CanonicalExplanation) == 0 ||
		!matchesResearchRegistryHash(write.ExplanationHash, write.CanonicalExplanation) || write.CandleViewRevision <= 0 ||
		write.MarketViewRevision <= 0 || !write.RecordedAt.Valid {
		return fmt.Errorf("research_registry_trend_decision_invalid")
	}
	if _, err := generated.New(repository.pool).InsertTrendDecision(ctx, write); err != nil {
		return fmt.Errorf("research_registry_trend_decision_failed")
	}
	return nil
}

// RecordReport appends one immutable research report manifest.
func (repository *ResearchRegistryRepository) RecordReport(ctx context.Context, write generated.InsertResearchReportParams) error {
	if write.ID == "" || write.ResearchGenerationID == "" || write.ManifestHash == nil || write.ArtifactHash == nil ||
		len(write.CanonicalManifest) == 0 || !matchesResearchRegistryHash(write.ManifestHash, write.CanonicalManifest) ||
		!validResearchRegistryHash(write.ArtifactHash) || len(write.RunReferences) == 0 || !write.CreatedAt.Valid ||
		write.DisclaimerPolicy != "no_production_profitability_claim" {
		return fmt.Errorf("research_registry_research_report_invalid")
	}
	if _, err := generated.New(repository.pool).InsertResearchReport(ctx, write); err != nil {
		return fmt.Errorf("research_registry_research_report_failed")
	}
	return nil
}

func validResearchRegistration(write ResearchRegistryRegistrationWrite, expectedParameters int) bool {
	if write.Definition.ID == "" || write.Version.ID == "" || write.Version.StrategyID != write.Definition.ID ||
		write.Version.ManifestHash == nil || len(write.Version.CanonicalManifest) == 0 || len(write.Parameters) != expectedParameters ||
		!matchesResearchRegistryHash(write.Version.ManifestHash, write.Version.CanonicalManifest) ||
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

func matchesResearchRegistryHash(value any, payload []byte) bool {
	digest := sha256.Sum256(payload)
	return hashText(value) == hex.EncodeToString(digest[:])
}

func validResearchRegistryHash(value any) bool {
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
