package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

// Shadows returns bounded, newest-first public-data simulation history.
func (store *A11ConsoleStore) Shadows(
	ctx context.Context,
	cursor string,
	limit int,
	state string,
) (generated.ShadowSessionPage, error) {
	created, id, _, err := decodeA11TimeCursor(store.cursor, "d3-shadow-sessions:"+state, cursor)
	if err != nil {
		return generated.ShadowSessionPage{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT session.id,session.state,session.revision,
session.configuration_id,CASE WHEN strategy.id='trend-v1a-1' THEN 'trend.v1a.1' ELSE strategy.version::text END,
session.created_at,session.stopped_at,session.failure_code
FROM shadow_sessions session JOIN strategy_versions strategy ON strategy.id=session.strategy_version_id
WHERE ($1='' OR session.state=$1)
  AND ($2::timestamptz IS NULL OR session.created_at<$2 OR (session.created_at=$2 AND session.id<$3))
ORDER BY session.created_at DESC,session.id DESC LIMIT $4`, state, nullableA11Time(created), id, limit+1)
	if err != nil {
		return generated.ShadowSessionPage{}, err
	}
	defer rows.Close()
	items, err := scanD3ShadowSummaries(rows, limit+1)
	if err != nil {
		return generated.ShadowSessionPage{}, err
	}
	revision, err := b8SnapshotRevision(ctx, store.pool)
	if err != nil {
		return generated.ShadowSessionPage{}, err
	}
	page := generated.ShadowSessionPage{Items: items, Revision: revision}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		next := encodeA11TimeCursor(store.cursor, "d3-shadow-sessions:"+state, last.CreatedAt, last.Id)
		page.NextCursor = &next
	}
	return page, nil
}

func scanD3ShadowSummaries(rows pgx.Rows, capacity int) ([]generated.ShadowSessionSummary, error) {
	items := make([]generated.ShadowSessionSummary, 0, capacity)
	for rows.Next() {
		var item generated.ShadowSessionSummary
		var itemState, strategy string
		var revision int64
		if err := rows.Scan(&item.Id, &itemState, &revision, &item.ConfigurationId, &strategy,
			&item.CreatedAt, &item.StoppedAt, &item.FailureCode); err != nil {
			return nil, err
		}
		item.State = generated.ShadowSessionSummaryState(itemState)
		item.StrategyVersion = generated.ShadowSessionSummaryStrategyVersion(strategy)
		item.Revision = strconv.FormatInt(revision, 10)
		item.PublicOnly = true
		item.SimulationOnly = true
		items = append(items, item)
	}
	return items, rows.Err()
}

func populateD3JobEvidence(
	ctx context.Context,
	store *A11ConsoleStore,
	item *generated.JobResource,
	request a11OfflineRequest,
	inputHash string,
	runID string,
) error {
	manifest := d3LabInputManifest(request)
	item.InputManifest = &manifest
	lifecycle := d3LabLifecycle(string(item.State))
	item.Lifecycle = &lifecycle
	if runID == "" {
		return nil
	}
	bundle, err := store.d3ReproductionBundle(ctx, item, inputHash, runID)
	if err != nil {
		return err
	}
	item.ReproductionBundle = bundle
	return nil
}

func d3LabInputManifest(request a11OfflineRequest) generated.LabInputManifest {
	manifest := generated.LabInputManifest{
		ConfigurationId: request.ConfigurationID, DatasetId: request.DatasetID,
		ResearchGenerationId: request.ResearchGenerationID, RootSeedHash: request.RootSeedHash,
		StrategyVersion: generated.LabInputManifestStrategyVersion(request.StrategyVersion),
	}
	if request.Speed != nil {
		speed := generated.LabInputManifestSpeed(*request.Speed)
		manifest.Speed = &speed
	}
	manifest.IncidentId = request.IncidentID
	manifest.FirstOrdinal, manifest.LastOrdinal = request.FirstOrdinal, request.LastOrdinal
	return manifest
}

func (store *A11ConsoleStore) d3ReproductionBundle(
	ctx context.Context, item *generated.JobResource, inputHash, runID string,
) (*generated.ReproductionBundle, error) {
	var bundle generated.ReproductionBundle
	var datasetRevision int64
	var canonical []byte
	err := store.pool.QueryRow(ctx, `SELECT manifest_hash::text,code_commit,go_version,architecture,
operating_system,dataset_manifest_hash::text,dataset_revision,source_commit,configuration_hash::text,
model_namespace_id,starting_balance_hash::text,confidence_tier,canonical_payload
FROM run_manifests WHERE run_id=$1`, runID).Scan(&bundle.ManifestHash, &bundle.CodeCommit,
		&bundle.GoVersion, &bundle.Architecture, &bundle.OperatingSystem, &bundle.DatasetManifestHash,
		&datasetRevision, &bundle.SourceCommit, &bundle.ConfigurationHash, &bundle.ModelNamespaceId,
		&bundle.StartingBalanceHash, &bundle.ConfidenceTier, &canonical)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(canonical) < 2 || len(canonical) > 1_048_576 || !json.Valid(canonical) {
		return nil, fmt.Errorf("d3_run_manifest_invalid")
	}
	bundle.RunId = runID
	bundle.InputHash = inputHash
	bundle.DatasetRevision = strconv.FormatInt(datasetRevision, 10)
	bundle.CanonicalManifest = string(canonical)
	if item.Result != nil {
		bundle.ResultHash = &item.Result.ResultHash
	}
	return &bundle, nil
}

func d3LabLifecycle(state string) generated.LabLifecycleCapabilities {
	return generated.LabLifecycleCapabilities{
		Pause: state == "RUNNING", Resume: state == "PAUSED",
		Cancel:    strings.Contains(" QUEUED RUNNING PAUSE_REQUESTED PAUSED ", " "+state+" "),
		Reproduce: strings.Contains(" CANCELED SUCCEEDED FAILED ", " "+state+" "),
		Compare:   true, Export: true,
	}
}

func (store *A11ConsoleStore) d3ReplayCheckpoints(ctx context.Context, runID string) ([]generated.ReplayCheckpoint, error) {
	items := []generated.ReplayCheckpoint{}
	if runID == "" {
		return items, nil
	}
	rows, err := store.pool.Query(ctx, `SELECT revision,input_ordinal,state_hash::text,
deterministic_state_hash::text,model_namespace_id,created_at
FROM run_checkpoints WHERE run_id=$1 ORDER BY revision DESC LIMIT 20`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item generated.ReplayCheckpoint
		var revision, ordinal int64
		if err = rows.Scan(&revision, &ordinal, &item.StateHash, &item.DeterministicStateHash,
			&item.ModelNamespaceId, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Revision = strconv.FormatInt(revision, 10)
		item.InputOrdinal = strconv.FormatInt(ordinal, 10)
		items = append(items, item)
	}
	return items, rows.Err()
}
