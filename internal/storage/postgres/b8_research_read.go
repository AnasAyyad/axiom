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

const b8ChampionChallengerSQL = `SELECT report.id,report.champion_strategy_version_id,
  report.challenger_strategy_version_id,report.champion_suite_id,
  report.challenger_suite_id,challenger.confidence_label,
  challenger.viability_disposition,report.disposition,report.disclaimer_policy,
  report.manifest_hash,report.created_at
FROM b7_champion_challenger_reports report
JOIN b7_validation_suites challenger ON challenger.id=report.challenger_suite_id
WHERE $1::timestamptz IS NULL OR report.created_at<$1 OR
  (report.created_at=$1 AND report.id<$2)
ORDER BY report.created_at DESC,report.id DESC LIMIT $3`

// ChampionChallenger returns immutable B7 comparison reports.
func (store *A11ConsoleStore) ChampionChallenger(
	ctx context.Context, cursor string, limit int,
) (generated.ChampionChallengerPage, error) {
	var cursorTime time.Time
	var cursorID string
	var err error
	if cursor != "" {
		cursorTime, cursorID, err = decodeB8TimeCursor(store.cursor, "b8-champion-challenger", cursor)
		if err != nil {
			return generated.ChampionChallengerPage{}, err
		}
	}
	rows, err := store.pool.Query(ctx, b8ChampionChallengerSQL,
		nullableB8Time(cursorTime), cursorID, limit+1)
	if err != nil {
		return generated.ChampionChallengerPage{}, err
	}
	defer rows.Close()
	items := make([]generated.ChampionChallengerReport, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanB8Champion(rows)
		if scanErr != nil {
			return generated.ChampionChallengerPage{}, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return generated.ChampionChallengerPage{}, err
	}
	snapshot, err := b8SnapshotRevision(ctx, store.pool)
	if err != nil {
		return generated.ChampionChallengerPage{}, err
	}
	page := generated.ChampionChallengerPage{Items: items, Revision: snapshot, SnapshotRevision: snapshot}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		last := items[len(items)-1]
		next := encodeB8TimeCursor(store.cursor, "b8-champion-challenger", last.CreatedAt, last.Id)
		page.NextCursor = &next
	}
	return page, nil
}

func scanB8Champion(row b8RowScanner) (generated.ChampionChallengerReport, error) {
	var item generated.ChampionChallengerReport
	if err := row.Scan(&item.Id, &item.ChampionStrategyVersion,
		&item.ChallengerStrategyVersion, &item.ChampionSuiteId, &item.ChallengerSuiteId,
		&item.Confidence, &item.Viability, &item.Disposition, &item.Disclaimer,
		&item.ManifestHash, &item.CreatedAt); err != nil {
		return generated.ChampionChallengerReport{}, err
	}
	item.Revision = strconv.FormatInt(item.CreatedAt.UnixNano(), 10)
	return item, nil
}

// ReplayFaults returns an immutable simulation-only schedule for one replay.
func (store *A11ConsoleStore) ReplayFaults(
	ctx context.Context, replayID string,
) (generated.ReplayFaultPage, error) {
	var kind string
	if err := store.pool.QueryRow(ctx, `SELECT job_type FROM jobs WHERE id=$1`, replayID).Scan(&kind); errors.Is(err, pgx.ErrNoRows) {
		return generated.ReplayFaultPage{}, console.ErrNotFound
	} else if err != nil {
		return generated.ReplayFaultPage{}, err
	}
	if kind != "replay" {
		return generated.ReplayFaultPage{}, console.ErrNotFound
	}
	rows, err := store.pool.Query(ctx, `SELECT id,replay_id,fault_kind,event_ordinal,
	  delay_nanos,repeatable,schedule_revision,created_at
	FROM b8_replay_fault_schedules WHERE replay_id=$1 ORDER BY schedule_revision`, replayID)
	if err != nil {
		return generated.ReplayFaultPage{}, err
	}
	defer rows.Close()
	items := []generated.ReplayFaultResource{}
	var revision int64
	for rows.Next() {
		var item generated.ReplayFaultResource
		if err = rows.Scan(&item.Id, &item.ReplayId, &item.Fault, &item.Ordinal,
			&item.DelayNanos, &item.Repeatable, &revision, &item.CreatedAt); err != nil {
			return generated.ReplayFaultPage{}, err
		}
		item.Revision = strconv.FormatInt(revision, 10)
		item.SimulationOnly = true
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return generated.ReplayFaultPage{}, err
	}
	err = store.pool.QueryRow(ctx, `SELECT revision FROM b8_replay_fault_schedule_states
	  WHERE replay_id=$1`, replayID).Scan(&revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return generated.ReplayFaultPage{}, err
	}
	return generated.ReplayFaultPage{Items: items, Revision: strconv.FormatInt(revision, 10),
		SimulationOnly: true}, nil
}
