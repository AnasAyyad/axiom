package postgres

const evaluationRecorderQualificationQuery = `WITH ordered_observations AS (
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
	WHERE request.campaign_id=$1`
