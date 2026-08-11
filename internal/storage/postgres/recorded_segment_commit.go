package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RecordedSegmentCommitter atomically registers a finalized recorder segment
// and, when the session belongs to an active evaluation campaign, charges its
// immutable byte evidence to that campaign. Exact replays are idempotent;
// conflicting segment identities fail closed.
type RecordedSegmentCommitter struct{ pool *pgxpool.Pool }

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
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
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
	var campaignID string
	err = tx.QueryRow(ctx, `SELECT campaign_id FROM evaluation_recorder_requests WHERE state='ACTIVE'
  AND (binance_session_id=$1 OR bybit_session_id=$1) FOR UPDATE`, session).Scan(&campaignID)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == nil {
		if err = chargeEvaluationSegment(ctx, tx, campaignID, manifest, finalizedAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func verifyRecordedSegmentIdentity(ctx context.Context, tx pgx.Tx, session, exchange string,
	manifest segments.Manifest) error {
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
		last != int64(manifest.Spec.LastOrdinal) || state != "ready" {
		return fmt.Errorf("recorded_segment_immutable_conflict")
	}
	return nil
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
