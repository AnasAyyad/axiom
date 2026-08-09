package postgres

import (
	"context"

	"encoding/json"
	"errors"

	"strings"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

// ExecuteOwnerControl applies one closed, audited, idempotent owner control command. This service
// owns no exchange client and cannot submit an external order.
func (store *OwnerConsoleStore) ExecuteOwnerControl(
	ctx context.Context,
	principal authentication.Principal,
	command console.OwnerControlCommand,
) (generated.CommandAccepted, error) {
	payload, hash, err := ownerConsoleCommandPayload(map[string]any{
		"kind": command.Kind, "target_id": command.TargetID, "action": command.Action,
		"state": command.State, "expected_revision": command.ExpectedRevision,
		"reason": command.Reason, "payload": command.Payload,
	})
	if err != nil || command.ExpectedRevision <= 0 || command.Reason == "" ||
		command.IdempotencyKey == "" || command.TargetID == "" {
		return generated.CommandAccepted{}, console.ErrInvalidRequest
	}
	_ = payload
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	lock := "axiom:owner_console:owner_control:" + command.Kind + ":" + command.TargetID
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lock); err != nil {
		return generated.CommandAccepted{}, err
	}
	if existing, found, lookupErr := lookupOwnerConsoleCommand(
		ctx, tx, principal.UserID, command.IdempotencyKey, hash,
	); lookupErr != nil {
		return generated.CommandAccepted{}, lookupErr
	} else if found {
		return existing, tx.Commit(ctx)
	}
	if err = validateOwnerControlAuthorization(principal, command); err != nil {
		return generated.CommandAccepted{}, err
	}
	now := store.clock.Now().UTC
	commandID, _ := ownerConsoleIdentifier("command")
	auditID, _ := ownerConsoleIdentifier("audit")
	if err = insertOwnerConsoleCommand(ctx, tx, commandID, principal, command.IdempotencyKey,
		hash, command.Kind, command.Kind, command.TargetID, command.Reason,
		now, auditID, commandID); err != nil {
		return generated.CommandAccepted{}, err
	}
	result, err := store.applyOwnerControlCommand(ctx, tx, principal, command, commandID, now)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	accepted, err := completeOwnerConsoleCommand(ctx, tx, commandID, auditID, principal,
		command.Kind, command.TargetID, hash, result, now, commandID)
	return commitOwnerConsoleAccepted(ctx, tx, accepted, err)
}

func validateOwnerControlAuthorization(
	principal authentication.Principal,
	command console.OwnerControlCommand,
) error {
	var purpose authentication.AuthorizationPurpose
	switch {
	case command.Kind == "strategy_configuration":
		purpose = authentication.PurposeStrategyConfiguration
	case command.Kind == "risk_control" && command.State == "normal":
		purpose = authentication.PurposeRiskControl
	case command.Kind == "qualification" && command.Action == "start":
		purpose = authentication.PurposeQualificationStart
	case command.Kind == "configuration_activation":
		purpose = authentication.PurposeConfigurationActivation
	case command.Kind == "artifact_hold":
		purpose = authentication.PurposeArtifactHold
	default:
		return nil
	}
	if command.Authorization == nil || command.Authorization.TargetRevision == nil ||
		*command.Authorization.TargetRevision != command.ExpectedRevision ||
		command.Authorization.UserID != principal.UserID ||
		command.Authorization.Purpose != purpose ||
		command.Authorization.ReasonHash != authentication.AuthorizationBindingHash(command.Reason) {
		return console.ErrPrecondition
	}
	return nil
}

func (store *OwnerConsoleStore) applyOwnerControlCommand(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.OwnerControlCommand,
	commandID string,
	now time.Time,
) (map[string]any, error) {
	switch command.Kind {
	case "strategy_configuration":
		return store.applyOwnerControlStrategyConfiguration(ctx, tx, principal, command, now)
	case "strategy_runtime":
		return store.applyOwnerControlStrategyRuntime(ctx, tx, principal, command, now)
	case "risk_control":
		return store.applyOwnerControlRiskControl(ctx, tx, principal, command, now)
	case "alert":
		return applyOwnerControlAlert(ctx, tx, principal, command, now)
	case "report":
		return applyOwnerControlReport(ctx, tx, principal, command, commandID, now)
	case "report_schedule":
		return applyOperationalEvidenceReportSchedule(ctx, tx, principal, command, now)
	case "export":
		return applyOwnerControlExportDelete(ctx, tx, principal, command, now)
	case "artifact_hold":
		return applyOwnerControlArtifactHold(ctx, tx, principal, command, now)
	case "incident":
		return applyOwnerControlIncident(ctx, tx, principal, command, now)
	case "incident_create":
		return applyOperationalEvidenceIncidentCreate(ctx, tx, principal, command, now)
	case "incident_update":
		return applyOperationalEvidenceIncidentUpdate(ctx, tx, principal, command, now)
	case "alert_test":
		return applyOperationalEvidenceAlertTest(ctx, tx, principal, command, commandID, now)
	case "configuration_activation":
		return applyOwnerControlConfigurationActivation(ctx, tx, principal, command, now)
	case "lab_run":
		return applyOwnerControlLabControl(ctx, tx, principal, command, now)
	case "qualification":
		if command.Action == "start" {
			return applyOwnerControlQualificationStart(ctx, tx, principal, command, commandID, now)
		}
		return applyOwnerControlQualificationAbort(ctx, tx, principal, command, now)
	default:
		return nil, console.ErrInvalidRequest
	}
}

