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
SELECT id,mode,state,revision,created_at,updated_at
FROM (
  SELECT id,job_type AS mode,state,progress_revision AS revision,created_at,updated_at
  FROM jobs WHERE job_type IN ('backtest','replay') AND request_payload IS NOT NULL
  UNION ALL
  SELECT id,'shadow' AS mode,state,revision,created_at,coalesce(stopped_at,created_at)
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
SELECT id,mode,state,revision,created_at,updated_at
FROM (
  SELECT id,job_type AS mode,state,progress_revision AS revision,created_at,updated_at
  FROM jobs WHERE job_type IN ('backtest','replay') AND request_payload IS NOT NULL
  UNION ALL
  SELECT id,'shadow' AS mode,state,revision,created_at,coalesce(stopped_at,created_at)
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
	var id, mode, state string
	var revision int64
	var created, updated time.Time
	if err := scanner.Scan(&id, &mode, &state, &revision, &created, &updated); err != nil {
		return generated.RunResource{}, err
	}
	item := generated.RunResource{Id: id, Mode: generated.RunResourceMode(mode), State: state,
		Revision: strconv.FormatInt(revision, 10), CreatedAt: created.UTC(), UpdatedAt: ptr(updated.UTC()),
		StrategyId: "trend-following", StrategyVersion: "trend-following@1.0.0", OrderCapable: true}
	switch mode {
	case "backtest":
		item.FriendlyName, item.Environment = "Trend Following backtest", generated.RunResourceEnvironment("recorded_data")
	case "replay":
		item.FriendlyName, item.Environment = "Trend Following replay", generated.RunResourceEnvironment("recorded_data")
	case "shadow":
		item.FriendlyName, item.Environment = "Trend Following live shadow", generated.RunResourceEnvironment("production_public")
	default:
		return generated.RunResource{}, console.ErrNotFound
	}
	if reason := ownerRunWaitingReason(mode, state); reason != "" {
		item.WaitingReason = &reason
	}
	return item, nil
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
