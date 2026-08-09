package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/research"
	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ResearchPromotionRepository owns immutable preregistration, statistical suite,
// champion/challenger, and authenticated maturity-command persistence.
type ResearchPromotionRepository struct{ pool *pgxpool.Pool }

// NewResearchPromotionRepository constructs the research promotion persistence boundary.
func NewResearchPromotionRepository(pool *pgxpool.Pool) (*ResearchPromotionRepository, error) {
	if pool == nil {
		return nil, fmt.Errorf("research_promotion_repository_pool_missing")
	}
	return &ResearchPromotionRepository{pool: pool}, nil
}

// RecordPreregistration stores exact canonical registration evidence.
func (repository *ResearchPromotionRepository) RecordPreregistration(
	ctx context.Context,
	canonical []byte,
	expectedHash string,
) error {
	manifest, err := research.ValidateExperimentRegistrationCanonical(canonical, expectedHash)
	if err != nil {
		return fmt.Errorf("research_promotion_preregistration_invalid")
	}
	minimumProbability, err := researchPromotionNumeric(manifest.MinimumDeflatedSharpeProbability)
	if err != nil {
		return fmt.Errorf("research_promotion_preregistration_invalid")
	}
	write := generated.InsertResearchPromotionExperimentPreregistrationParams{
		ID: manifest.ID, ResearchGenerationID: manifest.ResearchGenerationID,
		StrategyVersionID: manifest.StrategyVersionID, RegistrationHash: expectedHash,
		CanonicalRegistration: canonical, MinimumSamples: int64(manifest.MinimumSamples),
		MinimumTrades:                    int64(manifest.MinimumTrades),
		MinimumShadowDurationNanos:       int64(manifest.MinimumShadowDuration),
		MinimumDeflatedSharpeProbability: minimumProbability,
		RegisteredAt:                     researchPromotionTimestamp(manifest.RegisteredAt),
		FinalTestStart:                   researchPromotionTimestamp(manifest.Split.FinalTest.Start),
		CreatedAt:                        researchPromotionTimestamp(manifest.RegisteredAt),
	}
	if _, err = generated.New(repository.pool).InsertResearchPromotionExperimentPreregistration(ctx, write); err != nil {
		return fmt.Errorf("research_promotion_preregistration_failed: %w", err)
	}
	return nil
}

// RecordValidationSuite stores a canonical suite only when it reproduces
// exactly against its persisted preregistration.
func (repository *ResearchPromotionRepository) RecordValidationSuite(
	ctx context.Context,
	preregistrationID string,
	canonical []byte,
	expectedHash string,
	evidenceHash string,
) error {
	queries := generated.New(repository.pool)
	stored, err := queries.GetResearchPromotionExperimentPreregistration(ctx, preregistrationID)
	if err != nil {
		return fmt.Errorf("research_promotion_preregistration_load_failed")
	}
	registration, err := research.ValidateExperimentRegistrationCanonical(
		stored.CanonicalRegistration, hashText(stored.RegistrationHash),
	)
	if err != nil {
		return fmt.Errorf("research_promotion_preregistration_load_failed")
	}
	manifest, err := research.ValidateValidationSuiteCanonical(registration, canonical, expectedHash)
	if err != nil || !validHashParameter(evidenceHash) {
		return fmt.Errorf("research_promotion_validation_suite_invalid")
	}
	write := researchPromotionValidationSuiteWrite(preregistrationID, manifest, canonical, expectedHash, evidenceHash)
	if _, err = queries.InsertResearchPromotionValidationSuite(ctx, write); err != nil {
		return fmt.Errorf("research_promotion_validation_suite_failed: %w", err)
	}
	return nil
}

// RecordChampionChallenger stores comparison evidence without changing
// maturity.
func (repository *ResearchPromotionRepository) RecordChampionChallenger(
	ctx context.Context,
	championSuiteID string,
	challengerSuiteID string,
	canonical []byte,
	expectedHash string,
) error {
	report, err := research.ValidateChampionChallengerCanonical(canonical, expectedHash)
	if err != nil || championSuiteID == "" || challengerSuiteID == "" {
		return fmt.Errorf("research_promotion_champion_challenger_invalid")
	}
	write := generated.InsertResearchPromotionChampionChallengerReportParams{
		ID: report.ID, ChampionStrategyVersionID: report.ChampionVersionID,
		ChallengerStrategyVersionID: report.ChallengerVersionID,
		ChampionSuiteID:             championSuiteID, ChallengerSuiteID: challengerSuiteID,
		ChampionEvidenceHash:   report.ChampionEvidenceHash,
		ChallengerEvidenceHash: report.ChallengerEvidenceHash,
		ManifestHash:           expectedHash,
		CanonicalManifest:      canonical,
		Disposition:            report.Disposition,
		DisclaimerPolicy:       "no_production_profitability_claim",
		CreatedAt:              researchPromotionTimestamp(report.CreatedAt),
	}
	if _, err = generated.New(repository.pool).InsertResearchPromotionChampionChallengerReport(ctx, write); err != nil {
		return fmt.Errorf("research_promotion_champion_challenger_failed: %w", err)
	}
	return nil
}

