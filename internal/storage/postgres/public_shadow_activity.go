package postgres

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

var publicShadowActivityReasonPattern = regexp.MustCompile(`^[a-z0-9_]{1,96}$`)

// PublicShadowInputHealth is one exact required public book at the observation
// boundary. Age is monotonic-derived; ObservedAt is the UTC display instant.
type PublicShadowInputHealth struct {
	ExchangeID   string
	InstrumentID string
	State        string
	Reason       string
	Fresh        bool
	BookVersion  uint64
	Age          time.Duration
	ObservedAt   time.Time
}

// PublicShadowActivity is one append-only explanation of current shadow work.
type PublicShadowActivity struct {
	State            string
	ReasonCode       string
	Summary          string
	NextEvaluationAt *time.Time
	TriggerCondition string
	ObservedAt       time.Time
	Inputs           []PublicShadowInputHealth
}

// RecordActivity appends a complete status/input-health observation while the
// worker still owns the durable session lease. It never updates old evidence.
func (store *PublicShadowStore) RecordActivity(
	ctx context.Context,
	claim PublicShadowClaim,
	activity PublicShadowActivity,
) error {
	if !validPublicShadowActivity(claim, activity) {
		return fmt.Errorf("owner_console_shadow_activity_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = verifyPublicShadowEvidenceLease(ctx, tx, store.owner, claim.ID); err != nil {
		return err
	}
	var revision int64
	if err = tx.QueryRow(ctx, `SELECT coalesce(max(observation.revision),0)+1
	  FROM shadow_session_activity_observations observation WHERE observation.session_id=$1`,
		claim.ID).Scan(&revision); err != nil || revision <= 0 {
		return fmt.Errorf("owner_console_shadow_activity_revision_invalid")
	}
	if _, err = tx.Exec(ctx, `INSERT INTO shadow_session_activity_observations(
	  session_id,revision,activity_state,reason_code,summary,next_evaluation_at,
	  trigger_condition,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		claim.ID, revision, activity.State, activity.ReasonCode, activity.Summary,
		nullableOwnerConsoleActivityTime(activity.NextEvaluationAt), activity.TriggerCondition,
		activity.ObservedAt); err != nil {
		return err
	}
	for _, input := range activity.Inputs {
		tag, insertErr := tx.Exec(ctx, `INSERT INTO shadow_session_input_health_observations(
		  session_id,activity_revision,exchange_id,instrument_id,state,reason,fresh,
		  book_version,age_nanoseconds,observed_at)
		  SELECT $1,$2,$3,instrument.id,$5,$6,$7,$8,$9,$10
		  FROM instruments instrument
		  WHERE instrument.base_asset || instrument.quote_asset=$4 AND instrument.product='spot'`,
			claim.ID, revision, input.ExchangeID, input.InstrumentID, input.State,
			input.Reason, input.Fresh, input.BookVersion, input.Age.Nanoseconds(), input.ObservedAt)
		if insertErr != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("owner_console_shadow_activity_input_persistence_failed")
		}
	}
	return tx.Commit(ctx)
}

func validPublicShadowActivity(claim PublicShadowClaim, activity PublicShadowActivity) bool {
	if claim.ID == "" || !validPublicShadowActivityState(activity.State) ||
		!publicShadowActivityReasonPattern.MatchString(activity.ReasonCode) ||
		len(activity.Summary) == 0 || len(activity.Summary) > 500 ||
		len(activity.TriggerCondition) == 0 || len(activity.TriggerCondition) > 500 ||
		activity.ObservedAt.IsZero() || activity.ObservedAt.Location() != time.UTC ||
		(activity.NextEvaluationAt != nil && (activity.NextEvaluationAt.IsZero() ||
			activity.NextEvaluationAt.Location() != time.UTC)) || len(activity.Inputs) == 0 {
		return false
	}
	wanted := make(map[string]bool, len(claim.MarketScopes))
	for _, scope := range claim.MarketScopes {
		wanted[scope.ExchangeID+"\x00"+scope.InstrumentID] = true
	}
	seen := make(map[string]bool, len(activity.Inputs))
	for _, input := range activity.Inputs {
		key := input.ExchangeID + "\x00" + input.InstrumentID
		if input.ExchangeID == "" || input.InstrumentID == "" || seen[key] ||
			!validPublicShadowInputHealthState(input.State) || len(input.Reason) == 0 ||
			len(input.Reason) > 500 || input.Age < 0 || input.ObservedAt.IsZero() ||
			input.ObservedAt.Location() != time.UTC || input.Fresh != (input.State == "HEALTHY") ||
			(len(wanted) > 0 && !wanted[key]) {
			return false
		}
		seen[key] = true
	}
	return !claim.MarketScopeRequired || len(seen) == len(wanted)
}

func validPublicShadowActivityState(value string) bool {
	switch value {
	case "preparing", "warming_up", "waiting", "evaluating", "running", "paused", "blocked", "stopped":
		return true
	default:
		return false
	}
}

func validPublicShadowInputHealthState(value string) bool {
	switch value {
	case "CONNECTING", "SYNCING", "HEALTHY", "STALE", "PAUSED", "DISCONNECTED", "UNAVAILABLE":
		return true
	default:
		return false
	}
}

func nullableOwnerConsoleActivityTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
