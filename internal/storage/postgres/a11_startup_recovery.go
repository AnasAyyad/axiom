package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/buildinfo"
	"axiom/internal/config"
	runtimecore "axiom/internal/runtime"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnsureA11StartupRecovery records the ordered 14-stage recovery proof only
// after the current build, schema, public evidence, and protected state pass.
// Completion establishes PAUSED administrative readiness; it never resumes risk.
func EnsureA11StartupRecovery(ctx context.Context, pool *pgxpool.Pool, owner string,
	build buildinfo.Info, now time.Time) error {
	if pool == nil || owner == "" || now.IsZero() || now.Location() != time.UTC || build.Dirty ||
		!a11BuildIdentityValid(build.Commit, build.GoSumHash, build.PNPMLockHash) {
		return fmt.Errorf("a11_startup_recovery_build_invalid")
	}
	buildPayload, _ := json.Marshal(build)
	buildHash := a11SHA256(buildPayload)
	if err := ensureA11RecoveryPause(ctx, pool, buildHash, now); err != nil {
		return err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "axiom:startup-recovery"); err != nil {
		return err
	}
	if err = recordA11StartupRecovery(ctx, tx, owner, build, buildHash, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func recordA11StartupRecovery(ctx context.Context, tx pgx.Tx, owner string, build buildinfo.Info,
	buildHash string, now time.Time) error {
	configurationID, configurationHash, err := currentA11RecoveryConfiguration(ctx, tx)
	if err != nil {
		return err
	}
	attemptSuffix := a11SHA256([]byte(owner + ":" + buildHash + ":" + configurationHash))[:24]
	attemptID, runID := "startup-recovery-"+attemptSuffix, "startup-recovery-run-"+attemptSuffix
	var existing int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM startup_recovery_attempts attempt
      WHERE attempt.id=$1 AND attempt.state='ready_paused' AND attempt.build_hash=$2 AND
      attempt.configuration_hash=$3 AND (SELECT count(*) FROM startup_recovery_evidence
      WHERE attempt_id=attempt.id)=14`, attemptID, buildHash, configurationHash).Scan(&existing); err != nil {
		return err
	}
	if existing == 1 {
		return nil
	}
	evidence, err := collectA11RecoveryEvidence(ctx, tx, build, configurationID, configurationHash, now)
	if err != nil {
		return err
	}
	return persistA11RecoveryAttempt(ctx, tx, attemptID, runID, attemptSuffix, configurationID,
		configurationHash, buildHash, evidence, now)
}

func persistA11RecoveryAttempt(ctx context.Context, tx pgx.Tx, attemptID, runID, attemptSuffix,
	configurationID, configurationHash, buildHash string, evidence []string, now time.Time) error {
	rootHash := a11SHA256([]byte("startup-recovery:" + attemptSuffix))
	if _, err := tx.Exec(ctx, `INSERT INTO runs(id,mode,configuration_id,strategy_version_id,root_seed_hash,
      reproducibility_hash,state,created_at) VALUES($1,'shadow',$2,'trend-v1a-1',$3,$4,'created',$5)`,
		runID, configurationID, rootHash, a11SHA256([]byte(buildHash+configurationHash)), now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE runs SET state='running',started_at=$2 WHERE id=$1`, runID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO startup_recovery_attempts(id,run_id,state,build_hash,
      configuration_hash,started_at) VALUES($1,$2,'locked',$3,$4,$5)`, attemptID, runID,
		buildHash, configurationHash, now); err != nil {
		return err
	}
	for ordinal, stage := range runtimecore.RecoverySequence() {
		if _, err := tx.Exec(ctx, `INSERT INTO startup_recovery_evidence(attempt_id,ordinal,stage,
        evidence_hash,recorded_at) VALUES($1,$2,$3,$4,$5)`, attemptID, ordinal, string(stage),
			evidence[ordinal], now); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE startup_recovery_attempts SET state='ready_paused',completed_at=$2
      WHERE id=$1 AND state='locked'`, attemptID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE runs SET state='completed',completed_at=$2
      WHERE id=$1 AND state='running'`, runID, now); err != nil {
		return err
	}
	return nil
}

func ensureA11RecoveryPause(ctx context.Context, pool *pgxpool.Pool, evidenceHash string, now time.Time) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('axiom:risk-startup-pause'))`); err != nil {
		return err
	}
	if err = forceA11RecoveryPause(ctx, tx, evidenceHash, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func forceA11RecoveryPause(ctx context.Context, tx pgx.Tx, evidenceHash string, now time.Time) error {
	var current string
	_ = tx.QueryRow(ctx, `SELECT next_state FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1`).Scan(&current)
	if current == "" || current == "PAUSED" || current == "LOCKED" {
		return nil
	}
	if current != "NORMAL" && current != "CAUTIOUS" {
		return fmt.Errorf("a11_startup_recovery_risk_state_invalid")
	}
	var revision int64
	if err := tx.QueryRow(ctx, `UPDATE api_entity_revisions SET revision=revision+1,updated_at=$1
      WHERE entity_type='risk' AND entity_id='global' RETURNING revision`, now).Scan(&revision); err != nil {
		return err
	}
	id := fmt.Sprintf("risk-startup-pause-%d", revision)
	_, err := tx.Exec(ctx, `INSERT INTO risk_state_events(id,prior_state,next_state,reason_code,actor,
      evidence_hash,occurred_at,entity_revision) VALUES($1,$2,'PAUSED','startup_recovery','engine-shadow',$3,$4,$5)`,
		id, current, evidenceHash, now, revision)
	return err
}

func currentA11RecoveryConfiguration(ctx context.Context, tx pgx.Tx) (string, string, error) {
	var id, hash string
	var canonical []byte
	err := tx.QueryRow(ctx, `SELECT cv.id,cv.configuration_hash,cv.canonical_payload
      FROM configuration_activations activation JOIN configuration_versions cv ON cv.id=activation.configuration_id
      ORDER BY activation.revision DESC LIMIT 1`).Scan(&id, &hash, &canonical)
	var configuration config.Configuration
	if err != nil || json.Unmarshal(canonical, &configuration) != nil || config.Validate(configuration) != nil ||
		a11SHA256(canonical) != hash {
		return "", "", fmt.Errorf("a11_startup_recovery_configuration_invalid")
	}
	return id, hash, nil
}

func collectA11RecoveryEvidence(ctx context.Context, tx pgx.Tx, build buildinfo.Info,
	configurationID, configurationHash string, now time.Time) ([]string, error) {
	foundation, riskState, err := a11RecoveryFoundationFacts(ctx, tx, build, configurationID, configurationHash, now)
	if err != nil {
		return nil, err
	}
	state, err := a11RecoveryStateFacts(ctx, tx)
	if err != nil {
		return nil, err
	}
	market, err := a11RecoveryMarketFacts(ctx, tx, riskState, now)
	if err != nil {
		return nil, err
	}
	stageFacts := append(append(foundation, state...), market...)
	return hashA11RecoveryFacts(stageFacts)
}

func a11RecoveryFoundationFacts(ctx context.Context, tx pgx.Tx, build buildinfo.Info,
	configurationID, configurationHash string, now time.Time) ([]any, string, error) {
	facts := make([]any, 0, 7)
	var serverVersion string
	if err := tx.QueryRow(ctx, `SELECT current_setting('server_version_num')`).Scan(&serverVersion); err != nil {
		return nil, "", err
	}
	version, versionErr := strconv.Atoi(serverVersion)
	if versionErr != nil || version < 180000 {
		return nil, "", fmt.Errorf("a11_startup_recovery_postgres_18_required")
	}
	facts = append(facts, map[string]any{"server_version_num": serverVersion})
	var expiredLeases int64
	if err := tx.QueryRow(ctx, `WITH reclaimed AS (
      DELETE FROM execution_leases WHERE expires_at<=$1 RETURNING resource)
      SELECT count(*) FROM reclaimed`, now).Scan(&expiredLeases); err != nil {
		return nil, "", fmt.Errorf("a11_startup_recovery_fencing_invalid")
	}
	facts = append(facts, map[string]any{"expired_execution_leases_reclaimed": expiredLeases}, build,
		map[string]any{"configuration_id": configurationID, "configuration_hash": configurationHash})
	var migrationVersion string
	if err := tx.QueryRow(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&migrationVersion); err != nil || migrationVersion != "000010" {
		return nil, "", fmt.Errorf("a11_startup_recovery_schema_invalid")
	}
	facts = append(facts, map[string]any{"migration_version": migrationVersion})
	var invalidCursors int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM consumer_cursors WHERE outbox_revision<0`).Scan(&invalidCursors); err != nil || invalidCursors != 0 {
		return nil, "", fmt.Errorf("a11_startup_recovery_cursor_invalid")
	}
	facts = append(facts, map[string]any{"invalid_cursors": invalidCursors})
	var recoveryRiskState string
	_ = tx.QueryRow(ctx, `SELECT next_state FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1`).Scan(&recoveryRiskState)
	if recoveryRiskState == "" {
		recoveryRiskState = "PAUSED"
	}
	if recoveryRiskState != "PAUSED" && recoveryRiskState != "LOCKED" {
		return nil, "", fmt.Errorf("a11_startup_recovery_protected_state_invalid")
	}
	facts = append(facts, map[string]any{"risk_state": recoveryRiskState})
	return facts, recoveryRiskState, nil
}

func a11RecoveryStateFacts(ctx context.Context, tx pgx.Tx) ([]any, error) {
	facts := make([]any, 0, 3)
	var invalidOutbox int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM outbox_events
      WHERE schema_version<>'axiom.stream.v1' OR entity_revision<=0 OR payload IS NULL`).Scan(&invalidOutbox); err != nil || invalidOutbox != 0 {
		return nil, fmt.Errorf("a11_startup_recovery_outbox_invalid")
	}
	facts = append(facts, map[string]any{"invalid_outbox": invalidOutbox})
	var unbalanced int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM (
      SELECT transaction_id,asset_symbol FROM ledger_entries GROUP BY transaction_id,asset_symbol
      HAVING sum(CASE direction WHEN 'debit' THEN quantity ELSE -quantity END)<>0) imbalance`).Scan(&unbalanced); err != nil || unbalanced != 0 {
		return nil, fmt.Errorf("a11_startup_recovery_journal_invalid")
	}
	facts = append(facts, map[string]any{"unbalanced_journals": unbalanced})
	var nonterminal int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM orders WHERE state NOT IN
      ('filled','canceled','rejected','expired','recovered')`).Scan(&nonterminal); err != nil || nonterminal != 0 {
		return nil, fmt.Errorf("a11_startup_recovery_simulation_invalid")
	}
	facts = append(facts, map[string]any{"nonterminal_simulated_orders": nonterminal})
	return facts, nil
}

func a11RecoveryMarketFacts(ctx context.Context, tx pgx.Tx, riskState string, now time.Time) ([]any, error) {
	facts := make([]any, 0, 4)
	var recorderSegments int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM market_data_segments WHERE state='ready'`).Scan(&recorderSegments); err != nil || recorderSegments == 0 {
		return nil, fmt.Errorf("a11_startup_recovery_recorder_unavailable")
	}
	facts = append(facts, map[string]any{"ready_segments": recorderSegments})
	var publicReady bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM market_data_segments segment JOIN exchanges exchange
      ON exchange.id=segment.exchange_id WHERE exchange.id='binance' AND exchange.environment='production_public'
      AND segment.state='ready' AND segment.ended_at>=$1) AND
      (SELECT count(*) FROM instrument_metadata_versions metadata JOIN exchanges exchange
      ON exchange.id=metadata.exchange_id WHERE exchange.id='binance' AND exchange.environment='production_public')>=2`,
		now.Add(-5*time.Minute)).Scan(&publicReady); err != nil || !publicReady {
		return nil, fmt.Errorf("a11_startup_recovery_public_market_invalid")
	}
	facts = append(facts, map[string]any{"public_market_ready": publicReady})
	var invariantBlockers int
	if err := tx.QueryRow(ctx, `SELECT
      (SELECT count(*) FROM incidents WHERE state<>'resolved' AND severity='critical')+
      (SELECT count(*) FROM quarantined_scopes WHERE released_at IS NULL)+
      (SELECT count(*) FROM circuit_breaker_events WHERE breaker_kind IN ('disk_failure','persistence_failure','lease_loss'))`).Scan(&invariantBlockers); err != nil || invariantBlockers != 0 {
		return nil, fmt.Errorf("a11_startup_recovery_invariants_invalid")
	}
	facts = append(facts, map[string]any{"invariant_blockers": invariantBlockers},
		map[string]any{"administrative_state": riskState, "auto_unpause": false})
	return facts, nil
}

func hashA11RecoveryFacts(stageFacts []any) ([]string, error) {
	if len(stageFacts) != len(runtimecore.RecoverySequence()) {
		return nil, fmt.Errorf("a11_startup_recovery_evidence_invalid")
	}
	hashes := make([]string, len(stageFacts))
	for index, fact := range stageFacts {
		payload, _ := json.Marshal(struct {
			Stage string `json:"stage"`
			Fact  any    `json:"fact"`
		}{Stage: string(runtimecore.RecoverySequence()[index]), Fact: fact})
		hashes[index] = a11SHA256(payload)
	}
	return hashes, nil
}
