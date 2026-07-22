package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
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

// RegisterTierA atomically registers a verified multi-exchange aggregate, its
// exact child identities, inherited segments, and per-exchange coverage.
func (catalog *A11DatasetCatalog) RegisterTierA(ctx context.Context, manifest recorder.TierAManifest) (string, error) {
	if catalog == nil || catalog.pool == nil || recorder.ValidateTierAManifest(manifest) != nil {
		return "", fmt.Errorf("b2_tier_a_manifest_invalid")
	}
	tx, err := catalog.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return "", fmt.Errorf("b2_tier_a_catalog_unavailable")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	id := "dataset-" + manifest.Hash[:24]
	inserted, err := insertTierAHeader(ctx, tx, id, manifest)
	if err != nil {
		return "", err
	}
	if !inserted {
		if err = verifyB2TierARegistration(ctx, tx, id, manifest); err != nil {
			return "", err
		}
		return id, tx.Commit(ctx)
	}
	compatibility, err := json.Marshal(manifest.Compatibility)
	if err != nil {
		return "", fmt.Errorf("b2_tier_a_coverage_invalid")
	}
	if err = insertTierAManifestEvidence(ctx, tx, id, manifest, compatibility); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, "UPDATE dataset_manifests SET state='ready' WHERE id=$1", id); err != nil {
		return "", fmt.Errorf("b2_tier_a_completeness_rejected")
	}
	if _, err = tx.Exec(ctx, "UPDATE dataset_manifests SET state='qualified' WHERE id=$1", id); err != nil {
		return "", fmt.Errorf("b2_tier_a_qualification_rejected")
	}
	if err = tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("b2_tier_a_commit_rejected")
	}
	return id, nil
}

func insertTierAHeader(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	manifest recorder.TierAManifest,
) (bool, error) {
	coverageStart, coverageEnd := manifest.Members[0].Coverage.CoverageStart, manifest.Members[0].Coverage.CoverageEnd
	for _, member := range manifest.Members[1:] {
		if member.Coverage.CoverageStart.Before(coverageStart) {
			coverageStart = member.Coverage.CoverageStart
		}
		if member.Coverage.CoverageEnd.After(coverageEnd) {
			coverageEnd = member.Coverage.CoverageEnd
		}
	}
	tag, err := tx.Exec(ctx, `INSERT INTO dataset_manifests
(id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at,dataset_kind,
 manifest_schema_version,quality_tier)
VALUES($1,$2,$3,$4,$5,'building',$6,'public_market','axiom.multi-exchange-dataset.v1','A')
ON CONFLICT (id) DO NOTHING`,
		id, manifest.Hash, manifest.Compatibility.MinimumReaderVersion, coverageStart, coverageEnd, manifest.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("b2_tier_a_header_rejected")
	}
	return tag.RowsAffected() == 1, nil
}

func insertTierAManifestEvidence(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	manifest recorder.TierAManifest,
	compatibility []byte,
) error {
	segmentOrdinal := 0
	seenSegments := make(map[string]struct{})
	for memberOrdinal, member := range manifest.Members {
		childID, err := insertTierAMember(ctx, tx, id, memberOrdinal, member, compatibility)
		if err != nil {
			return err
		}
		segmentOrdinal, err = inheritTierASegments(ctx, tx, id, childID, segmentOrdinal, seenSegments)
		if err != nil {
			return err
		}
	}
	if segmentOrdinal == 0 {
		return fmt.Errorf("b2_tier_a_segments_missing")
	}
	return nil
}

