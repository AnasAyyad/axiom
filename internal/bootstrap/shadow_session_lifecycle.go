package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"axiom/internal/buildinfo"
	"axiom/internal/domain"
	"axiom/internal/portfolio"
	marketrecorder "axiom/internal/recorder"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/strategies/trend"
)

// SetEntriesEnabled changes only the local fail-closed gate after durable control.
func (session *ownerConsoleLiveShadowSession) SetEntriesEnabled(enabled bool) {
	session.entries.Store(enabled)
}

// Flush finalizes both evidence streams and registers their immutable manifests.
func (session *ownerConsoleLiveShadowSession) Flush(ctx context.Context) error {
	return session.flush(ctx, true)
}

// FlushAvailable persists only complete event pairs during live collection.
func (session *ownerConsoleLiveShadowSession) FlushAvailable(ctx context.Context) error {
	return session.flush(ctx, false)
}

func (session *ownerConsoleLiveShadowSession) flush(ctx context.Context, final bool) error {
	session.flushMutex.Lock()
	defer session.flushMutex.Unlock()
	if err := session.flushRecorder(ctx, session.public, false, final); err != nil {
		return err
	}
	return session.flushRecorder(ctx, session.decisions, true, final)
}

func (session *ownerConsoleLiveShadowSession) flushRecorder(ctx context.Context, recorder *marketrecorder.Recorder,
	decisionInputs, final bool) error {
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
	if err != nil {
		return err
	}
	if !flushed {
		return nil
	}
	if decisionInputs {
		id, registerErr := session.catalog.RegisterDecisionInputs(ctx, manifest, session.commit)
		if registerErr != nil {
			return registerErr
		}
		if manifest.Complete {
			if qualifyErr := session.catalog.QualifyDecisionInputs(ctx, id); qualifyErr != nil {
				return qualifyErr
			}
			if linkErr := session.store.LinkDecisionDataset(ctx, session.claim.ID, id); linkErr != nil {
				return linkErr
			}
			session.stateMutex.Lock()
			session.datasetID = id
			session.stateMutex.Unlock()
		}
		return nil
	}
	_, err = session.catalog.Register(ctx, manifest, session.commit)
	return err
}

type publicShadowCheckpointState struct {
	Balances          portfolio.Snapshot            `json:"balances"`
	Instruments       []publicShadowInstrumentState `json:"instruments"`
	DecisionDatasetID string                        `json:"decision_dataset_id,omitempty"`
	LastMarketViewID  string                        `json:"last_market_view_id,omitempty"`
}

type publicShadowInstrumentState struct {
	Instrument domain.Instrument   `json:"instrument"`
	Position   trend.PositionState `json:"position"`
	Cooldown   uint64              `json:"cooldown"`
	LastCandle time.Time           `json:"last_candle,omitempty"`
}

// Checkpoint captures state only after Run has stopped mutating the session.
func (session *ownerConsoleLiveShadowSession) Checkpoint(ctx context.Context) error {
	session.stateMutex.Lock()
	state := publicShadowCheckpointState{Balances: session.balances, DecisionDatasetID: session.datasetID,
		LastMarketViewID: session.lastMarketViewID}
	for instrument, position := range session.positions {
		state.Instruments = append(state.Instruments, publicShadowInstrumentState{Instrument: instrument,
			Position: position, Cooldown: session.cooldowns[instrument], LastCandle: session.seen[instrument]})
	}
	lastOrdinal := session.lastOrdinal
	session.stateMutex.Unlock()
	sort.Slice(state.Instruments, func(left, right int) bool {
		return state.Instruments[left].Instrument.Symbol() < state.Instruments[right].Instrument.Symbol()
	})
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("shadow_checkpoint_encode_failed")
	}
	return session.store.Checkpoint(ctx, session.claim, postgresstore.PublicShadowCheckpoint{
		InputOrdinal: lastOrdinal, CursorLogicalTime: session.client.MonotonicOffset(), Canonical: payload})
}

func claimConfigurationCommit() string {
	commit := buildinfo.Current().Commit
	decoded, err := hex.DecodeString(commit)
	if err != nil || (len(decoded) != 20 && len(decoded) != sha256.Size) {
		return ""
	}
	return commit
}

func ownerConsoleLocalHash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

var _ shadowSession = (*ownerConsoleLiveShadowSession)(nil)
