package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/evaluation"
	"axiom/internal/storage/segments"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	evaluationRecordingLimitBytes            int64 = 200 * 1024 * 1024 * 1024
	evaluationRecorderFinalizeAllowance      int64 = 1 * 1024 * 1024 * 1024
	evaluationRecorderMaxObservationInterval       = 15 * time.Minute
)

// EvaluationRecorderRotation is the safe recorder-role handshake.
type EvaluationRecorderRotation struct {
	CampaignID       string
	DesiredSessionID string
	State            string
	RecordedBytes    int64
	ValidSeconds     int64
	ActivatedAt      time.Time
}

// EvaluationRecorderQualification is the durable, low-cardinality recorder
// gate consumed by campaign orchestration.
type EvaluationRecorderQualification struct {
	State                  string
	Reason                 evaluation.ReasonCode
	ValidSeconds           int64
	RecordedBytes          int64
	MeasuredBytesPerHour   int64
	ShadowReservedBytes    int64
	ObservationCount       int64
	LastObservedAt         time.Time
	LastLossObservedAt     time.Time
	LatestAllEligible      bool
	LatestPersistence      bool
	LatestIntervalValid    bool
	LossObserved           bool
	UnresolvedObservations int64
}

// EvaluationRecorderInstrumentObservation is one of the fixed six public
// exchange/instrument health facts.
type EvaluationRecorderInstrumentObservation struct {
	ExchangeID, Instrument                    string
	Eligible, BookFresh, ClockEligible        bool
	LatestEventAt                             time.Time
	Messages, QueueDrops, Gaps, DecoderErrors uint64
}

type evaluationPriorRecorderObservation struct {
	ordinal                                   int64
	session                                   string
	at                                        time.Time
	messages, queueDrops, gaps, decoderErrors int64
}

type evaluationRecorderInterval struct {
	valid        bool
	start        any
	validSeconds int64
}

// EvaluationRecorderControlStore coordinates campaign worker and recorder
// role without signals, shell access, or a second execution platform.
type EvaluationRecorderControlStore struct {
	pool  *pgxpool.Pool
	clock domain.Clock
}

// NewEvaluationRecorderControlStore constructs the durable campaign-recorder handshake.
func NewEvaluationRecorderControlStore(pool *pgxpool.Pool,
	clock domain.Clock) (*EvaluationRecorderControlStore, error) {
	if pool == nil || clock == nil {
		return nil, fmt.Errorf("evaluation_recorder_control_dependencies_missing")
	}
	return &EvaluationRecorderControlStore{pool: pool, clock: clock}, nil
}

