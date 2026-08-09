package postgres

import (
	"context"

	"strconv"

	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

// CreateOwnerControlExport creates one redacted hash-sealed seven-day artifact.
func (store *OwnerConsoleStore) CreateOwnerControlExport(
	ctx context.Context,
	principal authentication.Principal,
	key string,
	request generated.ExportRequest,
) (generated.ExportArtifact, error) {
	payload, hash, err := ownerConsoleCommandPayload(request)
	if err != nil || !request.Format.Valid() || !request.ResourceType.Valid() || request.Reason == "" {
		return generated.ExportArtifact{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.ExportArtifact{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	artifact, err := store.createOwnerControlExportTx(ctx, tx, principal, key, request, string(payload), hash)
	if err != nil {
		return generated.ExportArtifact{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return generated.ExportArtifact{}, err
	}
	return artifact, nil
}

func (store *OwnerConsoleStore) createOwnerControlExportTx(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	key string, request generated.ExportRequest, requestPayload, requestHash string,
) (generated.ExportArtifact, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		ownerConsoleDedupe(principal.UserID, key)); err != nil {
		return generated.ExportArtifact{}, err
	}
	if artifact, found, err := ownerControlReplayExport(ctx, tx, principal.UserID, key, requestHash); err != nil || found {
		return artifact, err
	}
	if err := store.ownerControlCheckExportCapacity(ctx, tx, principal.UserID); err != nil {
		return generated.ExportArtifact{}, err
	}
	content, contentType, err := ownerControlExportContent(ctx, tx, request)
	if err != nil {
		return generated.ExportArtifact{}, err
	}
	now := store.clock.Now().UTC
	commandID, _ := ownerConsoleIdentifier("command")
	auditID, _ := ownerConsoleIdentifier("audit")
	artifactID, _ := ownerConsoleIdentifier("export")
	jobID, _ := ownerConsoleIdentifier("export-job")
	if err = insertOwnerConsoleCommand(ctx, tx, commandID, principal, key, requestHash, "export.create",
		"export", artifactID, request.Reason, now, auditID, commandID); err != nil {
		return generated.ExportArtifact{}, err
	}
	if err = insertOwnerControlExportJob(ctx, tx, principal.UserID, key, jobID,
		requestPayload, requestHash, now); err != nil {
		return generated.ExportArtifact{}, err
	}
	contentHash := ownerConsoleHash([]byte(content))
	if err = insertOwnerControlExportArtifact(ctx, tx, principal.UserID, commandID, jobID, artifactID,
		request, content, contentType, contentHash, now); err != nil {
		return generated.ExportArtifact{}, err
	}
	if err = recordOwnerControlExportCreation(ctx, tx, principal.UserID, artifactID, request.Reason, now); err != nil {
		return generated.ExportArtifact{}, err
	}
	if err = completeOwnerControlExportJob(ctx, tx, jobID, artifactID, contentHash, now); err != nil {
		return generated.ExportArtifact{}, err
	}
	if _, err = completeOwnerConsoleCommand(ctx, tx, commandID, auditID, principal, "export.create",
		artifactID, requestHash, map[string]any{"artifact_id": artifactID, "content_hash": contentHash},
		now, commandID); err != nil {
		return generated.ExportArtifact{}, err
	}
	return ownerControlReadExport(ctx, tx, artifactID)
}

func insertOwnerControlExportJob(
	ctx context.Context, tx pgx.Tx, userID, key, jobID, payload, payloadHash string, now time.Time,
) error {
	if _, err := tx.Exec(ctx, `INSERT INTO jobs(
  id,job_type,idempotency_key,state,payload_hash,created_at,updated_at,
  owner_user_id,request_payload,max_attempts
) VALUES ($1,'export',$2,'QUEUED',$3,$4,$4,$5,$6,1)`, jobID,
		ownerConsoleDedupe(userID, key+":export-job"), payloadHash, now, userID, payload); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE jobs SET state='RUNNING',claim_owner='owner_control-export',
  claim_epoch=1,claim_expires_at=$2::timestamptz+interval '5 minutes',progress_revision=2,
  started_at=$2,updated_at=$2 WHERE id=$1`, jobID, now)
	return err
}

func completeOwnerControlExportJob(
	ctx context.Context, tx pgx.Tx, jobID, artifactID, contentHash string, now time.Time,
) error {
	_, err := tx.Exec(ctx, `UPDATE jobs SET state='SUCCEEDED',claim_owner=NULL,
  claim_epoch=NULL,claim_expires_at=NULL,progress_revision=3,completed_at=$4,updated_at=$4,
  result_payload=jsonb_build_object('artifact_id',$2::text,'content_hash',$3::text)
WHERE id=$1`, jobID, artifactID, contentHash, now)
	return err
}

func ownerControlReplayExport(
	ctx context.Context, tx pgx.Tx, userID, key, requestHash string,
) (generated.ExportArtifact, bool, error) {
	existing, found, err := lookupOwnerConsoleCommand(ctx, tx, userID, key, requestHash)
	if err != nil || !found {
		return generated.ExportArtifact{}, found, err
	}
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM owner_console_export_artifacts WHERE command_id=$1`, existing.Id).Scan(&id); err != nil {
		return generated.ExportArtifact{}, true, err
	}
	artifact, err := ownerControlReadExport(ctx, tx, id)
	return artifact, true, err
}

func (store *OwnerConsoleStore) ownerControlCheckExportCapacity(ctx context.Context, tx pgx.Tx, userID string) error {
	var userCount, globalCount int
	err := tx.QueryRow(ctx, `SELECT
      count(*) FILTER (WHERE owner_user_id=$1 AND deleted_at IS NULL AND expires_at>$2)::integer,
	  count(*) FILTER (WHERE deleted_at IS NULL AND expires_at>$2)::integer
    FROM owner_console_export_artifacts`, userID, store.clock.Now().UTC).Scan(
		&userCount, &globalCount,
	)
	if err != nil {
		return err
	}
	storageReady, err := operationalReadinessHeavyWorkAllowed(ctx, tx)
	if err != nil {
		return err
	}
	if userCount >= 20 || globalCount >= 100 || !storageReady {
		return console.ErrQuota
	}
	return nil
}

func ownerControlExportContent(
	ctx context.Context, tx pgx.Tx, request generated.ExportRequest,
) (string, string, error) {
	revision, err := positiveOwnerControlRequestRevision(request.ExpectedRevision)
	if err != nil {
		return "", "", err
	}
	record, err := ownerControlExportRecord(ctx, tx, string(request.ResourceType), request.ResourceId, revision)
	if err != nil {
		return "", "", err
	}
	content, contentType, err := encodeOwnerControlExport(record, string(request.Format))
	if err != nil || len(content) == 0 || len(content) > 10<<20 {
		return "", "", console.ErrInvalidRequest
	}
	return content, contentType, nil
}

func insertOwnerControlExportArtifact(
	ctx context.Context, tx pgx.Tx, userID, commandID, jobID, artifactID string,
	request generated.ExportRequest, content, contentType, contentHash string, now time.Time,
) error {
	_, err := tx.Exec(ctx, `
INSERT INTO owner_console_export_artifacts(
  id,command_id,job_id,owner_user_id,resource_type,resource_id,format,content_type,
  content,content_hash,size_bytes,redaction_version,created_at,expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'owner_console.redaction.v1',$12,$13)`,
		artifactID, commandID, jobID, userID, request.ResourceType, request.ResourceId,
		request.Format, contentType, content, contentHash, len(content), now, now.Add(7*24*time.Hour))
	return err
}

func recordOwnerControlExportCreation(
	ctx context.Context, tx pgx.Tx, userID, artifactID, reason string, now time.Time,
) error {
	eventID, _ := ownerConsoleIdentifier("artifact-access")
	_, err := tx.Exec(ctx, `
INSERT INTO owner_console_artifact_access_events(
  id,artifact_id,actor_user_id,action,reason,correlation_id,occurred_at
) VALUES ($1,$2,$3,'created',$4,$1,$5)`, eventID, artifactID,
		userID, reason, now)
	return err
}

func positiveOwnerControlRequestRevision(value generated.Revision) (int64, error) {
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 {
		return 0, console.ErrInvalidRequest
	}
	return revision, nil
}
