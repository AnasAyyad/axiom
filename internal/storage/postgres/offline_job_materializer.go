package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/recorder"
	"axiom/internal/replay"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ownerConsoleOfflineRequest struct {
	ConfigurationID      string  `json:"configuration_id"`
	DatasetID            string  `json:"dataset_id"`
	IncidentID           *string `json:"incident_id,omitempty"`
	ResearchGenerationID string  `json:"research_generation_id"`
	RootSeedHash         string  `json:"root_seed_hash"`
	Speed                *string `json:"speed,omitempty"`
	StrategyVersion      string  `json:"strategy_version"`
	FirstOrdinal         *string `json:"first_ordinal"`
	LastOrdinal          *string `json:"last_ordinal"`
	EvaluationCampaignID string  `json:"evaluation_campaign_id,omitempty"`
	EvaluationMemberID   string  `json:"evaluation_member_id,omitempty"`
	ConfigurationKey     string  `json:"configuration_key,omitempty"`
	CapitalMicros        int64   `json:"capital_micros,omitempty"`
	CostStressBPS        int32   `json:"cost_stress_bps,omitempty"`
}

type ownerConsoleMaterializer struct {
	pool *pgxpool.Pool
	root string
}

type ownerConsoleMaterializedInputs struct {
	source      replay.Source
	descriptor  backtest.DatasetDescriptor
	inputKind   string
	metadata    map[string]domain.InstrumentMetadata
	metadataIDs map[string]string
}

// NewOfflineJobMaterializer builds verified credential-free replay claims.
func NewOfflineJobMaterializer(pool *pgxpool.Pool, root string) (OfflineJobMaterializer, error) {
	if pool == nil || !filepath.IsAbs(root) {
		return nil, fmt.Errorf("owner_console_job_materializer_dependencies_missing")
	}
	materializer := &ownerConsoleMaterializer{pool: pool, root: filepath.Clean(root)}
	return materializer.materialize, nil
}

func (materializer *ownerConsoleMaterializer) materialize(ctx context.Context, jobID, kind string, payload json.RawMessage) (backtest.JobClaim, error) {
	request, err := decodeOwnerConsoleOfflineRequest(kind, payload)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	dataset, configuration, configurationHash, err := materializer.loadInputs(ctx, request)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	inputs, err := materializer.openOfflineInputs(ctx, request, dataset)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	namespace, err := materializer.modelNamespace(ctx, configuration)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	timing, acceleration, err := offlineJobTiming(kind, request.Speed)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	manifest, err := ownerConsoleRunManifest(jobID, kind, request, configuration, configurationHash, inputs.descriptor, namespace,
		timing, acceleration)
	if err != nil {
		return backtest.JobClaim{}, err
	}
	if kind == "replay" {
		inputs.source, err = materializer.multiExchangeConsoleFaultSource(ctx, jobID, inputs.source)
		if err != nil {
			return backtest.JobClaim{}, err
		}
	}
	stress := request.CostStressBPS
	if stress == 0 {
		stress = 10_000
	}
	return backtest.JobClaim{ID: jobID, Manifest: manifest, Configuration: configuration, Source: inputs.source,
		TimingMode: timing, Acceleration: acceleration, CostStressBPS: stress,
		EvaluationInputKind: inputs.inputKind, EvaluationMetadata: inputs.metadata, EvaluationMetadataID: inputs.metadataIDs,
		EvaluationConfigurationID: request.ConfigurationID, EvaluationDatasetID: request.DatasetID}, nil
}

func (materializer *ownerConsoleMaterializer) openOfflineInputs(ctx context.Context,
	request ownerConsoleOfflineRequest, dataset recordedDatasetInput) (ownerConsoleMaterializedInputs, error) {
	value := ownerConsoleMaterializedInputs{inputKind: "decision_inputs"}
	var err error
	if request.EvaluationCampaignID != "" {
		value.source, value.descriptor, value.inputKind, err = materializer.openEvaluationDataset(ctx, request, dataset)
		if err == nil {
			value.metadata, value.metadataIDs, err = materializer.evaluationMetadata(ctx, request.EvaluationCampaignID)
		}
		return value, err
	}
	reader, descriptor, err := materializer.openDataset(dataset)
	if err != nil {
		return ownerConsoleMaterializedInputs{}, err
	}
	value.source, err = ownerConsoleReplaySource(reader, request)
	value.descriptor = descriptor
	return value, err
}

