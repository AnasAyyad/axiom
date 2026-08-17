package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

// Qualification returns the durable valid-time, storage, and feed eligibility
// projection for one campaign recorder session.
func (store *EvaluationRecorderControlStore) Qualification(ctx context.Context,
	campaignID string) (EvaluationRecorderQualification, error) {
	var value EvaluationRecorderQualification
	var reason *string
	var rate, reserve *int64
	var observed, lastLoss *time.Time
	err := store.pool.QueryRow(ctx, `WITH ordered_observations AS (
	  SELECT observation.ordinal,observation.observed_at,observation.interval_valid,
	    observation.queue_drop_count>COALESCE(lag(observation.queue_drop_count)
	      OVER (ORDER BY observation.ordinal),0) OR
	    observation.gap_count>COALESCE(lag(observation.gap_count)
	      OVER (ORDER BY observation.ordinal),0) OR
	    observation.decoder_error_count>COALESCE(lag(observation.decoder_error_count)
	      OVER (ORDER BY observation.ordinal),0) AS new_loss
	  FROM evaluation_recorder_observations observation WHERE observation.campaign_id=$1
	), recovery AS (
	  SELECT COALESCE(bool_or(new_loss),false) AS loss_observed,
	    max(observed_at) FILTER (WHERE new_loss) AS last_loss_at,
	    max(ordinal) FILTER (WHERE new_loss) AS last_loss_ordinal,
	    max(ordinal) FILTER (WHERE interval_valid) AS last_valid_ordinal
	  FROM ordered_observations
	), recovery_summary AS (
	  SELECT recovery.*,
	    CASE WHEN last_loss_ordinal IS NOT NULL AND
	      COALESCE(last_valid_ordinal,0)<=last_loss_ordinal THEN
	      (SELECT count(*) FROM ordered_observations WHERE ordinal>=last_loss_ordinal)
	    ELSE 0 END AS unresolved_observations
	  FROM recovery
	)
	SELECT request.state,request.reason_code,
	  request.valid_recording_seconds,request.recorded_bytes,request.measured_bytes_per_hour,
	  request.shadow_reserved_bytes,
	  (SELECT count(*) FROM ordered_observations),
	  (SELECT observation.observed_at FROM evaluation_recorder_observations observation
	    WHERE observation.campaign_id=request.campaign_id ORDER BY observation.ordinal DESC LIMIT 1),
	  COALESCE((SELECT observation.all_collectors_eligible FROM evaluation_recorder_observations observation
	    WHERE observation.campaign_id=request.campaign_id ORDER BY observation.ordinal DESC LIMIT 1),false),
	  COALESCE((SELECT observation.persistence_healthy FROM evaluation_recorder_observations observation
	    WHERE observation.campaign_id=request.campaign_id ORDER BY observation.ordinal DESC LIMIT 1),false),
	  COALESCE((SELECT observation.interval_valid FROM evaluation_recorder_observations observation
	    WHERE observation.campaign_id=request.campaign_id ORDER BY observation.ordinal DESC LIMIT 1),false),
	  recovery_summary.loss_observed,recovery_summary.last_loss_at,
	  recovery_summary.unresolved_observations
	FROM evaluation_recorder_requests request CROSS JOIN recovery_summary
	WHERE request.campaign_id=$1`, campaignID).Scan(&value.State,
		&reason, &value.ValidSeconds, &value.RecordedBytes, &rate, &reserve, &value.ObservationCount,
		&observed, &value.LatestAllEligible, &value.LatestPersistence, &value.LatestIntervalValid,
		&value.LossObserved, &lastLoss, &value.UnresolvedObservations)
	if err != nil {
		return EvaluationRecorderQualification{}, err
	}
	if reason != nil {
		value.Reason = evaluation.ReasonCode(*reason)
	}
	if rate != nil {
		value.MeasuredBytesPerHour = *rate
	}
	if reserve != nil {
		value.ShadowReservedBytes = *reserve
	}
	if observed != nil {
		value.LastObservedAt = observed.UTC()
	}
	if lastLoss != nil {
		value.LastLossObservedAt = lastLoss.UTC()
	}
	return value, nil
}