func (store *OwnerConsoleStore) applyOwnerControlStrategyConfiguration(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.OwnerControlCommand,
	now time.Time,
) (map[string]any, error) {
	runtimeState, revision, err := ownerControlStrategyControlState(ctx, tx, command.TargetID)
	if err != nil {
		return nil, err
	}
	if revision != command.ExpectedRevision ||
		(command.State != "enabled" && command.State != "disabled") {
		return nil, console.ErrConflict
	}
	configurationID, ok := command.Payload["configuration_id"].(string)
	if !ok || configurationID == "" {
		return nil, console.ErrInvalidRequest
	}
	configurationExists, err := ownerControlConfigurationExists(ctx, tx, configurationID)
	if err != nil {
		return nil, err
	}
	if !configurationExists {
		return nil, console.ErrPrecondition
	}
	blockerValues, err := store.ownerControlReadinessBlockers(ctx, tx, now)
	if err != nil {
		return nil, err
	}
	if command.State == "enabled" && len(blockerValues) > 0 {
		return nil, console.ErrPrecondition
	}
	nextRuntime := runtimeState
	if command.State == "disabled" {
		nextRuntime = "paused"
	}
	blockerJSON, _ := json.Marshal(blockerValues)
	if _, err = tx.Exec(ctx, `
UPDATE owner_console_strategy_controls SET configured_state=$2,runtime_state=$3,
  blocking_prerequisites=$4,configuration_id=$5,revision=revision+1,
  updated_by=$6,updated_at=$7 WHERE strategy_id=$1`, command.TargetID,
		command.State, nextRuntime, blockerJSON, configurationID, principal.UserID, now); err != nil {
		return nil, err
	}
	return map[string]any{"strategy_id": command.TargetID, "configured_state": command.State,
		"runtime_state": nextRuntime, "revision": revision + 1,
		"blocking_prerequisites": blockerValues, "real_trading_enabled": false}, nil
}

func ownerControlStrategyControlState(ctx context.Context, tx pgx.Tx, strategyID string) (string, int64, error) {
	var runtimeState string
	var revision int64
	err := tx.QueryRow(ctx, `SELECT runtime_state,revision FROM owner_console_strategy_controls
WHERE strategy_id=$1 FOR UPDATE`, strategyID).Scan(&runtimeState, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, console.ErrNotFound
	}
	return runtimeState, revision, err
}

func ownerControlConfigurationExists(ctx context.Context, tx pgx.Tx, configurationID string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
      SELECT 1 FROM configuration_versions WHERE id=$1
    )`, configurationID).Scan(&exists)
	return exists, err
}

func (store *OwnerConsoleStore) applyOwnerControlStrategyRuntime(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.OwnerControlCommand,
	now time.Time,
) (map[string]any, error) {
	var configured, current string
	var revision int64
	err := tx.QueryRow(ctx, `
SELECT configured_state,runtime_state,revision FROM owner_console_strategy_controls
WHERE strategy_id=$1 FOR UPDATE`, command.TargetID).Scan(&configured, &current, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, console.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if revision != command.ExpectedRevision ||
		(command.State != "running" && command.State != "paused") {
		return nil, console.ErrConflict
	}
	blockers, err := store.ownerControlReadinessBlockers(ctx, tx, now)
	if err != nil {
		return nil, err
	}
	if command.State == "running" && (configured != "enabled" || len(blockers) > 0) {
		return nil, console.ErrPrecondition
	}
	blockerJSON, _ := json.Marshal(blockers)
	if _, err = tx.Exec(ctx, `
