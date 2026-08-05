package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

// Runs joins the existing durable research-job and public-shadow lifecycles
// into one semantic owner projection. The underlying records stay immutable
// evidence; this method only changes how they are presented to the browser.
func (store *A11ConsoleStore) Runs(ctx context.Context) (generated.RunPage, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id,mode,state,revision,created_at,updated_at,strategy_version_id
FROM (
  SELECT job.id,job.job_type AS mode,job.state,job.progress_revision AS revision,job.created_at,job.updated_at,
    run.strategy_version_id
  FROM jobs job JOIN runs run ON run.id=job.id
  WHERE job.job_type IN ('backtest','replay') AND job.request_payload IS NOT NULL
  UNION ALL
  SELECT id,'shadow' AS mode,state,revision,created_at,coalesce(stopped_at,created_at),strategy_version_id
  FROM shadow_sessions
) run_records
ORDER BY created_at DESC,id DESC LIMIT 100`)
	if err != nil {
		return generated.RunPage{}, err
	}
	defer rows.Close()
	page := generated.RunPage{Items: make([]generated.RunResource, 0)}
	for rows.Next() {
		item, scanErr := scanOwnerRun(rows)
		if scanErr != nil {
			return generated.RunPage{}, scanErr
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

// Run reads an existing opaque run ID selected from the owner-facing list.
// It never asks the browser for an internal configuration or dataset ID.
func (store *A11ConsoleStore) Run(ctx context.Context, id string) (generated.RunResource, error) {
	row := store.pool.QueryRow(ctx, `
SELECT id,mode,state,revision,created_at,updated_at,strategy_version_id
FROM (
  SELECT job.id,job.job_type AS mode,job.state,job.progress_revision AS revision,job.created_at,job.updated_at,
    run.strategy_version_id
  FROM jobs job JOIN runs run ON run.id=job.id
  WHERE job.job_type IN ('backtest','replay') AND job.request_payload IS NOT NULL
  UNION ALL
  SELECT id,'shadow' AS mode,state,revision,created_at,coalesce(stopped_at,created_at),strategy_version_id
  FROM shadow_sessions
) run_records WHERE id=$1`, id)
	item, err := scanOwnerRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.RunResource{}, console.ErrNotFound
	}
	return item, err
}

type ownerRunScanner interface{ Scan(...any) error }

func scanOwnerRun(scanner ownerRunScanner) (generated.RunResource, error) {
	var id, mode, state, strategyVersionID string
	var revision int64
	var created, updated time.Time
	if err := scanner.Scan(&id, &mode, &state, &revision, &created, &updated, &strategyVersionID); err != nil {
		return generated.RunResource{}, err
	}
	strategyID, strategyVersion, strategyName, ok := ownerRunStrategy(strategyVersionID)
	if !ok {
		return generated.RunResource{}, console.ErrNotFound
	}
	item := generated.RunResource{Id: id, Mode: generated.RunResourceMode(mode), State: state,
		Revision: strconv.FormatInt(revision, 10), CreatedAt: created.UTC(), UpdatedAt: ptr(updated.UTC()),
		StrategyId: strategyID, StrategyVersion: strategyVersion, OrderCapable: true}
	switch mode {
	case "backtest":
		item.FriendlyName, item.Environment = strategyName+" backtest", generated.RunResourceEnvironment("recorded_data")
	case "replay":
		item.FriendlyName, item.Environment = strategyName+" replay", generated.RunResourceEnvironment("recorded_data")
	case "shadow":
		item.FriendlyName, item.Environment = strategyName+" live shadow", generated.RunResourceEnvironment("production_public")
	default:
		return generated.RunResource{}, console.ErrNotFound
	}
	if reason := ownerRunWaitingReason(mode, state); reason != "" {
		item.WaitingReason = &reason
	}
	return item, nil
}

func ownerRunStrategy(value string) (id, version, name string, ok bool) {
	switch value {
	case "trend-v1a-1":
		return "trend-following", "trend-following@1.0.0", "Trend Following", true
	case "mean-reversion-v1b-1":
		return "mean-reversion", "mean-reversion@1.0.0", "Mean Reversion", true
	default:
		return "", "", "", false
	}
}

func ownerRunWaitingReason(mode, state string) string {
	switch state {
	case "QUEUED":
		if mode == "shadow" {
			return "Waiting for the public-data shadow worker to prepare this simulation."
		}
		return "Waiting for a worker to claim this recorded-data run."
	case "PAUSED", "PAUSE_REQUESTED":
		return "Paused by the owner; no new evaluation will start until resumed."
	case "CANCEL_REQUESTED":
		return "Stopping safely; existing work is being reconciled before the run closes."
	}
	return ""
}

var _ console.RunReadService = (*A11ConsoleStore)(nil)
