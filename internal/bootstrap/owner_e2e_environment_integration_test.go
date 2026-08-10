package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/recorder"
	"axiom/internal/replay"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/storage/segments"
	"axiom/internal/strategies/trend"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestOwnerConsolePrepareIntegratedBrowserDataset records the immutable decision inputs
// before the PostgreSQL fixture catalogs their exact manifest identity.
func TestOwnerConsolePrepareIntegratedBrowserDataset(t *testing.T) {
	root := os.Getenv("AXIOM_OWNER_CONSOLE_E2E_RECORDER_ROOT")
	if root == "" {
		t.Skip("owner console integrated browser dataset setup is not enabled")
	}
	if !filepath.IsAbs(root) || !strings.HasPrefix(filepath.Clean(root), "/tmp/") {
		t.Fatal("owner console integrated dataset setup requires an isolated /tmp recorder root")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("owner console recorder root is unavailable: %v", err)
	}
	if len(entries) == 0 {
		configuration := config.DefaultConfiguration()
		ownerConsoleE2EDataset(t, root, ownerConsoleE2ETrendInput(t, configuration, time.Now().UTC()))
	}
	manifest, err := recorder.ReadManifest(filepath.Join(root, "owner_console-e2e-000001.dataset.json"))
	if err != nil || !manifest.Complete || manifest.RawRecordCount != ownerConsoleE2EReplayEventCount ||
		manifest.CanonicalCount != ownerConsoleE2EReplayEventCount || len(manifest.Segments) != 2 {
		t.Fatalf("owner console decision dataset is invalid: %#v %v", manifest, err)
	}
	t.Logf("AXIOM_OWNER_CONSOLE_E2E_DATASET_MANIFEST=%s", filepath.Join(root, "owner_console-e2e-000001.dataset.json"))
}