// ProtectShadowReserve freezes a 20 percent buffered seven-day projection.
// It also leaves one aggregate in-memory recorder allowance outside that
// projection. The recorder's pre-flush forecast uses the same 1 GiB allowance,
// so its final safe-boundary flush cannot consume the frozen shadow reserve.
// It blocks before shadow when the exact 200 GiB cap cannot be respected.
func (store *EvaluationRecorderControlStore) ProtectShadowReserve(ctx context.Context,
	campaignID string) (int64, error) {
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var state string
	var recorded, validSeconds int64
	var rate, existing *int64
	err = tx.QueryRow(ctx, `SELECT state,recorded_bytes,valid_recording_seconds,
	  measured_bytes_per_hour,shadow_reserved_bytes FROM evaluation_recorder_requests
	  WHERE campaign_id=$1 FOR UPDATE`, campaignID).Scan(&state, &recorded, &validSeconds, &rate, &existing)
	if err != nil {
		return 0, err
	}
	if existing != nil && *existing > 0 {
		return *existing, tx.Commit(ctx)
	}
	if state != "ACTIVE" || validSeconds < int64(evaluation.RequiredRecordingValidTime/time.Second) || rate == nil || *rate <= 0 {
		return 0, fmt.Errorf("evaluation_recorder_rate_unqualified")
	}
	if *rate > math.MaxInt64/(7*24) {
		return 0, fmt.Errorf("evaluation_recorder_projection_overflow")
	}
	week := *rate * 7 * 24
	if week > math.MaxInt64/12 {
		return 0, fmt.Errorf("evaluation_recorder_projection_overflow")
	}
	reserve := (week*12 + 9) / 10
	if reserve <= 0 || !evaluationShadowReserveFits(recorded, reserve) {
		_, _ = tx.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='BLOCKED',
		  reason_code='STORAGE_INSUFFICIENT',updated_at=$2 WHERE campaign_id=$1`, campaignID, now)
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return 0, commitErr
		}
		return 0, fmt.Errorf("evaluation_recorder_storage_insufficient")
	}
	_, err = tx.Exec(ctx, `UPDATE evaluation_recorder_requests SET shadow_reserved_bytes=$2,
	  updated_at=$3 WHERE campaign_id=$1`, campaignID, reserve, now)
	if err == nil {
		_, err = tx.Exec(ctx, `UPDATE evaluation_campaigns SET shadow_reserved_bytes=$2,updated_at=$3
		  WHERE id=$1`, campaignID, reserve, now)
	}
	if err != nil {
		return 0, err
	}
	return reserve, tx.Commit(ctx)
}

func evaluationShadowReserveFits(recorded, reserve int64) bool {
	if recorded < 0 || reserve <= 0 || reserve > evaluationRecordingLimitBytes-evaluationRecorderFinalizeAllowance {
		return false
	}
	return recorded <= evaluationRecordingLimitBytes-reserve-evaluationRecorderFinalizeAllowance
}

// Observe appends one fixed-cardinality health snapshot and advances valid
// time only across a continuous interval with healthy feeds, clocks,
// persistence, and unchanged loss counters.
func (store *EvaluationRecorderControlStore) Observe(ctx context.Context, session string,
	observedAt time.Time, persistenceHealthy bool,
	instruments []EvaluationRecorderInstrumentObservation) error {
	if err := validateEvaluationRecorderObservationIdentity(session, observedAt); err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	campaignID, recorded, validSeconds, found, err := lockEvaluationRecorderSession(ctx, tx, session)
	if err != nil {
		return err
	}
	if !found {
		return tx.Commit(ctx)
	}
	if err = validateEvaluationRecorderObservation(session, observedAt, instruments); err != nil {
		return err
	}
	messages, queueDrops, gaps, decoderErrors, allEligible := evaluationObservationCounters(instruments)
	if messages > math.MaxInt64 || queueDrops > math.MaxInt64 || gaps > math.MaxInt64 || decoderErrors > math.MaxInt64 {
		return fmt.Errorf("evaluation_recorder_observation_overflow")
	}
	prior, priorFound, err := loadPriorEvaluationObservation(ctx, tx, campaignID)
	if err != nil {
		return err
	}
	interval := calculateEvaluationRecorderInterval(prior, priorFound, session, observedAt,
		persistenceHealthy, allEligible, int64(messages), int64(queueDrops), int64(gaps), int64(decoderErrors), instruments)
	ordinal := prior.ordinal + 1
	if err = insertEvaluationRecorderObservation(ctx, tx, campaignID, session, observedAt, recorded,
		ordinal, interval, persistenceHealthy, allEligible, int64(messages), int64(queueDrops),
		int64(gaps), int64(decoderErrors)); err != nil {
		return err
	}
	if err = insertEvaluationInstrumentObservations(ctx, tx, campaignID, ordinal, instruments); err != nil {
		return err
	}
	validSeconds += interval.validSeconds
	if err = updateEvaluationRecorderValidity(ctx, tx, campaignID, validSeconds, recorded,
		interval.validSeconds, interval.valid, observedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lockEvaluationRecorderSession(ctx context.Context, tx pgx.Tx,
	session string) (string, int64, int64, bool, error) {
	var campaignID, desiredSession, state string
	var recorded, validSeconds int64
	var activated time.Time
	err := tx.QueryRow(ctx, `SELECT campaign_id,desired_session_id,state,recorded_bytes,
valid_recording_seconds,activated_at FROM evaluation_recorder_requests
WHERE state='ACTIVE' AND (binance_session_id=$1 OR bybit_session_id=$1) FOR UPDATE`, session).
		Scan(&campaignID, &desiredSession, &state, &recorded, &validSeconds, &activated)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, 0, false, nil
	}
	if err != nil || state != "ACTIVE" || activated.IsZero() ||
		(session != desiredSession && session != desiredSession+"-bybit") {
		return "", 0, 0, false, fmt.Errorf("evaluation_recorder_observation_session_invalid")
	}
	return campaignID, recorded, validSeconds, true, nil
}

func loadPriorEvaluationObservation(ctx context.Context, tx pgx.Tx,
	campaignID string) (evaluationPriorRecorderObservation, bool, error) {
	var prior evaluationPriorRecorderObservation
	err := tx.QueryRow(ctx, `SELECT ordinal,session_id,observed_at,message_count,queue_drop_count,
gap_count,decoder_error_count FROM evaluation_recorder_observations WHERE campaign_id=$1
ORDER BY ordinal DESC LIMIT 1`, campaignID).Scan(&prior.ordinal, &prior.session, &prior.at,
		&prior.messages, &prior.queueDrops, &prior.gaps, &prior.decoderErrors)
	if errors.Is(err, pgx.ErrNoRows) {
		return prior, false, nil
	}
	return prior, err == nil, err
}

func calculateEvaluationRecorderInterval(prior evaluationPriorRecorderObservation, found bool, session string,
	observedAt time.Time, persistenceHealthy, allEligible bool, messages, queueDrops, gaps, decoderErrors int64,
	instruments []EvaluationRecorderInstrumentObservation) evaluationRecorderInterval {
	if !found {
		return evaluationRecorderInterval{}
	}
	elapsed := observedAt.Sub(prior.at.UTC())
	countersMonotonic := messages >= prior.messages && queueDrops >= prior.queueDrops &&
		gaps >= prior.gaps && decoderErrors >= prior.decoderErrors
	noNewLoss := queueDrops == prior.queueDrops && gaps == prior.gaps && decoderErrors == prior.decoderErrors
	freshFacts := true
	for _, item := range instruments {
		freshFacts = freshFacts && !item.LatestEventAt.Before(prior.at.UTC()) &&
			!item.LatestEventAt.After(observedAt) && observedAt.Sub(item.LatestEventAt) <= 30*time.Second
	}
	valid := prior.session == session && elapsed > 0 && elapsed <= evaluationRecorderMaxObservationInterval && countersMonotonic &&
		messages > prior.messages && noNewLoss && allEligible && persistenceHealthy && freshFacts
	result := evaluationRecorderInterval{valid: valid, start: prior.at.UTC()}
	if valid {
		result.validSeconds = int64(elapsed / time.Second)
	}
	return result
}

func insertEvaluationRecorderObservation(ctx context.Context, tx pgx.Tx, campaignID, session string,
	observedAt time.Time, recorded, ordinal int64, interval evaluationRecorderInterval,
	persistenceHealthy, allEligible bool, messages, queueDrops, gaps, decoderErrors int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO evaluation_recorder_observations(campaign_id,ordinal,
session_id,observed_at,interval_start,interval_valid,valid_interval_seconds,all_collectors_eligible,
persistence_healthy,message_count,queue_drop_count,gap_count,decoder_error_count,recorded_bytes)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, campaignID, ordinal,
		session, observedAt, interval.start, interval.valid, interval.validSeconds, allEligible, persistenceHealthy,
		messages, queueDrops, gaps, decoderErrors, recorded)
	return err
}

func insertEvaluationInstrumentObservations(ctx context.Context, tx pgx.Tx, campaignID string,
	ordinal int64, instruments []EvaluationRecorderInstrumentObservation) error {
	for _, item := range instruments {
		_, err := tx.Exec(ctx, `INSERT INTO evaluation_recorder_instrument_observations(campaign_id,
observation_ordinal,exchange_id,instrument,eligible,book_fresh,clock_eligible,latest_event_at,
message_count,queue_drop_count,gap_count,decoder_error_count)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, campaignID, ordinal, item.ExchangeID,
			item.Instrument, item.Eligible, item.BookFresh, item.ClockEligible, item.LatestEventAt,
			int64(item.Messages), int64(item.QueueDrops), int64(item.Gaps), int64(item.DecoderErrors))
		if err != nil {
			return err
		}
	}
	return nil
}

