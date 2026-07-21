package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/buildinfo"
	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/portfolio"
	marketrecorder "axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/strategies/trend"

	"github.com/jackc/pgx/v5/pgxpool"
)

type a11LiveShadowSession struct {
	claim       postgresstore.A11ShadowClaim
	store       *postgresstore.A11ShadowStore
	client      *binance.PublicClient
	collectors  map[domain.Instrument]*binance.InstrumentCollector
	public      *marketrecorder.Recorder
	decisions   *marketrecorder.Recorder
	catalog     *postgresstore.A11DatasetCatalog
	processor   backtest.Processor
	trendConfig trend.Configuration
	flushEvery  time.Duration
	commit      string
	entries     atomic.Bool
	flushMutex  sync.Mutex
	stateMutex  sync.Mutex
	metadata    map[domain.Instrument]domain.InstrumentMetadata
	metadataIDs map[domain.Instrument]string
	history     map[domain.Instrument][]exchangecontracts.Candle
	seen        map[domain.Instrument]time.Time
	positions   map[domain.Instrument]trend.PositionState
	cooldowns   map[domain.Instrument]uint64
	balances    portfolio.Snapshot
	lastOrdinal uint64
	datasetID   string
}

func newA11LiveShadowRoleWork(pool *pgxpool.Pool, runtimeConfig config.Runtime) (*shadowRoleWork, error) {
	store, err := postgresstore.NewA11ShadowStore(pool, runtimeConfig.InstanceID, &domain.SystemClock{})
	if err != nil {
		return nil, err
	}
	factory := func(ctx context.Context, claim postgresstore.A11ShadowClaim) (shadowSession, error) {
		return newA11LiveShadowSession(ctx, pool, runtimeConfig, store, claim)
	}
	work, err := newShadowRoleWork(store, factory, time.Second)
	if err != nil {
		return nil, err
	}
	recoveryIdentity := runtimeConfig.InstanceID + ":" + time.Now().UTC().Format(time.RFC3339Nano)
	work.preflight = func(ctx context.Context) error {
		return postgresstore.EnsureA11StartupRecovery(ctx, pool, recoveryIdentity,
			buildinfo.Current(), time.Now().UTC())
	}
	return work, nil
}

func newA11LiveShadowSession(ctx context.Context, pool *pgxpool.Pool, runtimeConfig config.Runtime,
	store *postgresstore.A11ShadowStore, claim postgresstore.A11ShadowClaim) (*a11LiveShadowSession, error) {
	if err := os.MkdirAll(runtimeConfig.Recorder.Root, 0o750); err != nil {
		return nil, fmt.Errorf("shadow_recorder_root_unavailable")
	}
	client, err := binance.NewPublicClient(claim.Configuration.Endpoint.Set, &domain.SystemClock{})
	if err != nil {
		return nil, err
	}
	publicRecorder, decisionRecorder, catalog, err := newA11ShadowRecorders(pool, runtimeConfig.Recorder.Root, claim.ID)
	if err != nil {
		return nil, err
	}
	sink, err := marketrecorder.NewBinanceStreamSink(publicRecorder)
	if err != nil {
		return nil, err
	}
	collectors, err := newRecorderCollectors(claim.Configuration, runtimeConfig.Recorder, client, sink, &domain.SystemClock{})
	if err != nil {
		return nil, err
	}
	processor, configuredTrend, balances, err := newA11ShadowProcessor(claim)
	if err != nil {
		return nil, err
	}
	session := &a11LiveShadowSession{claim: claim, store: store, client: client, collectors: collectors,
		public: publicRecorder, decisions: decisionRecorder, catalog: catalog, processor: processor,
		trendConfig: configuredTrend, flushEvery: runtimeConfig.Recorder.FlushInterval,
		commit: claimConfigurationCommit(), metadata: make(map[domain.Instrument]domain.InstrumentMetadata),
		metadataIDs: make(map[domain.Instrument]string),
		history:     make(map[domain.Instrument][]exchangecontracts.Candle), seen: make(map[domain.Instrument]time.Time),
		positions: make(map[domain.Instrument]trend.PositionState), cooldowns: make(map[domain.Instrument]uint64),
		balances: balances}
	if session.commit == "" {
		return nil, fmt.Errorf("shadow_build_identity_invalid")
	}
	_ = ctx
	return session, nil
}

