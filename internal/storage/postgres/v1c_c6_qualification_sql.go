package postgres

const (
	c6BaselineRestartsSQL = `
SELECT coalesce(sum(greatest(startup_cycle-1,0)),0)
FROM v1c_engine_observations`
	c6InsertRunSQL = `
INSERT INTO v1c_c6_qualification_runs(
 id,mode,state,commit_sha,build_hash,executable_hash,image_hash,
 configuration_hash,source_dirty,required_duration_seconds,
 observed_duration_seconds,profitability_evidence,qualified,revision,
 created_at,updated_at
) VALUES($1,$2,'PENDING',$3,$4,$5,$6,$7,$8,$9,0,false,false,1,$10,$10)`
	c6InsertAccountSQL = `
INSERT INTO v1c_c6_qualification_accounts(
 run_id,account_id,exchange,environment,account_epoch,
 credential_generation,configuration_hash
) VALUES($1,$2,$3,$4,$5,$6,$7)`
	c6StartRunSQL = `
UPDATE v1c_c6_qualification_runs
SET state='RUNNING',started_at=$2,updated_at=$2,revision=2
WHERE id=$1`
	c6InsertSampleSQL = `
INSERT INTO v1c_c6_qualification_samples(
 run_id,sample_ordinal,observed_at,orders_acknowledged,
 duplicate_creates,lost_fills,double_posted_fills,unknown_orders,
 oldest_unknown_seconds,reconciliation_mismatches,suspense_items,
 reconnects,restarts,recovery_duration_ms,critical_alert_latency_ms,
 resident_memory_bytes,daily_submitted_microunits,
 largest_order_microunits,maximum_account_open,global_open,
 all_accounts_fresh,all_leases_held,persistence_healthy,restart_safe,
	 entry_safe,production_target_observed,account_observations
) VALUES(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
	 $18,$19,$20,$21,$22,$23,$24,$25,$26,$27
)`
	c6InsertFailureSQL = `
INSERT INTO v1c_c6_qualification_failures(
 id,run_id,reason,evidence_hash,occurred_at
) VALUES($1,$2,$3,$4,$5)`
	c6FinishRunSQL = `
UPDATE v1c_c6_qualification_runs
SET state=$2,observed_duration_seconds=$3,qualified=$4,ended_at=$5,
    evidence_hash=$6,updated_at=$5,revision=revision+1
WHERE id=$1 AND state='RUNNING'`
)

const c6ObserveOrdersSQL = `
SELECT
 count(*) FILTER (WHERE order_state IN (
   'ACKNOWLEDGED','PARTIALLY_FILLED','FILLED','CANCELED','EXPIRED'
 )),
 count(*) FILTER (WHERE lost_fill),
 coalesce(sum(double_posted_fills),0),
 count(*) FILTER (WHERE order_state='UNKNOWN'),
 coalesce(extract(epoch FROM (
   $2-min(updated_at) FILTER (WHERE order_state='UNKNOWN')
 )),0)::bigint,
 coalesce(max(reserved_notional_microunits),0),
 coalesce((
   SELECT (reserved_notional*1000000)::bigint
   FROM v1c_daily_cap_counters WHERE utc_day=$2::date
 ),0),
 coalesce(max(account_open),0),
 count(*) FILTER (WHERE state IN (
   'PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN'
 ))
FROM (
 SELECT observation.*,
   count(*) FILTER (WHERE state IN (
     'PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN'
   )) OVER (PARTITION BY exchange) AS account_open
 FROM v1c_c6_order_observations observation WHERE approved_at >= $1
) observed`

const c6ObserveDuplicatesSQL = `
SELECT coalesce(sum(request_count-1),0)
FROM (
 SELECT request_hash,count(*)::bigint AS request_count
 FROM v1c_authenticated_request_evidence
 WHERE recorded_at >= $1 AND method='POST'
   AND path IN ('/api/v3/order','/v5/order/create')
 GROUP BY request_hash HAVING count(*)>1
) duplicates`

