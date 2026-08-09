package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/domain"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
)

// SetEntriesEnabled atomically enables or disables new virtual entries.
func (session *ownerConsoleCrossExchangeShadowSession) SetEntriesEnabled(enabled bool) {
	session.entries.Store(enabled)
}

// FlushAvailable persists all currently available recorded public evidence.
func (session *ownerConsoleCrossExchangeShadowSession) FlushAvailable(ctx context.Context) error {
	return session.flush(ctx, false)
}

// Flush persists the final recorded public evidence for shutdown.
func (session *ownerConsoleCrossExchangeShadowSession) Flush(ctx context.Context) error {
	return session.flush(ctx, true)
}

func (session *ownerConsoleCrossExchangeShadowSession) flush(ctx context.Context, final bool) error {
	session.flushMutex.Lock()
	defer session.flushMutex.Unlock()
	for _, exchange := range []string{"binance", "bybit"} {
		if err := session.flushRecorder(ctx, session.public[exchange], false, final); err != nil {
			return err
		}
	}
	return session.flushRecorder(ctx, session.decisions, true, final)
}

func (session *ownerConsoleCrossExchangeShadowSession) flushRecorder(ctx context.Context,
	recorder *marketrecorder.Recorder, decisions, final bool,
) error {
	raw, canonical := recorder.PendingCounts()
	if raw == 0 && canonical == 0 {
		return nil
	}
	if final && raw != canonical {
		return fmt.Errorf("shadow_recorder_segment_incomplete")
	}
	var manifest marketrecorder.DatasetManifest
	flushed := true
	var err error
	if final {
		manifest, err = recorder.Flush()
	} else {
		manifest, flushed, err = recorder.FlushReady()
	}
	if err != nil || !flushed {
		return err
	}
	if !decisions {
		_, err = session.catalog.Register(ctx, manifest, session.commit)
		return err
	}
	id, err := session.catalog.RegisterDecisionInputs(ctx, manifest, session.commit)
	if err != nil {
		return err
	}
	if manifest.Complete {
		if err = session.catalog.QualifyDecisionInputs(ctx, id); err != nil {
			return err
		}
		if err = session.store.LinkDecisionDataset(ctx, session.claim.ID, id); err != nil {
			return err
		}
		session.stateMutex.Lock()
		session.datasetID = id
		session.stateMutex.Unlock()
	}
	return nil
}

// Checkpoint persists the session's durable replay checkpoint.
func (session *ownerConsoleCrossExchangeShadowSession) Checkpoint(ctx context.Context) error {
	session.stateMutex.Lock()
	payload, err := json.Marshal(struct {
		Balances          map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot `json:"balances"`
		DecisionDatasetID string                                                       `json:"decision_dataset_id,omitempty"`
		LastMarketViewID  string                                                       `json:"last_market_view_id,omitempty"`
	}{cloneOwnerConsoleCrossExchangeSnapshots(session.balances), session.datasetID, session.lastViewID})
	ordinal := session.lastOrdinal
	session.stateMutex.Unlock()
	if err != nil {
		return fmt.Errorf("shadow_checkpoint_encode_failed")
	}
	return session.store.Checkpoint(ctx, session.claim, postgresstore.PublicShadowCheckpoint{InputOrdinal: ordinal,
		CursorLogicalTime: session.clients["binance"].MonotonicOffset(), Canonical: payload})
}

func (session *ownerConsoleCrossExchangeShadowSession) currentInputHealth(now time.Time) []postgresstore.PublicShadowInputHealth {
	keys := make([]runtimecore.MarketKey, 0, len(session.collectors))
	for key := range session.collectors {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Exchange < keys[j].Exchange })
	logical := session.clients["binance"].MonotonicOffset()
	result := make([]postgresstore.PublicShadowInputHealth, 0, len(keys))
	for _, key := range keys {
		collector := session.collectors[key]
		health := collector.HealthSnapshot()
		book, err := collectorBook(collector, key)
		state, reason, fresh := publicShadowInputState(health.BookHealth, health.Eligible, err)
		age, version := time.Duration(0), uint64(0)
		if err == nil {
			age, version = ownerConsoleBookAge(logical, book.Observation().PublishedOffsetNanos), book.Version()
		}
		result = append(result, postgresstore.PublicShadowInputHealth{ExchangeID: key.Exchange,
			InstrumentID: key.Instrument.Symbol(), State: state, Reason: reason, Fresh: fresh,
			BookVersion: version, Age: age, ObservedAt: now})
	}
	return result
}

func (session *ownerConsoleCrossExchangeShadowSession) currentActivity(now time.Time) postgresstore.PublicShadowActivity {
	inputs := session.currentInputHealth(now)
	fresh := len(inputs) == 2
	for _, input := range inputs {
		fresh = fresh && input.Fresh
	}
	next := now.Add(time.Second)
	state, code := "waiting", "waiting_for_coherent_market_view"
	summary := "The strategy is waiting for one coherent Binance and Bybit public-book pair."
	if !session.entries.Load() {
		state, code = "paused", "entries_disabled"
		summary = "New virtual entries are paused by the durable safety controls."
	} else if !fresh {
		code = "public_input_not_ready"
		summary = "Both required production-public books are not yet healthy and coherent."
	}
	return postgresstore.PublicShadowActivity{State: state, ReasonCode: code, Summary: summary,
		NextEvaluationAt: &next, TriggerCondition: "When the Binance and Bybit books are jointly fresh within the approved clock and skew limits.",
		ObservedAt: now, Inputs: inputs}
}

func (session *ownerConsoleCrossExchangeShadowSession) warmingActivity(now time.Time) postgresstore.PublicShadowActivity {
	activity := session.currentActivity(now)
	activity.State = "warming_up"
	activity.ReasonCode = "loading_paired_metadata"
	activity.Summary = "The worker is loading exact filters for both venues before coherent pair evaluation and virtual prefunding."
	return activity
}

func (session *ownerConsoleCrossExchangeShadowSession) evaluatingActivity(now time.Time, instrument domain.Instrument) postgresstore.PublicShadowActivity {
	activity := session.currentActivity(now)
	activity.State = "evaluating"
	activity.ReasonCode = "strategy_evaluation_in_progress"
	activity.Summary = fmt.Sprintf("Cross-Exchange Arbitrage is evaluating the coherent %s pair through two-sided allocation, central risk, concurrent simulation, accounting, and reconciliation.", instrument.Symbol())
	return activity
}

func (session *ownerConsoleCrossExchangeShadowSession) recordActivity(ctx context.Context, activity postgresstore.PublicShadowActivity) error {
	signature := publicShadowActivitySignature(activity)
	session.activityMutex.Lock()
	defer session.activityMutex.Unlock()
	if signature == session.lastActivity && activity.ObservedAt.Sub(session.lastActivityAt) < publicShadowActivityRefresh {
		return nil
	}
	if err := session.store.RecordActivity(ctx, session.claim, activity); err != nil {
		return err
	}
	session.lastActivity, session.lastActivityAt = signature, activity.ObservedAt
	return nil
}

var _ SandboxSagaMarketViewSource = (*ownerConsoleCrossExchangeMarketSource)(nil)
var _ shadowSession = (*ownerConsoleCrossExchangeShadowSession)(nil)
