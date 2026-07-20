package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// TestA11PrepareIntegratedBrowserDataset records the immutable decision inputs
// before the PostgreSQL fixture catalogs their exact manifest identity.
func TestA11PrepareIntegratedBrowserDataset(t *testing.T) {
	root := os.Getenv("AXIOM_A11_E2E_RECORDER_ROOT")
	if root == "" {
		t.Skip("A11 integrated browser dataset setup is not enabled")
	}
	if !filepath.IsAbs(root) || !strings.HasPrefix(filepath.Clean(root), "/tmp/") {
		t.Fatal("A11 integrated dataset setup requires an isolated /tmp recorder root")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("A11 recorder root is unavailable: %v", err)
	}
	if len(entries) == 0 {
		configuration := config.DefaultConfiguration()
		a11E2EDataset(t, root, a11E2ETrendInput(t, configuration, time.Now().UTC()))
	}
	manifest, err := recorder.ReadManifest(filepath.Join(root, "a11-e2e-000001.dataset.json"))
	if err != nil || !manifest.Complete || manifest.RawRecordCount != a11E2EReplayEventCount ||
		manifest.CanonicalCount != a11E2EReplayEventCount || len(manifest.Segments) != 2 {
		t.Fatalf("A11 decision dataset is invalid: %#v %v", manifest, err)
	}
	t.Logf("AXIOM_A11_E2E_DATASET_MANIFEST=%s", filepath.Join(root, "a11-e2e-000001.dataset.json"))
}