func updateEvaluationRecorderValidity(ctx context.Context, tx pgx.Tx, campaignID string,
	validSeconds, recorded, validInterval int64, intervalValid bool, observedAt time.Time) error {
	var measuredRate any
	if rate, available := evaluationMeasuredBytesPerHour(recorded, validSeconds); available {
		measuredRate = rate
	}
	var lastValid any
	if intervalValid {
		lastValid = observedAt
	}
	if _, err := tx.Exec(ctx, `UPDATE evaluation_recorder_requests SET valid_recording_seconds=$2,
last_valid_at=COALESCE($3,last_valid_at),measured_bytes_per_hour=COALESCE($4,measured_bytes_per_hour),
updated_at=$5 WHERE campaign_id=$1`, campaignID, validSeconds, lastValid, measuredRate, observedAt); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE evaluation_campaigns SET valid_recording_seconds=$2,
recording_last_valid_at=CASE WHEN $6>0 THEN $5 ELSE recording_last_valid_at END,
valid_shadow_seconds=valid_shadow_seconds + CASE WHEN current_stage='COMBINED_SHADOW' THEN $6 ELSE 0 END,
shadow_last_valid_at=CASE WHEN current_stage='COMBINED_SHADOW' AND $6>0 THEN $5 ELSE shadow_last_valid_at END,
campaign_recorded_bytes=$3,measured_bytes_per_hour=COALESCE($4,measured_bytes_per_hour),updated_at=$5
WHERE id=$1`, campaignID, validSeconds, recorded, measuredRate, observedAt, validInterval)
	return err
}

func evaluationMeasuredBytesPerHour(recorded, validSeconds int64) (int64, bool) {
	secondsPerHour := int64(time.Hour / time.Second)
	if recorded < 0 || validSeconds < secondsPerHour || recorded > math.MaxInt64/secondsPerHour {
		return 0, false
	}
	return recorded * secondsPerHour / validSeconds, true
}

// Block records a terminal recorder qualification failure for the campaign.
func (store *EvaluationRecorderControlStore) Block(ctx context.Context, campaignID string,
	reason evaluation.ReasonCode) error {
	if reason == "" {
		return fmt.Errorf("evaluation_recorder_block_reason_missing")
	}
	_, err := store.pool.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='BLOCKED',reason_code=$2,
	  updated_at=$3 WHERE campaign_id=$1 AND state NOT IN ('BLOCKED','COMPLETED')`, campaignID,
		string(reason), store.clock.Now().UTC)
	return err
}

