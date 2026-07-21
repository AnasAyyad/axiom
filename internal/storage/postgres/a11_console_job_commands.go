package postgres

import (
	"context"
	"errors"
	"strconv"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

// CreateJob creates exactly one durable offline run request per actor/key/payload.
func (store *A11ConsoleStore) CreateJob(ctx context.Context, principal authentication.Principal, kind, key string, request any) (generated.JobResource, error) {
	payload, hash, err := a11CommandPayload(request)
	if err != nil || (kind != "backtest" && kind != "replay") {
		return generated.JobResource{}, console.ErrInvalidRequest
	}
	configurationID, datasetID, strategyID, generationID, validRequest := a11JobReferences(request)
	if !validRequest {
		return generated.JobResource{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.JobResource{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	dedupe := a11Dedupe(principal.UserID, key)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, dedupe); err != nil {
		return generated.JobResource{}, err
	}
	existingID, found, err := a11ExistingJob(ctx, tx, dedupe, hash)
	if err != nil {
		return generated.JobResource{}, err
	}
	if found {
		if err = tx.Commit(ctx); err != nil {
			return generated.JobResource{}, err
		}
		return store.Job(ctx, existingID, "")
	}
	if err = validateA11JobCreate(ctx, tx, principal.UserID, configurationID, datasetID, strategyID, generationID, request); err != nil {
		return generated.JobResource{}, err
	}
	now := store.clock.Now().UTC
	jobID, _ := a11Identifier(kind)
	commandID, _ := a11Identifier("command")
	auditID, _ := a11Identifier("audit")
	if err = insertA11Command(ctx, tx, commandID, principal, key, hash, "create_"+kind, kind, jobID, "create durable research run", now, auditID, commandID); err != nil {
		return generated.JobResource{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO jobs(id,job_type,idempotency_key,state,payload_hash,created_at,updated_at,owner_user_id,request_payload,max_attempts) VALUES($1,$2,$3,'QUEUED',$4,$5,$5,$6,$7,3)`, jobID, kind, dedupe, hash, now, principal.UserID, string(payload)); err != nil {
		return generated.JobResource{}, a11ConstraintError(err)
	}
	if _, err = completeA11Command(ctx, tx, commandID, auditID, principal, "create_"+kind, jobID, hash, map[string]any{"job_id": jobID, "state": "QUEUED"}, now, commandID); err != nil {
		return generated.JobResource{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return generated.JobResource{}, err
	}
	return store.Job(ctx, jobID, "")
}

func a11ExistingJob(ctx context.Context, tx pgx.Tx, dedupe, payloadHash string) (string, bool, error) {
	var id, storedHash string
	err := tx.QueryRow(ctx, `SELECT id,payload_hash FROM jobs WHERE idempotency_key=$1`, dedupe).Scan(&id, &storedHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if storedHash != payloadHash {
		return "", false, console.ErrIdempotencyConflict
	}
	return id, true, nil
}

func validateA11JobCreate(ctx context.Context, tx pgx.Tx, owner, configurationID, datasetID, strategyID, generationID string, request any) error {
	var references int
	err := tx.QueryRow(ctx, `SELECT
      (SELECT count(*) FROM configuration_versions WHERE id=$1)+
      (SELECT count(*) FROM dataset_manifests WHERE id=$2 AND state='qualified' AND dataset_kind='decision_inputs')+
	  (SELECT count(*) FROM strategy_versions WHERE id=$3)+
	  (SELECT count(*) FROM research_generations generation JOIN experiment_registrations experiment
	   ON experiment.id=generation.experiment_id WHERE generation.id=$4 AND experiment.configuration_id=$1
	   AND experiment.dataset_id=$2 AND experiment.strategy_version_id=$3
	   AND experiment.status IN ('registered','running','completed','locked'))`,
		configurationID, datasetID, strategyID, generationID).Scan(&references)
	if err != nil {
		return err
	}
	if references != 4 {
		return console.ErrPrecondition
	}
	if replay, ok := request.(generated.ReplayJobRequest); ok && replay.IncidentId != nil {
		if err = validateA11IncidentReplay(ctx, tx, replay); err != nil {
			return err
		}
	}
	var userQueued, globalQueued, diskPressure int
	err = tx.QueryRow(ctx, `SELECT
      count(*) FILTER (WHERE owner_user_id=$1 AND state='QUEUED')::integer,
      count(*) FILTER (WHERE state='QUEUED')::integer,
      (SELECT count(*)::integer FROM circuit_breaker_events WHERE breaker_kind='disk_failure')
      FROM jobs`, owner).Scan(&userQueued, &globalQueued, &diskPressure)
	if err != nil {
		return err
	}
	if userQueued >= 4 || globalQueued >= 32 || diskPressure > 0 {
		return console.ErrQuota
	}
	return nil
}

func validateA11IncidentReplay(ctx context.Context, tx pgx.Tx, request generated.ReplayJobRequest) error {
	if request.IncidentId == nil || *request.IncidentId == "" || request.FirstOrdinal == nil || request.LastOrdinal == nil {
		return console.ErrPrecondition
	}
	datasetID, first, last, err := a11IncidentReplayWindow(ctx, tx, *request.IncidentId)
	if err != nil {
		return err
	}
	if datasetID != request.DatasetId || strconv.FormatInt(first, 10) != *request.FirstOrdinal ||
		strconv.FormatInt(last, 10) != *request.LastOrdinal {
		return console.ErrPrecondition
	}
	return nil
}

func a11JobReferences(request any) (string, string, string, string, bool) {
	switch value := request.(type) {
	case generated.OfflineJobRequest:
		return value.ConfigurationId, value.DatasetId, a11StrategyVersionID(string(value.StrategyVersion)), value.ResearchGenerationId,
			value.RootSeedHash != ""
	case generated.ReplayJobRequest:
		return value.ConfigurationId, value.DatasetId, a11StrategyVersionID(string(value.StrategyVersion)), value.ResearchGenerationId,
			value.RootSeedHash != ""
	default:
		return "", "", "", "", false
	}
}

func a11StrategyVersionID(value string) string {
	if value == "trend.v1a.1" {
		return "trend-v1a-1"
	}
	return value
}