func insertTierAMember(
	ctx context.Context,
	tx pgx.Tx,
	datasetID string,
	memberOrdinal int,
	member recorder.TierAMember,
	compatibility []byte,
) (string, error) {
	var childID string
	if err := tx.QueryRow(ctx, `SELECT id FROM dataset_manifests
WHERE dataset_hash=$1 AND state IN ('ready','qualified')`, member.ManifestHash).Scan(&childID); err != nil {
		return "", fmt.Errorf("b2_tier_a_child_missing")
	}
	if member.Revision > math.MaxInt64 || member.Verification.RecordCount > math.MaxInt64 ||
		member.Coverage.FirstOrdinal > math.MaxInt64 || member.Coverage.LastOrdinal > math.MaxInt64 ||
		member.Coverage.RawRecordCount > math.MaxInt64 || member.Coverage.CanonicalRecordCount > math.MaxInt64 {
		return "", fmt.Errorf("b2_tier_a_coverage_invalid")
	}
	_, err := tx.Exec(ctx, `INSERT INTO dataset_tier_a_members
(dataset_id,member_ordinal,exchange_id,member_dataset_id,member_manifest_hash,member_revision,replay_hash,record_count)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, datasetID, memberOrdinal, member.Exchange, childID, member.ManifestHash,
		int64(member.Revision), member.Verification.ReplaySHA256, int64(member.Verification.RecordCount))
	if err != nil {
		return "", fmt.Errorf("b2_tier_a_member_rejected")
	}
	generationHistory, err := json.Marshal(member.Coverage.GenerationHistory)
	if err != nil {
		return "", fmt.Errorf("b2_tier_a_coverage_invalid")
	}
	_, err = tx.Exec(ctx, `INSERT INTO dataset_exchange_coverage
(dataset_id,exchange_id,collector_instance,collector_region,coverage_start,coverage_end,first_ordinal,last_ordinal,
 generation_history,schema_versions,parser_versions,normalization_versions,compatibility_requirements,
 raw_record_count,canonical_record_count,raw_canonical_linkage_complete,hidden_gap_count,complete)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,true,0,true)`, datasetID, member.Exchange,
		member.Coverage.CollectorInstance, member.Coverage.CollectorRegion, member.Coverage.CoverageStart,
		member.Coverage.CoverageEnd, int64(member.Coverage.FirstOrdinal), int64(member.Coverage.LastOrdinal),
		generationHistory, member.Coverage.SchemaVersions, member.Coverage.ParserVersions,
		member.Coverage.NormalizationVersions, compatibility, int64(member.Coverage.RawRecordCount),
		int64(member.Coverage.CanonicalRecordCount))
	if err != nil {
		return "", fmt.Errorf("b2_tier_a_coverage_rejected")
	}
	return childID, nil
}

func inheritTierASegments(
	ctx context.Context,
	tx pgx.Tx,
	datasetID, childID string,
	nextOrdinal int,
	seen map[string]struct{},
) (int, error) {
	rows, err := tx.Query(ctx, `SELECT segment_id FROM dataset_segments
WHERE dataset_id=$1 ORDER BY ordinal`, childID)
	if err != nil {
		return nextOrdinal, fmt.Errorf("b2_tier_a_segments_missing")
	}
	segments := make([]string, 0)
	for rows.Next() {
		var segmentID string
		if err = rows.Scan(&segmentID); err != nil {
			rows.Close()
			return nextOrdinal, fmt.Errorf("b2_tier_a_segments_missing")
		}
		segments = append(segments, segmentID)
	}
	if rows.Err() != nil {
		rows.Close()
		return nextOrdinal, fmt.Errorf("b2_tier_a_segments_missing")
	}
	rows.Close()
	for _, segmentID := range segments {
		if _, duplicate := seen[segmentID]; duplicate {
			continue
		}
		if _, err = tx.Exec(ctx, `INSERT INTO dataset_segments(dataset_id,segment_id,ordinal)
VALUES($1,$2,$3)`, datasetID, segmentID, nextOrdinal); err != nil {
			return nextOrdinal, fmt.Errorf("b2_tier_a_segments_rejected")
		}
		seen[segmentID], nextOrdinal = struct{}{}, nextOrdinal+1
	}
	return nextOrdinal, nil
}

func verifyB2TierARegistration(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	manifest recorder.TierAManifest,
) error {
	var hash, state, schemaVersion, qualityTier string
	var members, coverages, segments int
	err := tx.QueryRow(ctx, `SELECT dataset_hash,state,manifest_schema_version,quality_tier,
  (SELECT count(*) FROM dataset_tier_a_members WHERE dataset_id=$1),
  (SELECT count(*) FROM dataset_exchange_coverage WHERE dataset_id=$1),
  (SELECT count(*) FROM dataset_segments WHERE dataset_id=$1)
FROM dataset_manifests WHERE id=$1`, id).Scan(&hash, &state, &schemaVersion, &qualityTier,
		&members, &coverages, &segments)
	if err != nil || hash != manifest.Hash || state != "qualified" ||
		schemaVersion != "axiom.multi-exchange-dataset.v1" || qualityTier != "A" ||
		members != len(manifest.Members) || coverages != len(manifest.Members) || segments == 0 {
		return fmt.Errorf("b2_tier_a_registration_conflict")
	}
	return nil
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
	var manifestSchemaVersion any
	if len(manifest.ExchangeCoverage) > 0 {
		manifestSchemaVersion = manifest.SchemaVersion
	}
	tag, err := tx.Exec(ctx, `INSERT INTO dataset_manifests(id,dataset_hash,schema_compatibility,coverage_start,coverage_end,state,created_at,
      recorder_dataset_id,manifest_revision,manifest_path,source_commit,dataset_kind,manifest_schema_version)
	  VALUES($1,$2,$3,$4,$5,'building',$6,$7,$8,$9,$10,$11,$12) ON CONFLICT (id) DO NOTHING`, id, manifest.Hash,
		manifest.SchemaVersion, first, last, manifest.CreatedAt.UTC(), manifest.DatasetID, manifest.Revision, path, sourceCommit, kind,
		manifestSchemaVersion)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		if err = verifyA11DatasetRegistration(ctx, tx, id, manifest, kind); err != nil {
			return "", err
		}
		return id, tx.Commit(ctx)
	}
	if err = insertA11DatasetEvidence(ctx, tx, id, manifest); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE dataset_manifests SET state='ready' WHERE id=$1 AND state='building'`, id); err != nil {
		return "", err
	}
	if err = verifyA11DatasetRegistration(ctx, tx, id, manifest, kind); err != nil {
		return "", err
	}
	return id, tx.Commit(ctx)
}

