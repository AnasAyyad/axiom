package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// CreateCanarySession binds one fresh READY_PAUSED account epoch to a new
// single-account session. It never arms the account.
func (store *V1CDispatcherStore) CreateCanarySession(
	ctx context.Context,
	command sandbox.CanarySessionCommand,
) (sandbox.CanarySession, error) {
	if !validCanarySessionCommand(command) {
		return sandbox.CanarySession{}, fmt.Errorf("v1c_canary_session_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return sandbox.CanarySession{}, fmt.Errorf("v1c_canary_session_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	result, err := selectCanaryAccount(ctx, tx, command)
	if err != nil {
		return sandbox.CanarySession{}, err
	}
	if err = insertCanarySession(ctx, tx, command, result); err != nil {
		return sandbox.CanarySession{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return sandbox.CanarySession{}, fmt.Errorf("v1c_canary_session_commit_failed")
	}
	return result, nil
}

func selectCanaryAccount(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.CanarySessionCommand,
) (sandbox.CanarySession, error) {
	var result sandbox.CanarySession
	var epoch, cycle int64
	var observedAt time.Time
	err := tx.QueryRow(ctx, canaryAccountSelectionSQL,
		command.Exchange,
		command.Instrument,
		command.CreatedAt,
	).Scan(&result.AccountID, &epoch, &cycle, &observedAt)
	if err != nil || epoch <= 0 || cycle <= 0 ||
		observedAt.After(command.CreatedAt) ||
		command.CreatedAt.Sub(observedAt) > 250*time.Millisecond {
		return sandbox.CanarySession{}, fmt.Errorf("v1c_canary_account_not_ready")
	}
	return sandbox.CanarySession{
		ID: command.ID, AccountID: result.AccountID,
		AccountEpoch: uint64(epoch), Exchange: command.Exchange,
		StartupCycle: uint64(cycle), Revision: 1,
	}, nil
}

const canaryAccountSelectionSQL = `
SELECT account.id,account.current_epoch,observation.startup_cycle,
       observation.observed_at
FROM v1c_exchange_accounts account
JOIN v1c_engine_observations observation
  ON observation.account_id=account.id
 AND observation.account_epoch=account.current_epoch
JOIN v1c_account_leases lease
  ON lease.account_id=account.id
 AND lease.fencing_token=observation.startup_cycle
WHERE account.exchange=$1
  AND account.state='READY_PAUSED'
  AND observation.exchange=$1
  AND observation.private_stream_healthy
  AND observation.reconciliation_clean
  AND observation.evidence_healthy
  AND observation.eligibility->>'instrument'=$2
  AND (observation.eligibility->>'eligible')::boolean
  AND lease.expires_at>$3
  AND NOT EXISTS(
    SELECT 1
    FROM v1c_sandbox_session_accounts membership
    JOIN v1c_sandbox_sessions session ON session.id=membership.session_id
    WHERE membership.account_id=account.id
      AND membership.account_epoch=account.current_epoch
      AND session.state IN ('READY_PAUSED','ARMED','PAUSED')
  )
ORDER BY account.id
FOR UPDATE OF account
LIMIT 1`

func insertCanarySession(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.CanarySessionCommand,
	result sandbox.CanarySession,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_sandbox_sessions(
 id,state,configuration_id,strategy_set_hash,revision,created_by,
 created_at,updated_at
) VALUES ($1,'READY_PAUSED',$2,$3,1,$4,$5,$5)`,
		command.ID,
		command.ConfigurationID,
		command.StrategySetHash,
		command.CreatedBy,
		command.CreatedAt,
	); err != nil {
		return fmt.Errorf("v1c_canary_session_insert_failed")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_sandbox_session_accounts(
 session_id,account_id,account_epoch
) VALUES ($1,$2,$3)`,
		command.ID,
		result.AccountID,
		result.AccountEpoch,
	); err != nil {
		return fmt.Errorf("v1c_canary_membership_insert_failed")
	}
	return nil
}

// CanaryAdmission returns the complete current entry-safety projection for one
// already-created arm. The caller-provided switches come from the immutable
// product snapshot and are revalidated by BuildCanaryPlan.
func (store *V1CDispatcherStore) CanaryAdmission(
	ctx context.Context,
	session sandbox.SessionID,
	armID string,
	account sandbox.AccountID,
	exchange sandbox.Exchange,
	instrument string,
	approvedAt time.Time,
	switches [4]bool,
) (sandbox.EligibilitySnapshot, sandbox.EntrySafetySnapshot, uint64, error) {
	if session == "" || armID == "" || account == "" || instrument == "" ||
		approvedAt.IsZero() || approvedAt.Location() != time.UTC {
		return sandbox.EligibilitySnapshot{}, sandbox.EntrySafetySnapshot{}, 0,
			fmt.Errorf("v1c_canary_admission_invalid")
	}
	facts, err := store.readCanaryAdmissionFacts(
		ctx, session, armID, account, exchange, instrument, approvedAt,
	)
	if err != nil {
		return sandbox.EligibilitySnapshot{},
			sandbox.EntrySafetySnapshot{}, 0, err
	}
	return buildCanaryAdmission(account, exchange, approvedAt, switches, facts)
}

type canaryAdmissionFacts struct {
	eligibilityJSON                     []byte
	epoch, cycle                        int64
	state                               string
	privateHealthy, reconciliationClean bool
	evidenceHealthy, leaseHeld          bool
	openAccount, openGlobal             int
	dailyAvailable                      bool
}

func (store *V1CDispatcherStore) readCanaryAdmissionFacts(
	ctx context.Context,
	session sandbox.SessionID,
	armID string,
	account sandbox.AccountID,
	exchange sandbox.Exchange,
	instrument string,
	approvedAt time.Time,
) (canaryAdmissionFacts, error) {
	var facts canaryAdmissionFacts
	err := store.pool.QueryRow(ctx, canaryAdmissionFactsSQL,
		account,
		exchange,
		instrument,
		session,
		armID,
		approvedAt,
		approvedAt.Format("2006-01-02"),
	).Scan(
		&facts.epoch, &facts.state, &facts.cycle, &facts.eligibilityJSON,
		&facts.privateHealthy, &facts.reconciliationClean,
		&facts.evidenceHealthy, &facts.leaseHeld,
		&facts.openAccount, &facts.openGlobal, &facts.dailyAvailable,
	)
	if err != nil || facts.epoch <= 0 || facts.cycle <= 0 {
		return canaryAdmissionFacts{},
			fmt.Errorf("v1c_canary_admission_unavailable")
	}
	return facts, nil
}

const canaryAdmissionFactsSQL = `
SELECT account.current_epoch,account.state,observation.startup_cycle,
       observation.eligibility,
       observation.private_stream_healthy,
       observation.reconciliation_clean,
       observation.evidence_healthy,
       EXISTS(
         SELECT 1 FROM v1c_account_leases lease
         WHERE lease.account_id=account.id
           AND lease.fencing_token=observation.startup_cycle
           AND lease.expires_at>$6
       ),
       (SELECT count(*) FROM v1c_submission_outbox
        WHERE account_id=account.id
          AND state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')),
       (SELECT count(*) FROM v1c_submission_outbox
        WHERE state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')),
       coalesce((
         SELECT reserved_notional<50 FROM v1c_daily_cap_counters
         WHERE utc_day=$7
       ),true)
FROM v1c_exchange_accounts account
JOIN v1c_engine_observations observation
  ON observation.account_id=account.id
 AND observation.account_epoch=account.current_epoch
JOIN v1c_sandbox_session_accounts membership
  ON membership.account_id=account.id
 AND membership.account_epoch=account.current_epoch
JOIN v1c_sandbox_sessions session ON session.id=membership.session_id
JOIN v1c_sandbox_arms arm
  ON arm.sandbox_session_id=session.id
WHERE account.id=$1
  AND account.exchange=$2
  AND observation.exchange=$2
  AND observation.eligibility->>'instrument'=$3
  AND session.id=$4
  AND session.state='ARMED'
  AND arm.id=$5
  AND arm.revoked_at IS NULL
  AND arm.expires_at>$6
  AND observation.observed_at<=$6
  AND $6-observation.observed_at<=interval '250 milliseconds'`

func buildCanaryAdmission(
	account sandbox.AccountID,
	exchange sandbox.Exchange,
	approvedAt time.Time,
	switches [4]bool,
	facts canaryAdmissionFacts,
) (sandbox.EligibilitySnapshot, sandbox.EntrySafetySnapshot, uint64, error) {
	var eligibility sandbox.EligibilitySnapshot
	if json.Unmarshal(facts.eligibilityJSON, &eligibility) != nil ||
		!eligibility.Eligible {
		return sandbox.EligibilitySnapshot{}, sandbox.EntrySafetySnapshot{}, 0,
			fmt.Errorf("v1c_canary_eligibility_invalid")
	}
	safety := sandbox.EntrySafetySnapshot{
		AccountID: account, AccountEpoch: uint64(facts.epoch), Exchange: exchange,
		ObservedAt: approvedAt, State: sandbox.EngineState(facts.state), ArmActive: true,
		GlobalIntegrationEnabled:   switches[0],
		GlobalSubmissionEnabled:    switches[1],
		ExchangeIntegrationEnabled: switches[2],
		ExchangeSubmissionEnabled:  switches[3],
		PublicEligible:             eligibility.Eligible,
		PrivateStreamHealthy:       facts.privateHealthy, AccountStateFresh: true,
		ReconciliationClean: facts.reconciliationClean, LeaseHeld: facts.leaseHeld,
		EvidenceHealthy:        facts.evidenceHealthy,
		OpenCapacityAvailable:  facts.openAccount == 0 && facts.openGlobal < 2,
		DailyCapacityAvailable: facts.dailyAvailable,
	}
	if safety.CanSubmitEntry() != nil {
		return sandbox.EligibilitySnapshot{}, sandbox.EntrySafetySnapshot{}, 0,
			fmt.Errorf("v1c_canary_entry_blocked")
	}
	return eligibility, safety, uint64(facts.cycle), nil
}

// RecordCanaryEvidence appends one immutable, hash-only lifecycle fact.
func (store *V1CDispatcherStore) RecordCanaryEvidence(
	ctx context.Context,
	record sandbox.CanaryEvidence,
) (string, error) {
	if !validCanaryEvidence(record) {
		return "", fmt.Errorf("v1c_canary_evidence_invalid")
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{
		record.CanaryID, string(record.Exchange), string(record.AccountID),
		fmt.Sprint(record.AccountEpoch), string(record.SessionID), record.PlanID,
		string(record.Stage), fmt.Sprint(record.StartupCycle),
		record.FactHash, record.ObservedAt.Format(time.RFC3339Nano),
	}, "\x00")))
	evidenceHash := hex.EncodeToString(digest[:])
	id := "v1c-canary-" + evidenceHash[:32]
	tag, err := store.pool.Exec(ctx, `
INSERT INTO v1c_canary_evidence(
 id,canary_id,exchange,account_id,account_epoch,sandbox_session_id,
 plan_id,stage,startup_cycle,evidence_hash,observed_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (exchange,canary_id,stage) DO NOTHING`,
		id, record.CanaryID, record.Exchange, record.AccountID,
		record.AccountEpoch, record.SessionID, record.PlanID, record.Stage,
		record.StartupCycle, evidenceHash, record.ObservedAt,
	)
	if err != nil {
		return "", fmt.Errorf("v1c_canary_evidence_insert_failed")
	}
	if tag.RowsAffected() == 0 {
		var same bool
		err = store.pool.QueryRow(ctx, `
SELECT evidence_hash=$3
FROM v1c_canary_evidence
WHERE exchange=$1 AND canary_id=$2 AND stage=$4`,
			record.Exchange, record.CanaryID, evidenceHash, record.Stage,
		).Scan(&same)
		if err != nil || !same {
			return "", fmt.Errorf("v1c_canary_evidence_conflict")
		}
	}
	return evidenceHash, nil
}