func newA11ShadowRecorders(pool *pgxpool.Pool, root, id string) (*marketrecorder.Recorder,
	*marketrecorder.Recorder, *postgresstore.A11DatasetCatalog, error) {
	catalog, err := postgresstore.NewA11DatasetCatalog(pool)
	if err != nil {
		return nil, nil, nil, err
	}
	publicSession, decisionSession := id+"-public", id+"-decisions"
	publicRecorder, err := marketrecorder.New(root, id+"-public-evidence", publicSession, "binance",
		&runtimecore.IngestOrdinals{}, segmentCommitter(pool, publicSession), nil)
	if err != nil {
		return nil, nil, nil, err
	}
	decisionRecorder, err := marketrecorder.New(root, id+"-decision-inputs", decisionSession, "binance",
		&runtimecore.IngestOrdinals{}, segmentCommitter(pool, decisionSession), nil)
	if err != nil {
		return nil, nil, nil, err
	}
	return publicRecorder, decisionRecorder, catalog, nil
}

func newA11ShadowProcessor(claim postgresstore.A11ShadowClaim) (backtest.Processor,
	trend.Configuration, portfolio.Snapshot, error) {
	runID, runErr := domain.NewRunID(claim.RunID)
	portfolioID, portfolioErr := domain.NewPortfolioID(claim.PortfolioID)
	accountID, accountErr := domain.NewVirtualAccountID(claim.AccountID)
	capital, capitalErr := domain.ParseBalance(claim.Configuration.Portfolio.StartingCapital.Value)
	if runErr != nil || portfolioErr != nil || accountErr != nil || capitalErr != nil {
		return nil, trend.Configuration{}, portfolio.Snapshot{}, fmt.Errorf("shadow_identity_invalid")
	}
	owned, err := portfolio.InitializeTrend(runID, portfolioID, accountID, claim.ConfigurationHash, capital,
		accounting.NewMemoryJournal(), domain.EventTime{UTC: time.Unix(0, 1).UTC(), Sequence: 1})
	if err != nil {
		return nil, trend.Configuration{}, portfolio.Snapshot{}, err
	}
	seed := a11LocalHash([]byte("shadow-seed:" + claim.ID))
	processor, err := newA11OperationalProcessorWithPortfolio(backtest.JobClaim{ID: claim.ID,
		Configuration: claim.Configuration, Manifest: backtest.RunManifest{RunID: runID, Mode: "shadow",
			ConfigurationHash: claim.ConfigurationHash, Seed: seed, Models: claim.Models}}, owned)
	if err != nil {
		return nil, trend.Configuration{}, portfolio.Snapshot{}, err
	}
	configuredTrend, err := trend.NewConfiguration(claim.Configuration.Trend)
	return processor, configuredTrend, owned.Snapshot(), err
}

// Run owns production-public collectors and decision evaluation until stopped.
func (session *a11LiveShadowSession) Run(ctx context.Context) error {
	if err := session.loadReferenceData(ctx); err != nil {
		return err
	}
	workContext, cancel := context.WithCancel(ctx)
	defer cancel()
	errorsChannel := make(chan error, len(session.collectors))
	var group sync.WaitGroup
	for _, collector := range session.collectors {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsChannel <- collector.Run(workContext)
		}()
	}
	evaluate := time.NewTicker(500 * time.Millisecond)
	flush := time.NewTicker(session.flushEvery)
	defer evaluate.Stop()
	defer flush.Stop()
	for {
		select {
		case <-workContext.Done():
			group.Wait()
			return nil
		case err := <-errorsChannel:
			if err != nil {
				cancel()
				group.Wait()
				return err
			}
		case <-evaluate.C:
			if err := session.evaluateReadyInputs(workContext); err != nil {
				cancel()
				group.Wait()
				return err
			}
		case <-flush.C:
			if err := session.FlushAvailable(workContext); err != nil {
				cancel()
				group.Wait()
				return err
			}
		}
	}
}

