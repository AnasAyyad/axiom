package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/qualification/sandboxqualification"
)

// AssertChaosRun verifies that deterministic fault evidence will be bound to
// the exact clean-source run while it is actively being observed.
func (store *SandboxQualificationStore) AssertChaosRun(
	ctx context.Context,
	runID, commitSHA string,
) (time.Time, error) {
	var (
		state   string
		commit  string
		started time.Time
	)
	err := store.pool.QueryRow(ctx, `
SELECT state,commit_sha,started_at
FROM sandbox_qualification_runs WHERE id=$1`, runID).Scan(
		&state, &commit, &started,
	)
	if err != nil || state != "RUNNING" || commit != commitSHA ||
		started.IsZero() {
		return time.Time{}, fmt.Errorf("sandbox_qualification_chaos_run_rejected")
	}
	return started.UTC(), nil
}

// Events returns the immutable deterministic chaos events for one sandbox qualification run.
func (store *SandboxQualificationStore) Events(
	ctx context.Context,
	runID string,
) ([]sandboxQualification.ChaosEvent, error) {
	rows, err := store.pool.Query(ctx, `
SELECT scenario,outcome,deterministic_seed_hash,evidence_hash,occurred_at
FROM sandbox_qualification_chaos_events WHERE run_id=$1 ORDER BY scenario,id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]sandboxQualification.ChaosEvent, 0, len(sandboxQualification.RequiredChaosScenarios))
	for rows.Next() {
		var event sandboxQualification.ChaosEvent
		if err = rows.Scan(
			&event.Scenario, &event.Outcome, &event.DeterministicSeedHash,
			&event.EvidenceHash, &event.OccurredAt,
		); err != nil {
			return nil, err
		}
		event.OccurredAt = event.OccurredAt.UTC()
		result = append(result, event)
	}
	return result, rows.Err()
}

// AppendChaosEvents records the complete run-bound deterministic scenario set.
// Rows are immutable and may preserve FAILED outcomes for terminal fail-closed
// evidence.
func (store *SandboxQualificationStore) AppendChaosEvents(
	ctx context.Context,
	runID string,
	events []sandboxQualification.ChaosEvent,
) error {
	if runID == "" || sandboxQualification.ValidateChaosEvidence(events) != nil {
		return fmt.Errorf("sandbox_qualification_chaos_evidence_rejected")
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var (
		state   string
		started time.Time
	)
	if err = tx.QueryRow(ctx, `
SELECT state,started_at
FROM sandbox_qualification_runs WHERE id=$1 FOR UPDATE`, runID).Scan(
		&state, &started,
	); err != nil || state != "RUNNING" || started.IsZero() {
		return fmt.Errorf("sandbox_qualification_chaos_run_rejected")
	}
	for index, event := range events {
		if event.OccurredAt.Before(started) {
			return fmt.Errorf("sandbox_qualification_chaos_evidence_predates_run")
		}
		if _, err = tx.Exec(ctx, `
INSERT INTO sandbox_qualification_chaos_events(
 id,run_id,scenario,outcome,deterministic_seed_hash,evidence_hash,occurred_at
) VALUES($1,$2,$3,$4,$5,$6,$7)`,
			fmt.Sprintf("%s-chaos-%03d", runID, index+1),
			runID,
			event.Scenario,
			event.Outcome,
			event.DeterministicSeedHash,
			event.EvidenceHash,
			event.OccurredAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