// TestOwnerConsolePrepareIntegratedBrowserEnvironment turns the already-qualified OwnerConsole
// PostgreSQL fixture into a deterministic, unmocked browser environment. It
// produces shadow evidence through the production Trend/allocation/risk/
// simulation pipeline and PostgreSQL stores. It is opt-in and never ships a
// runtime bypass in the platform binary.
func TestOwnerConsolePrepareIntegratedBrowserEnvironment(t *testing.T) {
	dsn := os.Getenv("AXIOM_OWNER_CONSOLE_E2E_SETUP_DSN")
	root := os.Getenv("AXIOM_OWNER_CONSOLE_E2E_RECORDER_ROOT")
	commit := os.Getenv("AXIOM_OWNER_CONSOLE_E2E_SOURCE_COMMIT")
	if dsn == "" || root == "" || commit == "" {
		t.Skip("owner console integrated browser setup is not enabled")
	}
	if !filepath.IsAbs(root) || !strings.HasPrefix(filepath.Clean(root), "/tmp/") || !ownerConsoleE2EHexIdentity(commit) {
		t.Fatal("owner console integrated setup requires an isolated /tmp recorder root and source identity")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("owner console recorder root is unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	configuration := config.DefaultConfiguration()
	canonicalConfiguration, _ := json.Marshal(configuration)
	configurationHash := ownerConsoleE2EHash(canonicalConfiguration)
	var storedHash string
	if err = pool.QueryRow(ctx, `SELECT configuration_hash::text FROM configuration_versions WHERE id='configuration-research_registry'`).Scan(&storedHash); err != nil || storedHash != configurationHash {
		t.Fatalf("owner console qualification configuration mismatch: %s %v", storedHash, err)
	}

	if len(entries) == 0 {
		t.Fatal("owner console immutable decision dataset must be prepared before PostgreSQL qualification")
	}
	input := ownerConsoleE2ETrendInput(t, configuration, time.Now().UTC())
	manifest, err := recorder.ReadManifest(filepath.Join(root, "owner_console-e2e-000001.dataset.json"))
	if err != nil || !manifest.Complete || manifest.RawRecordCount != ownerConsoleE2EReplayEventCount ||
		manifest.CanonicalCount != ownerConsoleE2EReplayEventCount || len(manifest.Segments) != 2 {
		t.Fatalf("owner console existing decision dataset is invalid: %#v %v", manifest, err)
	}
	manifestPath := fmt.Sprintf("%s-%06d.dataset.json", manifest.SessionID, manifest.Revision)
	var matched bool
	if err = pool.QueryRow(ctx, `SELECT dataset_hash=$1 AND recorder_dataset_id=$2 AND manifest_revision=$3
	  AND manifest_path=$4 AND source_commit=$5 AND dataset_kind='decision_inputs'
	  FROM dataset_manifests WHERE id='dataset-public-data-formal-pending'`, manifest.Hash, manifest.DatasetID,
		int64(manifest.Revision), manifestPath, commit).Scan(&matched); err != nil || !matched {
		t.Fatalf("owner console immutable dataset catalog identity mismatch: %t %v", matched, err)
	}
	ownerConsoleE2EPrepareShadowEvidence(t, ctx, pool, input)
}

// TestOwnerConsoleRunIntegratedBrowserLocalShadowDriver exercises the real durable
// shadow lifecycle without constructing a public exchange client. It is an
// opt-in local E2E fixture only and is absent from the platform binary.
func TestOwnerConsoleRunIntegratedBrowserLocalShadowDriver(t *testing.T) {
	dsn := os.Getenv("AXIOM_OWNER_CONSOLE_E2E_LOCAL_DRIVER_DSN")
	stopPath := os.Getenv("AXIOM_OWNER_CONSOLE_E2E_LOCAL_DRIVER_STOP")
	if dsn == "" || stopPath == "" {
		t.Skip("owner console integrated local shadow driver is not enabled")
	}
	if !filepath.IsAbs(stopPath) || !strings.HasPrefix(filepath.Clean(stopPath), "/tmp/") {
		t.Fatal("owner console integrated local shadow driver requires an isolated /tmp stop path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store, err := postgresstore.NewPublicShadowStore(pool, "owner_console-e2e-local-driver", &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	work, err := newShadowRoleWork(store, func(context.Context, postgresstore.PublicShadowClaim) (shadowSession, error) {
		return ownerConsoleE2ELocalShadowSession{}, nil
	}, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	go ownerConsoleE2ERefreshLocalDriverPressure(ctx, cancel, pool, stopPath)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err = work.Run(ctx, logger); err != nil || ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("owner console integrated local shadow driver stopped unexpectedly: %v", err)
	}
}

type ownerConsoleE2ELocalShadowSession struct{}

func (ownerConsoleE2ELocalShadowSession) Run(ctx context.Context) error { <-ctx.Done(); return nil }
func (ownerConsoleE2ELocalShadowSession) SetEntriesEnabled(bool)        {}
func (ownerConsoleE2ELocalShadowSession) FlushAvailable(context.Context) error {
	return nil
}
func (ownerConsoleE2ELocalShadowSession) Flush(context.Context) error      { return nil }
func (ownerConsoleE2ELocalShadowSession) Checkpoint(context.Context) error { return nil }

func ownerConsoleE2ERefreshLocalDriverPressure(ctx context.Context, cancel context.CancelFunc,
	pool *pgxpool.Pool, stopPath string) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		_, _ = pool.Exec(ctx, `UPDATE owner_console_storage_pressure_state SET level='NORMAL',
available_bytes=21474836480,total_bytes=107374182400,revision=revision+1,
observed_at=CURRENT_TIMESTAMP,source_instance='owner_console-e2e-local-driver'
WHERE scope_id='market-data'`)
		if _, err := os.Stat(stopPath); err == nil {
			cancel()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func ownerConsoleE2EPrepareShadowEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, input trend.Input) {
	t.Helper()
	var err error
	now := ownerConsoleE2ESeedMarketWindow(t, ctx, pool)

	principal := ownerConsoleE2EPrincipal(t, ctx, pool)
	clock := &domain.SystemClock{}
	consoleStore, err := postgresstore.NewOwnerConsoleStore(pool, []byte(strings.Repeat("e", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	shadow, err := consoleStore.CreateShadow(ctx, principal, "owner_console-e2e-evidence-shadow", generated.ShadowSessionRequest{
		ConfigurationId: "configuration-research_registry", PortfolioId: "portfolio-owner_console",
		StrategyVersion: generated.ShadowSessionRequestStrategyVersionTrendFollowing100,
	})
	if err != nil {
		t.Fatal(err)
	}
	shadowStore, err := postgresstore.NewPublicShadowStore(pool, "owner_console-e2e-evidence-driver", clock)
	if err != nil {
		t.Fatal(err)
	}
	claim, found, err := shadowStore.Claim(ctx)
	if err != nil || !found || claim.ID != shadow.Id {
		t.Fatalf("owner console evidence shadow claim = %#v %t %v", claim, found, err)
	}
	if err = shadowStore.Activate(ctx, claim.ID); err != nil {
		t.Fatal(err)
	}
	ownerConsoleE2ERecordShadowDecision(t, ctx, shadowStore, claim, input)
	projection, err := consoleStore.Shadow(ctx, claim.ID)
	if err != nil || projection.AcceptedDecisions != 1 || projection.Orders == nil || len(*projection.Orders) == 0 || projection.JournalTransactions == 0 {
		t.Fatalf("owner console evidence shadow projection = %#v %v", projection, err)
	}
	if _, err = consoleStore.StopShadow(ctx, principal, claim.ID, "owner_console-e2e-evidence-stop", generated.RevisionCommandRequest{
		ExpectedRevision: projection.Revision, Reason: "complete deterministic integrated qualification evidence",
	}); err != nil {
		t.Fatal(err)
	}
	checkpoint := json.RawMessage(`{"schema_version":"owner_console.e2e.shadow.v1","input_ordinal":1}`)
	if err = shadowStore.Checkpoint(ctx, claim, postgresstore.PublicShadowCheckpoint{InputOrdinal: input.Ordinal,
		CursorLogicalTime: input.LogicalTime, Canonical: checkpoint}); err != nil {
		t.Fatal(err)
	}
	if err = shadowStore.CompleteStop(ctx, claim.ID); err != nil {
		t.Fatal(err)
	}

	ownerConsoleE2ELinkIncident(t, ctx, pool, claim.ID, now)
}

func ownerConsoleE2ERecordShadowDecision(t *testing.T, ctx context.Context, shadowStore *postgresstore.PublicShadowStore,
	claim postgresstore.PublicShadowClaim, input trend.Input) {
	t.Helper()
	input.Evidence.FeeModelID = claim.Configuration.Models.Fee
	input.Evidence.LatencyModelID = claim.Configuration.Models.Latency
	input.Evidence.FillModelID = claim.Models.FillDomain
	input.Evidence.SlippageModelID = claim.SlippageModelID
	input.Evidence.GapModelID = claim.GapModelID
	input.Sizing.LiquidityDomain = claim.Models.LiquidityDomain
	processor, err := newOwnerConsoleOperationalProcessorWithPortfolio(ownerConsoleE2EClaim(claim), nil)
	if err != nil {
		t.Fatal(err)
	}
	canonicalInput, _ := json.Marshal(input)
	result, err := processor.Process(ctx, replay.Event{Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: canonicalInput})
	if err != nil {
		t.Fatal(err)
	}
	if err = shadowStore.RecordShadowDecision(ctx, claim, input, result); err != nil {
		t.Fatal(err)
	}
}

func ownerConsoleE2ELinkIncident(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shadowID string, now time.Time) {
	t.Helper()
	var commandID string
	if err := pool.QueryRow(ctx, `SELECT command_id FROM shadow_sessions WHERE id=$1`, shadowID).Scan(&commandID); err != nil {
		t.Fatal(err)
	}
	var windowLinked bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(
	  SELECT 1 FROM dataset_segments
	  WHERE dataset_id='dataset-public-data-formal-pending'
	    AND segment_id='segment-owner_console-e2e-window'
	    AND ordinal=1
	)`).Scan(&windowLinked); err != nil || !windowLinked {
		t.Fatalf("owner console immutable incident window is not cataloged: %t %v", windowLinked, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO incidents(id,severity,state,reason_code,opened_at)
	  VALUES($1,'warning','resolved','owner_console_integrated_reproduction',$2)`, commandID, now); err != nil {
		t.Fatal(err)
	}
	t.Logf("OWNER_CONSOLE_E2E_EVIDENCE_SHADOW_ID=%s", shadowID)
}

func ownerConsoleE2ESeedMarketWindow(t *testing.T, ctx context.Context, pool *pgxpool.Pool) time.Time {
	t.Helper()
	var err error
	if _, err = pool.Exec(ctx, `INSERT INTO assets(symbol) VALUES('ETH') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	windowHash := strings.Repeat("9", 64)
	if _, err = pool.Exec(ctx, `INSERT INTO market_data_segments(id,recorder_session,exchange_id,instrument_id,
	  event_type,schema_version,parser_version,normalization_version,compression,path,checksum,ordered_content_hash,
	  record_count,first_ordinal,last_ordinal,started_at,ended_at,state,finalized_at)
	  VALUES('segment-owner_console-e2e-window','owner_console-e2e-window','binance','instrument-research_registry','candle',
	  'market-wire.v1','decision-input-v1','decision-input-v1','zstd','owner_console/e2e-window.zst',$1,$1,1,1,1,$2,$3,'ready',$3)
	  ON CONFLICT (id) DO NOTHING`,
		windowHash, now.Add(-time.Second), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var incidentAt time.Time
	if err = pool.QueryRow(ctx, `SELECT started_at+(ended_at-started_at)/2
	  FROM market_data_segments WHERE id='segment-owner_console-e2e-window'`).Scan(&incidentAt); err != nil {
		t.Fatal(err)
	}
	return incidentAt.UTC()
}

const ownerConsoleE2EReplayEventCount = 1024

func ownerConsoleE2EDataset(t *testing.T, root string, input trend.Input) recorder.DatasetManifest {
	t.Helper()
	instrument := input.Instrument
	stream, err := recorder.New(root, "recorder-owner_console-e2e", "owner_console-e2e", "binance",
		&runtimecore.IngestOrdinals{}, func(segments.Manifest) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Maximum-speed replay intentionally adds no artificial wall-clock delay.
	// A qualification-sized stream leaves enough real event-boundary control
	// checks for the browser to exercise durable pause, step, and resume.
	for index := 0; index < ownerConsoleE2EReplayEventCount; index++ {
		eventInput := input
		eventInput.Candles = append([]exchangecontracts.Candle(nil), input.Candles...)
		// Alternate accepted entries and protective-stop exits so the immutable
		// fixture stays consistent with the portfolio's owned-position boundary.
		// The small position also keeps all 512 round trips above the
		// conservative reserve floor after simulated fees.
		if index%2 == 1 {
			eventInput.Position = trend.PositionState{
				Open:                  true,
				Quantity:              ownerConsoleE2EQuantity(t, "0.0563"),
				ActualEntryPrice:      ownerConsoleE2EPrice(t, "300.3"),
				SignalATR:             ownerConsoleE2EPrice(t, "3.071428571428571429"),
				InitialStop:           ownerConsoleE2EPrice(t, "300"),
				TrailingStop:          ownerConsoleE2EPrice(t, "300"),
				HighestFavorableClose: ownerConsoleE2EPrice(t, "301"),
			}
		}
		eventInput.LogicalTime += uint64(time.Duration(index) * 5 * time.Second)
		eventInput.Candles[len(eventInput.Candles)-1].RawPayloadHash = fmt.Sprintf("owner_console-e2e-replay-%d", index+1)
		eventInput.Evidence.CausationID = fmt.Sprintf("owner_console-e2e-candle-%d", index+1)
		ordinal, recordErr := stream.RecordDecisionInputBuilt(recorder.DecisionInput{Instrument: instrument,
			EventID: fmt.Sprintf("decision-input-owner_console-e2e-%d", index+1), LogicalTime: eventInput.LogicalTime,
			ReceivedAt: eventInput.Now}, func(assigned uint64) ([]byte, error) {
			eventInput.Ordinal = assigned
			return json.Marshal(eventInput)
		})
		if recordErr != nil || ordinal != uint64(index+1) {
			t.Fatalf("owner console decision input record %d = %d %v", index+1, ordinal, recordErr)
		}
	}
	manifest, err := stream.Flush()
	if err != nil || !manifest.Complete || len(manifest.Segments) != 2 {
		t.Fatalf("owner console decision dataset = %#v %v", manifest, err)
	}
	return manifest
}

func ownerConsoleE2ETrendInput(t *testing.T, configuration config.Configuration, now time.Time) trend.Input {
	t.Helper()
	configured, err := trend.NewConfiguration(configuration.Trend)
	if err != nil {
		t.Fatal(err)
	}
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	lastClose := now.Truncate(4 * time.Hour)
	start := lastClose.Add(-200 * 4 * time.Hour)
	candles := make([]exchangecontracts.Candle, 200)
	for index := range candles {
		closeValue := 100 + index
		if index == len(candles)-1 {
			closeValue = 301
		}
		open := start.Add(time.Duration(index) * 4 * time.Hour)
		closeTime := open.Add(4 * time.Hour)
		candles[index] = exchangecontracts.Candle{Exchange: "binance", Instrument: instrument, Interval: "4h",
			OpenTime: open, CloseTime: closeTime, Open: ownerConsoleE2EPrice(t, fmt.Sprint(closeValue-1)),
			High: ownerConsoleE2EPrice(t, fmt.Sprint(closeValue+1)), Low: ownerConsoleE2EPrice(t, fmt.Sprint(closeValue-2)),
			Close: ownerConsoleE2EPrice(t, fmt.Sprint(closeValue)), Volume: ownerConsoleE2EQuantity(t, "1"), Closed: true,
			ReceivedAt: domain.EventTime{UTC: closeTime, Sequence: uint64(index + 1)}, RawPayloadHash: fmt.Sprintf("owner_console-e2e-candle-%03d", index)}
	}
	metadata := domain.InstrumentMetadata{Instrument: instrument, Version: 1, EffectiveAt: start,
		PriceTick: ownerConsoleE2EPrice(t, "0.01"), QuantityStep: ownerConsoleE2EQuantity(t, "0.0001"),
		MinimumQuantity: ownerConsoleE2EQuantity(t, "0.0001"), MinimumNotional: ownerConsoleE2ENotional(t, "10")}
	return trend.Input{Ordinal: 1, LogicalTime: uint64(100 * time.Second), Now: lastClose.Add(3 * time.Second),
		Instrument: instrument, Candles: candles, MarketHealthy: true, BookAge: time.Millisecond,
		Sizing: trend.SizingState{Equity: ownerConsoleE2EMoney(t, "100"), AvailableCash: ownerConsoleE2EMoney(t, "500"),
			MinimumReserve: ownerConsoleE2EMoney(t, "75"), NotionalLimits: []domain.Money{ownerConsoleE2EMoney(t, "150")},
			EntryReference: ownerConsoleE2EPrice(t, "300"), FirstExecutablePrice: ownerConsoleE2EPrice(t, "300"),
			GapAllowance: ownerConsoleE2EPrice(t, "0.5"), LatencyDeterioration: ownerConsoleE2EPrice(t, "0.1"),
			EntryFeeRate: ownerConsoleE2ERate(t, "0.001"), ExitFeeRate: ownerConsoleE2ERate(t, "0.001"),
			InstrumentMetadata: metadata, CentralRiskEligible: true, LiquidityDomain: "combined-owner_console", FencingToken: 1},
		Evidence: trend.InputEvidence{CandleViewID: "owner_console-e2e-candles-btc", CandleViewRevision: 200,
			MarketViewID: "owner_console-e2e-book-btc", MarketViewRevision: 1, InstrumentMetadataID: "metadata-research_registry",
			AssetEligibilityVersion: 1, ConfigurationVersion: configuration.SchemaVersion,
			ConfigurationHash: configured.Hash, StrategyVersion: configured.Version, PortfolioRevision: 1,
			PositionRevision: 1, FeeModelID: "fixed-bps-v1", LatencyModelID: "fixed-zero-v1",
			FillModelID: "fill-v1", SlippageModelID: "slippage-v1", GapModelID: "gap-v1",
			CorrelationID: "owner_console-e2e-shadow", CausationID: "owner_console-e2e-candle-199"}}
}

func ownerConsoleE2EClaim(claim postgresstore.PublicShadowClaim) backtest.JobClaim {
	runID, _ := domain.NewRunID(claim.RunID)
	return backtest.JobClaim{ID: claim.ID, Configuration: claim.Configuration,
		Manifest: backtest.RunManifest{RunID: runID, Mode: "shadow", ConfigurationHash: claim.ConfigurationHash,
			StrategyVersion: "trend-following@1.0.0", Seed: ownerConsoleLocalHash([]byte("shadow-seed:" + claim.ID)), Models: claim.Models}}
}

func ownerConsoleE2EPrincipal(t *testing.T, ctx context.Context, pool *pgxpool.Pool) authentication.Principal {
	t.Helper()
	var userID, email, sessionID string
	if err := pool.QueryRow(ctx, `SELECT u.id,u.email,session.id FROM users u
	  JOIN LATERAL (SELECT id FROM sessions WHERE user_id=u.id ORDER BY created_at DESC,id DESC LIMIT 1) session ON true
	  WHERE u.normalized_email='owner@example.test'`).Scan(&userID, &email, &sessionID); err != nil {
		t.Fatal(err)
	}
	return authentication.Principal{UserID: userID, Email: email, SessionID: sessionID,
		ReauthenticatedAt: time.Now().UTC(), SessionRevision: 1}
}

func ownerConsoleE2EHexIdentity(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && (len(decoded) == sha256.Size || len(decoded) == 20)
}

func ownerConsoleE2EHash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func ownerConsoleE2EPrice(t *testing.T, value string) domain.Price {
	t.Helper()
	result, err := domain.ParsePrice(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func ownerConsoleE2EQuantity(t *testing.T, value string) domain.Quantity {
	t.Helper()
	result, err := domain.ParseQuantity(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func ownerConsoleE2ENotional(t *testing.T, value string) domain.Notional {
	t.Helper()
	result, err := domain.ParseNotional(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func ownerConsoleE2EMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	result, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func ownerConsoleE2ERate(t *testing.T, value string) domain.Rate {
	t.Helper()
	result, err := domain.ParseRate(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