const c6ObserveAccountsSQL = `
SELECT count(*)::integer,
 count(*) FILTER (
   WHERE account.state='READY_PAUSED'
     AND observation.private_stream_healthy
     AND observation.reconciliation_clean
     AND observation.evidence_healthy
     AND EXISTS(
       SELECT 1 FROM v1c_engine_runtime_events runtime
       WHERE runtime.account_id=account.id
         AND runtime.account_epoch=account.current_epoch
         AND runtime.kind='RECONCILIATION' AND runtime.succeeded
         AND runtime.occurred_at >=
           $1::timestamptz-interval '2 minutes'
     )
 )::integer,
 count(*) FILTER (WHERE lease.expires_at>$1::timestamptz)::integer,
 coalesce(sum(greatest(observation.startup_cycle-1,0)),0)
FROM v1c_exchange_accounts account
LEFT JOIN v1c_engine_observations observation
 ON observation.account_id=account.id
 AND observation.account_epoch=account.current_epoch
LEFT JOIN v1c_account_leases lease ON lease.account_id=account.id
WHERE account.exchange IN ('binance','bybit')`

const c6ObserveRuntimeSQL = `
SELECT
 count(*) FILTER (WHERE kind='PRIVATE_RECONNECT'),
 coalesce(max(duration_ms),0),
 coalesce(bool_and(
   succeeded OR (
     kind='RECONCILIATION' AND
     failure_kind IN ('transient_outage','maintenance')
   )
 ),true)
FROM v1c_engine_runtime_events WHERE occurred_at >= $1`

const c6ObserveAccountDetailsSQL = `
SELECT account.id,account.exchange,account.environment,account.current_epoch,
       account.state,
       coalesce(observation.private_stream_healthy,false),
       coalesce(observation.evidence_healthy,false),
       coalesce(lease.expires_at>$1::timestamptz,false),
       coalesce(observation.reconciliation_clean,false),
       coalesce(runtime.succeeded,true),coalesce(runtime.failure_kind,''),
       coalesce(runtime.cause_code,''),runtime.occurred_at,
       coalesce(runtime_failure.failure_kind,''),
       coalesce(runtime_failure.cause_code,''),runtime_failure.occurred_at,
       coalesce(runtime_failures.failure_count,0),
       coalesce(runtime_failures.has_terminal_failure,false),
       coalesce(recovery.event,''),coalesce(recovery.state,''),
       coalesce(recovery.failure_kind,''),coalesce(recovery.cause_code,''),
       recovery.deadline_at,coalesce(recovery.clean_check_count,0),
       recovery.recovery_timestamp
FROM v1c_exchange_accounts account
LEFT JOIN v1c_engine_observations observation
 ON observation.account_id=account.id
 AND observation.account_epoch=account.current_epoch
LEFT JOIN v1c_account_leases lease ON lease.account_id=account.id
LEFT JOIN LATERAL (
 SELECT succeeded,failure_kind,cause_code,occurred_at
 FROM v1c_engine_runtime_events runtime
 WHERE runtime.account_id=account.id
   AND runtime.account_epoch=account.current_epoch
   AND runtime.kind='RECONCILIATION'
   AND runtime.occurred_at >= $3::timestamptz
 ORDER BY runtime.occurred_at DESC,runtime.id DESC LIMIT 1
) runtime ON true
LEFT JOIN LATERAL (
 SELECT failure_kind,cause_code,occurred_at
 FROM v1c_engine_runtime_events runtime_failure
 WHERE runtime_failure.account_id=account.id
   AND runtime_failure.account_epoch=account.current_epoch
   AND runtime_failure.kind='RECONCILIATION'
   AND NOT runtime_failure.succeeded
   AND runtime_failure.occurred_at >= $3::timestamptz
 ORDER BY runtime_failure.occurred_at DESC,runtime_failure.id DESC LIMIT 1
) runtime_failure ON true
LEFT JOIN LATERAL (
 SELECT count(*) FILTER (WHERE NOT succeeded)::integer AS failure_count,
        coalesce(bool_or(
          NOT succeeded AND (
            failure_kind IS NULL OR
            failure_kind NOT IN ('transient_outage','maintenance')
          )
        ),false) AS has_terminal_failure
 FROM v1c_engine_runtime_events runtime_failure
 WHERE runtime_failure.account_id=account.id
   AND runtime_failure.account_epoch=account.current_epoch
   AND runtime_failure.kind='RECONCILIATION'
   AND runtime_failure.occurred_at >= $3::timestamptz
) runtime_failures ON true
LEFT JOIN LATERAL (
 SELECT event,state,failure_kind,cause_code,deadline_at,
        clean_check_count,recovery_timestamp
 FROM v1c_c6_recovery_events recovery
 WHERE recovery.run_id=$2 AND recovery.account_id=account.id
 ORDER BY recovery.occurred_at DESC,recovery.id DESC LIMIT 1
) recovery ON true
WHERE account.exchange IN ('binance','bybit')
ORDER BY account.exchange,account.id`
