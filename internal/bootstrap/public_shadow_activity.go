package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"axiom/internal/domain"
	"axiom/internal/marketdata"
	postgresstore "axiom/internal/storage/postgres"
)

const publicShadowActivityRefresh = 5 * time.Second

func (session *ownerConsoleLiveShadowSession) currentShadowActivity(now time.Time) postgresstore.PublicShadowActivity {
	inputs := session.currentShadowInputHealth(now)
	allFresh := len(inputs) > 0
	for _, input := range inputs {
		allFresh = allFresh && input.Fresh
	}
	next, trigger, reasonCode, summary := session.nextShadowEvaluation(now)
	state := "waiting"
	if !session.entries.Load() {
		state, reasonCode = "paused", "entries_disabled"
		summary = "New virtual entries are paused while the durable safety controls prepare or hold this session."
	} else if !allFresh {
		reasonCode = "public_input_not_ready"
		summary = "At least one required production-public input is not healthy and fresh; strategy evaluation is waiting."
	}
	return postgresstore.PublicShadowActivity{State: state, ReasonCode: reasonCode, Summary: summary,
		NextEvaluationAt: &next, TriggerCondition: trigger, ObservedAt: now, Inputs: inputs}
}

func (session *ownerConsoleLiveShadowSession) warmingShadowActivity(now time.Time) postgresstore.PublicShadowActivity {
	activity := session.currentShadowActivity(now)
	activity.State = "warming_up"
	activity.ReasonCode = "loading_reference_history"
	activity.Summary = "The shadow worker is loading the approved public instrument metadata and market history required before live evaluation."
	if session.claim.StrategyID == "triangular-arbitrage-1-0-0" {
		activity.ReasonCode = "loading_multimarket_metadata"
		activity.Summary = "The shadow worker is loading exact public filters for all three Triangle markets before synchronized book evaluation."
	}
	return activity
}

func (session *ownerConsoleLiveShadowSession) evaluatingShadowActivity(
	now time.Time,
	instrument domain.Instrument,
) postgresstore.PublicShadowActivity {
	activity := session.currentShadowActivity(now)
	activity.State = "evaluating"
	activity.ReasonCode = "strategy_evaluation_in_progress"
	activity.Summary = fmt.Sprintf("The strategy is evaluating the finalized input for %s through allocation, central risk, virtual execution, accounting, and reconciliation.",
		instrument.Symbol())
	return activity
}

func (session *ownerConsoleLiveShadowSession) nextShadowEvaluation(
	now time.Time,
) (time.Time, string, string, string) {
	session.stateMutex.Lock()
	lastEvaluated := time.Time{}
	for _, evaluated := range session.seen {
		if lastEvaluated.IsZero() || evaluated.Before(lastEvaluated) {
			lastEvaluated = evaluated
		}
	}
	session.stateMutex.Unlock()
	switch session.claim.StrategyID {
	case "trend-following-1-0-0":
		next := ownerConsoleNextPendingFinalizedCandle(now, 4*time.Hour, session.trendConfig.FinalizationDelay,
			lastEvaluated)
		return next,
			"After the next finalized four-hour candle and its configured finalization delay.",
			"waiting_for_finalized_4h_candle",
			fmt.Sprintf("No Trend decision is due yet. The next eligible four-hour candle becomes usable at %s.", next.Format(time.RFC3339))
	case "mean-reversion-1-0-0":
		next := ownerConsoleNextPendingFinalizedCandle(now, time.Hour, session.meanConfig.FinalizationDelay,
			lastEvaluated)
		return next,
			"After the next finalized one-hour signal candle with a healthy four-hour regime view.",
			"waiting_for_finalized_1h_candle",
			fmt.Sprintf("No Mean Reversion decision is due yet. The next eligible one-hour candle becomes usable at %s.", next.Format(time.RFC3339))
	default:
		next := now.Add(time.Second)
		return next, "When every required synchronized public book is healthy and coherent.",
			"waiting_for_coherent_market_view", "The strategy is waiting for a complete coherent public market view."
	}
}