// EnsureRotation creates one deterministic request and persists the baseline;
// it never stops the recorder itself.
func (store *EvaluationRecorderControlStore) EnsureRotation(ctx context.Context,
	campaign evaluation.Campaign, baselineBytes int64) (EvaluationRecorderRotation, error) {
	if campaign.ID == "" || baselineBytes < 0 {
		return EvaluationRecorderRotation{}, fmt.Errorf("evaluation_recorder_request_invalid")
	}
	session := evaluation.EvaluationRecorderSessionID(campaign.ID)
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return EvaluationRecorderRotation{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `INSERT INTO evaluation_recorder_requests(campaign_id,desired_session_id,
	  state,binance_session_id,bybit_session_id,storage_baseline_bytes,requested_at,updated_at)
	  VALUES($1,$2,'REQUESTED',$2,$2||'-bybit',$3,$4,$4) ON CONFLICT (campaign_id) DO NOTHING`,
		campaign.ID, session, baselineBytes, now); err != nil {
		return EvaluationRecorderRotation{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE evaluation_campaigns SET campaign_storage_baseline_bytes=$2,
	  updated_at=GREATEST(updated_at,$3) WHERE id=$1 AND campaign_storage_baseline_bytes IN (0,$2)`,
		campaign.ID, baselineBytes, now); err != nil {
		return EvaluationRecorderRotation{}, err
	}
	rotation, err := readEvaluationRecorderRotation(tx.QueryRow(ctx, evaluationRecorderRotationSelect+" WHERE campaign_id=$1", campaign.ID))
	if err != nil || rotation.DesiredSessionID != session {
		return EvaluationRecorderRotation{}, fmt.Errorf("evaluation_recorder_request_conflict")
	}
	return rotation, tx.Commit(ctx)
}

// StartupRotation claims the latest finalized request for activation or
// returns the already-active request needed for a safe process restart.
func (store *EvaluationRecorderControlStore) StartupRotation(ctx context.Context) (EvaluationRecorderRotation, bool, error) {
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return EvaluationRecorderRotation{}, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rotation, err := readEvaluationRecorderRotation(tx.QueryRow(ctx, evaluationRecorderRotationSelect+
		" WHERE state IN ('FINALIZED','ACTIVATING','ACTIVE','PAUSED','FINALIZING') ORDER BY requested_at DESC LIMIT 1 FOR UPDATE"))
	if errors.Is(err, pgx.ErrNoRows) {
		return EvaluationRecorderRotation{}, false, tx.Commit(ctx)
	}
	if err != nil {
		return EvaluationRecorderRotation{}, false, err
	}
	if rotation.State == "FINALIZED" {
		if _, err = tx.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='ACTIVATING',updated_at=$2
		  WHERE campaign_id=$1 AND state='FINALIZED'`, rotation.CampaignID, now); err != nil {
			return EvaluationRecorderRotation{}, false, err
		}
		rotation.State = "ACTIVATING"
	}
	return rotation, true, tx.Commit(ctx)
}

// FlushAllowance forecasts a complete safe-boundary flush before any file is
// written. Existing/non-campaign sessions are unrestricted by the campaign
// budget. protectReserve requests a controlled pause when the frozen shadow
// reserve would otherwise be consumed.
func (store *EvaluationRecorderControlStore) FlushAllowance(ctx context.Context, session string,
	predictedBytes int64, protectReserve bool) (bool, error) {
	if session == "" || predictedBytes < 0 {
		return false, fmt.Errorf("evaluation_recorder_flush_forecast_invalid")
	}
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var campaignID string
	var recorded int64
	var reserve *int64
	err = tx.QueryRow(ctx, `SELECT campaign_id,recorded_bytes,shadow_reserved_bytes
FROM evaluation_recorder_requests WHERE state IN ('ACTIVE','FINALIZING')
AND (binance_session_id=$1 OR bybit_session_id=$1) FOR UPDATE`, session).Scan(&campaignID, &recorded, &reserve)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, tx.Commit(ctx)
	}
	if err != nil {
		return false, err
	}
	if predictedBytes > evaluationRecordingLimitBytes-recorded {
		if _, err = tx.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='BLOCKED',
  reason_code='STORAGE_INSUFFICIENT',updated_at=$2 WHERE campaign_id=$1`, campaignID, now); err != nil {
			return false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return false, err
		}
		return false, fmt.Errorf("evaluation_recorder_storage_cap_forecast")
	}
	pauseAfter := protectReserve && reserve != nil && *reserve > 0 &&
		(recorded > evaluationRecordingLimitBytes-*reserve ||
			predictedBytes > evaluationRecordingLimitBytes-*reserve-recorded)
	return pauseAfter, tx.Commit(ctx)
}

// PauseSession is called only after collectors stopped and the final safe
// segment boundary was committed.
func (store *EvaluationRecorderControlStore) PauseSession(ctx context.Context, session string) error {
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='PAUSED',updated_at=$2
WHERE state='ACTIVE' AND (binance_session_id=$1 OR bybit_session_id=$1)`, session, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_recorder_pause_conflict")
	}
	return nil
}

