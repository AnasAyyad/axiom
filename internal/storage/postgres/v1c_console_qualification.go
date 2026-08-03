package postgres

import (
	"context"
	"errors"
	"time"

	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

const v1cConsoleLatestQualificationSQL = `
SELECT id,mode,state,commit_sha,build_hash,executable_hash,image_hash,
       configuration_hash,required_duration_seconds,
       observed_duration_seconds,profitability_evidence,qualified,
       started_at,ended_at,evidence_hash
FROM v1c_c6_qualification_runs
ORDER BY created_at DESC,id DESC LIMIT 1`

// C6Qualification exposes the latest immutable runner and chaos state. A
// smoke pass remains explicitly non-qualified and leaves the formal soak open.
func (store *A11ConsoleStore) C6Qualification(
	ctx context.Context,
) (generated.C6QualificationStatus, error) {
	status := defaultC6QualificationStatus(store.clock.Now().UTC)
	row, err := store.loadC6Qualification(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		chaos, chaosErr := store.v1cConsoleChaos(ctx, "")
		status.Chaos = chaos
		return status, chaosErr
	}
	if err != nil {
		return generated.C6QualificationStatus{}, err
	}
	applyC6QualificationRow(&status, row)
	status.Failures, err = store.v1cConsoleQualificationFailures(ctx, row.id)
	if err != nil {
		return generated.C6QualificationStatus{}, err
	}
	status.Chaos, err = store.v1cConsoleChaos(ctx, row.id)
	if err == nil {
		status.Slo, err = store.v1cConsoleQualificationSLO(ctx, row.id)
	}
	return status, err
}

type c6QualificationRow struct {
	id, mode, state, commit, build, executable, configuration string
	image, evidence                                           *string
	required, observed                                        int64
	profitability, qualified                                  bool
	started, ended                                            *time.Time
}

func defaultC6QualificationStatus(now time.Time) generated.C6QualificationStatus {
	return generated.C6QualificationStatus{
		State:                   generated.C6QualificationStatusStateNotStarted,
		Mode:                    generated.C6QualificationStatusModeNone,
		RequiredDurationSeconds: 259200,
		ObservedDurationSeconds: 0,
		ProfitabilityEvidence:   false,
		Qualified:               false,
		Failures:                []string{},
		FormalSoakPending:       true,
		AuditUrl:                "/api/v1/audit-events?event_type=v1c_c6",
		Chaos: generated.C6ChaosSummary{
			Status:         generated.C6ChaosSummaryStatusNotRun,
			Passed:         0,
			Failed:         0,
			LastObservedAt: now,
		},
		Slo: generated.C6SLOSummary{},
	}
}

func (store *A11ConsoleStore) loadC6Qualification(
	ctx context.Context,
) (c6QualificationRow, error) {
	var row c6QualificationRow
	err := store.pool.QueryRow(ctx, v1cConsoleLatestQualificationSQL).Scan(
		&row.id, &row.mode, &row.state, &row.commit, &row.build,
		&row.executable, &row.image, &row.configuration, &row.required,
		&row.observed, &row.profitability, &row.qualified,
		&row.started, &row.ended, &row.evidence,
	)
	return row, err
}

func applyC6QualificationRow(
	status *generated.C6QualificationStatus,
	row c6QualificationRow,
) {
	status.Id = &row.id
	status.Mode = generated.C6QualificationStatusMode(row.mode)
	status.State = generated.C6QualificationStatusState(row.state)
	status.CommitSha = &row.commit
	status.BuildHash = &row.build
	status.ExecutableHash = &row.executable
	status.ImageHash = row.image
	status.ConfigurationHash = &row.configuration
	status.RequiredDurationSeconds = row.required
	status.ObservedDurationSeconds = row.observed
	status.ProfitabilityEvidence =
		generated.C6QualificationStatusProfitabilityEvidence(row.profitability)
	status.Qualified = row.qualified
	status.StartedAt = utcPointer(row.started)
	status.EndedAt = utcPointer(row.ended)
	status.EvidenceHash = row.evidence
	status.FormalSoakPending = row.mode != "formal" || row.state != "PASSED"
}

func (store *A11ConsoleStore) v1cConsoleQualificationFailures(
	ctx context.Context,
	runID string,
) ([]string, error) {
	rows, err := store.pool.Query(ctx, `
SELECT reason FROM v1c_c6_qualification_failures
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

func (store *A11ConsoleStore) v1cConsoleChaos(
	ctx context.Context,
	runID string,
) (generated.C6ChaosSummary, error) {
	var passed, failed int
	var observed *time.Time
	err := store.pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE outcome='PASSED')::integer,
       count(*) FILTER (WHERE outcome='FAILED')::integer,
       max(occurred_at)
FROM v1c_c6_chaos_events
WHERE ($1='' OR run_id=$1)`, runID).Scan(&passed, &failed, &observed)
	if err != nil {
		return generated.C6ChaosSummary{}, err
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
	return generated.C6ChaosSummary{
		Status:         generated.C6ChaosSummaryStatus(status),
		Passed:         passed,
		Failed:         failed,
		LastObservedAt: last,
	}, nil
}