type multiExchangeConsoleMaterializedFault struct {
	id, actor string
	fault     replay.Fault
}

func (materializer *ownerConsoleMaterializer) multiExchangeConsoleFaultSource(
	ctx context.Context, jobID string, source replay.Source,
) (replay.Source, error) {
	rows, err := materializer.pool.Query(ctx, `SELECT id,fault_kind,
	  event_ordinal,delay_nanos,repeatable,actor_user_id
	FROM multi_exchange_console_replay_fault_schedules WHERE replay_id=$1 ORDER BY schedule_revision`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	scheduled := []multiExchangeConsoleMaterializedFault{}
	faults := []replay.Fault{}
	for rows.Next() {
		var item multiExchangeConsoleMaterializedFault
		var ordinal uint64
		var delay int64
		if err = rows.Scan(&item.id, &item.fault.Kind, &ordinal, &delay,
			&item.fault.Repeatable, &item.actor); err != nil {
			return nil, err
		}
		item.fault.Ordinal = ordinal
		item.fault.Delay = time.Duration(delay)
		scheduled = append(scheduled, item)
		faults = append(faults, item.fault)
	}
	if err = rows.Err(); err != nil || len(faults) == 0 {
		return source, err
	}
	return replay.NewFaultSource(source, faults, func(event replay.FaultEvent) error {
		for _, item := range scheduled {
			if item.fault.Ordinal == event.Ordinal {
				return materializer.recordMultiExchangeConsoleFault(jobID, item, event)
			}
		}
		return fmt.Errorf("multi_exchange_console_fault_schedule_missing")
	})
}

func (materializer *ownerConsoleMaterializer) recordMultiExchangeConsoleFault(
	jobID string, scheduled multiExchangeConsoleMaterializedFault, event replay.FaultEvent,
) error {
	now := time.Now().UTC()
	payload, err := json.Marshal(map[string]any{
		"fault_id": scheduled.id, "fault": event.Kind, "ordinal": event.Ordinal,
		"replay_id": jobID, "simulation_only": true,
	})
	if err != nil {
		return err
	}
	hash := ownerConsoleHash(payload)
	auditID, _ := ownerConsoleIdentifier("audit")
	outboxID, _ := ownerConsoleIdentifier("event")
	tx, err := materializer.pool.BeginTx(context.Background(), pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(context.Background(), `INSERT INTO audit_events(
	  id,event_type,actor,causation_id,correlation_id,event_hash,recorded_at
	) VALUES($1,'replay.fault.injected',$2,$3,$4,$5,$6)`,
		auditID, scheduled.actor, scheduled.id, jobID, hash, now); err != nil {
		return err
	}
	if _, err = tx.Exec(context.Background(), `INSERT INTO outbox_events(
	  id,topic,payload_hash,created_at,stream,schema_version,entity_type,entity_id,
	  entity_revision,event_time,correlation_id,causation_id,payload
	) VALUES($1,'replay.fault.injected',$2,$3,'job','axiom.stream.v1','replay',$4,
	  $5,$3,$4,$6,$7)`, outboxID, hash, now, jobID, event.Ordinal, scheduled.id,
		string(payload)); err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

type recordedDatasetInput struct {
	id, recorderID, hash, path, sourceCommit, kind string
	revision                                       int64
}

func (materializer *ownerConsoleMaterializer) loadInputs(ctx context.Context, request ownerConsoleOfflineRequest) (recordedDatasetInput, config.Configuration, string, error) {
	var dataset recordedDatasetInput
	var configurationHash string
	var canonical []byte
	err := materializer.pool.QueryRow(ctx, `SELECT dm.id,dm.recorder_dataset_id,dm.dataset_hash,dm.manifest_revision,
      dm.manifest_path,dm.source_commit,dm.dataset_kind,cv.configuration_hash,cv.canonical_payload
	  FROM dataset_manifests dm
	  JOIN configuration_versions cv ON cv.id=$2
	  JOIN experiment_registrations experiment ON experiment.configuration_id=cv.id AND experiment.dataset_id=dm.id
	  JOIN research_generations generation ON generation.experiment_id=experiment.id
	  WHERE dm.id=$1 AND ((dm.state='qualified' AND dm.dataset_kind='decision_inputs') OR
	    ($5<>'' AND dm.state IN ('ready','qualified') AND dm.dataset_kind='public_market' AND EXISTS(
	      SELECT 1 FROM evaluation_campaign_datasets selected
	      WHERE selected.campaign_id=$5 AND selected.strategy_id=$6 AND selected.dataset_id=dm.id)))
	    AND generation.id=$3 AND experiment.strategy_version_id=$4
	    AND experiment.status IN ('registered','running','completed','locked')`, request.DatasetID, request.ConfigurationID,
		request.ResearchGenerationID, ownerConsoleStrategyVersionID(request.StrategyVersion),
		request.EvaluationCampaignID, evaluationStrategyID(request.StrategyVersion)).
		Scan(&dataset.id, &dataset.recorderID, &dataset.hash, &dataset.revision, &dataset.path, &dataset.sourceCommit,
			&dataset.kind,
			&configurationHash, &canonical)
	if err != nil {
		return recordedDatasetInput{}, config.Configuration{}, "", fmt.Errorf("owner_console_job_inputs_unavailable")
	}
	var configuration config.Configuration
	if json.Unmarshal(canonical, &configuration) != nil || config.Validate(configuration) != nil ||
		ownerConsoleSHA256(canonical) != configurationHash || !ownerConsoleConfigurationMatchesStrategy(configuration, request.StrategyVersion) {
		return recordedDatasetInput{}, config.Configuration{}, "", fmt.Errorf("owner_console_job_configuration_invalid")
	}
	return dataset, configuration, configurationHash, nil
}

func (materializer *ownerConsoleMaterializer) openDataset(input recordedDatasetInput) (*backtest.DatasetReader, backtest.DatasetDescriptor, error) {
	if input.revision <= 0 {
		return nil, backtest.DatasetDescriptor{}, fmt.Errorf("owner_console_job_dataset_identity_invalid")
	}
	root, manifestPath, err := materializer.findDatasetManifest(input)
	if err != nil {
		return nil, backtest.DatasetDescriptor{}, err
	}
	manifest, err := recorder.ReadManifest(manifestPath)
	if err != nil || manifest.Hash != input.hash || manifest.DatasetID != input.recorderID || int64(manifest.Revision) != input.revision || len(manifest.Segments) < 2 {
		return nil, backtest.DatasetDescriptor{}, fmt.Errorf("owner_console_job_dataset_identity_invalid")
	}
	canonical := manifest.Segments[1].Manifest.Spec
	compatibility := backtest.DatasetCompatibility{SourceCommit: input.sourceCommit, ParserVersion: canonical.ParserVersion,
		NormalizationVersion: canonical.NormalizationVersion, MinimumRecordsPerPair: 1, MaximumLowDensityPairs: 0}
	reader, err := backtest.OpenDataset(root, manifestPath, compatibility)
	if err != nil {
		return nil, backtest.DatasetDescriptor{}, err
	}
	descriptor := reader.Descriptor()
	if descriptor.RequireDecisionGrade() != nil {
		return nil, backtest.DatasetDescriptor{}, fmt.Errorf("owner_console_job_dataset_not_decision_grade")
	}
	return reader, descriptor, nil
}

func (materializer *ownerConsoleMaterializer) modelNamespace(ctx context.Context, configuration config.Configuration) (backtest.ModelNamespace, error) {
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
		return backtest.ModelNamespace{}, fmt.Errorf("owner_console_job_model_namespace_ambiguous")
	}
	return items[0], nil
}
