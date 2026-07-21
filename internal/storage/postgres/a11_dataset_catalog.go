package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"

	"axiom/internal/recorder"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A11DatasetCatalog binds recorder files to durable immutable dataset revisions.
type A11DatasetCatalog struct{ pool *pgxpool.Pool }

// NewA11DatasetCatalog constructs the recorder catalog boundary.
func NewA11DatasetCatalog(pool *pgxpool.Pool) (*A11DatasetCatalog, error) {
	if pool == nil {
		return nil, fmt.Errorf("a11_dataset_catalog_pool_missing")
	}
	return &A11DatasetCatalog{pool: pool}, nil
}

// Register records one cumulative recorder manifest and all referenced evidence.
func (catalog *A11DatasetCatalog) Register(ctx context.Context, manifest recorder.DatasetManifest, sourceCommit string) (string, error) {
	return catalog.register(ctx, manifest, sourceCommit, "public_market")
}

// RegisterDecisionInputs records a dataset that is safe to feed directly to
// the Trend operational processor.
func (catalog *A11DatasetCatalog) RegisterDecisionInputs(ctx context.Context, manifest recorder.DatasetManifest, sourceCommit string) (string, error) {
	return catalog.register(ctx, manifest, sourceCommit, "decision_inputs")
}

// QualifyDecisionInputs promotes only a complete, gap-free registered decision dataset.
func (catalog *A11DatasetCatalog) QualifyDecisionInputs(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("a11_decision_dataset_invalid")
	}
	tag, err := catalog.pool.Exec(ctx, `UPDATE dataset_manifests SET state='qualified' WHERE id=$1
      AND state='ready' AND dataset_kind='decision_inputs' AND NOT EXISTS
      (SELECT 1 FROM dataset_gaps WHERE dataset_id=$1)`, id)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("a11_decision_dataset_qualification_rejected")
	}
	return nil
}

func (catalog *A11DatasetCatalog) register(ctx context.Context, manifest recorder.DatasetManifest, sourceCommit, kind string) (string, error) {
	if !validA11RecorderManifest(manifest, sourceCommit) {
		return "", fmt.Errorf("a11_dataset_manifest_invalid")
	}
	tx, err := catalog.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	id := "dataset-" + manifest.Hash[:24]
	first := manifest.Segments[0].Manifest.Spec.StartedAt.UTC()
	last := manifest.Segments[len(manifest.Segments)-1].Manifest.Spec.EndedAt.UTC()
	path := fmt.Sprintf("%s-%06d.dataset.json", manifest.SessionID, manifest.Revision)
	_, err = tx.Exec(ctx, `INSERT INTO dataset_manifests(id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at,
      recorder_dataset_id,manifest_revision,manifest_path,source_commit,dataset_kind)
	  VALUES($1,$2,$3,$4,$5,'building',$6,$7,$8,$9,$10,$11) ON CONFLICT (id) DO NOTHING`, id, manifest.Hash,
		manifest.SchemaVersion, first, last, manifest.CreatedAt.UTC(), manifest.DatasetID, manifest.Revision, path, sourceCommit, kind)
	if err != nil {
		return "", err
	}
	for ordinal, reference := range manifest.Segments {
		if _, err = tx.Exec(ctx, `INSERT INTO dataset_segments(dataset_id,segment_id,ordinal) VALUES($1,$2,$3)
        ON CONFLICT (dataset_id,ordinal) DO NOTHING`, id, reference.Manifest.Spec.Name, ordinal); err != nil {
			return "", err
		}
	}
	for _, gap := range manifest.Gaps {
		gapID := a11DatasetGapID(id, gap)
		if _, err = tx.Exec(ctx, `INSERT INTO dataset_gaps(id,dataset_id,first_source_sequence,last_source_sequence,reason_code,detected_at)
		VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (id) DO NOTHING`, gapID, id,
			strconv.FormatUint(gap.FirstSourceSequence, 10), strconv.FormatUint(gap.LastSourceSequence, 10),
			gap.Reason, manifest.CreatedAt.UTC()); err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE dataset_manifests SET state='ready' WHERE id=$1 AND state='building'`, id); err != nil {
		return "", err
	}
	if err = verifyA11DatasetRegistration(ctx, tx, id, manifest, kind); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func validA11RecorderManifest(manifest recorder.DatasetManifest, sourceCommit string) bool {
	commit, err := hex.DecodeString(sourceCommit)
	return err == nil && (len(commit) == 20 || len(commit) == sha256.Size) && manifest.Hash != "" &&
		manifest.DatasetID != "" && manifest.SessionID != "" && manifest.Revision > 0 && len(manifest.Segments) > 0 &&
		filepath.Base(manifest.SessionID) == manifest.SessionID
}

func a11DatasetGapID(datasetID string, gap recorder.Gap) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%s", datasetID, gap.FirstSourceSequence, gap.LastSourceSequence, gap.Reason)))
	return "gap-" + hex.EncodeToString(digest[:12])
}

func verifyA11DatasetRegistration(ctx context.Context, tx pgx.Tx, id string, manifest recorder.DatasetManifest, kind string) error {
	var hash, recorderID, storedKind string
	var revision int64
	var segments int
	err := tx.QueryRow(ctx, `SELECT dm.dataset_hash,dm.recorder_dataset_id,dm.manifest_revision,dm.dataset_kind,count(ds.segment_id)
      FROM dataset_manifests dm LEFT JOIN dataset_segments ds ON ds.dataset_id=dm.id WHERE dm.id=$1
	  GROUP BY dm.id`, id).Scan(&hash, &recorderID, &revision, &storedKind, &segments)
	if err != nil || hash != manifest.Hash || recorderID != manifest.DatasetID || revision != int64(manifest.Revision) ||
		storedKind != kind || segments != len(manifest.Segments) {
		return fmt.Errorf("a11_dataset_registration_conflict")
	}
	return nil
}
