package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"

	"axiom/internal/backtest"
	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/recorder"
	"axiom/internal/replay"

	"github.com/jackc/pgx/v5/pgxpool"
)

type a11OfflineRequest struct {
	ConfigurationID string  `json:"configuration_id"`
	DatasetID       string  `json:"dataset_id"`
	RootSeedHash    string  `json:"root_seed_hash"`
	StrategyVersion string  `json:"strategy_version"`
	FirstOrdinal    *string `json:"first_ordinal"`
	LastOrdinal     *string `json:"last_ordinal"`
}

type a11Materializer struct {
	pool *pgxpool.Pool
	root string
}

// NewA11JobMaterializer builds verified credential-free replay claims.
func NewA11JobMaterializer(pool *pgxpool.Pool, root string) (A11JobMaterializer, error) {
	if pool == nil || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("a11_job_materializer_dependencies_missing")
	}
	materializer := &a11Materializer{pool: pool, root: filepath.Clean(root)}
	return materializer.materialize, nil
}

func (materializer *a11Materializer) materialize(ctx context.Context, jobID, kind string, payload json.RawMessage) (backtest.JobClaim, error) {
	request, err := decodeA11OfflineRequest(kind, payload)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	dataset, configuration, configurationHash, err := materializer.loadInputs(ctx, request)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	reader, descriptor, err := materializer.openDataset(dataset)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	namespace, err := materializer.modelNamespace(ctx, configuration)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	manifest, err := a11RunManifest(jobID, kind, request, configuration, configurationHash, descriptor, namespace)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	source, err := a11ReplaySource(reader, request)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	return backtest.JobClaim{ID: jobID, Manifest: manifest, Configuration: configuration, Source: source}, nil
}

type a11DatasetInput struct {
	recorderID, hash, path, sourceCommit string
	revision                             int64
}

func (materializer *a11Materializer) loadInputs(ctx context.Context, request a11OfflineRequest) (a11DatasetInput, config.Configuration, string, error) {
	var dataset a11DatasetInput
	var configurationHash string
	var canonical []byte
	err := materializer.pool.QueryRow(ctx, `SELECT dm.recorder_dataset_id,dm.dataset_hash,dm.manifest_revision,
      dm.manifest_path,dm.source_commit,cv.configuration_hash,cv.canonical_payload
      FROM dataset_manifests dm CROSS JOIN configuration_versions cv
      WHERE dm.id=$1 AND dm.state='qualified' AND dm.dataset_kind='decision_inputs' AND cv.id=$2`, request.DatasetID, request.ConfigurationID).
		Scan(&dataset.recorderID, &dataset.hash, &dataset.revision, &dataset.path, &dataset.sourceCommit,
			&configurationHash, &canonical)
	if err != nil {
		return a11DatasetInput{}, config.Configuration{}, "", fmt.Errorf("a11_job_inputs_unavailable")
	}
	var configuration config.Configuration
	if json.Unmarshal(canonical, &configuration) != nil || config.Validate(configuration) != nil ||
		a11SHA256(canonical) != configurationHash || configuration.Trend.StrategyVersion != request.StrategyVersion {
		return a11DatasetInput{}, config.Configuration{}, "", fmt.Errorf("a11_job_configuration_invalid")
	}
	return dataset, configuration, configurationHash, nil
}

func (materializer *a11Materializer) openDataset(input a11DatasetInput) (*backtest.DatasetReader, backtest.DatasetDescriptor, error) {
	if filepath.Base(input.path) != input.path || input.revision <= 0 {
		return nil, backtest.DatasetDescriptor{}, fmt.Errorf("a11_job_dataset_identity_invalid")
	}
	manifestPath := filepath.Join(materializer.root, input.path)
	manifest, err := recorder.ReadManifest(manifestPath)
	if err != nil || manifest.Hash != input.hash || manifest.DatasetID != input.recorderID || int64(manifest.Revision) != input.revision || len(manifest.Segments) < 2 {
		return nil, backtest.DatasetDescriptor{}, fmt.Errorf("a11_job_dataset_identity_invalid")
	}
	canonical := manifest.Segments[1].Manifest.Spec
	compatibility := backtest.DatasetCompatibility{SourceCommit: input.sourceCommit, ParserVersion: canonical.ParserVersion,
		NormalizationVersion: canonical.NormalizationVersion, MinimumRecordsPerPair: 1, MaximumLowDensityPairs: 0}
	reader, err := backtest.OpenDataset(materializer.root, manifestPath, compatibility)
	if err != nil {
		return nil, backtest.DatasetDescriptor{}, err
	}
	descriptor := reader.Descriptor()
	if descriptor.RequireDecisionGrade() != nil {
		return nil, backtest.DatasetDescriptor{}, fmt.Errorf("a11_job_dataset_not_decision_grade")
	}
	return reader, descriptor, nil
}