// TestA11PrepareIntegratedBrowserEnvironment turns the already-qualified A11
// PostgreSQL fixture into a deterministic, unmocked browser environment. It
// produces shadow evidence through the production Trend/allocation/risk/
// simulation pipeline and PostgreSQL stores. It is opt-in and never ships a
// runtime bypass in the platform binary.
func TestA11PrepareIntegratedBrowserEnvironment(t *testing.T) {
	dsn := os.Getenv("AXIOM_A11_E2E_SETUP_DSN")
	root := os.Getenv("AXIOM_A11_E2E_RECORDER_ROOT")
	commit := os.Getenv("AXIOM_A11_E2E_SOURCE_COMMIT")
	if dsn == "" || root == "" || commit == "" {
		t.Skip("A11 integrated browser setup is not enabled")
	}
	if !filepath.IsAbs(root) || !strings.HasPrefix(filepath.Clean(root), "/tmp/") || !a11E2EHexIdentity(commit) {
		t.Fatal("A11 integrated setup requires an isolated /tmp recorder root and source identity")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("A11 recorder root is unavailable: %v", err)
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
	configurationHash := a11E2EHash(canonicalConfiguration)
	var storedHash string
	if err = pool.QueryRow(ctx, `SELECT configuration_hash::text FROM configuration_versions WHERE id='configuration-a10'`).Scan(&storedHash); err != nil || storedHash != configurationHash {
		t.Fatalf("A11 qualification configuration mismatch: %s %v", storedHash, err)
	}

	if len(entries) == 0 {
		t.Fatal("A11 immutable decision dataset must be prepared before PostgreSQL qualification")
	}
	input := a11E2ETrendInput(t, configuration, time.Now().UTC())
	manifest, err := recorder.ReadManifest(filepath.Join(root, "a11-e2e-000001.dataset.json"))
	if err != nil || !manifest.Complete || manifest.RawRecordCount != a11E2EReplayEventCount ||
		manifest.CanonicalCount != a11E2EReplayEventCount || len(manifest.Segments) != 2 {
		t.Fatalf("A11 existing decision dataset is invalid: %#v %v", manifest, err)
	}
	manifestPath := fmt.Sprintf("%s-%06d.dataset.json", manifest.SessionID, manifest.Revision)
	var matched bool
	if err = pool.QueryRow(ctx, `SELECT dataset_hash=$1 AND recorder_dataset_id=$2 AND manifest_revision=$3
	  AND manifest_path=$4 AND source_commit=$5 AND dataset_kind='decision_inputs'
	  FROM dataset_manifests WHERE id='dataset-a7-formal-pending'`, manifest.Hash, manifest.DatasetID,
		int64(manifest.Revision), manifestPath, commit).Scan(&matched); err != nil || !matched {
		t.Fatalf("A11 immutable dataset catalog identity mismatch: %t %v", matched, err)
	}
	a11E2EPrepareShadowEvidence(t, ctx, pool, input)
}

func a11E2EPrepareShadowEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, input trend.Input) {
	t.Helper()
	var err error
	now := a11E2ESeedMarketWindow(t, ctx, pool)

	principal := a11E2EPrincipal(t, ctx, pool)
	clock := &domain.SystemClock{}
	consoleStore, err := postgresstore.NewA11ConsoleStore(pool, []byte(strings.Repeat("e", 32)), clock)
	if err != nil {
		t.Fatal(err)
	}
	shadow, err := consoleStore.CreateShadow(ctx, principal, "a11-e2e-evidence-shadow", generated.ShadowSessionRequest{
		ConfigurationId: "configuration-a10", PortfolioId: "portfolio-a11",
		StrategyVersion: generated.ShadowSessionRequestStrategyVersionTrendV1a1,
	})
	if err != nil {
		t.Fatal(err)
	}
	shadowStore, err := postgresstore.NewA11ShadowStore(pool, "a11-e2e-evidence-driver", clock)
	if err != nil {
		t.Fatal(err)
	}
	claim, found, err := shadowStore.Claim(ctx)
	if err != nil || !found || claim.ID != shadow.Id {
		t.Fatalf("A11 evidence shadow claim = %#v %t %v", claim, found, err)
	}
	if err = shadowStore.Activate(ctx, claim.ID); err != nil {
		t.Fatal(err)
	}
	a11E2ERecordShadowDecision(t, ctx, shadowStore, claim, input)
	projection, err := consoleStore.Shadow(ctx, claim.ID)
	if err != nil || projection.AcceptedDecisions != 1 || projection.Orders == nil || len(*projection.Orders) == 0 || projection.JournalTransactions == 0 {
		t.Fatalf("A11 evidence shadow projection = %#v %v", projection, err)
	}
	if _, err = consoleStore.StopShadow(ctx, principal, claim.ID, "a11-e2e-evidence-stop", generated.RevisionCommandRequest{
		ExpectedRevision: projection.Revision, Reason: "complete deterministic integrated qualification evidence",
	}); err != nil {
		t.Fatal(err)
	}
	checkpoint := json.RawMessage(`{"schema_version":"a11.e2e.shadow.v1","input_ordinal":1}`)
	if err = shadowStore.Checkpoint(ctx, claim, postgresstore.A11ShadowCheckpoint{InputOrdinal: input.Ordinal,
		CursorLogicalTime: input.LogicalTime, Canonical: checkpoint}); err != nil {
		t.Fatal(err)
	}
	if err = shadowStore.CompleteStop(ctx, claim.ID); err != nil {
		t.Fatal(err)
	}

	a11E2ELinkIncident(t, ctx, pool, claim.ID, now)
}