func ownerConsoleNextPendingFinalizedCandle(now time.Time, interval, delay time.Duration, lastEvaluated time.Time) time.Time {
	if now.IsZero() || interval <= 0 || delay < 0 {
		return time.Time{}
	}
	seconds := int64(interval / time.Second)
	boundary := time.Unix(now.Unix()/seconds*seconds, 0).UTC()
	if !lastEvaluated.Before(boundary) {
		boundary = boundary.Add(interval)
	}
	return boundary.Add(delay)
}

func (session *ownerConsoleLiveShadowSession) currentShadowInputHealth(
	now time.Time,
) []postgresstore.PublicShadowInputHealth {
	instruments := make([]domain.Instrument, 0, len(session.collectors))
	for instrument := range session.collectors {
		instruments = append(instruments, instrument)
	}
	sort.Slice(instruments, func(left, right int) bool {
		return instruments[left].Symbol() < instruments[right].Symbol()
	})
	logical := session.client.MonotonicOffset()
	inputs := make([]postgresstore.PublicShadowInputHealth, 0, len(instruments))
	for _, instrument := range instruments {
		collector := session.collectors[instrument]
		health := collector.HealthSnapshot()
		book, err := collector.Views().Book(session.claim.ExchangeID, instrument)
		state, reason, fresh := publicShadowInputState(health.BookHealth, health.Eligible, err)
		age, version := time.Duration(0), uint64(0)
		if err == nil {
			age, version = ownerConsoleBookAge(logical, book.Observation().PublishedOffsetNanos), book.Version()
		}
		inputs = append(inputs, postgresstore.PublicShadowInputHealth{ExchangeID: session.claim.ExchangeID,
			InstrumentID: instrument.Symbol(), State: state, Reason: reason, Fresh: fresh,
			BookVersion: version, Age: age, ObservedAt: now})
	}
	return inputs
}

func publicShadowInputState(bookHealth string, eligible bool, viewErr error) (string, string, bool) {
	if viewErr != nil {
		return "UNAVAILABLE", "No normalized public order-book view is available yet.", false
	}
	if eligible {
		return "HEALTHY", "The production-public order book and clock evidence are healthy and fresh.", true
	}
	switch marketdata.HealthState(bookHealth) {
	case marketdata.HealthConnecting:
		return "CONNECTING", "The public collector is connecting.", false
	case marketdata.HealthSyncing:
		return "SYNCING", "The public collector is rebuilding a synchronized order book.", false
	case marketdata.HealthStale:
		return "STALE", "The public order book is older than the approved freshness limit.", false
	case marketdata.HealthDisconnected:
		return "DISCONNECTED", "The public collector is disconnected.", false
	default:
		return "PAUSED", "The public order book or clock evidence is not currently eligible.", false
	}
}

func (session *ownerConsoleLiveShadowSession) recordShadowActivity(
	ctx context.Context,
	activity postgresstore.PublicShadowActivity,
) error {
	signature := publicShadowActivitySignature(activity)
	session.activityMutex.Lock()
	if signature == session.lastActivity && activity.ObservedAt.Sub(session.lastActivityAt) < publicShadowActivityRefresh {
		session.activityMutex.Unlock()
		return nil
	}
	if err := session.store.RecordActivity(ctx, session.claim, activity); err != nil {
		session.activityMutex.Unlock()
		return err
	}
	session.lastActivity, session.lastActivityAt = signature, activity.ObservedAt
	session.activityMutex.Unlock()
	return nil
}

func publicShadowActivitySignature(activity postgresstore.PublicShadowActivity) string {
	type inputSignature struct {
		ExchangeID, InstrumentID, State, Reason string
		Fresh                                   bool
	}
	inputs := make([]inputSignature, 0, len(activity.Inputs))
	for _, input := range activity.Inputs {
		inputs = append(inputs, inputSignature{ExchangeID: input.ExchangeID, InstrumentID: input.InstrumentID,
			State: input.State, Reason: input.Reason, Fresh: input.Fresh})
	}
	encoded, _ := json.Marshal(struct {
		State, ReasonCode, Summary, Trigger string
		Next                                *time.Time
		Inputs                              []inputSignature
	}{activity.State, activity.ReasonCode, activity.Summary, activity.TriggerCondition,
		activity.NextEvaluationAt, inputs})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
