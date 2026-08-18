package postgres

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const recordedSegmentCommitAttempts = 8

type recordedSegmentCommitAttempt func(context.Context, string, string, segments.Manifest, time.Time) error
type recordedSegmentRetryWait func(context.Context, time.Duration) error

// RecordedSegmentCommitter atomically registers a finalized recorder segment
// and, when the session belongs to an active evaluation campaign, charges its
// immutable byte evidence to that campaign. Exact replays are idempotent;
// conflicting segment identities fail closed.
type RecordedSegmentCommitter struct {
	pool    *pgxpool.Pool
	attempt recordedSegmentCommitAttempt
	wait    recordedSegmentRetryWait
}

// NewRecordedSegmentCommitter constructs the atomic recorder-segment registrar.
func NewRecordedSegmentCommitter(pool *pgxpool.Pool) (*RecordedSegmentCommitter, error) {
	if pool == nil {
		return nil, fmt.Errorf("recorded_segment_committer_pool_missing")
	}
	return &RecordedSegmentCommitter{pool: pool}, nil
}

// Commit registers a finalized segment and charges active campaign storage exactly once.
func (store *RecordedSegmentCommitter) Commit(ctx context.Context, session, exchange string,
	manifest segments.Manifest, finalizedAt time.Time) error {
	if session == "" || (exchange != "binance" && exchange != "bybit") ||
		manifest.Spec.Name == "" || manifest.Size <= 0 || finalizedAt.IsZero() ||
		finalizedAt.Location() != time.UTC || manifest.Spec.RecordCount > math.MaxInt64 ||
		manifest.Spec.FirstOrdinal > math.MaxInt64 || manifest.Spec.LastOrdinal > math.MaxInt64 {
		return fmt.Errorf("recorded_segment_commit_invalid")
	}
	attempt := store.attempt
	if attempt == nil {
		attempt = store.commitOnce
	}
	wait := store.wait
	if wait == nil {
		wait = waitForRecordedSegmentRetry
	}
	for ordinal := 0; ordinal < recordedSegmentCommitAttempts; ordinal++ {
		err := attempt(ctx, session, exchange, manifest, finalizedAt)
		if err == nil || !isRetryableRecordedSegmentCommit(err) || ordinal == recordedSegmentCommitAttempts-1 {
			return err
		}
		if err = wait(ctx, recordedSegmentRetryDelay(session, exchange, manifest.Spec.Name, ordinal)); err != nil {
			return err
		}
	}
	return fmt.Errorf("recorded_segment_commit_retry_exhausted")
}

func recordedSegmentRetryDelay(session, exchange, name string, ordinal int) time.Duration {
	base := time.Duration(1<<ordinal) * 50 * time.Millisecond
	hash := fnv.New64a()
	_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%d", session, exchange, name, ordinal)
	return base + time.Duration(hash.Sum64()%uint64(base/4+1))
}

