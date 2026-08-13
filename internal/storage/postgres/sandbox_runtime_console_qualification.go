package postgres

import (
	"context"
	"errors"
	"time"

	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

const sandboxRuntimeConsoleLatestQualificationSQL = `
SELECT id,mode,state,commit_sha,build_hash,executable_hash,image_hash,
       configuration_hash,required_duration_seconds,
       observed_duration_seconds,profitability_evidence,qualified,
       started_at,ended_at,evidence_hash
FROM sandbox_qualification_runs
ORDER BY created_at DESC,id DESC LIMIT 1`

const sandboxRuntimeConsoleRecoveryIncidentsSQL = `
WITH latest AS (
 SELECT DISTINCT ON (account_id)
   account_id,exchange,environment,state,incident_source,failure_kind,cause_code,
   deadline_at,clean_check_count,recovery_timestamp,evidence_hash
 FROM sandbox_qualification_recovery_events
 WHERE run_id=$1
 ORDER BY account_id,occurred_at DESC,id DESC
)
SELECT latest.account_id,latest.exchange,latest.environment,latest.state,
       latest.incident_source,latest.failure_kind,latest.cause_code,latest.deadline_at,
       latest.clean_check_count,detected.occurred_at,
       latest.recovery_timestamp,latest.evidence_hash
FROM latest
JOIN LATERAL (
 SELECT occurred_at FROM sandbox_qualification_recovery_events event
 WHERE event.run_id=$1 AND event.account_id=latest.account_id
 ORDER BY occurred_at,id LIMIT 1
) detected ON true
ORDER BY latest.exchange,latest.account_id`

// SandboxQualification exposes the latest immutable runner and chaos state. A
// smoke pass remains explicitly non-qualified and leaves the formal soak open.
func (store *OwnerConsoleStore) SandboxQualification(
	ctx context.Context,
) (generated.SandboxQualificationStatus, error) {
	status := defaultSandboxQualificationStatus(store.clock.Now().UTC)
	row, err := store.loadSandboxQualification(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		chaos, chaosErr := store.sandboxRuntimeConsoleChaos(ctx, "")
		status.Chaos = chaos
		return status, chaosErr
	}
	if err != nil {
		return generated.SandboxQualificationStatus{}, err
	}
	applySandboxQualificationRow(&status, row)
	status.Failures, err = store.sandboxRuntimeConsoleQualificationFailures(ctx, row.id)
	if err != nil {
		return generated.SandboxQualificationStatus{}, err
	}
	status.Chaos, err = store.sandboxRuntimeConsoleChaos(ctx, row.id)
	if err == nil {
		status.Slo, err = store.sandboxRuntimeConsoleQualificationSLO(ctx, row.id)
	}
	if err == nil {
		status.RecoveryIncidents, err = store.sandboxRuntimeConsoleRecoveryIncidents(
			ctx, row.id,
		)
	}
	return status, err
}

type sandboxQualificationRow struct {
	id, mode, state, commit, build, executable, configuration string
	image, evidence                                           *string
	required, observed                                        int64
	profitability, qualified                                  bool
	started, ended                                            *time.Time
}

func defaultSandboxQualificationStatus(now time.Time) generated.SandboxQualificationStatus {
	return generated.SandboxQualificationStatus{
		State:                   generated.SandboxQualificationStatusStateNotStarted,
		Mode:                    generated.SandboxQualificationStatusModeNone,
		RequiredDurationSeconds: 259200,
		ObservedDurationSeconds: 0,
		ProfitabilityEvidence:   false,
		Qualified:               false,
		Failures:                []string{},
		RecoveryIncidents:       []generated.SandboxRecoveryIncident{},
		FormalSoakPending:       true,
		AuditUrl:                "/api/v1/audit-events?event_type=sandbox_runtime_sandbox_qualification",
		Chaos: generated.SandboxChaosSummary{
			Status:         generated.SandboxChaosSummaryStatusNotRun,
			Passed:         0,
			Failed:         0,
			LastObservedAt: now,
		},
		Slo: generated.SandboxSLOSummary{},
	}
}

func (store *OwnerConsoleStore) sandboxRuntimeConsoleRecoveryIncidents(
	ctx context.Context,
	runID string,
) ([]generated.SandboxRecoveryIncident, error) {
	rows, err := store.pool.Query(ctx, sandboxRuntimeConsoleRecoveryIncidentsSQL, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]generated.SandboxRecoveryIncident, 0)
	for rows.Next() {
		item, scanErr := scanSandboxRuntimeConsoleRecoveryIncident(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanSandboxRuntimeConsoleRecoveryIncident(
	scanner sandboxRuntimeConsoleRowScanner,
) (generated.SandboxRecoveryIncident, error) {
	var item generated.SandboxRecoveryIncident
	var state, incidentSource, reasonCategory, causeCode string
	var accountID, exchange, environment string
	var deadline, detected time.Time
	var recoveryAt *time.Time
	var cleanChecks int
	err := scanner.Scan(
		&accountID, &exchange, &environment, &state, &incidentSource, &reasonCategory,
		&causeCode, &deadline, &cleanChecks, &detected, &recoveryAt,
		&item.EvidenceHash,
	)
	if err != nil {
		return generated.SandboxRecoveryIncident{}, err
	}
	item.AccountId = accountID
	item.Exchange = generated.SandboxRecoveryIncidentExchange(exchange)
	item.Environment = generated.SandboxRecoveryIncidentEnvironment(environment)
	item.State = generated.SandboxRecoveryIncidentState(state)
	item.IncidentSource = generated.SandboxRecoveryIncidentIncidentSource(incidentSource)
	item.ReasonCategory = reasonCategory
	item.CauseCode = causeCode
	item.DeadlineAt = deadline
	item.CleanCheckCount = cleanChecks
	item.DetectedAt = detected
	item.RecoveryTimestamp = recoveryAt
	return item, nil
}

func (store *OwnerConsoleStore) loadSandboxQualification(
	ctx context.Context,
) (sandboxQualificationRow, error) {
	var row sandboxQualificationRow
	err := store.pool.QueryRow(ctx, sandboxRuntimeConsoleLatestQualificationSQL).Scan(
		&row.id, &row.mode, &row.state, &row.commit, &row.build,
		&row.executable, &row.image, &row.configuration, &row.required,
		&row.observed, &row.profitability, &row.qualified,
		&row.started, &row.ended, &row.evidence,
	)
	return row, err
}

func applySandboxQualificationRow(
	status *generated.SandboxQualificationStatus,
	row sandboxQualificationRow,
) {
	status.Id = &row.id
	status.Mode = generated.SandboxQualificationStatusMode(row.mode)
	status.State = generated.SandboxQualificationStatusState(row.state)
	status.CommitSha = &row.commit
	status.BuildHash = &row.build
	status.ExecutableHash = &row.executable
	status.ImageHash = row.image
	status.ConfigurationHash = &row.configuration
	status.RequiredDurationSeconds = row.required
	status.ObservedDurationSeconds = row.observed
	status.ProfitabilityEvidence =
		generated.SandboxQualificationStatusProfitabilityEvidence(row.profitability)
	status.Qualified = row.qualified
	status.StartedAt = utcPointer(row.started)
	status.EndedAt = utcPointer(row.ended)
	status.EvidenceHash = row.evidence
	status.FormalSoakPending = row.mode != "formal" || row.state != "PASSED"
}

func (store *OwnerConsoleStore) sandboxRuntimeConsoleQualificationFailures(
	ctx context.Context,
	runID string,
) ([]string, error) {
	rows, err := store.pool.Query(ctx, `
SELECT reason FROM sandbox_qualification_failures
WHERE run_id=$1 ORDER BY occurred_at,id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var reason string
		if err = rows.Scan(&reason); err != nil {
			return nil, err
		}
		result = append(result, reason)
	}
	return result, rows.Err()
}

func (store *OwnerConsoleStore) sandboxRuntimeConsoleChaos(
	ctx context.Context,
	runID string,
) (generated.SandboxChaosSummary, error) {
	var passed, failed int
	var observed *time.Time
	err := store.pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE outcome='PASSED')::integer,
       count(*) FILTER (WHERE outcome='FAILED')::integer,
       max(occurred_at)
FROM sandbox_qualification_chaos_events
WHERE ($1='' OR run_id=$1)`, runID).Scan(&passed, &failed, &observed)
	if err != nil {
		return generated.SandboxChaosSummary{}, err
	}
	status := "not_run"
	if failed > 0 {
		status = "failed"
	} else if passed > 0 {
		status = "passed"
	}
	last := store.clock.Now().UTC
	if observed != nil {
		last = observed.UTC()
	}
	return generated.SandboxChaosSummary{
		Status:         generated.SandboxChaosSummaryStatus(status),
		Passed:         passed,
		Failed:         failed,
		LastObservedAt: last,
	}, nil
}