func (session *a11LiveShadowSession) loadReferenceData(ctx context.Context) error {
	instruments := make([]domain.Instrument, 0, len(session.collectors))
	for instrument := range session.collectors {
		instruments = append(instruments, instrument)
	}
	sort.Slice(instruments, func(left, right int) bool { return instruments[left].Symbol() < instruments[right].Symbol() })
	records, err := session.client.Instruments(ctx, instruments)
	if err != nil || len(records) != len(instruments) {
		return fmt.Errorf("shadow_metadata_unavailable")
	}
	for _, record := range records {
		evidence, registerErr := session.store.RegisterMetadata(ctx, record.Metadata)
		if registerErr != nil {
			return registerErr
		}
		session.metadata[evidence.Metadata.Instrument] = evidence.Metadata
		session.metadataIDs[evidence.Metadata.Instrument] = evidence.ID
	}
	end := time.Now().UTC()
	start := end.Add(-1000 * 4 * time.Hour)
	for _, instrument := range instruments {
		candles, candleErr := session.client.Candles(ctx, exchangecontracts.CandleRequest{
			HistoryRequest: exchangecontracts.HistoryRequest{Instrument: instrument, Start: start, End: end, Limit: 1000},
			Interval:       "4h"})
		if candleErr != nil || len(candles) < session.trendConfig.EMARegime {
			return fmt.Errorf("shadow_candle_history_unavailable")
		}
		session.history[instrument] = candles
	}
	return nil
}

// SetEntriesEnabled changes only the local fail-closed gate after durable control.
func (session *a11LiveShadowSession) SetEntriesEnabled(enabled bool) { session.entries.Store(enabled) }

// Flush finalizes both evidence streams and registers their immutable manifests.
func (session *a11LiveShadowSession) Flush(ctx context.Context) error {
	return session.flush(ctx, true)
}

// FlushAvailable persists only complete event pairs during live collection.
func (session *a11LiveShadowSession) FlushAvailable(ctx context.Context) error {
	return session.flush(ctx, false)
}

func (session *a11LiveShadowSession) flush(ctx context.Context, final bool) error {
	session.flushMutex.Lock()
	defer session.flushMutex.Unlock()
	if err := session.flushRecorder(ctx, session.public, false, final); err != nil {
		return err
	}
	return session.flushRecorder(ctx, session.decisions, true, final)
}

func (session *a11LiveShadowSession) flushRecorder(ctx context.Context, recorder *marketrecorder.Recorder,
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

type a11ShadowCheckpointState struct {
	Balances          portfolio.Snapshot         `json:"balances"`
	Instruments       []a11ShadowInstrumentState `json:"instruments"`
	DecisionDatasetID string                     `json:"decision_dataset_id,omitempty"`
}

type a11ShadowInstrumentState struct {
	Instrument domain.Instrument   `json:"instrument"`
	Position   trend.PositionState `json:"position"`
	Cooldown   uint64              `json:"cooldown"`
	LastCandle time.Time           `json:"last_candle,omitempty"`
}

// Checkpoint captures state only after Run has stopped mutating the session.
func (session *a11LiveShadowSession) Checkpoint(ctx context.Context) error {
	session.stateMutex.Lock()
	state := a11ShadowCheckpointState{Balances: session.balances, DecisionDatasetID: session.datasetID}
	for instrument, position := range session.positions {
		state.Instruments = append(state.Instruments, a11ShadowInstrumentState{Instrument: instrument,
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
	return session.store.Checkpoint(ctx, session.claim, postgresstore.A11ShadowCheckpoint{
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

func a11LocalHash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

var _ shadowSession = (*a11LiveShadowSession)(nil)