UPDATE owner_console_strategy_controls SET runtime_state=$2,blocking_prerequisites=$3,
  revision=revision+1,updated_by=$4,updated_at=$5 WHERE strategy_id=$1`,
		command.TargetID, command.State, blockerJSON, principal.UserID, now); err != nil {
		return nil, err
	}
	return map[string]any{"strategy_id": command.TargetID, "runtime_state": command.State,
		"revision": revision + 1, "blocking_prerequisites": blockers}, nil
}

func (store *OwnerConsoleStore) ownerControlReadinessBlockers(
	ctx context.Context,
	tx pgx.Tx,
	now time.Time,
) ([]string, error) {
	return store.ownerControlReadinessBlockersExceptRisk(ctx, tx, now, "", "")
}

func (store *OwnerConsoleStore) ownerControlReadinessBlockersExceptRisk(
	ctx context.Context,
	tx pgx.Tx,
	now time.Time,
	excludedScope string,
	excludedScopeID string,
) ([]string, error) {
	var recentData, startupReady bool
	var critical, reconciliation, unknown, locked int
	err := tx.QueryRow(ctx, `SELECT
  EXISTS(SELECT 1 FROM market_data_segments WHERE state='ready' AND ended_at >= $1),
  EXISTS(SELECT 1 FROM startup_recovery_attempts WHERE state='ready_paused'),
  (SELECT count(*)::integer FROM incidents WHERE state<>'resolved' AND severity='critical'),
  (SELECT count(*)::integer FROM reconciliation_cases WHERE state IN ('open','quarantined')),
  (SELECT count(*)::integer FROM orders WHERE state='unknown'),
	  (SELECT count(*)::integer FROM owner_console_risk_controls
	   WHERE state='locked' AND ($2='' OR NOT (scope_type=$2 AND scope_id=$3)))`,
		now.Add(-5*time.Minute), excludedScope, excludedScopeID).Scan(
		&recentData, &startupReady, &critical, &reconciliation, &unknown, &locked,
	)
	if err != nil {
		return nil, err
	}
	blockers := make([]string, 0, 6)
	if !recentData {
		blockers = append(blockers, "market_data_unready")
	}
	if !startupReady {
		blockers = append(blockers, "persistence_recovery_unready")
	}
	if critical > 0 {
		blockers = append(blockers, "critical_incident_open")
	}
	if reconciliation > 0 {
		blockers = append(blockers, "reconciliation_unresolved")
	}
	if unknown > 0 {
		blockers = append(blockers, "unknown_order_present")
	}
	if locked > 0 {
		blockers = append(blockers, "risk_locked")
	}
	return blockers, nil
}

func (store *OwnerConsoleStore) applyOwnerControlRiskControl(
	ctx context.Context,
	tx pgx.Tx,
	principal authentication.Principal,
	command console.OwnerControlCommand,
	now time.Time,
) (map[string]any, error) {
	scope, scopeID, err := ownerControlRiskScope(command)
	if err != nil {
		return nil, console.ErrInvalidRequest
	}
	revision, err := ownerControlRiskRevision(ctx, tx, principal, scope, scopeID, command.ExpectedRevision, now)
	if err != nil {
		return nil, err
	}
	if revision != command.ExpectedRevision {
		return nil, console.ErrConflict
	}
	if command.State == "normal" {
		blockers, readinessErr := store.ownerControlReadinessBlockersExceptRisk(ctx, tx, now, scope, scopeID)
		if readinessErr != nil {
			return nil, readinessErr
		}
		if len(blockers) > 0 {
			return nil, console.ErrPrecondition
		}
	}
	if _, err = tx.Exec(ctx, `
UPDATE owner_console_risk_controls SET state=$3,revision=revision+1,
  reason_code=$4,updated_by=$5,updated_at=$6
WHERE scope_type=$1 AND scope_id=$2`, scope, scopeID, command.State,
		"manual_"+command.State, principal.UserID, now); err != nil {
		return nil, err
	}
	return map[string]any{"scope": scope, "scope_id": scopeID,
		"state": command.State, "revision": revision + 1}, nil
}

func ownerControlRiskScope(command console.OwnerControlCommand) (string, string, error) {
	parts := strings.SplitN(command.TargetID, ":", 2)
	validScope := len(parts) == 2 && strings.Contains(
		" global strategy instrument exchange new_entries ", " "+parts[0]+" ",
	)
	if !validScope || !strings.Contains(" normal paused locked ", " "+command.State+" ") {
		return "", "", console.ErrInvalidRequest
	}
	return parts[0], parts[1], nil
}

func ownerControlRiskRevision(
	ctx context.Context, tx pgx.Tx, principal authentication.Principal,
	scope, scopeID string, expected int64, now time.Time,
) (int64, error) {
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision FROM owner_console_risk_controls
WHERE scope_type=$1 AND scope_id=$2 FOR UPDATE`, scope, scopeID).Scan(&revision)
	if !errors.Is(err, pgx.ErrNoRows) {
		return revision, err
	}
	if expected != 1 {
		return 0, console.ErrConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO owner_console_risk_controls(
  scope_type,scope_id,state,revision,reason_code,updated_by,updated_at
) VALUES ($1,$2,'locked',1,'activity.unknown',$3,$4)`, scope, scopeID, principal.UserID, now)
	return 1, err
}