func a11E2ERecordShadowDecision(t *testing.T, ctx context.Context, shadowStore *postgresstore.A11ShadowStore,
	claim postgresstore.A11ShadowClaim, input trend.Input) {
	t.Helper()
	input.Evidence.FeeModelID = claim.Configuration.Models.Fee
	input.Evidence.LatencyModelID = claim.Configuration.Models.Latency
	input.Evidence.FillModelID = claim.Models.FillDomain
	input.Evidence.SlippageModelID = claim.SlippageModelID
	input.Evidence.GapModelID = claim.GapModelID
	input.Sizing.LiquidityDomain = claim.Models.LiquidityDomain
	processor, err := newA11OperationalProcessorWithPortfolio(a11E2EClaim(claim), nil)
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

func a11E2ELinkIncident(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shadowID string, now time.Time) {
	t.Helper()
	var commandID string
	if err := pool.QueryRow(ctx, `SELECT command_id FROM shadow_sessions WHERE id=$1`, shadowID).Scan(&commandID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO dataset_segments(dataset_id,segment_id,ordinal)
	  VALUES('dataset-a7-formal-pending','segment-a11-e2e-window',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO incidents(id,severity,state,reason_code,opened_at)
	  VALUES($1,'warning','resolved','a11_integrated_reproduction',$2)`, commandID, now); err != nil {
		t.Fatal(err)
	}
	t.Logf("A11_E2E_EVIDENCE_SHADOW_ID=%s", shadowID)
}

func a11E2ESeedMarketWindow(t *testing.T, ctx context.Context, pool *pgxpool.Pool) time.Time {
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
	  VALUES('segment-a11-e2e-window','a11-e2e-window','binance','instrument-a10','candle',
	  'market-wire.v1','decision-input-v1','decision-input-v1','zstd','a11/e2e-window.zst',$1,$1,1,1,1,$2,$3,'ready',$3)
	  ON CONFLICT (id) DO NOTHING`,
		windowHash, now.Add(-time.Second), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	return now
}

const a11E2EReplayEventCount = 3072

func a11E2EDataset(t *testing.T, root string, input trend.Input) recorder.DatasetManifest {
	t.Helper()
	instrument := input.Instrument
	stream, err := recorder.New(root, "recorder-a11-e2e", "a11-e2e", "binance",
		&runtimecore.IngestOrdinals{}, func(segments.Manifest) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Maximum-speed replay intentionally adds no artificial wall-clock delay.
	// A qualification-sized stream leaves enough real event-boundary control
	// checks for the browser to exercise durable pause, step, and resume.
	for index := 0; index < a11E2EReplayEventCount; index++ {
		eventInput := input
		eventInput.Candles = append([]exchangecontracts.Candle(nil), input.Candles...)
		// Three accepted entries consume the qualification portfolio's usable
		// virtual cash. Later events remain canonical replay inputs but become
		// explicit risk rejections instead of advertising a stale cash snapshot.
		eventInput.Sizing.CentralRiskEligible = index < 3
		eventInput.LogicalTime += uint64(time.Duration(index) * 5 * time.Second)
		eventInput.Candles[len(eventInput.Candles)-1].RawPayloadHash = fmt.Sprintf("a11-e2e-replay-%d", index+1)
		eventInput.Evidence.CausationID = fmt.Sprintf("a11-e2e-candle-%d", index+1)
		ordinal, recordErr := stream.RecordDecisionInputBuilt(recorder.DecisionInput{Instrument: instrument,
			EventID: fmt.Sprintf("decision-input-a11-e2e-%d", index+1), LogicalTime: eventInput.LogicalTime,
			ReceivedAt: eventInput.Now}, func(assigned uint64) ([]byte, error) {
			eventInput.Ordinal = assigned
			return json.Marshal(eventInput)
		})
		if recordErr != nil || ordinal != uint64(index+1) {
			t.Fatalf("A11 decision input record %d = %d %v", index+1, ordinal, recordErr)
		}
	}
	manifest, err := stream.Flush()
	if err != nil || !manifest.Complete || len(manifest.Segments) != 2 {
		t.Fatalf("A11 decision dataset = %#v %v", manifest, err)
	}
	return manifest
}

func a11E2ETrendInput(t *testing.T, configuration config.Configuration, now time.Time) trend.Input {
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
			OpenTime: open, CloseTime: closeTime, Open: a11E2EPrice(t, fmt.Sprint(closeValue-1)),
			High: a11E2EPrice(t, fmt.Sprint(closeValue+1)), Low: a11E2EPrice(t, fmt.Sprint(closeValue-2)),
			Close: a11E2EPrice(t, fmt.Sprint(closeValue)), Volume: a11E2EQuantity(t, "1"), Closed: true,
			ReceivedAt: domain.EventTime{UTC: closeTime, Sequence: uint64(index + 1)}, RawPayloadHash: fmt.Sprintf("a11-e2e-candle-%03d", index)}
	}
	metadata := domain.InstrumentMetadata{Instrument: instrument, Version: 1, EffectiveAt: start,
		PriceTick: a11E2EPrice(t, "0.01"), QuantityStep: a11E2EQuantity(t, "0.0001"),
		MinimumQuantity: a11E2EQuantity(t, "0.0001"), MinimumNotional: a11E2ENotional(t, "10")}
	return trend.Input{Ordinal: 1, LogicalTime: uint64(100 * time.Second), Now: lastClose.Add(3 * time.Second),
		Instrument: instrument, Candles: candles, MarketHealthy: true, BookAge: time.Millisecond,
		Sizing: trend.SizingState{Equity: a11E2EMoney(t, "500"), AvailableCash: a11E2EMoney(t, "500"),
			MinimumReserve: a11E2EMoney(t, "75"), NotionalLimits: []domain.Money{a11E2EMoney(t, "150")},
			EntryReference: a11E2EPrice(t, "300"), FirstExecutablePrice: a11E2EPrice(t, "300"),
			GapAllowance: a11E2EPrice(t, "0.5"), LatencyDeterioration: a11E2EPrice(t, "0.1"),
			EntryFeeRate: a11E2ERate(t, "0.001"), ExitFeeRate: a11E2ERate(t, "0.001"),
			InstrumentMetadata: metadata, CentralRiskEligible: true, LiquidityDomain: "combined-a11", FencingToken: 1},
		Evidence: trend.InputEvidence{CandleViewID: "a11-e2e-candles-btc", CandleViewRevision: 200,
			MarketViewID: "a11-e2e-book-btc", MarketViewRevision: 1, InstrumentMetadataID: "metadata-a10",
			AssetEligibilityVersion: 1, ConfigurationVersion: configuration.SchemaVersion,
			ConfigurationHash: configured.Hash, StrategyVersion: configured.Version, PortfolioRevision: 1,
			PositionRevision: 1, FeeModelID: "fixed-bps-v1", LatencyModelID: "fixed-zero-v1",
			FillModelID: "fill-v1", SlippageModelID: "slippage-v1", GapModelID: "gap-v1",
			CorrelationID: "a11-e2e-shadow", CausationID: "a11-e2e-candle-199"}}
}

func a11E2EClaim(claim postgresstore.A11ShadowClaim) backtest.JobClaim {
	runID, _ := domain.NewRunID(claim.RunID)
	return backtest.JobClaim{ID: claim.ID, Configuration: claim.Configuration,
		Manifest: backtest.RunManifest{RunID: runID, Mode: "shadow", ConfigurationHash: claim.ConfigurationHash,
			Seed: a11LocalHash([]byte("shadow-seed:" + claim.ID)), Models: claim.Models}}
}

func a11E2EPrincipal(t *testing.T, ctx context.Context, pool *pgxpool.Pool) authentication.Principal {
	t.Helper()
	var userID, email, sessionID string
	if err := pool.QueryRow(ctx, `SELECT u.id,u.email,session.id FROM users u
	  JOIN LATERAL (SELECT id FROM sessions WHERE user_id=u.id ORDER BY created_at DESC,id DESC LIMIT 1) session ON true
	  WHERE u.normalized_email='owner@example.test'`).Scan(&userID, &email, &sessionID); err != nil {
		t.Fatal(err)
	}
	return authentication.Principal{UserID: userID, Email: email, SessionID: sessionID,
		Roles: []string{"owner"}, Permissions: []string{"operations.read", "commands.write", "incident.raw", "audit.raw"},
		ReauthenticatedAt: time.Now().UTC(), SessionRevision: 1}
}

func a11E2EHexIdentity(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && (len(decoded) == sha256.Size || len(decoded) == 20)
}

func a11E2EHash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func a11E2EPrice(t *testing.T, value string) domain.Price {
	t.Helper()
	result, err := domain.ParsePrice(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func a11E2EQuantity(t *testing.T, value string) domain.Quantity {
	t.Helper()
	result, err := domain.ParseQuantity(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func a11E2ENotional(t *testing.T, value string) domain.Notional {
	t.Helper()
	result, err := domain.ParseNotional(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func a11E2EMoney(t *testing.T, value string) domain.Money {
	t.Helper()
	result, err := domain.ParseMoney(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func a11E2ERate(t *testing.T, value string) domain.Rate {
	t.Helper()
	result, err := domain.ParseRate(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