func (store *RecordedSegmentCommitter) commitOnce(ctx context.Context, session, exchange string,
	manifest segments.Manifest, finalizedAt time.Time) error {
	// The request row is the serialization point for campaign byte accounting.
	// READ COMMITTED lets PostgreSQL safely proceed or wait through a concurrent
	// campaign-row update; SERIALIZABLE aborted the foreign-key check instead
	// and could exhaust a short retry window while the owner worker held it.
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var campaignID string
	campaignErr := tx.QueryRow(ctx, `SELECT campaign_id FROM evaluation_recorder_requests
WHERE state IN ('ACTIVE','FINALIZING') AND (binance_session_id=$1 OR bybit_session_id=$1)
FOR UPDATE`, session).Scan(&campaignID)
	if campaignErr != nil && campaignErr != pgx.ErrNoRows {
		return campaignErr
	}
	if err = registerRecordedSegment(ctx, tx, session, exchange, manifest, finalizedAt); err != nil {
		return err
	}
	if campaignErr == nil {
		if err = chargeEvaluationSegment(ctx, tx, campaignID, manifest, finalizedAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func registerRecordedSegment(ctx context.Context, tx pgx.Tx, session, exchange string,
	manifest segments.Manifest, finalizedAt time.Time) error {
	tag, err := tx.Exec(ctx, `INSERT INTO market_data_segments(
  id,recorder_session,exchange_id,event_type,schema_version,parser_version,normalization_version,
  compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,
  started_at,ended_at,state,finalized_at)
VALUES($1,$2,$3,'mixed_public',$4,$5,$6,'zstd',$7,$8,$9,$10,$11,$12,$13,$14,'ready',$15)
ON CONFLICT (id) DO NOTHING`, manifest.Spec.Name, session, exchange, manifest.Spec.SchemaVersion,
		manifest.Spec.ParserVersion, manifest.Spec.NormalizationVersion, manifest.Path, manifest.Checksum,
		manifest.OrderedContentHash, int64(manifest.Spec.RecordCount), int64(manifest.Spec.FirstOrdinal),
		int64(manifest.Spec.LastOrdinal), manifest.Spec.StartedAt.UTC(), manifest.Spec.EndedAt.UTC(), finalizedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if err = verifyRecordedSegmentIdentity(ctx, tx, session, exchange, manifest); err != nil {
			return fmt.Errorf("recorded_segment_immutable_conflict")
		}
	}
	return nil
}

func isRetryableRecordedSegmentCommit(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && (postgresError.Code == "40001" || postgresError.Code == "40P01")
}

// QuarantineRecorderArtifacts preserves a startup-recovered recorder incident.
// Proof-backed files are registered and charged before every unreferenced
// segment is moved out of ready service. Replays are idempotent.
func (store *RecordedSegmentCommitter) QuarantineRecorderArtifacts(ctx context.Context,
	session, exchange string, names []string, proven []segments.Manifest, quarantinedAt time.Time) error {
	if err := validateRecorderArtifacts(session, exchange, names, proven, quarantinedAt); err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	campaignID, campaignFound, err := lockRecorderCampaign(ctx, tx, session)
	if err != nil {
		return err
	}
	if err = registerQuarantinedEvidence(ctx, tx, session, exchange, campaignID,
		campaignFound, proven, quarantinedAt); err != nil {
		return err
	}
	if err = quarantineRecordedSegmentRows(ctx, tx, session, names); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func validateRecorderArtifacts(session, exchange string, names []string,
	proven []segments.Manifest, quarantinedAt time.Time) error {
	if session == "" || (exchange != "binance" && exchange != "bybit") || len(names) == 0 ||
		quarantinedAt.IsZero() || quarantinedAt.Location() != time.UTC {
		return fmt.Errorf("recorded_segment_quarantine_invalid")
	}
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		if !segments.ValidName(name) || !strings.HasPrefix(name, session+"-") {
			return fmt.Errorf("recorded_segment_quarantine_invalid")
		}
		allowed[name] = struct{}{}
	}
	for _, manifest := range proven {
		if _, ok := allowed[manifest.Spec.Name]; !ok || manifest.Size <= 0 {
			return fmt.Errorf("recorded_segment_quarantine_invalid")
		}
	}
	return nil
}

func lockRecorderCampaign(ctx context.Context, tx pgx.Tx, session string) (string, bool, error) {
	var campaignID string
	err := tx.QueryRow(ctx, `SELECT campaign_id FROM evaluation_recorder_requests
WHERE state IN ('ACTIVE','PAUSED','FINALIZING') AND (binance_session_id=$1 OR bybit_session_id=$1)
FOR UPDATE`, session).Scan(&campaignID)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	return campaignID, err == nil, err
}

func registerQuarantinedEvidence(ctx context.Context, tx pgx.Tx, session, exchange, campaignID string,
	campaignFound bool, proven []segments.Manifest, quarantinedAt time.Time) error {
	for _, manifest := range proven {
		var state string
		stateErr := tx.QueryRow(ctx, `SELECT state FROM market_data_segments WHERE id=$1`, manifest.Spec.Name).Scan(&state)
		if stateErr == pgx.ErrNoRows {
			if err := registerRecordedSegment(ctx, tx, session, exchange, manifest, quarantinedAt); err != nil {
				return err
			}
		} else if stateErr != nil || verifyRecordedSegmentIdentityStates(ctx, tx, session, exchange,
			manifest, "ready", "quarantined") != nil {
			return fmt.Errorf("recorded_segment_immutable_conflict")
		}
		if campaignFound {
			if err := chargeEvaluationSegment(ctx, tx, campaignID, manifest, quarantinedAt); err != nil {
				return err
			}
		}
	}
	return nil
}

func quarantineRecordedSegmentRows(ctx context.Context, tx pgx.Tx, session string, names []string) error {
	var referenced int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM dataset_segments
WHERE segment_id=ANY($1::text[])`, names).Scan(&referenced); err != nil {
		return err
	}
	if referenced != 0 {
		return fmt.Errorf("recorded_segment_quarantine_referenced")
	}
	if _, err := tx.Exec(ctx, `UPDATE market_data_segments SET state='quarantined'
WHERE recorder_session=$1 AND id=ANY($2::text[]) AND state='ready'`, session, names); err != nil {
		return err
	}
	var foreign int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM market_data_segments
WHERE id=ANY($1::text[]) AND recorder_session<>$2`, names, session).Scan(&foreign); err != nil || foreign != 0 {
		return fmt.Errorf("recorded_segment_quarantine_identity_conflict")
	}
	return nil
}

func waitForRecordedSegmentRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func verifyRecordedSegmentIdentity(ctx context.Context, tx pgx.Tx, session, exchange string,
	manifest segments.Manifest) error {
	return verifyRecordedSegmentIdentityStates(ctx, tx, session, exchange, manifest, "ready")
}

func verifyRecordedSegmentIdentityStates(ctx context.Context, tx pgx.Tx, session, exchange string,
	manifest segments.Manifest, allowedStates ...string) error {
	var storedSession, storedExchange, schema, parser, normalizer, path, checksum, ordered, state string
	var records, first, last int64
	err := tx.QueryRow(ctx, `SELECT recorder_session,exchange_id,schema_version,parser_version,
  normalization_version,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,state
FROM market_data_segments WHERE id=$1`, manifest.Spec.Name).Scan(&storedSession, &storedExchange, &schema,
		&parser, &normalizer, &path, &checksum, &ordered, &records, &first, &last, &state)
	if err != nil || storedSession != session || storedExchange != exchange || schema != manifest.Spec.SchemaVersion ||
		parser != manifest.Spec.ParserVersion || normalizer != manifest.Spec.NormalizationVersion ||
		path != manifest.Path || checksum != manifest.Checksum || ordered != manifest.OrderedContentHash ||
		records != int64(manifest.Spec.RecordCount) || first != int64(manifest.Spec.FirstOrdinal) ||
		last != int64(manifest.Spec.LastOrdinal) || !containsRecordedSegmentState(allowedStates, state) {
		return fmt.Errorf("recorded_segment_immutable_conflict")
	}
	return nil
}

func containsRecordedSegmentState(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func chargeEvaluationSegment(ctx context.Context, tx pgx.Tx, campaignID string,
	manifest segments.Manifest, recordedAt time.Time) error {
	tag, err := tx.Exec(ctx, `INSERT INTO evaluation_campaign_recording_segments(
  campaign_id,segment_id,byte_count,recorded_at) VALUES($1,$2,$3,$4)
ON CONFLICT (campaign_id,segment_id) DO NOTHING`, campaignID, manifest.Spec.Name, manifest.Size, recordedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var size int64
		if err = tx.QueryRow(ctx, `SELECT byte_count FROM evaluation_campaign_recording_segments
  WHERE campaign_id=$1 AND segment_id=$2`, campaignID, manifest.Spec.Name).Scan(&size); err != nil ||
			size != manifest.Size {
			return fmt.Errorf("evaluation_recorder_segment_immutable_conflict")
		}
	}
	var recorded int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(byte_count),0)
FROM evaluation_campaign_recording_segments WHERE campaign_id=$1`, campaignID).Scan(&recorded); err != nil {
		return err
	}
	if recorded < 0 || recorded > evaluationRecordingLimitBytes {
		return fmt.Errorf("evaluation_recorder_storage_cap_exceeded")
	}
	_, err = tx.Exec(ctx, `UPDATE evaluation_recorder_requests SET recorded_bytes=$2,updated_at=$3
WHERE campaign_id=$1`, campaignID, recorded, recordedAt)
	return err
}