func validCanarySessionCommand(command sandbox.CanarySessionCommand) bool {
	return command.ID != "" &&
		(command.Exchange == sandbox.ExchangeBinance ||
			command.Exchange == sandbox.ExchangeBybit) &&
		(command.Instrument == "BTCUSDT" ||
			command.Instrument == "ETHUSDT") &&
		command.ConfigurationID != "" && validHash(command.StrategySetHash) &&
		command.CreatedBy != "" && !command.CreatedAt.IsZero() &&
		command.CreatedAt.Location() == time.UTC
}

func validCanaryEvidence(record sandbox.CanaryEvidence) bool {
	validStage := record.Stage == sandbox.CanaryPlanApproved ||
		record.Stage == sandbox.CanaryQuerySucceeded ||
		record.Stage == sandbox.CanaryCancelOrFillConfirmed ||
		record.Stage == sandbox.CanaryReconciled ||
		record.Stage == sandbox.CanaryRestartVerified
	return record.CanaryID != "" &&
		(record.Exchange == sandbox.ExchangeBinance ||
			record.Exchange == sandbox.ExchangeBybit) &&
		record.AccountID != "" && record.AccountEpoch > 0 &&
		record.SessionID != "" && record.PlanID != "" && validStage &&
		record.StartupCycle > 0 && validHash(record.FactHash) &&
		!record.ObservedAt.IsZero() &&
		record.ObservedAt.Location() == time.UTC
}