// RequestCompletion asks the recorder role to stop collectors, flush both
// exchange recorders at a safe boundary, and preserve the final manifests.
func (store *EvaluationRecorderControlStore) RequestCompletion(ctx context.Context,
	campaignID string) error {
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='FINALIZING',updated_at=$2
WHERE campaign_id=$1 AND state='ACTIVE'`, campaignID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var state string
		if readErr := store.pool.QueryRow(ctx, `SELECT state FROM evaluation_recorder_requests
WHERE campaign_id=$1`, campaignID).Scan(&state); readErr != nil || (state != "FINALIZING" && state != "COMPLETED") {
			return fmt.Errorf("evaluation_recorder_completion_conflict")
		}
	}
	return nil
}

// CompletionRequested is the recorder role's polling boundary. It performs no
// mutation and therefore cannot acknowledge evidence that is still buffered.
func (store *EvaluationRecorderControlStore) CompletionRequested(ctx context.Context,
	session string) (string, bool, error) {
	var campaignID string
	err := store.pool.QueryRow(ctx, `SELECT campaign_id FROM evaluation_recorder_requests
WHERE state='FINALIZING' AND (binance_session_id=$1 OR bybit_session_id=$1)`, session).Scan(&campaignID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return campaignID, err == nil, err
}

// MarkCompleted is called only after the recorder role has stopped collectors
// and committed the final complete manifests.
func (store *EvaluationRecorderControlStore) MarkCompleted(ctx context.Context,
	campaignID, session string) error {
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE evaluation_recorder_requests SET state='COMPLETED',
completed_at=$3,updated_at=$3 WHERE campaign_id=$1 AND state='FINALIZING'
AND (binance_session_id=$2 OR bybit_session_id=$2)`, campaignID, session, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_recorder_completion_conflict")
	}
	return nil
}