func (materializer *a11Materializer) modelNamespace(ctx context.Context, configuration config.Configuration) (backtest.ModelNamespace, error) {
	rows, err := materializer.pool.Query(ctx, `SELECT id,market_context,liquidity_domain,fee_model_id,latency_model_id,fill_model_id
      FROM model_namespaces WHERE market_context='production-public' AND fee_model_id=$1 AND latency_model_id=$2
      ORDER BY id LIMIT 2`, configuration.Models.Fee, configuration.Models.Latency)
	if err != nil {
		return backtest.ModelNamespace{}, err
	}
	defer rows.Close()
	items := make([]backtest.ModelNamespace, 0, 2)
	for rows.Next() {
		var item backtest.ModelNamespace
		if err = rows.Scan(&item.ID, &item.MarketContext, &item.LiquidityDomain, &item.FeeDomain,
			&item.LatencyDomain, &item.FillDomain); err != nil {
			return backtest.ModelNamespace{}, err
		}
		items = append(items, item)
	}
	if rows.Err() != nil || len(items) != 1 {
		return backtest.ModelNamespace{}, fmt.Errorf("a11_job_model_namespace_ambiguous")
	}
	return items[0], nil
}

func decodeA11OfflineRequest(kind string, payload json.RawMessage) (a11OfflineRequest, error) {
	var request a11OfflineRequest
	if json.Unmarshal(payload, &request) != nil {
		return request, fmt.Errorf("a11_job_request_invalid")
	}
	seed, err := hex.DecodeString(request.RootSeedHash)
	if (kind != "backtest" && kind != "replay") || request.ConfigurationID == "" || request.DatasetID == "" ||
		request.StrategyVersion != "trend.v1a.1" || err != nil || len(seed) != sha256.Size {
		return request, fmt.Errorf("a11_job_request_invalid")
	}
	if (request.FirstOrdinal == nil) != (request.LastOrdinal == nil) {
		return request, fmt.Errorf("a11_job_window_invalid")
	}
	return request, nil
}

func a11RunManifest(jobID, kind string, request a11OfflineRequest, configuration config.Configuration, configurationHash string,
	dataset backtest.DatasetDescriptor, namespace backtest.ModelNamespace) (backtest.RunManifest, error) {
	build := buildinfo.Current()
	if build.Dirty || !a11BuildIdentityValid(build.Commit, build.GoSumHash, build.PNPMLockHash) {
		return backtest.RunManifest{}, fmt.Errorf("a11_job_build_identity_invalid")
	}
	runID, err := domain.NewRunID(jobID)
	if err != nil {
		return backtest.RunManifest{}, fmt.Errorf("a11_job_run_identity_invalid")
	}
	startingPayload, _ := json.Marshal(struct {
		Asset    string `json:"asset"`
		Quantity string `json:"quantity"`
	}{Asset: configuration.Portfolio.SettlementAsset, Quantity: configuration.Portfolio.StartingCapital.Value})
	return backtest.RunManifest{RunID: runID, Mode: kind, CodeCommit: build.Commit,
		Build: backtest.CurrentBuildIdentity([]string{"trimpath"}, build.GoSumHash, build.PNPMLockHash), Dataset: dataset,
		ConfigurationHash: configurationHash, Seed: request.RootSeedHash,
		SchedulerVersion: "deterministic-scheduler-v1", SerializationVersion: "canonical-json-v1",
		Models: namespace, StartingBalanceHash: a11SHA256(startingPayload)}, nil
}

func a11ReplaySource(reader *backtest.DatasetReader, request a11OfflineRequest) (replay.Source, error) {
	if request.FirstOrdinal == nil {
		return backtest.NewDatasetSource(reader)
	}
	first, firstErr := strconv.ParseUint(*request.FirstOrdinal, 10, 64)
	last, lastErr := strconv.ParseUint(*request.LastOrdinal, 10, 64)
	if firstErr != nil || lastErr != nil {
		return nil, fmt.Errorf("a11_job_window_invalid")
	}
	return backtest.NewDatasetWindowSource(reader, first, last)
}

func a11BuildIdentityValid(values ...string) bool {
	for _, value := range values {
		decoded, err := hex.DecodeString(value)
		if err != nil || (len(decoded) != sha256.Size && len(decoded) != 20) {
			return false
		}
	}
	return true
}

func a11SHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
