package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/evaluation"
	"axiom/internal/observability"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// evaluationMetricWorker projects one bounded, identifier-free Prometheus
// snapshot from durable campaign state. Campaign IDs and arbitrary error text
// remain in structured logs and the owner-visible timeline, never labels.
type evaluationMetricWorker struct {
	pool    *pgxpool.Pool
	metrics *observability.Metrics
	clock   domain.Clock
}

func newEvaluationMetricWorker(pool *pgxpool.Pool, metrics *observability.Metrics,
	clock domain.Clock) (*evaluationMetricWorker, error) {
	if pool == nil || metrics == nil || clock == nil {
		return nil, roleError("evaluation_metrics_dependencies_missing")
	}
	return &evaluationMetricWorker{pool: pool, metrics: metrics, clock: clock}, nil
}

// RunOne refreshes one bounded identifier-free metrics projection from the
// latest campaign state.
func (worker *evaluationMetricWorker) RunOne(ctx context.Context) (bool, error) {
	worker.metrics.ResetEvaluationProjection()
	var campaignID, state string
	var stage, reason *string
	var validRecording, validShadow, recorded, reserve int64
	err := worker.pool.QueryRow(ctx, `SELECT id,state,current_stage,reason_code,valid_recording_seconds,
valid_shadow_seconds,campaign_recorded_bytes,COALESCE(shadow_reserved_bytes,0)
FROM evaluation_campaigns ORDER BY (state IN ('PENDING','RUNNING','PAUSED_RECOVERABLE')) DESC,
created_at DESC,id DESC LIMIT 1`).Scan(&campaignID, &state, &stage, &reason, &validRecording,
		&validShadow, &recorded, &reserve)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	stageLabel := "NONE"
	if stage != nil {
		stageLabel = *stage
	}
	if err = worker.metrics.SetEvaluationCampaign(stageLabel, state, validRecording, validShadow,
		recorded, evaluation.CampaignStorageLimitBytes, reserve); err != nil {
		return false, err
	}
	if reason != nil {
		if err = worker.metrics.SetEvaluationFailure(stageLabel, boundedEvaluationReason(*reason)); err != nil {
			return false, err
		}
	}
	if err = worker.projectFeeds(ctx, campaignID); err != nil {
		return false, err
	}
	if err = worker.projectMembers(ctx, campaignID); err != nil {
		return false, err
	}
	return false, worker.projectShadowEvidence(ctx, campaignID)
}

func (worker *evaluationMetricWorker) projectFeeds(ctx context.Context, campaignID string) error {
	rows, err := worker.pool.Query(ctx, `SELECT exchange_id,instrument,eligible,book_fresh,clock_eligible,
latest_event_at FROM evaluation_recorder_instrument_observations WHERE campaign_id=$1
AND observation_ordinal=(SELECT max(ordinal) FROM evaluation_recorder_observations WHERE campaign_id=$1)`, campaignID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var exchange, instrument string
		var eligible, fresh, clockEligible bool
		var latest time.Time
		if err = rows.Scan(&exchange, &instrument, &eligible, &fresh, &clockEligible, &latest); err != nil {
			return err
		}
		age := worker.clock.Now().UTC.Sub(latest.UTC())
		if age < 0 {
			age = 0
		}
		if err = worker.metrics.SetEvaluationFeed(exchange, instrument, age,
			eligible && fresh && clockEligible); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (worker *evaluationMetricWorker) projectMembers(ctx context.Context, campaignID string) error {
	rows, err := worker.pool.Query(ctx, `SELECT strategy_id,mode,state,count(*) FROM evaluation_campaign_members
WHERE campaign_id=$1 GROUP BY strategy_id,mode,state`, campaignID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var strategy, mode, state string
		var count int
		if err = rows.Scan(&strategy, &mode, &state, &count); err != nil {
			return err
		}
		if err = worker.metrics.SetEvaluationMembers(evaluationMetricStrategy(strategy), mode, state, count); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (worker *evaluationMetricWorker) projectShadowEvidence(ctx context.Context, campaignID string) error {
	rows, err := worker.pool.Query(ctx, `SELECT strategy_id,metrics_payload
FROM evaluation_shadow_member_checkpoints WHERE campaign_id=$1`, campaignID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var strategy string
		var payload []byte
		if err = rows.Scan(&strategy, &payload); err != nil {
			return err
		}
		var metrics backtest.Metrics
		if json.Unmarshal(payload, &metrics) != nil {
			continue
		}
		strategy = evaluationMetricStrategy(strategy)
		fees := addEvaluationDecimals(metrics.ByStrategy["fees_winners"], metrics.ByStrategy["fees_losers"])
		for measure, value := range map[string]string{
			"net": metrics.ByStrategy["net_result"], "gross_profit": metrics.ByStrategy["gross_profit"],
			"gross_loss": metrics.ByStrategy["gross_loss"], "fees": fees,
			"spread": metrics.ByStrategy["spread_cost"], "slippage": metrics.ByStrategy["slippage_cost"],
			"latency": metrics.ByStrategy["latency_deterioration"], "recovery": metrics.ByStrategy["recovery_loss"],
		} {
			if err = worker.metrics.SetEvaluationFinancial(strategy, measure, evaluationDecimalMicros(value)); err != nil {
				return err
			}
		}
		for measure, key := range map[string]string{"opportunities": "opportunities", "accepted": "accepted_decisions",
			"orders": "simulated_orders", "filled": "full_fills", "partial": "partial_fills",
			"missed": "missed_fills", "canceled": "canceled_fills", "expired": "expired_fills",
			"rejected": "rejected_fills"} {
			count, _ := strconv.ParseInt(metrics.ByStrategy[key], 10, 64)
			if err = worker.metrics.SetEvaluationFunnel(strategy, measure, count); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

func evaluationMetricStrategy(value string) string {
	switch value {
	case "trend-following":
		return "trend"
	case "triangular-arbitrage":
		return "triangular"
	default:
		return value
	}
}

func boundedEvaluationReason(value string) observability.Reason {
	upper := strings.ToUpper(value)
	switch {
	case strings.Contains(upper, "STORAGE"), strings.Contains(upper, "DISK"):
		return observability.ReasonDiskPressure
	case strings.Contains(upper, "CLOCK"):
		return observability.ReasonClockDrift
	case strings.Contains(upper, "ACCOUNT"), strings.Contains(upper, "RECONCIL"):
		return observability.ReasonReconciliation
	case strings.Contains(upper, "PERSIST"), strings.Contains(upper, "DATABASE"):
		return observability.ReasonPersistence
	case strings.Contains(upper, "DATA"), strings.Contains(upper, "FEED"):
		return observability.ReasonStaleBook
	case strings.Contains(upper, "SAFETY"), strings.Contains(upper, "RISK"):
		return observability.ReasonRisk
	default:
		return observability.ReasonUnsupported
	}
}

func addEvaluationDecimals(left, right string) string {
	leftValue, leftOK := new(big.Rat).SetString(left)
	rightValue, rightOK := new(big.Rat).SetString(right)
	if !leftOK || !rightOK {
		return "0"
	}
	return new(big.Rat).Add(leftValue, rightValue).RatString()
}

func evaluationDecimalMicros(value string) int64 {
	parsed, ok := new(big.Rat).SetString(value)
	if !ok {
		return 0
	}
	parsed.Mul(parsed, big.NewRat(1_000_000, 1))
	return new(big.Int).Quo(parsed.Num(), parsed.Denom()).Int64()
}
