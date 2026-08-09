package postgres

import (
	"context"
	"fmt"

	"axiom/internal/storage/postgres/generated"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InventoryRebalancingFactSetWrite is one immutable reviewed fact set and every versioned route
// fact it contains.
type InventoryRebalancingFactSetWrite struct {
	FactSet generated.InsertRebalancingFactSetParams
	Facts   []generated.InsertRebalancingRouteFactParams
}

// InventoryRebalancingRecommendationWrite is one complete advisory recommendation, its selected
// provenance links, and the explicit operator checklist.
type InventoryRebalancingRecommendationWrite struct {
	Recommendation generated.InsertRebalancingRecommendationParams
	Steps          []generated.InsertRebalancingRecommendationStepParams
	Checklist      []generated.InsertRebalancingChecklistStepParams
}

// InventoryRebalancingRepository owns immutable inventory rebalancing reviewed-fact and advisory evidence.
type InventoryRebalancingRepository struct{ pool *pgxpool.Pool }

// NewInventoryRebalancingRepository constructs the inventory rebalancing persistence boundary.
func NewInventoryRebalancingRepository(pool *pgxpool.Pool) (*InventoryRebalancingRepository, error) {
	if pool == nil {
		return nil, fmt.Errorf("inventory_rebalancing_repository_pool_missing")
	}
	return &InventoryRebalancingRepository{pool: pool}, nil
}

// RecordFactSet atomically commits one immutable reviewed fact graph.
func (repository *InventoryRebalancingRepository) RecordFactSet(ctx context.Context, write InventoryRebalancingFactSetWrite) error {
	if !validInventoryRebalancingFactSetWrite(write) {
		return fmt.Errorf("inventory_rebalancing_fact_set_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("inventory_rebalancing_fact_set_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.InsertRebalancingFactSet(ctx, write.FactSet); err != nil {
		return fmt.Errorf("inventory_rebalancing_fact_set_insert_failed: %w", err)
	}
	for _, fact := range write.Facts {
		if _, err = queries.InsertRebalancingRouteFact(ctx, fact); err != nil {
			return fmt.Errorf("inventory_rebalancing_route_fact_insert_failed: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("inventory_rebalancing_fact_set_commit_failed: %w", err)
	}
	return nil
}

// RecordRecommendation atomically commits one complete advisory result.
func (repository *InventoryRebalancingRepository) RecordRecommendation(
	ctx context.Context,
	write InventoryRebalancingRecommendationWrite,
) error {
	if !validInventoryRebalancingRecommendationWrite(write) {
		return fmt.Errorf("inventory_rebalancing_recommendation_invalid")
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("inventory_rebalancing_recommendation_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	queries := generated.New(tx)
	if _, err = queries.InsertRebalancingRecommendation(ctx, write.Recommendation); err != nil {
		return fmt.Errorf("inventory_rebalancing_recommendation_insert_failed: %w", err)
	}
	for _, step := range write.Steps {
		if _, err = queries.InsertRebalancingRecommendationStep(ctx, step); err != nil {
			return fmt.Errorf("inventory_rebalancing_recommendation_step_insert_failed: %w", err)
		}
	}
	for _, step := range write.Checklist {
		if _, err = queries.InsertRebalancingChecklistStep(ctx, step); err != nil {
			return fmt.Errorf("inventory_rebalancing_checklist_step_insert_failed: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("inventory_rebalancing_recommendation_commit_failed: %w", err)
	}
	return nil
}

// LoadFactSet returns one immutable fact-set aggregate.
func (repository *InventoryRebalancingRepository) LoadFactSet(
	ctx context.Context,
	id string,
) (generated.RebalancingFactSet, []generated.RebalancingRouteFact, error) {
	if id == "" {
		return generated.RebalancingFactSet{}, nil, fmt.Errorf("inventory_rebalancing_fact_set_invalid")
	}
	queries := generated.New(repository.pool)
	factSet, err := queries.GetRebalancingFactSet(ctx, id)
	if err != nil {
		return generated.RebalancingFactSet{}, nil, fmt.Errorf("inventory_rebalancing_fact_set_load_failed")
	}
	rows, err := queries.ListRebalancingRouteFacts(ctx, id)
	if err != nil || len(rows) == 0 {
		return generated.RebalancingFactSet{}, nil, fmt.Errorf("inventory_rebalancing_fact_set_load_failed")
	}
	facts := make([]generated.RebalancingRouteFact, len(rows))
	for index, row := range rows {
		facts[index] = *row
	}
	return *factSet, facts, nil
}

// LoadRecommendation returns the complete ordered advisory aggregate.
func (repository *InventoryRebalancingRepository) LoadRecommendation(
	ctx context.Context,
	id string,
) (
	generated.RebalancingRecommendation,
	[]generated.RebalancingRecommendationStep,
	[]generated.RebalancingChecklistStep,
	error,
) {
	if id == "" {
		return generated.RebalancingRecommendation{}, nil, nil, fmt.Errorf("inventory_rebalancing_recommendation_invalid")
	}
	queries := generated.New(repository.pool)
	recommendation, err := queries.GetRebalancingRecommendation(ctx, id)
	if err != nil {
		return generated.RebalancingRecommendation{}, nil, nil, fmt.Errorf("inventory_rebalancing_recommendation_load_failed")
	}
	stepRows, stepErr := queries.ListRebalancingRecommendationSteps(ctx, id)
	checklistRows, checklistErr := queries.ListRebalancingChecklistSteps(ctx, id)
	if stepErr != nil || checklistErr != nil || len(stepRows) == 0 || len(checklistRows) < 4 {
		return generated.RebalancingRecommendation{}, nil, nil, fmt.Errorf("inventory_rebalancing_recommendation_load_failed")
	}
	steps := make([]generated.RebalancingRecommendationStep, len(stepRows))
	for index, row := range stepRows {
		steps[index] = *row
	}
	checklist := make([]generated.RebalancingChecklistStep, len(checklistRows))
	for index, row := range checklistRows {
		checklist[index] = *row
	}
	return *recommendation, steps, checklist, nil
}

func validInventoryRebalancingFactSetWrite(write InventoryRebalancingFactSetWrite) bool {
	factSet := write.FactSet
	if factSet.ID == "" || factSet.ConfigurationID == "" ||
		factSet.FactSchemaVersion != "rebalancing-fact.v1" ||
		factSet.CostModelVersion != "rebalancing-cost.v1" ||
		!validHashParameter(factSet.ConfigurationHash) ||
		!validHashParameter(factSet.CanonicalHash) || !factSet.RecordedAt.Valid ||
		len(write.Facts) == 0 {
		return false
	}
	identities := make(map[string]struct{}, len(write.Facts))
	for _, fact := range write.Facts {
		if fact.FactSetID != factSet.ID || fact.FactID == "" || fact.LogicalKey == "" ||
			fact.FactVersion <= 0 || (fact.FactKind != "trade" && fact.FactKind != "transfer") ||
			fact.FromExchangeID == "" || fact.FromAssetSymbol == "" ||
			fact.ToExchangeID == "" || fact.ToAssetSymbol == "" ||
			fact.MinimumQuantity == nil || fact.FeeCost == nil || fact.SpreadCost == nil ||
			fact.DepthCost == nil || fact.DelayCost == nil || fact.NetworkFeeCost == nil ||
			fact.CompatibilityCost == nil || fact.VolatilityRiskCost == nil ||
			fact.OperationalRiskCost == nil || fact.RiskScore == nil ||
			fact.MinimumDurationNanos <= 0 ||
			fact.MaximumDurationNanos < fact.MinimumDurationNanos ||
			fact.Source == "" || fact.Observer == "" || !fact.ObservedAt.Valid ||
			!fact.ExpiresAt.Valid || fact.Confidence == nil ||
			!validHashParameter(fact.ProvenanceHash) {
			return false
		}
		key := fact.LogicalKey + "#" + fmt.Sprint(fact.FactVersion)
		if _, duplicate := identities[key]; duplicate {
			return false
		}
		identities[key] = struct{}{}
		if fact.Approved && (fact.ApprovalActor == nil || fact.ApprovalReference == nil ||
			!fact.ApprovedAt.Valid) {
			return false
		}
	}
	return true
}

func validInventoryRebalancingRecommendationWrite(write InventoryRebalancingRecommendationWrite) bool {
	recommendation := write.Recommendation
	if recommendation.ID == "" || recommendation.RequestID == "" ||
		recommendation.ConfigurationID == "" || recommendation.FactSetID == "" ||
		!validHashParameter(recommendation.ConfigurationHash) ||
		!validHashParameter(recommendation.FactSetHash) ||
		!validHashParameter(recommendation.CanonicalHash) ||
		recommendation.SourceExchangeID == recommendation.DestinationExchangeID ||
		recommendation.SourceAssetSymbol == "" ||
		recommendation.SourceAssetSymbol != recommendation.DestinationAssetSymbol ||
		recommendation.Quantity == nil || recommendation.TotalCost == nil ||
		recommendation.FeeCost == nil || recommendation.SpreadCost == nil ||
		recommendation.DepthCost == nil || recommendation.DelayCost == nil ||
		recommendation.NetworkFeeCost == nil || recommendation.CompatibilityCost == nil ||
		recommendation.VolatilityRiskCost == nil || recommendation.OperationalRiskCost == nil ||
		recommendation.RiskScore == nil || recommendation.MinimumDurationNanos <= 0 ||
		recommendation.MaximumDurationNanos < recommendation.MinimumDurationNanos ||
		!recommendation.AdvisoryOnly || !recommendation.RecordedAt.Valid ||
		len(write.Steps) == 0 || len(write.Steps) > 6 || len(write.Checklist) < 4 {
		return false
	}
	if (recommendation.Method == "natural_reverse_arbitrage") !=
		(recommendation.SourceCrossExchangeArbitrageDecisionID != nil) ||
		(recommendation.Method != "natural_reverse_arbitrage" &&
			recommendation.Method != "reviewed_graph_route") {
		return false
	}
	for index, step := range write.Steps {
		if step.RecommendationID != recommendation.ID || step.StepIndex != int32(index) ||
			step.FactSetID != recommendation.FactSetID || step.FactID == "" ||
			step.FactVersion <= 0 || !validHashParameter(step.ProvenanceHash) {
			return false
		}
	}
	for index, step := range write.Checklist {
		if step.RecommendationID != recommendation.ID ||
			step.StepIndex != int32(index) || step.Instruction == "" {
			return false
		}
	}
	return true
}