const evaluationRecorderRotationSelect = `SELECT campaign_id,desired_session_id,state,recorded_bytes,
  valid_recording_seconds,activated_at FROM evaluation_recorder_requests`

type evaluationRecorderScanner interface{ Scan(...any) error }

func readEvaluationRecorderRotation(row evaluationRecorderScanner) (EvaluationRecorderRotation, error) {
	var value EvaluationRecorderRotation
	var activated *time.Time
	err := row.Scan(&value.CampaignID, &value.DesiredSessionID, &value.State, &value.RecordedBytes,
		&value.ValidSeconds, &activated)
	if activated != nil {
		value.ActivatedAt = activated.UTC()
	}
	return value, err
}

func evaluationObservationCounters(values []EvaluationRecorderInstrumentObservation) (messages,
	queueDrops, gaps, decoderErrors uint64, allEligible bool) {
	allEligible = len(values) == 6
	for _, value := range values {
		if math.MaxUint64-messages < value.Messages || math.MaxUint64-queueDrops < value.QueueDrops ||
			math.MaxUint64-gaps < value.Gaps || math.MaxUint64-decoderErrors < value.DecoderErrors {
			return 0, 0, 0, 0, false
		}
		messages += value.Messages
		queueDrops += value.QueueDrops
		gaps += value.Gaps
		decoderErrors += value.DecoderErrors
		allEligible = allEligible && value.Eligible && value.BookFresh && value.ClockEligible
	}
	return
}