// ApplyPromotion executes the SECURITY DEFINER command boundary. PostgreSQL
// independently rechecks session, permission, evidence, revision, transition,
// idempotency, and audit rules.
func (repository *ResearchPromotionRepository) ApplyPromotion(
	ctx context.Context,
	command research.PromotionCommand,
) (research.PromotionResult, error) {
	if !validResearchPromotionPromotionCommand(command) {
		return research.PromotionResult{}, fmt.Errorf("research_promotion_promotion_command_invalid")
	}
	var result research.PromotionResult
	var maturity string
	var revision int64
	var failure pgtype.Text
	err := repository.pool.QueryRow(ctx, `SELECT command_id,outcome,maturity,revision,failure_code
FROM apply_research_promotion_maturity_promotion($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		command.CommandID, command.StrategyVersionID, command.EvidenceID,
		command.EvidenceHash, string(command.Target), int64(command.ExpectedRevision),
		command.ActorUserID, command.SessionID, command.IdempotencyKey,
		command.PayloadHash, command.Reason, command.CommandTime,
	).Scan(&result.CommandID, &result.Outcome, &maturity, &revision, &failure)
	if err != nil || revision <= 0 {
		return research.PromotionResult{}, fmt.Errorf("research_promotion_promotion_failed: %w", err)
	}
	result.Maturity = research.Maturity(maturity)
	result.Revision = uint64(revision)
	if failure.Valid {
		result.FailureCode = failure.String
	}
	return result, nil
}

func researchPromotionValidationSuiteWrite(
	preregistrationID string,
	manifest research.ValidationSuiteManifest,
	canonical []byte,
	expectedHash string,
	evidenceHash string,
) generated.InsertResearchPromotionValidationSuiteParams {
	modes, datasetTier, confidence, integrationOnly := researchPromotionPrimaryEvidence(manifest.Sources)
	maturities := make([]string, len(manifest.EligibleMaturities))
	for index, maturity := range manifest.EligibleMaturities {
		maturities[index] = string(maturity)
	}
	return generated.InsertResearchPromotionValidationSuiteParams{
		ID: manifest.ID, PreregistrationID: preregistrationID,
		ResearchGenerationID: manifest.ResearchGenerationID,
		StrategyVersionID:    manifest.StrategyVersionID, ManifestHash: expectedHash,
		CanonicalManifest: canonical, EvidenceHash: evidenceHash,
		FinalTestConsumptionHash: manifest.FinalTestConsumptionHash,
		PrimaryModes:             modes, PrimaryDatasetTier: datasetTier,
		PrimaryConfidenceLabel:    confidence,
		HasIntegrationOnlyPrimary: integrationOnly,
		EligibleMaturities:        maturities, ConfidenceLabel: manifest.ConfidenceLabel,
		ViabilityDisposition: manifest.ViabilityDisposition,
		DisclaimerPolicy:     "no_production_profitability_claim",
		CreatedAt:            researchPromotionTimestamp(manifest.CreatedAt),
	}
}

func researchPromotionPrimaryEvidence(sources []research.EvidenceSource) ([]string, string, string, bool) {
	modes := make([]string, 0, len(sources))
	datasetTier, confidence := "tier_a", "formal_tier_a"
	integrationOnly := false
	for _, source := range sources {
		if !source.Primary {
			continue
		}
		modes = append(modes, source.Mode)
		if source.DatasetTier != "tier_a" {
			datasetTier = source.DatasetTier
		}
		if source.ConfidenceLabel != "formal_tier_a" {
			confidence = source.ConfidenceLabel
		}
		if source.Mode == "paper" || source.Mode == "integration" ||
			source.DatasetTier == "integration_only" {
			integrationOnly = true
		}
	}
	return modes, datasetTier, confidence, integrationOnly
}

func validResearchPromotionPromotionCommand(command research.PromotionCommand) bool {
	return command.CommandID != "" && command.StrategyVersionID != "" &&
		command.EvidenceID != "" && validHashParameter(command.EvidenceHash) &&
		command.ExpectedRevision > 0 && command.ActorUserID != "" &&
		command.SessionID != "" && command.IdempotencyKey != "" &&
		validHashParameter(command.PayloadHash) && command.Reason != "" &&
		command.CommandTime.Location() == time.UTC
}

func researchPromotionNumeric(value string) (pgtype.Numeric, error) {
	var result pgtype.Numeric
	if err := result.Scan(value); err != nil {
		return pgtype.Numeric{}, err
	}
	return result, nil
}

func researchPromotionTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