// ResumeCampaign reopens the same durable session for combined-shadow input
// evidence; no new campaign or byte counter is created.
func (store *EvaluationRecorderControlStore) ResumeCampaign(ctx context.Context, campaignID string) error {
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='ACTIVE',
shadow_reserved_bytes=0,updated_at=$2 WHERE campaign_id=$1 AND state IN ('PAUSED','ACTIVE')`, campaignID, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_recorder_resume_conflict")
	}
	return nil
}

// PendingRotation returns the oldest recorder rotation awaiting a safe boundary.
func (store *EvaluationRecorderControlStore) PendingRotation(ctx context.Context) (EvaluationRecorderRotation, bool, error) {
	rotation, err := readEvaluationRecorderRotation(store.pool.QueryRow(ctx, evaluationRecorderRotationSelect+
		" WHERE state='REQUESTED' ORDER BY requested_at LIMIT 1"))
	if errors.Is(err, pgx.ErrNoRows) {
		return EvaluationRecorderRotation{}, false, nil
	}
	return rotation, err == nil, err
}

// MarkFinalized acknowledges that both prior exchange recorders were flushed
// and their manifests registered before process restart.
func (store *EvaluationRecorderControlStore) MarkFinalized(ctx context.Context, campaignID,
	previousSession string) error {
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='FINALIZED',
	  previous_session_id=$2,finalized_at=$3,updated_at=$3 WHERE campaign_id=$1 AND state='REQUESTED'`,
		campaignID, previousSession, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_recorder_finalize_conflict")
	}
	return nil
}

// MarkActive completes startup only after the campaign-bound collectors have
// been constructed and launched.
func (store *EvaluationRecorderControlStore) MarkActive(ctx context.Context, campaignID,
	sessionID string) error {
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='ACTIVE',
	  activated_at=COALESCE(activated_at,$3),updated_at=$3 WHERE campaign_id=$1
	  AND desired_session_id=$2 AND state IN ('ACTIVATING','ACTIVE')`, campaignID, sessionID, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_recorder_activation_conflict")
	}
	return nil
}

// RegisterSegment links only campaign-session segment bytes. Existing files
// and historical imports never consume the 200 GiB recording budget.
func (store *EvaluationRecorderControlStore) RegisterSegment(ctx context.Context, session string,
	manifest segments.Manifest) error {
	if session == "" || manifest.Spec.Name == "" || manifest.Size <= 0 {
		return fmt.Errorf("evaluation_recorder_segment_invalid")
	}
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var campaignID string
	err = tx.QueryRow(ctx, `SELECT campaign_id FROM evaluation_recorder_requests WHERE state IN ('ACTIVE','FINALIZING')
	  AND (binance_session_id=$1 OR bybit_session_id=$1) FOR UPDATE`, session).Scan(&campaignID)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO evaluation_campaign_recording_segments(
	  campaign_id,segment_id,byte_count,recorded_at) VALUES($1,$2,$3,$4)
	  ON CONFLICT (campaign_id,segment_id) DO NOTHING`, campaignID, manifest.Spec.Name, manifest.Size, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var size int64
		if err = tx.QueryRow(ctx, `SELECT byte_count FROM evaluation_campaign_recording_segments
		  WHERE campaign_id=$1 AND segment_id=$2`, campaignID, manifest.Spec.Name).Scan(&size); err != nil || size != manifest.Size {
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
	if _, err = tx.Exec(ctx, `UPDATE evaluation_recorder_requests SET recorded_bytes=$2,updated_at=$3
	  WHERE campaign_id=$1`, campaignID, recorded, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Rotation returns the recorder handshake state for one campaign.
func (store *EvaluationRecorderControlStore) Rotation(ctx context.Context,
	campaignID string) (EvaluationRecorderRotation, error) {
	return readEvaluationRecorderRotation(store.pool.QueryRow(ctx, evaluationRecorderRotationSelect+
		" WHERE campaign_id=$1", campaignID))
}

// Qualification returns the campaign's durable recorder-validity evidence.
