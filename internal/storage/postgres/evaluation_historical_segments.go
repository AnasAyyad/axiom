package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
		manifest.Spec.RecordCount != 1 || manifest.Size <= 0 {
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
	tag, err := tx.Exec(ctx, `INSERT INTO evaluation_historical_import_segments(
	  import_id,page_start,kind,segment_id,manifest_payload,created_at)
	  VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (import_id,page_start,kind) DO NOTHING`,
		importID, pageStart, kind, manifest.Spec.Name, payload, store.clock.Now().UTC)
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
