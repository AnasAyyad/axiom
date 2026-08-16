package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"time"

	"axiom/internal/domain"
	"axiom/internal/evaluation"
	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EvaluationHistoricalSegmentStore is the DB half of raw/canonical Parquet
// finalization. Its rows are immutable in migration 55.
type EvaluationHistoricalSegmentStore struct {
	pool  *pgxpool.Pool
	clock domain.Clock
}

// NewEvaluationHistoricalSegmentStore constructs immutable import-segment storage.
func NewEvaluationHistoricalSegmentStore(pool *pgxpool.Pool,
	clock domain.Clock) (*EvaluationHistoricalSegmentStore, error) {
	if pool == nil || clock == nil {
		return nil, fmt.Errorf("evaluation_historical_segment_store_dependencies_missing")
	}
	return &EvaluationHistoricalSegmentStore{pool: pool, clock: clock}, nil
}

// CommitHistoricalSegment atomically records one immutable raw or canonical page artifact.
func (store *EvaluationHistoricalSegmentStore) CommitHistoricalSegment(ctx context.Context, importID string,
	pageStart time.Time, kind string, manifest segments.Manifest) error {
	if importID == "" || pageStart.IsZero() || pageStart.Location() != time.UTC ||
		(kind != "wire" && kind != "canonical") || manifest.Spec.Name == "" || manifest.Path == "" ||
		filepath.Base(manifest.Spec.Name) != manifest.Spec.Name || filepath.Base(manifest.Path) != manifest.Path ||
		manifest.Format != "parquet" || manifest.Compression != "zstd" ||
		manifest.Spec.RecordCount != 1 || manifest.Spec.FirstOrdinal == 0 ||
		manifest.Spec.FirstOrdinal != manifest.Spec.LastOrdinal || manifest.Spec.FirstOrdinal > math.MaxInt64 ||
		manifest.Spec.StartedAt.IsZero() || manifest.Spec.StartedAt.Location() != time.UTC ||
		manifest.Spec.EndedAt.IsZero() || manifest.Spec.EndedAt.Location() != time.UTC ||
		manifest.Spec.EndedAt.Before(manifest.Spec.StartedAt) || manifest.Size <= 0 {
		return fmt.Errorf("evaluation_historical_segment_invalid")
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("evaluation_historical_segment_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	finalizedAt := store.clock.Now().UTC
	if err = registerHistoricalMarketSegment(ctx, tx, importID, kind, manifest, finalizedAt); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO evaluation_historical_import_segments(
	  import_id,page_start,kind,segment_id,manifest_payload,created_at)
	  VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (import_id,page_start,kind) DO NOTHING`,
		importID, pageStart, kind, manifest.Spec.Name, payload, finalizedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var segmentID string
		var existing []byte
		if err = tx.QueryRow(ctx, `SELECT segment_id,manifest_payload
		  FROM evaluation_historical_import_segments WHERE import_id=$1 AND page_start=$2 AND kind=$3`,
			importID, pageStart, kind).Scan(&segmentID, &existing); err != nil ||
			segmentID != manifest.Spec.Name || !bytes.Equal(existing, payload) {
			return fmt.Errorf("evaluation_historical_segment_immutable_conflict")
		}
	}
	return tx.Commit(ctx)
}

func registerHistoricalMarketSegment(ctx context.Context, tx pgx.Tx, importID, kind string,
	manifest segments.Manifest, finalizedAt time.Time) error {
	// Aggregate dataset registration links through dataset_segments, whose
	// foreign key intentionally accepts only shared, ready segment identities.
	// Register the historical artifact in that catalogue in the same transaction
	// as its import-specific evidence so neither side can become authoritative alone.
	var sessionID, exchangeID, instrumentID string
	if err := tx.QueryRow(ctx, `SELECT imported.session_id,imported.exchange_id,market.id
FROM evaluation_historical_imports imported
JOIN instruments market ON market.base_asset=split_part(imported.instrument,'/',1)
 AND market.quote_asset=split_part(imported.instrument,'/',2) AND market.product='spot'
WHERE imported.id=$1`, importID).Scan(&sessionID, &exchangeID, &instrumentID); err != nil {
		return fmt.Errorf("evaluation_historical_segment_import_missing")
	}
	eventType := "historical_candle_" + kind
	tag, err := tx.Exec(ctx, `INSERT INTO market_data_segments(
id,recorder_session,exchange_id,instrument_id,event_type,schema_version,parser_version,normalization_version,
compression,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,
started_at,ended_at,state,finalized_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,'zstd',$9,$10,$11,1,$12,$12,$13,$14,'ready',$15)
ON CONFLICT (id) DO NOTHING`, manifest.Spec.Name, sessionID, exchangeID, instrumentID, eventType,
		manifest.Spec.SchemaVersion, manifest.Spec.ParserVersion, manifest.Spec.NormalizationVersion,
		manifest.Path, manifest.Checksum, manifest.OrderedContentHash, int64(manifest.Spec.FirstOrdinal),
		manifest.Spec.StartedAt.UTC(), manifest.Spec.EndedAt.UTC(), finalizedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var storedSession, storedExchange, storedInstrument, storedEvent, schema, parser, normalizer string
	var path, checksum, ordered, state string
	var records, first, last int64
	err = tx.QueryRow(ctx, `SELECT recorder_session,exchange_id,instrument_id,event_type,schema_version,
parser_version,normalization_version,path,checksum,ordered_content_hash,record_count,first_ordinal,last_ordinal,state
FROM market_data_segments WHERE id=$1`, manifest.Spec.Name).Scan(&storedSession, &storedExchange,
		&storedInstrument, &storedEvent, &schema, &parser, &normalizer, &path, &checksum, &ordered,
		&records, &first, &last, &state)
	if err != nil || storedSession != sessionID || storedExchange != exchangeID || storedInstrument != instrumentID ||
		storedEvent != eventType || schema != manifest.Spec.SchemaVersion || parser != manifest.Spec.ParserVersion ||
		normalizer != manifest.Spec.NormalizationVersion || path != manifest.Path || checksum != manifest.Checksum ||
		ordered != manifest.OrderedContentHash || records != 1 || first != int64(manifest.Spec.FirstOrdinal) ||
		last != first || state != "ready" {
		return fmt.Errorf("evaluation_historical_segment_immutable_conflict")
	}
	return nil
}

// HistoricalPageSegments returns the raw and canonical artifacts for one imported page.
func (store *EvaluationHistoricalSegmentStore) HistoricalPageSegments(ctx context.Context, importID string,
	pageStart time.Time) ([]evaluation.HistoricalStoredSegment, error) {
	return store.read(ctx, `SELECT page_start,kind,manifest_payload
	  FROM evaluation_historical_import_segments WHERE import_id=$1 AND page_start=$2
	  ORDER BY CASE kind WHEN 'wire' THEN 0 ELSE 1 END`, importID, pageStart)
}

// HistoricalImportSegments returns every immutable artifact for one import task.
func (store *EvaluationHistoricalSegmentStore) HistoricalImportSegments(ctx context.Context,
	importID string) ([]evaluation.HistoricalStoredSegment, error) {
	return store.read(ctx, `SELECT page_start,kind,manifest_payload
	  FROM evaluation_historical_import_segments WHERE import_id=$1
	  ORDER BY page_start,CASE kind WHEN 'wire' THEN 0 ELSE 1 END`, importID)
}

func (store *EvaluationHistoricalSegmentStore) read(ctx context.Context, query string,
	arguments ...any) ([]evaluation.HistoricalStoredSegment, error) {
	rows, err := store.pool.Query(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]evaluation.HistoricalStoredSegment, 0)
	for rows.Next() {
		var item evaluation.HistoricalStoredSegment
		var payload []byte
		if err = rows.Scan(&item.PageStart, &item.Kind, &payload); err != nil ||
			json.Unmarshal(payload, &item.Manifest) != nil || item.Manifest.Spec.Name == "" {
			return nil, fmt.Errorf("evaluation_historical_segment_record_invalid")
		}
		item.PageStart = item.PageStart.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}

var _ evaluation.HistoricalSegmentRepository = (*EvaluationHistoricalSegmentStore)(nil)