func insertA11DatasetEvidence(ctx context.Context, tx pgx.Tx, id string, manifest recorder.DatasetManifest) error {
	for ordinal, reference := range manifest.Segments {
		if _, err := tx.Exec(ctx, `INSERT INTO dataset_segments(dataset_id,segment_id,ordinal) VALUES($1,$2,$3)
        ON CONFLICT (dataset_id,ordinal) DO NOTHING`, id, reference.Manifest.Spec.Name, ordinal); err != nil {
			return err
		}
	}
	for _, gap := range manifest.Gaps {
		gapID := a11DatasetGapID(id, gap)
		if _, err := tx.Exec(ctx, `INSERT INTO dataset_gaps(id,dataset_id,first_source_sequence,last_source_sequence,reason_code,detected_at)
		VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (id) DO NOTHING`, gapID, id,
			strconv.FormatUint(gap.FirstSourceSequence, 10), strconv.FormatUint(gap.LastSourceSequence, 10),
			gap.Reason, manifest.CreatedAt.UTC()); err != nil {
			return err
		}
	}
	for _, coverage := range manifest.ExchangeCoverage {
		if err := insertA11DatasetCoverage(ctx, tx, id, coverage, manifest.Compatibility); err != nil {
			return err
		}
	}
	return nil
}

func insertA11DatasetCoverage(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	coverage recorder.ExchangeCoverage,
	compatibilityRequirements *recorder.CompatibilityRequirements,
) error {
	if coverage.FirstOrdinal > math.MaxInt64 || coverage.LastOrdinal > math.MaxInt64 ||
		coverage.RawRecordCount > math.MaxInt64 || coverage.CanonicalRecordCount > math.MaxInt64 ||
		coverage.HiddenGapCount > math.MaxInt64 || compatibilityRequirements == nil {
		return fmt.Errorf("a11_dataset_coverage_invalid")
	}
	generationHistory, err := json.Marshal(coverage.GenerationHistory)
	if err != nil {
		return fmt.Errorf("a11_dataset_coverage_invalid")
	}
	compatibility, err := json.Marshal(compatibilityRequirements)
	if err != nil {
		return fmt.Errorf("a11_dataset_coverage_invalid")
	}
	_, err = tx.Exec(ctx, `INSERT INTO dataset_exchange_coverage
(dataset_id,exchange_id,collector_instance,collector_region,coverage_start,coverage_end,first_ordinal,last_ordinal,
 generation_history,schema_versions,parser_versions,normalization_versions,compatibility_requirements,
 raw_record_count,canonical_record_count,raw_canonical_linkage_complete,hidden_gap_count,complete)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
ON CONFLICT (dataset_id,exchange_id) DO NOTHING`, id, coverage.Exchange, coverage.CollectorInstance,
		coverage.CollectorRegion, coverage.CoverageStart, coverage.CoverageEnd, int64(coverage.FirstOrdinal),
		int64(coverage.LastOrdinal), generationHistory, coverage.SchemaVersions, coverage.ParserVersions,
		coverage.NormalizationVersions, compatibility, int64(coverage.RawRecordCount),
		int64(coverage.CanonicalRecordCount), coverage.RawCanonicalLinkageComplete,
		int64(coverage.HiddenGapCount), coverage.Complete)
	return err
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
	if len(manifest.ExchangeCoverage) > 0 {
		var coverageCount int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM dataset_exchange_coverage WHERE dataset_id=$1`, id).
			Scan(&coverageCount); err != nil || coverageCount != len(manifest.ExchangeCoverage) {
			return fmt.Errorf("a11_dataset_registration_conflict")
		}
	}
	return nil
}
