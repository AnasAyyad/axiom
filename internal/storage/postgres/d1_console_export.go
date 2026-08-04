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

// CreateD1Export creates one redacted hash-sealed seven-day artifact.
func (store *A11ConsoleStore) CreateD1Export(
	ctx context.Context,
	principal authentication.Principal,
	key string,
	request generated.ExportRequest,
) (generated.ExportArtifact, error) {
	payload, hash, err := a11CommandPayload(request)
	if err != nil || !request.Format.Valid() || !request.ResourceType.Valid() || request.Reason == "" {
		return generated.ExportArtifact{}, console.ErrInvalidRequest
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.ExportArtifact{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	artifact, err := store.createD1ExportTx(ctx, tx, principal, key, request, string(payload), hash)
	if err != nil {
		return generated.ExportArtifact{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return generated.ExportArtifact{}, err
	}
	return artifact, nil
}

func (store *A11ConsoleStore) createD1ExportTx(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	key string, request generated.ExportRequest, requestPayload, requestHash string,
) (generated.ExportArtifact, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		a11Dedupe(principal.UserID, key)); err != nil {
		return generated.ExportArtifact{}, err
	}
	if artifact, found, err := d1ReplayExport(ctx, tx, principal.UserID, key, requestHash); err != nil || found {
		return artifact, err
	}
	if err := store.d1CheckExportCapacity(ctx, tx, principal.UserID); err != nil {
		return generated.ExportArtifact{}, err
	}
	content, contentType, err := d1ExportContent(ctx, tx, request)
	if err != nil {
		return generated.ExportArtifact{}, err
	}
	now := store.clock.Now().UTC
	commandID, _ := a11Identifier("command")
	auditID, _ := a11Identifier("audit")
	artifactID, _ := a11Identifier("export")
	jobID, _ := a11Identifier("export-job")
	if err = insertA11Command(ctx, tx, commandID, principal, key, requestHash, "export.create",
		"export", artifactID, request.Reason, now, auditID, commandID); err != nil {
		return generated.ExportArtifact{}, err
	}
	if err = insertD1ExportJob(ctx, tx, principal.UserID, key, jobID,
		requestPayload, requestHash, now); err != nil {
		return generated.ExportArtifact{}, err
	}
	contentHash := a11Hash([]byte(content))
	if err = insertD1ExportArtifact(ctx, tx, principal.UserID, commandID, jobID, artifactID,
		request, content, contentType, contentHash, now); err != nil {
		return generated.ExportArtifact{}, err
	}
	if err = recordD1ExportCreation(ctx, tx, principal.UserID, artifactID, request.Reason, now); err != nil {
		return generated.ExportArtifact{}, err
	}
	if err = completeD1ExportJob(ctx, tx, jobID, artifactID, contentHash, now); err != nil {
		return generated.ExportArtifact{}, err
	}
	if _, err = completeA11Command(ctx, tx, commandID, auditID, principal, "export.create",
		artifactID, requestHash, map[string]any{"artifact_id": artifactID, "content_hash": contentHash},
		now, commandID); err != nil {
		return generated.ExportArtifact{}, err
	}
	return d1ReadExport(ctx, tx, artifactID)
}

func insertD1ExportJob(
	ctx context.Context, tx pgx.Tx, userID, key, jobID, payload, payloadHash string, now time.Time,
) error {
	if _, err := tx.Exec(ctx, `INSERT INTO jobs(
  id,job_type,idempotency_key,state,payload_hash,created_at,updated_at,
  owner_user_id,request_payload,max_attempts
) VALUES ($1,'export',$2,'QUEUED',$3,$4,$4,$5,$6,1)`, jobID,
		a11Dedupe(userID, key+":export-job"), payloadHash, now, userID, payload); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE jobs SET state='RUNNING',claim_owner='d1-export',
  claim_epoch=1,claim_expires_at=$2::timestamptz+interval '5 minutes',progress_revision=2,
  started_at=$2,updated_at=$2 WHERE id=$1`, jobID, now)
	return err
}

func completeD1ExportJob(
	ctx context.Context, tx pgx.Tx, jobID, artifactID, contentHash string, now time.Time,
) error {
	_, err := tx.Exec(ctx, `UPDATE jobs SET state='SUCCEEDED',claim_owner=NULL,
  claim_epoch=NULL,claim_expires_at=NULL,progress_revision=3,completed_at=$4,updated_at=$4,
  result_payload=jsonb_build_object('artifact_id',$2::text,'content_hash',$3::text)
WHERE id=$1`, jobID, artifactID, contentHash, now)
	return err
}

func d1ReplayExport(
	ctx context.Context, tx pgx.Tx, userID, key, requestHash string,
) (generated.ExportArtifact, bool, error) {
	existing, found, err := lookupA11Command(ctx, tx, userID, key, requestHash)
	if err != nil || !found {
		return generated.ExportArtifact{}, found, err
	}
	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM v1d_export_artifacts WHERE command_id=$1`, existing.Id).Scan(&id); err != nil {
		return generated.ExportArtifact{}, true, err
	}
	artifact, err := d1ReadExport(ctx, tx, id)
	return artifact, true, err
}

func (store *A11ConsoleStore) d1CheckExportCapacity(ctx context.Context, tx pgx.Tx, userID string) error {
	var userCount, globalCount int
	err := tx.QueryRow(ctx, `SELECT
      count(*) FILTER (WHERE owner_user_id=$1 AND deleted_at IS NULL AND expires_at>$2)::integer,
	  count(*) FILTER (WHERE deleted_at IS NULL AND expires_at>$2)::integer
    FROM v1d_export_artifacts`, userID, store.clock.Now().UTC).Scan(
		&userCount, &globalCount,
	)
	if err != nil {
		return err
	}
	storageReady, err := d5HeavyWorkAllowed(ctx, tx)
	if err != nil {
		return err
	}
	if userCount >= 20 || globalCount >= 100 || !storageReady {
		return console.ErrQuota
	}
	return nil
}

func d1ExportContent(
	ctx context.Context, tx pgx.Tx, request generated.ExportRequest,
) (string, string, error) {
	revision, err := positiveD1RequestRevision(request.ExpectedRevision)
	if err != nil {
		return "", "", err
	}
	record, err := d1ExportRecord(ctx, tx, string(request.ResourceType), request.ResourceId, revision)
	if err != nil {
		return "", "", err
	}
	content, contentType, err := encodeD1Export(record, string(request.Format))
	if err != nil || len(content) == 0 || len(content) > 10<<20 {
		return "", "", console.ErrInvalidRequest
	}
	return content, contentType, nil
}

func insertD1ExportArtifact(
	ctx context.Context, tx pgx.Tx, userID, commandID, jobID, artifactID string,
	request generated.ExportRequest, content, contentType, contentHash string, now time.Time,
) error {
	_, err := tx.Exec(ctx, `
INSERT INTO v1d_export_artifacts(
  id,command_id,job_id,owner_user_id,resource_type,resource_id,format,content_type,
  content,content_hash,size_bytes,redaction_version,created_at,expires_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'v1d.redaction.v1',$12,$13)`,
		artifactID, commandID, jobID, userID, request.ResourceType, request.ResourceId,
		request.Format, contentType, content, contentHash, len(content), now, now.Add(7*24*time.Hour))
	return err
}

func recordD1ExportCreation(
	ctx context.Context, tx pgx.Tx, userID, artifactID, reason string, now time.Time,
) error {
	eventID, _ := a11Identifier("artifact-access")
	_, err := tx.Exec(ctx, `
INSERT INTO v1d_artifact_access_events(
  id,artifact_id,actor_user_id,action,reason,correlation_id,occurred_at
) VALUES ($1,$2,$3,'created',$4,$1,$5)`, eventID, artifactID,
		userID, reason, now)
	return err
}

func positiveD1RequestRevision(value generated.Revision) (int64, error) {
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision <= 0 {
		return 0, console.ErrInvalidRequest
	}
	return revision, nil
}
