package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"axiom/internal/authentication"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSandboxRuntimePostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openSandboxRuntimeTestDatabase(t, "AXIOM_SANDBOX_RUNTIME_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	migrations, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyMigrations(ctx, pool)
	if err != nil || applied != len(migrations) {
		t.Fatalf("clean migration applied=%d want=%d error=%v", applied, len(migrations), err)
	}
	assertSandboxRuntimeSchema(t, ctx, pool)
	assertSandboxRuntimeAuthenticatedRequestEvidence(t, ctx, pool)
	assertSandboxRuntimeCanaryEvidenceRuntimeReadGrant(t, ctx, pool)
	assertSandboxRuntimeFailClosedConstraints(t, ctx, pool)
	assertSandboxRuntimeEngineRuntimePersistence(t, ctx, pool)
	assertSandboxRuntimeLeaseIsolation(t, ctx, pool)
	assertSandboxRuntimeAuditChainSerialization(t, ctx, pool)
	assertSandboxRuntimeAuthorizationSessionBinding(t, ctx, pool)
	assertSandboxRuntimeCanarySessionPersistence(t, ctx, pool)
	assertSandboxRuntimeStrategySessionPersistence(t, ctx, pool)
	assertSandboxRuntimeDispatcherCrashRecovery(t, ctx, pool)
	assertSandboxRuntimeMultilegPersistence(t, ctx, pool)
	assertSandboxRuntimeControlRecoveryAndReset(t, ctx, pool)
	assertSandboxQualificationBoundary(t, ctx, pool)
	assertSandboxQualificationObserverQueryParameters(t, ctx, pool)
}

func TestSandboxRuntimePostgresMultiExchangeConsoleToSandboxRuntimeUpgradeQualification(t *testing.T) {
	ctx, pool := openSandboxRuntimeTestDatabase(t, "AXIOM_SANDBOX_RUNTIME_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyTriangularArbitrageMigrationPrefix(t, ctx, pool, 20)
	if _, err := pool.Exec(ctx, `INSERT INTO assets(symbol) VALUES ('SBXUPGRADE')`); err != nil {
		t.Fatal(err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	migrations, err := Migrations()
	if err != nil || len(migrations) < 54 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	for _, migration := range migrations[20:] {
		changed, applyErr := applyMigration(ctx, connection, migration)
		if applyErr != nil || !changed {
			t.Fatalf("multi-exchange-console-to-sandbox migration %s changed=%t error=%v", migration.Version, changed, applyErr)
		}
	}
	var sentinel int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM assets WHERE symbol='SBXUPGRADE'`).
		Scan(&sentinel); err != nil || sentinel != 1 {
		t.Fatalf("upgrade sentinel=%d error=%v", sentinel, err)
	}
	assertSandboxRuntimeSchema(t, ctx, pool)
	assertSandboxQualificationBoundary(t, ctx, pool)
	assertSandboxQualificationObserverQueryParameters(t, ctx, pool)
}

func openSandboxRuntimeTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_sandbox_runtime_test") {
		t.Fatal("sandbox runtime integration requires a dedicated database ending _sandbox_runtime_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return ctx, pool
}

func assertSandboxRuntimeSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	tables := []string{
		"sandbox_runtime_totp_replay_state", "sandbox_runtime_sandbox_authorizations",
		"sandbox_runtime_high_risk_audit_events", "sandbox_runtime_exchange_accounts",
		"sandbox_runtime_account_epochs", "sandbox_runtime_credential_generations",
		"sandbox_runtime_credential_rotations", "sandbox_runtime_sandbox_sessions", "sandbox_runtime_sandbox_arms",
		"sandbox_runtime_authenticated_request_evidence",
		"sandbox_runtime_account_snapshots", "sandbox_runtime_daily_cap_counters",
		"sandbox_runtime_submission_plans", "sandbox_runtime_plan_eligibility", "sandbox_runtime_plan_entry_safety",
		"sandbox_runtime_sandbox_reservations",
		"sandbox_runtime_submission_outbox", "sandbox_runtime_private_inbox", "sandbox_runtime_exchange_fills",
		"sandbox_runtime_exchange_metadata", "sandbox_runtime_reconciliation_differences",
		"sandbox_runtime_reconciliations", "sandbox_runtime_reset_incidents", "sandbox_runtime_external_adjustments",
		"sandbox_runtime_risk_unlocks", "sandbox_runtime_account_leases",
		"sandbox_runtime_engine_startup_evidence",
		"sandbox_runtime_engine_commands", "sandbox_runtime_engine_observations", "sandbox_runtime_engine_market_observations", "sandbox_runtime_canary_evidence",
		"sandbox_runtime_engine_runtime_events", "sandbox_qualification_order_observations",
		"sandbox_qualification_runs", "sandbox_qualification_accounts",
		"sandbox_qualification_samples", "sandbox_qualification_failures",
		"sandbox_qualification_chaos_events", "sandbox_qualification_recovery_events",
	}
	for _, table := range tables {
		var count int
		if err := pool.QueryRow(ctx, `
SELECT count(*) FROM information_schema.tables
WHERE table_schema='public' AND table_name=$1`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("sandbox runtime table %s count=%d error=%v", table, count, err)
		}
	}
}

func assertSandboxQualificationObserverQueryParameters(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	observedAt := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	var total, fresh, leases int
	var cycles int64
	err := pool.QueryRow(
		ctx,
		sandboxQualificationObserveAccountsSQL,
		observedAt,
	).Scan(&total, &fresh, &leases, &cycles)
	if err != nil {
		t.Fatalf("sandbox qualification account observer query parameters rejected: %v", err)
	}
	var reconnects, duration int64
	var runtimeHealthy bool
	err = pool.QueryRow(
		ctx,
		sandboxQualificationObserveRuntimeSQL,
		observedAt.Add(-time.Minute),
		observedAt,
	).Scan(&reconnects, &duration, &runtimeHealthy)
	if err != nil {
		t.Fatalf("sandbox qualification runtime observer cutoff parameters rejected: %v", err)
	}
	var details int
	err = pool.QueryRow(
		ctx,
		"SELECT count(*) FROM ("+
			sandboxQualificationObserveAccountDetailsSQL+
			") account_details",
		observedAt,
		observedAt.Add(-time.Minute),
	).Scan(&details)
	if err != nil {
		t.Fatalf("sandbox qualification account recovery observer query parameters rejected: %v", err)
	}
}

func assertSandboxQualificationBoundary(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	at := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	runID := fmt.Sprintf("sandbox_qualification-schema-%d", at.UnixNano())
	assertSandboxRuntimeFormalSandboxQualificationImageRequired(t, ctx, pool, runID, at)
	insertSandboxRuntimeSmokeSandboxQualificationRun(t, ctx, pool, runID, at)
	assertSandboxRuntimeSmokeCannotBecomeFormal(t, ctx, pool, runID, at)
	assertSandboxQualificationRunPending(t, ctx, pool, runID)
}

func assertSandboxRuntimeFormalSandboxQualificationImageRequired(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runID string,
	at time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_qualification_runs(
 id,mode,state,commit_sha,build_hash,executable_hash,
 configuration_hash,source_dirty,required_duration_seconds,
 observed_duration_seconds,profitability_evidence,qualified,revision,
 created_at,updated_at
) VALUES(
 $1,'formal','PENDING',$2,$3,$4,$5,false,259200,0,false,false,1,$6,$6
)`,
		runID+"-formal-no-image",
		strings.Repeat("a", 40),
		strings.Repeat("b", 64),
		strings.Repeat("c", 64),
		strings.Repeat("d", 64),
		at,
	); err == nil {
		t.Fatal("formal sandbox qualification run without image identity was accepted")
	}
}

func insertSandboxRuntimeSmokeSandboxQualificationRun(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runID string,
	at time.Time,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO sandbox_qualification_runs(
 id,mode,state,commit_sha,build_hash,executable_hash,
 configuration_hash,source_dirty,
 required_duration_seconds,observed_duration_seconds,
 profitability_evidence,qualified,revision,created_at,updated_at
) VALUES($1,'smoke','PENDING',$2,$3,$4,$5,false,2,0,false,false,1,$6,$6)`,
		runID,
		strings.Repeat("a", 40),
		strings.Repeat("b", 64),
		strings.Repeat("c", 64),
		strings.Repeat("d", 64),
		at,
	)
	if err != nil {
		t.Fatalf("sandbox qualification pending run insert failed: %v", err)
	}
}

func assertSandboxRuntimeSmokeCannotBecomeFormal(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runID string,
	at time.Time,
) {
	t.Helper()
	var orderObservations int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM sandbox_qualification_order_observations`,
	).Scan(&orderObservations); err != nil {
		t.Fatalf("sandbox qualification redacted order observation view failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE sandbox_qualification_runs
SET state='PASSED',started_at=$2,ended_at=$2,evidence_hash=$3,
    observed_duration_seconds=259200,qualified=true,revision=2,updated_at=$2
WHERE id=$1`, runID, at, strings.Repeat("d", 64)); err == nil {
		t.Fatal("smoke run fabricated a formal 72-hour pass")
	}
	if _, err := pool.Exec(
		ctx,
		`DELETE FROM sandbox_qualification_runs WHERE id=$1`,
		runID,
	); err == nil {
		t.Fatal("sandbox qualification qualification run deletion was accepted")
	}
}

func assertSandboxQualificationRunPending(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runID string,
) {
	t.Helper()
	var profitability, qualified bool
	var state string
	if err := pool.QueryRow(ctx, `
SELECT state,profitability_evidence,qualified
FROM sandbox_qualification_runs WHERE id=$1`, runID).Scan(
		&state,
		&profitability,
		&qualified,
	); err != nil || state != "PENDING" || profitability || qualified {
		t.Fatalf(
			"unsafe sandbox qualification qualification row state=%s profitability=%t qualified=%t err=%v",
			state,
			profitability,
			qualified,
			err,
		)
	}
}

func assertSandboxRuntimeAuthenticatedRequestEvidence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	store, err := NewSandboxRuntimeDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	record := sandboxRuntimeAuthenticatedRequestEvidence()
	if err = store.RecordAuthenticatedRequest(ctx, record); err != nil {
		t.Fatalf("authenticated request evidence insert failed: %v", err)
	}
	if err = store.RecordAuthenticatedRequest(ctx, record); err == nil {
		t.Fatal("authenticated request hash replay was accepted")
	}
	assertSandboxRuntimeAuthenticatedEvidencePersisted(t, ctx, pool, record)
	assertSandboxRuntimeAuthenticatedEvidenceSchema(t, ctx, pool)
	assertSandboxRuntimeUnsafeAuthenticatedEvidenceRejected(t, ctx, pool, record.RecordedAt)
	assertSandboxRuntimeAuthenticatedEvidenceImmutable(t, ctx, pool, record)
}

func sandboxRuntimeAuthenticatedRequestEvidence() exchangecontracts.AuthenticatedRequestEvidence {
	return exchangecontracts.AuthenticatedRequestEvidence{
		Exchange:   "binance",
		Host:       "testnet.binance.vision",
		Method:     "POST",
		Path:       "/api/v3/order",
		FieldNames: []string{"newOrderRespType", "side", "symbol", "timestamp", "type"},
		Enumerated: map[string]string{
			"newOrderRespType": "ACK",
			"side":             "BUY",
			"type":             "LIMIT_MAKER",
		},
		RequestHash:     sha256.Sum256([]byte("sandbox_runtime-redacted-request")),
		ConfigurationID: "sandbox_runtime-request-evidence-configuration",
		RecordedAt:      time.Date(2026, 7, 27, 0, 15, 0, 0, time.UTC),
	}
}

func assertSandboxRuntimeCanaryEvidenceRuntimeReadGrant(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	runtimeRole := testRole("AXIOM_SANDBOX_RUNTIME_RUNTIME_ROLE", "axiom_app")
	recorderRole := testRole("AXIOM_SANDBOX_RUNTIME_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_SANDBOX_RUNTIME_READONLY_ROLE", "axiom_readonly")
	if err := ApplyRoleGrants(
		ctx,
		pool,
		runtimeRole,
		recorderRole,
		readOnlyRole,
	); err != nil {
		t.Fatalf("sandbox runtime role grants failed: %v", err)
	}
	assertSandboxRuntimeCanaryEvidenceRuntimePrivileges(t, ctx, pool, runtimeRole)
	assertSandboxRuntimeCanaryEvidenceRuntimeQuery(t, ctx, pool, runtimeRole)
}

func assertSandboxRuntimeCanaryEvidenceRuntimePrivileges(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runtimeRole string,
) {
	t.Helper()
	for _, check := range []struct {
		privilege string
		want      bool
	}{
		{privilege: "SELECT", want: true},
		{privilege: "INSERT", want: false},
		{privilege: "UPDATE", want: false},
		{privilege: "DELETE", want: false},
	} {
		var allowed bool
		if err := pool.QueryRow(
			ctx,
			"SELECT has_table_privilege($1,$2,$3)",
			runtimeRole,
			"sandbox_runtime_authenticated_request_evidence",
			check.privilege,
		).Scan(&allowed); err != nil || allowed != check.want {
			t.Fatalf(
				"runtime authenticated-evidence %s=%t want=%t error=%v",
				check.privilege,
				allowed,
				check.want,
				err,
			)
		}
	}
}

func assertSandboxRuntimeCanaryEvidenceRuntimeQuery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runtimeRole string,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(
		ctx,
		"SET LOCAL ROLE "+pgx.Identifier{runtimeRole}.Sanitize(),
	); err != nil {
		t.Fatal(err)
	}
	recordedAt := sandboxRuntimeAuthenticatedRequestEvidence().RecordedAt
	var count int64
	if err = tx.QueryRow(
		ctx,
		countCanaryCreateEvidenceSQL,
		sandbox.ExchangeBinance,
		"/api/v3/order",
		recordedAt,
		recordedAt,
	).Scan(&count); err != nil || count != 1 {
		t.Fatalf(
			"runtime canary create-evidence count=%d want=1 error=%v",
			count,
			err,
		)
	}
}

func assertSandboxRuntimeAuthenticatedEvidencePersisted(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	record exchangecontracts.AuthenticatedRequestEvidence,
) {
	t.Helper()
	var fields []string
	var enumerated, host, method, path string
	if err := pool.QueryRow(ctx, `
SELECT field_names,enumerated_fields::text,host,method,path
FROM sandbox_runtime_authenticated_request_evidence
WHERE exchange='binance' AND request_hash=$1`,
		fmt.Sprintf("%x", record.RequestHash),
	).Scan(&fields, &enumerated, &host, &method, &path); err != nil {
		t.Fatal(err)
	}
	var storedEnumerated map[string]string
	if err := json.Unmarshal([]byte(enumerated), &storedEnumerated); err != nil {
		t.Fatal(err)
	}
	if strings.Join(fields, ",") != strings.Join(record.FieldNames, ",") ||
		len(storedEnumerated) != len(record.Enumerated) ||
		storedEnumerated["newOrderRespType"] != "ACK" ||
		storedEnumerated["side"] != "BUY" ||
		storedEnumerated["type"] != "LIMIT_MAKER" ||
		host != record.Host || method != record.Method || path != record.Path {
		t.Fatalf("stored authenticated evidence drifted: %v %s %s %s %s",
			fields, enumerated, host, method, path)
	}
}

func assertSandboxRuntimeAuthenticatedEvidenceSchema(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	var columns []string
	rows, err := pool.Query(ctx, `
SELECT column_name FROM information_schema.columns
WHERE table_schema='public' AND table_name='sandbox_runtime_authenticated_request_evidence'
ORDER BY ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var column string
		if err = rows.Scan(&column); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		columns = append(columns, column)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	columnSet := "," + strings.Join(columns, ",") + ","
	for _, forbidden := range []string{
		"api_key", "api_secret", "signature", "headers", "private_payload",
		"price", "quantity", "session", "totp",
	} {
		if strings.Contains(columnSet, ","+forbidden+",") {
			t.Fatalf("authenticated evidence exposes forbidden column %s", forbidden)
		}
	}
}

func assertSandboxRuntimeAuthenticatedEvidenceImmutable(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	record exchangecontracts.AuthenticatedRequestEvidence,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE sandbox_runtime_authenticated_request_evidence
SET configuration_id='mutated'
WHERE exchange='binance' AND request_hash=$1`,
		fmt.Sprintf("%x", record.RequestHash)); err == nil {
		t.Fatal("authenticated request evidence mutation was accepted")
	}
	if _, err := pool.Exec(ctx, `
DELETE FROM sandbox_runtime_authenticated_request_evidence
WHERE exchange='binance' AND request_hash=$1`,
		fmt.Sprintf("%x", record.RequestHash)); err == nil {
		t.Fatal("authenticated request evidence deletion was accepted")
	}
}

type unsafeAuthenticatedEvidenceRow struct {
	name       string
	exchange   string
	host       string
	method     string
	path       string
	fields     []string
	enumerated string
	hash       string
}

func assertSandboxRuntimeUnsafeAuthenticatedEvidenceRejected(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	at time.Time,
) {
	t.Helper()
	unsafeRows := []unsafeAuthenticatedEvidenceRow{
		{
			name: "production host", exchange: "binance", host: "api.binance.com",
			method: "GET", path: "/api/v3/account", fields: []string{"timestamp"},
			enumerated: `{}`, hash: strings.Repeat("1", 64),
		},
		{
			name: "private field", exchange: "binance", host: "testnet.binance.vision",
			method: "GET", path: "/api/v3/account", fields: []string{"apiSecret"},
			enumerated: `{}`, hash: strings.Repeat("2", 64),
		},
		{
			name: "leveraged category", exchange: "bybit", host: "api-demo.bybit.com",
			method: "POST", path: "/v5/order/create", fields: []string{"category"},
			enumerated: `{"category":"linear"}`, hash: strings.Repeat("3", 64),
		},
		{
			name: "missing enumeration", exchange: "bybit", host: "api-demo.bybit.com",
			method: "POST", path: "/v5/order/create", fields: []string{"category"},
			enumerated: `{}`, hash: strings.Repeat("4", 64),
		},
	}
	for _, candidate := range unsafeRows {
		if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_authenticated_request_evidence(
  exchange,host,method,path,field_names,enumerated_fields,
  request_hash,configuration_id,recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,'sandbox_runtime-unsafe-evidence',$8)`,
			candidate.exchange,
			candidate.host,
			candidate.method,
			candidate.path,
			candidate.fields,
			candidate.enumerated,
			candidate.hash,
			at,
		); err == nil {
			t.Fatalf("unsafe authenticated evidence accepted: %s", candidate.name)
		}
	}
}

func assertSandboxRuntimeFailClosedConstraints(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_exchange_accounts(
 id,exchange,environment,native_account_hash,state,current_epoch,
 credential_generation,revision,created_at,updated_at
) VALUES ('production-account','binance','live',$1,'LOCKED',1,1,1,$2,$2)`,
		strings.Repeat("a", 64), now); err == nil {
		t.Fatal("production environment accepted")
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_daily_cap_counters(utc_day,reserved_notional,revision,updated_at)
VALUES ('2026-07-27',20,1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE sandbox_runtime_daily_cap_counters
SET reserved_notional=10,revision=2,updated_at=$1
WHERE utc_day='2026-07-27'`, now.Add(time.Minute)); err == nil {
		t.Fatal("daily reservation refund accepted")
	}
}

func assertSandboxRuntimeLeaseIsolation(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 27, 0, 30, 0, 0, time.UTC)
	account := sandbox.AccountID("sandbox_runtime-lease-isolation")
	seedSandboxRuntimeLeaseIsolationAccount(t, ctx, pool, account, now)
	store, err := NewSandboxRuntimeDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	assertSandboxRuntimeLeasePreCommitCrash(t, ctx, pool, store, account, now)
	assertSandboxRuntimeLeaseOwnershipAndFencing(t, ctx, store, account, now)
	assertSandboxRuntimeLeasePostCommitCrash(t, ctx, pool, store, account, now)
}

func seedSandboxRuntimeLeaseIsolationAccount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_exchange_accounts(
 id,exchange,environment,native_account_hash,state,current_epoch,
 credential_generation,revision,created_at,updated_at
) VALUES ($1,'binance','spot_testnet',$2,'LOCKED',1,1,1,$3,$3)`,
		account, strings.Repeat("9", 64), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_account_epochs(account_id,epoch,reason,opened_at)
	VALUES ($1,1,'initial',$2)`, account, now); err != nil {
		t.Fatal(err)
	}
}

func assertSandboxRuntimeLeasePreCommitCrash(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := store.AcquireAccountLease(
		ctx,
		account,
		sandbox.EnvironmentBinanceSpotTestnet,
		"owner-a",
		now,
		time.Minute,
		&sandboxRuntimePostgresCrashOnce{boundary: sandbox.KillBeforeLeaseTransition},
	); !errors.Is(err, sandbox.ErrInjectedCrash) {
		t.Fatalf("pre-lease crash=%v", err)
	}
	var leaseRows int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_account_leases WHERE account_id=$1`, account).
		Scan(&leaseRows); err != nil || leaseRows != 0 {
		t.Fatalf("pre-commit lease rows=%d error=%v", leaseRows, err)
	}
}

func assertSandboxRuntimeLeaseOwnershipAndFencing(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := store.AcquireAccountLease(
		ctx, account, sandbox.EnvironmentBybitDemo, "wrong-environment", now, time.Minute,
		sandbox.NoKillPoint{},
	); err == nil {
		t.Fatal("lease acquired with another exchange environment")
	}
	token, err := store.AcquireAccountLease(
		ctx, account, sandbox.EnvironmentBinanceSpotTestnet, "owner-a", now, time.Minute,
		sandbox.NoKillPoint{},
	)
	if err != nil || token != 1 {
		t.Fatalf("initial lease token=%d error=%v", token, err)
	}
	if _, err = store.AcquireAccountLease(
		ctx, account, sandbox.EnvironmentBinanceSpotTestnet, "owner-b",
		now.Add(30*time.Second), time.Minute, sandbox.NoKillPoint{},
	); err == nil {
		t.Fatal("overlapping lease owner accepted")
	}
	token, err = store.AcquireAccountLease(
		ctx, account, sandbox.EnvironmentBinanceSpotTestnet, "owner-b",
		now.Add(time.Minute), time.Minute, sandbox.NoKillPoint{},
	)
	if err != nil || token != 2 {
		t.Fatalf("fenced takeover token=%d error=%v", token, err)
	}
}

func assertSandboxRuntimeLeasePostCommitCrash(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := store.AcquireAccountLease(
		ctx,
		account,
		sandbox.EnvironmentBinanceSpotTestnet,
		"owner-b",
		now.Add(2*time.Minute),
		time.Minute,
		&sandboxRuntimePostgresCrashOnce{boundary: sandbox.KillAfterLeaseTransition},
	); !errors.Is(err, sandbox.ErrInjectedCrash) {
		t.Fatalf("post-lease crash=%v", err)
	}
	var persistedFence int64
	if err := pool.QueryRow(ctx, `
SELECT fencing_token FROM sandbox_runtime_account_leases WHERE account_id=$1`, account).
		Scan(&persistedFence); err != nil || persistedFence != 3 {
		t.Fatalf("post-commit lease fence=%d error=%v", persistedFence, err)
	}
	token, err := store.AcquireAccountLease(
		ctx,
		account,
		sandbox.EnvironmentBinanceSpotTestnet,
		"owner-b",
		now.Add(2*time.Minute+time.Second),
		time.Minute,
		sandbox.NoKillPoint{},
	)
	if err != nil || token != 4 {
		t.Fatalf("post-crash lease retry token=%d error=%v", token, err)
	}
}

func assertSandboxRuntimeAuditChainSerialization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	_, _, login := ownerConsoleQualificationAuthentication(t, ctx, pool, clock)
	store, err := NewSandboxRuntimeAuthenticationStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	const events = 8
	appendConcurrentSandboxRuntimeAudits(t, ctx, store, login, now, events)
	assertSandboxRuntimeAuditRows(t, ctx, pool, events)
}

func appendConcurrentSandboxRuntimeAudits(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeAuthenticationStore,
	login authentication.LoginResult,
	now time.Time,
	events int,
) {
	t.Helper()
	failures := make(chan error, events)
	var group sync.WaitGroup
	for index := 0; index < events; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			failures <- store.AppendHighRiskAudit(ctx, authentication.HighRiskAudit{
				ID:          fmt.Sprintf("sandbox_runtime-concurrent-audit-%02d", index),
				ActorUserID: login.Principal.UserID, SessionID: login.Principal.SessionID,
				Purpose: authentication.PurposeSandboxArm, Outcome: "qualification_concurrent",
				SourceHash: strings.Repeat("a", 64), ReasonHash: strings.Repeat("b", 64),
				Revision:   login.Principal.SessionRevision,
				OccurredAt: now.Add(time.Duration(events-index) * time.Second),
			})
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatalf("concurrent audit append failed: %v", err)
		}
	}
}

func assertSandboxRuntimeAuditRows(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	events int,
) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT previous_hash,event_hash
FROM sandbox_runtime_high_risk_audit_events
WHERE outcome='qualification_concurrent'
ORDER BY chain_sequence`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	previous := ""
	count := 0
	for rows.Next() {
		var prior *string
		var current string
		if err = rows.Scan(&prior, &current); err != nil {
			t.Fatal(err)
		}
		if valueOrEmpty(prior) != previous {
			t.Fatalf("audit chain fork at %d: prior=%q want=%q", count, valueOrEmpty(prior), previous)
		}
		previous = current
		count++
	}
	if err := rows.Err(); err != nil || count != events {
		t.Fatalf("audit chain count=%d error=%v", count, err)
	}
}

func assertSandboxRuntimeAuthorizationSessionBinding(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 28, 0, 5, 0, 0, time.UTC)
	const sessionID = "sandbox_runtime-session-binding-session"
	var userID string
	if err := pool.QueryRow(ctx, `
SELECT id FROM users WHERE status='active' ORDER BY id LIMIT 1`,
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	seedSandboxRuntimeAuthorizationSession(t, ctx, pool, userID, sessionID, now)
	store, err := NewSandboxRuntimeAuthenticationStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	assertSandboxRuntimeStaleSessionAuthorizationRejected(t, ctx, pool, store, userID, sessionID, now)
	assertSandboxRuntimeActiveSessionAuthorization(t, ctx, store, userID, sessionID, now)
	assertSandboxRuntimeRevokedSessionAuthorization(t, ctx, pool, store, userID, sessionID, now)
}

func seedSandboxRuntimeAuthorizationSession(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sessions(
 id,user_id,token_hash,created_at,expires_at,csrf_token_hash,last_seen_at,
 idle_expires_at,reauthenticated_at,revision
) VALUES ($2,$1,$4,$3,$5,$6,$3,$7,$3,1)`,
		userID,
		sessionID,
		now,
		strings.Repeat("7", 64),
		now.Add(authentication.AbsoluteLifetime),
		strings.Repeat("8", 64),
		now.Add(authentication.IdleLifetime),
	); err != nil {
		t.Fatal(err)
	}
}

func assertSandboxRuntimeStaleSessionAuthorizationRejected(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeAuthenticationStore,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	stale := sandboxRuntimeAuthorizationWrite(
		"sandbox_runtime-stale-revision-auth",
		strings.Repeat("9", 64),
		userID,
		sessionID,
		1,
		2,
		now,
	)
	if err := store.CreateSandboxAuthorization(ctx, stale); err == nil {
		t.Fatal("authorization issued from a stale session revision")
	}
	var replayRows int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_totp_replay_state WHERE user_id=$1`, userID).
		Scan(&replayRows); err != nil || replayRows != 0 {
		t.Fatalf("stale authorization advanced TOTP state rows=%d error=%v", replayRows, err)
	}
}

func assertSandboxRuntimeActiveSessionAuthorization(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeAuthenticationStore,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	first := sandboxRuntimeAuthorizationWrite(
		"sandbox_runtime-active-session-auth",
		strings.Repeat("a", 64),
		userID,
		sessionID,
		1,
		1,
		now,
	)
	if err := store.CreateSandboxAuthorization(ctx, first); err != nil {
		t.Fatalf("active session authorization failed: %v", err)
	}
	if _, err := store.ConsumeSandboxAuthorization(
		ctx,
		first.TokenHash,
		sessionID,
		first.Purpose,
		now.Add(time.Second),
	); err != nil {
		t.Fatalf("active session authorization consume failed: %v", err)
	}
}

func assertSandboxRuntimeRevokedSessionAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeAuthenticationStore,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	second := sandboxRuntimeAuthorizationWrite(
		"sandbox_runtime-revoked-session-auth",
		strings.Repeat("b", 64),
		userID,
		sessionID,
		2,
		1,
		now.Add(2*time.Second),
	)
	if err := store.CreateSandboxAuthorization(ctx, second); err != nil {
		t.Fatalf("second active session authorization failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE sessions
SET revoked_at=$2,revoked_reason='qualification_revoke',revision=revision+1
WHERE id=$1`, sessionID, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeSandboxAuthorization(
		ctx,
		second.TokenHash,
		sessionID,
		second.Purpose,
		now.Add(4*time.Second),
	); err == nil {
		t.Fatal("revoked session consumed a high-risk authorization")
	}
	var consumed bool
	if err := pool.QueryRow(ctx, `
SELECT consumed_at IS NOT NULL FROM sandbox_runtime_sandbox_authorizations WHERE id=$1`,
		second.ID,
	).Scan(&consumed); err != nil || consumed {
		t.Fatalf("revoked-session consume was not rolled back consumed=%t error=%v", consumed, err)
	}
	assertSandboxRuntimeDirectRevokedSessionAuthorizationRejected(
		t, ctx, pool, userID, sessionID, now.Add(4*time.Second),
	)
}

func assertSandboxRuntimeDirectRevokedSessionAuthorizationRejected(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_authorizations(
 id,token_hash,user_id,session_id,purpose,totp_counter,session_revision,
 source_hash,reason_hash,created_at,expires_at
) VALUES (
 'sandbox_runtime-direct-revoked-session-auth',$1,$2,$3,'sandbox_arm',3,2,$4,$5,$6,$7
)`,
		strings.Repeat("c", 64),
		userID,
		sessionID,
		strings.Repeat("d", 64),
		strings.Repeat("e", 64),
		now,
		now.Add(authentication.SandboxReauthorizationLifetime),
	); err == nil {
		t.Fatal("database accepted an authorization for a revoked session")
	}
}

func sandboxRuntimeAuthorizationWrite(
	id, tokenHash, userID, sessionID string,
	counter, revision int64,
	now time.Time,
) authentication.NewSandboxAuthorization {
	sourceHash := strings.Repeat("1", 64)
	reasonHash := strings.Repeat("2", 64)
	return authentication.NewSandboxAuthorization{
		ID: id, TokenHash: tokenHash, UserID: userID, SessionID: sessionID,
		Purpose: authentication.PurposeSandboxArm, TOTPCounter: counter,
		SessionRevision: revision, CreatedAt: now,
		ExpiresAt:  now.Add(authentication.SandboxReauthorizationLifetime),
		SourceHash: sourceHash, ReasonHash: reasonHash,
		Audit: authentication.HighRiskAudit{
			ID: id + "-audit", ActorUserID: userID, SessionID: sessionID,
			Purpose: authentication.PurposeSandboxArm, Outcome: "authorization_issued",
			SourceHash: sourceHash, ReasonHash: reasonHash, Revision: revision,
			OccurredAt: now,
		},
	}
}

type sandboxRuntimePostgresCrashOnce struct {
	boundary sandbox.KillBoundary
	hit      bool
}

func (point *sandboxRuntimePostgresCrashOnce) Hit(
	_ context.Context,
	boundary sandbox.KillBoundary,
) error {
	if boundary == point.boundary && !point.hit {
		point.hit = true
		return sandbox.ErrInjectedCrash
	}
	return nil
}

func assertSandboxRuntimeDispatcherCrashRecovery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	fixture := newSandboxRuntimeDispatcherFixture(t, ctx, pool)
	assertSandboxRuntimePlanAccountLockUsesRuntimeRole(t, ctx, pool, fixture)
	store, outboxID := approveSandboxRuntimeDispatcherFixture(t, ctx, pool, fixture)
	acknowledgement := sandboxRuntimeAcknowledgementEvent(fixture)
	assertSandboxRuntimeInboxCrashRecovery(t, ctx, pool, store, outboxID, acknowledgement)
	assertSandboxRuntimeCancelPendingBeforeFill(t, ctx, pool, store, fixture, outboxID)
	fill := sandboxRuntimeFillEvent(fixture)
	assertSandboxRuntimeFillCrashRecovery(t, ctx, pool, store, outboxID, fill)
	assertSandboxRuntimeRecoveredState(t, ctx, pool, outboxID)
	assertSandboxRuntimeUnsentAttemptRecovery(t, ctx, pool, store, fixture)
}

func assertSandboxRuntimePlanAccountLockUsesRuntimeRole(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture sandboxRuntimeDispatcherFixture,
) {
	t.Helper()
	runtimeRole := testRole("AXIOM_SANDBOX_RUNTIME_RUNTIME_ROLE", "axiom_app")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	role := pgx.Identifier{runtimeRole}.Sanitize()
	for _, statement := range []string{
		"GRANT SELECT, UPDATE ON sandbox_runtime_exchange_accounts TO " + role,
		"GRANT SELECT ON sandbox_runtime_sandbox_session_accounts TO " + role,
		"REVOKE UPDATE ON sandbox_runtime_sandbox_session_accounts FROM " + role,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(
		ctx,
		"SET LOCAL ROLE "+role,
	); err != nil {
		t.Fatal(err)
	}
	var exchange, state string
	var epoch int64
	if err = tx.QueryRow(
		ctx,
		validateSandboxRuntimePlanAccountSQL,
		fixture.accountID,
		fixture.plan.SessionID,
		fixture.submission.AccountEpoch,
	).Scan(&exchange, &epoch, &state); err != nil {
		t.Fatalf("runtime plan-account lock failed: %v", err)
	}
	if exchange != "binance" || epoch != 1 || state != "ARMED" {
		t.Fatalf("runtime plan-account facts=%s/%d/%s", exchange, epoch, state)
	}
}

func assertSandboxRuntimeCancelPendingBeforeFill(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	fixture sandboxRuntimeDispatcherFixture,
	outboxID string,
) {
	t.Helper()
	cancelID, err := store.MarkCancelPending(
		ctx,
		fixture.accountID,
		1,
		fixture.submission.ClientOrderID,
		"sandbox_runtime-worker",
		1,
		fixture.now.Add(500*time.Millisecond),
		sandbox.NoKillPoint{},
	)
	if err != nil || cancelID != outboxID {
		t.Fatalf("cancel pending id=%q want=%q error=%v", cancelID, outboxID, err)
	}
	var orderState, reservationState string
	if err = pool.QueryRow(ctx, `
SELECT order_state FROM sandbox_runtime_submission_outbox WHERE id=$1`, outboxID).
		Scan(&orderState); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `
SELECT state FROM sandbox_runtime_sandbox_reservations WHERE order_id=$1`,
		fixture.orderID.String(),
	).Scan(&reservationState); err != nil {
		t.Fatal(err)
	}
	if orderState != "CANCEL_PENDING" || reservationState != "ACTIVE" {
		t.Fatalf("cancel pending order=%s reservation=%s", orderState, reservationState)
	}
}

type sandboxRuntimeDispatcherFixture struct {
	now        time.Time
	accountID  sandbox.AccountID
	orderID    domain.VirtualOrderID
	quantity   domain.Quantity
	price      domain.Price
	submission sandbox.Submission
	plan       sandbox.ApprovedSandboxPlan
}

func assertSandboxRuntimeMultilegPersistence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	fixture := newSandboxRuntimeMultilegFixture(t, ctx, pool)
	store, err := NewSandboxRuntimeDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	limits := sandbox.SubmissionLimits{MaximumOrderNotional: "10",
		MaximumDailyNotional: "50", MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2}
	if err = store.ApprovePlan(ctx, fixture.plan, limits, sandbox.NoKillPoint{}); err != nil {
		t.Fatalf("triangular plan approval failed: %v", err)
	}
	assertSandboxRuntimeMultilegRows(t, ctx, pool, fixture.plan.ID,
		[]string{"PENDING", "WAITING", "WAITING"})
	assertSandboxRuntimeMultilegReservationRows(t, ctx, pool, fixture.plan.ID,
		[]string{"ACTIVE", "WAITING", "WAITING"})
	runSandboxRuntimeMultilegPlan(t, ctx, pool, store, fixture.plan)
	assertSandboxRuntimeMultilegFinalState(t, ctx, pool, fixture.plan.ID)
	assertSandboxRuntimeMultilegAccountingProjection(t, ctx, pool, fixture.plan)
	assertSandboxRuntimeMultilegCandidateExpiry(t, ctx, pool, store, fixture.plan, limits)
}

func runSandboxRuntimeMultilegPlan(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore, plan sandbox.ApprovedSandboxPlan,
) {
	t.Helper()
	var err error
	for index, submission := range plan.Submissions {
		legAt := plan.ApprovedAt.Add(time.Duration(index) * 60 * time.Millisecond)
		claimed, claimErr := store.ClaimOutbox(ctx, submission.AccountID,
			submission.AccountEpoch, "sandbox_runtime-worker", 1, legAt, time.Minute, 3,
			sandbox.NoKillPoint{})
		if claimErr != nil || len(claimed) != 1 || claimed[0].LegIndex != uint32(index) {
			t.Fatalf("triangular leg %d claim=%#v error=%v", index, claimed, claimErr)
		}
		if err = store.MarkSubmitting(ctx, claimed[0].ID, 1, legAt, sandbox.NoKillPoint{}); err != nil {
			t.Fatalf("triangular leg %d submitting failed: %v", index, err)
		}
		ack, fill := sandboxRuntimeMultilegEvents(t, submission, index, legAt)
		if err = store.AppendPrivateEvent(ctx, claimed[0].ID, 1, ack, sandbox.NoKillPoint{}); err != nil {
			t.Fatalf("triangular leg %d acknowledgement failed: %v", index, err)
		}
		if err = store.AppendPrivateEvent(ctx, claimed[0].ID, 1, fill, sandbox.NoKillPoint{}); err != nil {
			t.Fatalf("triangular leg %d fill failed: %v", index, err)
		}
		var active int
		if err = pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_submission_outbox
WHERE plan_id=$1 AND state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`,
			plan.ID).Scan(&active); err != nil || active > 1 {
			t.Fatalf("triangular leg %d active=%d error=%v", index, active, err)
		}
		if index+1 < len(plan.Submissions) {
			states := []string{"TERMINAL", "WAITING", "WAITING"}
			states[index+1] = "PENDING"
			for completed := 1; completed <= index; completed++ {
				states[completed] = "TERMINAL"
			}
			assertSandboxRuntimeMultilegRows(t, ctx, pool, plan.ID, states)
			reservationStates := []string{"WAITING", "WAITING", "WAITING"}
			for completed := 0; completed <= index; completed++ {
				reservationStates[completed] = "CONSUMED"
			}
			reservationStates[index+1] = "ACTIVE"
			assertSandboxRuntimeMultilegReservationRows(
				t, ctx, pool, plan.ID, reservationStates,
			)
		}
	}
	assertSandboxRuntimeMultilegRows(t, ctx, pool, plan.ID,
		[]string{"TERMINAL", "TERMINAL", "TERMINAL"})
}

func assertSandboxRuntimeMultilegFinalState(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	planID string,
) {
	t.Helper()
	var err error
	var planState, disposition string
	var eligibility, consumed int
	if err = pool.QueryRow(ctx, `
SELECT state,coalesce(final_disposition,'')
FROM sandbox_runtime_submission_plans WHERE id=$1`, planID).Scan(
		&planState, &disposition); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_runtime_plan_eligibility WHERE plan_id=$1`,
		planID).Scan(&eligibility); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_sandbox_reservations
WHERE plan_id=$1 AND state='CONSUMED'`, planID).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if planState != "COMPLETED" || disposition != "all_legs_filled" ||
		eligibility != 3 || consumed != 3 {
		t.Fatalf("triangular durable result=%s/%s eligibility=%d consumed=%d",
			planState, disposition, eligibility, consumed)
	}
}

func assertSandboxRuntimeMultilegAccountingProjection(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	plan sandbox.ApprovedSandboxPlan,
) {
	t.Helper()
	var transactions, sourceCount int
	var instrument, quantity, totalCost, realized, state string
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_accounting_transactions WHERE plan_id=$1`, plan.ID,
	).Scan(&transactions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT instrument,quantity::text,total_cost::text,realized_pnl::text,
       valuation_state,source_transaction_count
FROM sandbox_accounting_positions
WHERE strategy_session_id=$1 AND account_id=$2 AND account_epoch=$3`,
		plan.SessionID, plan.Submissions[0].AccountID, plan.Submissions[0].AccountEpoch,
	).Scan(&instrument, &quantity, &totalCost, &realized, &state, &sourceCount); err != nil {
		t.Fatal(err)
	}
	parsedQuantity, quantityErr := domain.ParseBalance(quantity)
	parsedCost, costErr := domain.ParseMoney(totalCost)
	parsedRealized, realizedErr := domain.ParsePnL(realized)
	zeroBalance, _ := domain.ParseBalance("0")
	zeroMoney, _ := domain.ParseMoney("0")
	wantRealized, _ := domain.ParsePnL("-0.000002")
	if transactions != 3 || sourceCount != 3 || instrument != "BTCUSDT" ||
		quantityErr != nil || costErr != nil || realizedErr != nil ||
		parsedQuantity.Compare(zeroBalance) != 0 || parsedCost.Compare(zeroMoney) != 0 ||
		parsedRealized.Compare(wantRealized) != 0 ||
		state != sandboxAccountingValuationComplete {
		t.Fatalf("triangular accounting tx=%d source=%d instrument=%s quantity=%s cost=%s pnl=%s state=%s",
			transactions, sourceCount, instrument, quantity, totalCost, realized, state)
	}
}

func assertSandboxRuntimeMultilegCandidateExpiry(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	base sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
) {
	t.Helper()
	plan := reidentifySandboxRuntimeMultilegPlan(t, base, "expiry")
	if err := store.ApprovePlan(ctx, plan, limits, sandbox.NoKillPoint{}); err != nil {
		t.Fatalf("expiring triangular plan approval failed: %v", err)
	}
	claimed, err := store.ClaimOutbox(ctx, plan.Submissions[0].AccountID,
		plan.Submissions[0].AccountEpoch, "sandbox_runtime-worker", 1,
		plan.ApprovedAt, time.Minute, 1, sandbox.NoKillPoint{})
	if err != nil || len(claimed) != 1 || claimed[0].LegIndex != 0 {
		t.Fatalf("expiring triangular claim=%#v error=%v", claimed, err)
	}
	if err = store.MarkSubmitting(ctx, claimed[0].ID, 1, plan.ApprovedAt,
		sandbox.NoKillPoint{}); err != nil {
		t.Fatalf("expiring triangular submitting failed: %v", err)
	}
	ack, _ := sandboxRuntimeMultilegEvents(t, plan.Submissions[0], 0, plan.ApprovedAt)
	reidentifySandboxRuntimeMultilegEvent(t, &ack, "expiry-ack")
	if err = store.AppendPrivateEvent(ctx, claimed[0].ID, 1, ack,
		sandbox.NoKillPoint{}); err != nil {
		t.Fatalf("expiring triangular acknowledgement failed: %v", err)
	}
	_, fill := sandboxRuntimeMultilegEvents(
		t, plan.Submissions[0], 0, plan.ExecutionExpiresAt.Add(-10*time.Millisecond),
	)
	reidentifySandboxRuntimeMultilegEvent(t, &fill, "expiry-fill")
	if err = store.AppendPrivateEvent(ctx, claimed[0].ID, 1, fill,
		sandbox.NoKillPoint{}); err != nil {
		t.Fatalf("expiring triangular fill failed: %v", err)
	}
	assertSandboxRuntimeMultilegRows(t, ctx, pool, plan.ID,
		[]string{"TERMINAL", "WAITING", "WAITING"})
	assertSandboxRuntimeMultilegReservationRows(t, ctx, pool, plan.ID,
		[]string{"CONSUMED", "QUARANTINED", "QUARANTINED"})
	assertSandboxRuntimeMultilegRecoveryRequired(t, ctx, pool, plan.ID)
	assertSandboxRuntimeMultilegInitialOwnership(t, ctx, pool, store, base, limits)
}

func assertSandboxRuntimeMultilegRecoveryRequired(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	planID string,
) {
	t.Helper()
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM sandbox_runtime_submission_plans WHERE id=$1`, planID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "RECOVERY_REQUIRED" {
		t.Fatalf("expired triangular candidate plan=%s", state)
	}
}

func assertSandboxRuntimeMultilegInitialOwnership(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	base sandbox.ApprovedSandboxPlan,
	limits sandbox.SubmissionLimits,
) {
	t.Helper()
	plan := reidentifySandboxRuntimeMultilegPlan(t, base, "unfunded")
	available, _ := domain.ParseBalance("0.5")
	zero, _ := domain.ParseBalance("0")
	snapshot := sandbox.AccountSnapshot{AccountID: plan.Submissions[0].AccountID,
		Epoch:        plan.Submissions[0].AccountEpoch,
		Balances:     []sandbox.Balance{{Asset: "USDT", Available: available, Reserved: zero}},
		OrdersHash:   stableSandboxRuntimeHash("triangular-unfunded-orders"),
		FillsHash:    stableSandboxRuntimeHash("triangular-unfunded-fills"),
		SnapshotHash: stableSandboxRuntimeHash("triangular-unfunded-snapshot"),
		ObservedAt:   plan.ApprovedAt}
	if err := store.RecordAccountSnapshot(ctx, "sandbox_runtime-triangular-unfunded-snapshot", snapshot); err != nil {
		t.Fatal(err)
	}
	plan.AccountSnapshots = map[sandbox.AccountID]sandbox.AccountSnapshotReference{
		snapshot.AccountID: {AccountID: snapshot.AccountID, AccountEpoch: snapshot.Epoch,
			SnapshotHash: snapshot.SnapshotHash, ObservedAt: snapshot.ObservedAt},
	}
	plan.ApprovalHash = plan.Pipeline.HashFor(plan)
	if err := store.ApprovePlan(ctx, plan, limits, sandbox.NoKillPoint{}); err == nil {
		t.Fatal("triangular first reservation exceeded its authoritative available balance")
	}
	var persisted bool
	if err := pool.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM sandbox_runtime_submission_plans WHERE id=$1)`, plan.ID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted {
		t.Fatal("unfunded triangular plan partially committed")
	}
}

func reidentifySandboxRuntimeMultilegPlan(
	t *testing.T,
	base sandbox.ApprovedSandboxPlan,
	suffix string,
) sandbox.ApprovedSandboxPlan {
	t.Helper()
	plan := base
	planID, err := domain.NewExecutionPlanID("sandbox_runtime-triangular-" + suffix + "-plan")
	if err != nil {
		t.Fatal(err)
	}
	plan.ID = planID.String()
	if base.StrategyDecision != nil {
		plan.StrategyDecision = reidentifiedSandboxRuntimeDecisionEvidence(base.StrategyDecision, suffix)
	}
	plan.Submissions, plan.Reservations = reidentifiedSandboxRuntimeMultilegOrders(t, base, planID, suffix)
	plan.ApprovalHash = plan.Pipeline.HashFor(plan)
	return plan
}

func reidentifiedSandboxRuntimeDecisionEvidence(base *sandbox.StrategyDecisionEvidence,
	suffix string,
) *sandbox.StrategyDecisionEvidence {
	ordinal := uint64(2)
	if suffix == "unfunded" {
		ordinal = 3
	}
	input := json.RawMessage(fmt.Sprintf(`{"market":"triangular","scenario":%q}`, suffix))
	decisionID := "decision:sandbox_runtime-triangular:" + suffix
	decision := json.RawMessage(fmt.Sprintf(
		`{"id":%q,"ordinal":%d,"action":"entry","candidate":{}}`, decisionID, ordinal))
	inputHash := sha256.Sum256(input)
	decisionHash := sha256.Sum256(decision)
	evidence := *base
	evidence.DecisionID, evidence.EventOrdinal, evidence.EventLogicalTime = decisionID, ordinal, ordinal
	evidence.CanonicalInput, evidence.CanonicalDecision = input, decision
	evidence.InputHash, evidence.DecisionHash = fmt.Sprintf("%x", inputHash), fmt.Sprintf("%x", decisionHash)
	return &evidence
}

func reidentifiedSandboxRuntimeMultilegOrders(t *testing.T, base sandbox.ApprovedSandboxPlan,
	planID domain.ExecutionPlanID, suffix string,
) ([]sandbox.Submission, []sandbox.DurableReservation) {
	t.Helper()
	submissions := append([]sandbox.Submission(nil), base.Submissions...)
	reservations := append([]sandbox.DurableReservation(nil), base.Reservations...)
	for index := range submissions {
		orderID, orderErr := domain.NewVirtualOrderID(
			fmt.Sprintf("sandbox_runtime-triangular-%s-order-%d", suffix, index),
		)
		if orderErr != nil {
			t.Fatal(orderErr)
		}
		submissions[index].PlanID = planID
		submissions[index].OrderID = orderID
		submissions[index].ClientOrderID = fmt.Sprintf("ax-sandbox-runtime-triangular-%s-%d", suffix, index)
		submissions[index].RequestHash = stableSandboxRuntimeHash(
			"triangular-request", suffix, fmt.Sprint(index),
		)
		reservations[index].ID = fmt.Sprintf(
			"sandbox_runtime-triangular-%s-reservation-%d", suffix, index,
		)
		reservations[index].OrderID = orderID.String()
	}
	return submissions, reservations
}

func reidentifySandboxRuntimeMultilegEvent(
	t *testing.T,
	event *sandbox.PrivateEvent,
	suffix string,
) {
	t.Helper()
	event.Identity = "sandbox_runtime-triangular-private-" + suffix
	event.NativeOrderHash = stableSandboxRuntimeHash("triangular-native-order", suffix)
	event.NativeFillHash = ""
	event.OrderEvent.ID = "sandbox_runtime-triangular-order-event-" + suffix
	if event.Kind == sandbox.PrivateFillEvent {
		event.NativeFillHash = stableSandboxRuntimeHash("triangular-native-fill", suffix)
		fillID, err := domain.NewVirtualFillID("sandbox_runtime-triangular-" + suffix)
		if err != nil {
			t.Fatal(err)
		}
		event.OrderEvent.Fills[0].ID = fillID
	}
}

type sandboxRuntimeMultilegFixture struct {
	plan sandbox.ApprovedSandboxPlan
}

func newSandboxRuntimeMultilegFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) sandboxRuntimeMultilegFixture {
	t.Helper()
	base, snapshot, approvedAt := newSandboxRuntimeMultilegBase(t, ctx, pool)
	planID, submissions, reservations := newSandboxRuntimeMultilegOrders(t, base.accountID, approvedAt)
	plan := newSandboxRuntimeMultilegApprovedPlan(base, snapshot, approvedAt, planID, submissions, reservations)
	return sandboxRuntimeMultilegFixture{plan: plan}
}

func newSandboxRuntimeMultilegBase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (
	sandboxRuntimeDispatcherFixture, sandbox.AccountSnapshot, time.Time,
) {
	t.Helper()
	baseNow := time.Date(2026, 7, 28, 0, 10, 0, 0, time.UTC)
	userID, actorSessionID := sandboxQualificationOwner(t, ctx, pool)
	base := sandboxRuntimeDispatcherFixture{now: baseNow,
		accountID: sandbox.AccountID("binance-testnet-qualification")}
	base.plan = sandbox.ApprovedSandboxPlan{SessionID: "sandbox_runtime-session-qualification",
		Arm: sandbox.Arm{ID: "sandbox_runtime-arm-qualification", SessionID: "sandbox_runtime-session-qualification",
			AccountIDs:        []sandbox.AccountID{base.accountID},
			AuthorizationHash: strings.Repeat("5", 64), ActorUserID: userID,
			ActorSessionID: actorSessionID, ReasonHash: strings.Repeat("2", 64),
			CreatedAt: baseNow, ExpiresAt: baseNow.Add(sandbox.ArmLifetime), Revision: 1},
		ConfigurationID: "sandbox_runtime-config-qualification"}
	approvedAt := base.now.Add(5 * time.Minute)
	if _, err := pool.Exec(ctx, `INSERT INTO assets(symbol) VALUES ('ETH') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	availableBTC, _ := domain.ParseBalance("1")
	availableETH, _ := domain.ParseBalance("1")
	availableUSDT, _ := domain.ParseBalance("50")
	zero, _ := domain.ParseBalance("0")
	snapshot := sandbox.AccountSnapshot{AccountID: base.accountID, Epoch: 1,
		Balances: []sandbox.Balance{
			{Asset: "BTC", Available: availableBTC, Reserved: zero},
			{Asset: "ETH", Available: availableETH, Reserved: zero},
			{Asset: "USDT", Available: availableUSDT, Reserved: zero},
		}, OrdersHash: stableSandboxRuntimeHash("triangular-orders"),
		FillsHash:    stableSandboxRuntimeHash("triangular-fills"),
		SnapshotHash: stableSandboxRuntimeHash("triangular-snapshot"), ObservedAt: approvedAt}
	store, err := NewSandboxRuntimeDispatcherStore(pool)
	if err != nil || store.RecordAccountSnapshot(ctx, "sandbox_runtime-triangular-snapshot", snapshot) != nil {
		t.Fatalf("triangular snapshot failed: %v", err)
	}
	seedSandboxRuntimeMultilegStrategySession(t, ctx, pool, string(base.plan.SessionID), base.accountID, userID, baseNow)
	return base, snapshot, approvedAt
}

func newSandboxRuntimeMultilegOrders(t *testing.T, accountID sandbox.AccountID, approvedAt time.Time) (
	domain.ExecutionPlanID, []sandbox.Submission, []sandbox.DurableReservation,
) {
	t.Helper()
	planID, _ := domain.NewExecutionPlanID("sandbox_runtime-triangular-plan")
	strategyID, _ := domain.NewStrategyID(sandbox.StrategyTriangular)
	instruments := []domain.Instrument{
		mustSandboxRuntimeInstrument(t, "BTC", "USDT"),
		mustSandboxRuntimeInstrument(t, "ETH", "BTC"),
		mustSandboxRuntimeInstrument(t, "ETH", "USDT"),
	}
	sides := []domain.Side{domain.SideBuy, domain.SideBuy, domain.SideSell}
	quantities := []domain.Quantity{
		mustSandboxRuntimeQuantity(t, "0.0001"),
		mustSandboxRuntimeQuantity(t, "0.01"),
		mustSandboxRuntimeQuantity(t, "0.01"),
	}
	prices := []domain.Price{
		mustSandboxRuntimePrice(t, "10000"),
		mustSandboxRuntimePrice(t, "0.01"),
		mustSandboxRuntimePrice(t, "100"),
	}
	submissions := make([]sandbox.Submission, 0, 3)
	reservations := make([]sandbox.DurableReservation, 0, 3)
	for index := range instruments {
		orderID, _ := domain.NewVirtualOrderID(fmt.Sprintf("sandbox_runtime-triangular-order-%d", index))
		notional, calcErr := domain.CalculateNotional(prices[index], quantities[index], 18)
		if calcErr != nil {
			t.Fatal(calcErr)
		}
		submission := sandbox.Submission{PlanID: planID, OrderID: orderID,
			AccountID: accountID, AccountEpoch: 1,
			ClientOrderID: fmt.Sprintf("ax-sandbox-runtime-triangular-%d", index), StrategyID: strategyID,
			Instrument: instruments[index], Side: sides[index], Quantity: quantities[index],
			LimitPrice: prices[index], Notional: notional, Style: sandbox.OrderStyleLimitIOC,
			Action: sandbox.IntentEntry, RequestHash: stableSandboxRuntimeHash("triangular-request", fmt.Sprint(index)),
			PolicyHash: stableSandboxRuntimeHash("triangular-policy"), ApprovedAt: approvedAt}
		asset, amount := string(submission.Instrument.Quote), submission.Notional.String()
		if submission.Side == domain.SideSell {
			asset, amount = string(submission.Instrument.Base), submission.Quantity.String()
		}
		submissions = append(submissions, submission)
		reservations = append(reservations, sandbox.DurableReservation{
			ID:        fmt.Sprintf("sandbox_runtime-triangular-reservation-%d", index),
			AccountID: accountID, AccountEpoch: 1, OrderID: orderID.String(),
			Asset: asset, Quantity: amount,
		})
	}
	return planID, submissions, reservations
}

func newSandboxRuntimeMultilegApprovedPlan(base sandboxRuntimeDispatcherFixture, snapshot sandbox.AccountSnapshot,
	approvedAt time.Time, planID domain.ExecutionPlanID, submissions []sandbox.Submission,
	reservations []sandbox.DurableReservation,
) sandbox.ApprovedSandboxPlan {
	pipeline := sandbox.ApprovalPipelineEvidence{IntentKind: sandbox.ApprovalStrategyIntent,
		IntentHash:        stableSandboxRuntimeHash("triangular-intent"),
		AllocatorHash:     stableSandboxRuntimeHash("triangular-allocator"),
		RiskHash:          stableSandboxRuntimeHash("triangular-risk"),
		PlannerHash:       stableSandboxRuntimeHash("triangular-planner"),
		AssetApprovalHash: stableSandboxRuntimeHash("triangular-assets"),
		RiskApproved:      true, AssetApproved: true, ObservedAt: approvedAt}
	canonicalInput := json.RawMessage(`{"market":"triangular","coherent":true}`)
	canonicalDecision := json.RawMessage(`{"id":"decision:sandbox_runtime-triangular","ordinal":1,"action":"entry","candidate":{}}`)
	inputHash := sha256.Sum256(canonicalInput)
	decisionHash := sha256.Sum256(canonicalDecision)
	decisionEvidence := sandbox.StrategyDecisionEvidence{
		SessionID: base.plan.SessionID, AccountID: base.accountID, AccountEpoch: 1,
		StrategyRevision: 1, Strategy: sandbox.StrategyTriangular, Instrument: "BTCUSDT",
		DecisionID: "decision:sandbox_runtime-triangular", EventOrdinal: 1, EventLogicalTime: 1,
		CanonicalInput: canonicalInput, CanonicalDecision: canonicalDecision,
		InputHash: fmt.Sprintf("%x", inputHash), DecisionHash: fmt.Sprintf("%x", decisionHash),
	}
	expiresAt := approvedAt.Add(250 * time.Millisecond)
	plan := sandbox.ApprovedSandboxPlan{ID: planID.String(),
		SessionID: base.plan.SessionID, Submissions: submissions, Reservations: reservations,
		Arm: base.plan.Arm, MarketEligibility: []sandbox.EligibilitySnapshot{
			sandboxRuntimeMultilegEligibility(approvedAt, "BTCUSDT"),
			sandboxRuntimeMultilegEligibility(approvedAt, "ETHBTC"),
			sandboxRuntimeMultilegEligibility(approvedAt, "ETHUSDT"),
		}, EntrySafety: sandboxQualificationEntrySafety(submissions[0], approvedAt),
		AccountSnapshots: map[sandbox.AccountID]sandbox.AccountSnapshotReference{
			base.accountID: {AccountID: base.accountID, AccountEpoch: 1,
				SnapshotHash: snapshot.SnapshotHash, ObservedAt: approvedAt},
		}, StrategyDecision: &decisionEvidence, Pipeline: pipeline, ApprovedAt: approvedAt, ExecutionExpiresAt: &expiresAt,
		ConfigurationID: base.plan.ConfigurationID}
	plan.ApprovalHash = pipeline.HashFor(plan)
	return plan
}

func seedSandboxRuntimeMultilegStrategySession(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	sessionID string,
	accountID sandbox.AccountID,
	ownerID string,
	createdAt time.Time,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `
INSERT INTO sandbox_strategy_sessions(
 id,sandbox_session_id,strategy_id,instrument,state,created_by,created_at,revision
) VALUES ($1,$1,'triangular','BTCUSDT','prepared',$2,$3,1)`, sessionID, ownerID, createdAt); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `
INSERT INTO sandbox_strategy_session_accounts(
 strategy_session_id,account_id,account_epoch,exchange
) VALUES ($1,$2,1,'binance')`, sessionID, accountID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func sandboxRuntimeMultilegEligibility(at time.Time, instrument string) sandbox.EligibilitySnapshot {
	return sandbox.EligibilitySnapshot{ObservedAt: at, Exchange: "binance", Instrument: instrument,
		BookHealthy: true, BookFresh: true, BookEligible: true, ClockEligible: true, Eligible: true}
}

func mustSandboxRuntimeInstrument(t *testing.T, base, quote string) domain.Instrument {
	t.Helper()
	instrument, err := domain.NewSpotInstrument(domain.AssetSymbol(base), domain.AssetSymbol(quote))
	if err != nil {
		t.Fatal(err)
	}
	return instrument
}

func mustSandboxRuntimeQuantity(t *testing.T, value string) domain.Quantity {
	t.Helper()
	quantity, err := domain.ParseQuantity(value)
	if err != nil {
		t.Fatal(err)
	}
	return quantity
}

func mustSandboxRuntimePrice(t *testing.T, value string) domain.Price {
	t.Helper()
	price, err := domain.ParsePrice(value)
	if err != nil {
		t.Fatal(err)
	}
	return price
}

func sandboxRuntimeMultilegEvents(
	t *testing.T,
	submission sandbox.Submission,
	index int,
	at time.Time,
) (sandbox.PrivateEvent, sandbox.PrivateEvent) {
	t.Helper()
	zeroQuantity, _ := domain.ParseQuantity("0")
	ackFact := execution.OrderEvent{ID: fmt.Sprintf("sandbox_runtime-triangular-ack-%d", index),
		OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		State: execution.OrderAcknowledged, ExchangeStatus: "NEW",
		CumulativeQuantity: zeroQuantity, OccurredAt: at, Ordinal: 6}
	ack := sandbox.PrivateEvent{Identity: fmt.Sprintf("sandbox_runtime-triangular-private-ack-%d", index),
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: sandbox.PrivateOrderEvent, OrderID: submission.OrderID,
		ClientOrderID:   submission.ClientOrderID,
		NativeOrderHash: stableSandboxRuntimeHash("triangular-native-order", fmt.Sprint(index)),
		OrderEvent:      &ackFact, OccurredAt: at, ReceivedAt: at}
	fillID, err := domain.NewVirtualFillID(fmt.Sprintf("sandbox_runtime-triangular-fill-%d", index))
	if err != nil {
		t.Fatal(err)
	}
	fee, _ := domain.ParseFee("0.000001")
	if index == 1 {
		// The middle conversion spends BTC principal. Keep this fixture fee at
		// zero so predecessor output exactly funds it; quote-fee shortfalls have
		// a separate fail-closed accounting test.
		fee, _ = domain.ParseFee("0")
	}
	fillFact := execution.FillFact{ID: fillID, Quantity: submission.Quantity,
		Price: submission.LimitPrice, Fee: fee, FeeAsset: submission.Instrument.Quote,
		Ordinal: 7}
	filledAt := at.Add(10 * time.Millisecond)
	filledFact := execution.OrderEvent{ID: fmt.Sprintf("sandbox_runtime-triangular-filled-%d", index),
		OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		State: execution.OrderFilled, ExchangeStatus: "FILLED",
		CumulativeQuantity: submission.Quantity,
		Fees:               []execution.FeeFact{{Asset: submission.Instrument.Quote, Total: fee}},
		Fills:              []execution.FillFact{fillFact}, OccurredAt: filledAt, Ordinal: 7}
	fill := sandbox.PrivateEvent{Identity: fmt.Sprintf("sandbox_runtime-triangular-private-fill-%d", index),
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: sandbox.PrivateFillEvent, OrderID: submission.OrderID,
		ClientOrderID:   submission.ClientOrderID,
		NativeOrderHash: ack.NativeOrderHash,
		NativeFillHash:  stableSandboxRuntimeHash("triangular-native-fill", fmt.Sprint(index)),
		OrderEvent:      &filledFact, OccurredAt: filledAt, ReceivedAt: filledAt}
	return ack, fill
}

func assertSandboxRuntimeMultilegRows(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	planID string,
	want []string,
) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT leg_index,depends_on_leg_index,state
FROM sandbox_runtime_submission_outbox WHERE plan_id=$1 ORDER BY leg_index`, planID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var leg int
		var dependency *int
		var state string
		if err = rows.Scan(&leg, &dependency, &state); err != nil {
			t.Fatal(err)
		}
		if index >= len(want) || leg != index || state != want[index] ||
			(index == 0 && dependency != nil) ||
			(index > 0 && (dependency == nil || *dependency != index-1)) {
			t.Fatalf("multileg row index=%d leg=%d dependency=%v state=%s want=%v",
				index, leg, dependency, state, want)
		}
		index++
	}
	if rows.Err() != nil || index != len(want) {
		t.Fatalf("multileg row count=%d want=%d error=%v", index, len(want), rows.Err())
	}
}

func assertSandboxRuntimeMultilegReservationRows(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	planID string,
	want []string,
) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT outbox.leg_index,reservation.state
FROM sandbox_runtime_submission_outbox outbox
JOIN sandbox_runtime_sandbox_reservations reservation
  ON reservation.plan_id=outbox.plan_id AND reservation.order_id=outbox.order_id
WHERE outbox.plan_id=$1 ORDER BY outbox.leg_index`, planID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var leg int
		var state string
		if err = rows.Scan(&leg, &state); err != nil {
			t.Fatal(err)
		}
		if index >= len(want) || leg != index || state != want[index] {
			t.Fatalf("multileg reservation index=%d leg=%d state=%s want=%v",
				index, leg, state, want)
		}
		index++
	}
	if rows.Err() != nil || index != len(want) {
		t.Fatalf("multileg reservation count=%d want=%d error=%v",
			index, len(want), rows.Err())
	}
}

func newSandboxRuntimeDispatcherFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) sandboxRuntimeDispatcherFixture {
	t.Helper()
	now := time.Date(2026, 7, 28, 0, 10, 0, 0, time.UTC)
	userID, sessionID := sandboxQualificationOwner(t, ctx, pool)
	accountID := sandbox.AccountID("binance-testnet-qualification")
	seedSandboxRuntimeQualificationAccount(t, ctx, pool, accountID, now)
	seedSandboxRuntimeQualificationSession(t, ctx, pool, accountID, userID, sessionID, now)
	store, err := NewSandboxRuntimeDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	available, err := domain.ParseBalance("50")
	if err != nil {
		t.Fatal(err)
	}
	zero, err := domain.ParseBalance("0")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := sandbox.AccountSnapshot{AccountID: accountID, Epoch: 1,
		Balances:   []sandbox.Balance{{Asset: "USDT", Available: available, Reserved: zero}},
		OrdersHash: strings.Repeat("6", 64), FillsHash: strings.Repeat("7", 64),
		SnapshotHash: strings.Repeat("8", 64), ObservedAt: now}
	if err = store.RecordAccountSnapshot(ctx, "sandbox_runtime-strategy-plan-snapshot", snapshot); err != nil {
		t.Fatal(err)
	}
	submission, quantity, price := sandboxQualificationSubmission(accountID, now)
	return sandboxRuntimeDispatcherFixture{
		now: now, accountID: accountID, orderID: submission.OrderID,
		quantity: quantity, price: price, submission: submission,
		plan: sandboxQualificationPlan(submission, userID, sessionID, now, snapshot),
	}
}

func sandboxQualificationOwner(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) (string, string) {
	t.Helper()
	var userID, sessionID string
	if err := pool.QueryRow(ctx, `
SELECT users.id,sessions.id
FROM users JOIN sessions ON sessions.user_id=users.id
WHERE users.normalized_email='owner@example.test'
ORDER BY sessions.created_at LIMIT 1`).Scan(&userID, &sessionID); err != nil {
		t.Fatal(err)
	}
	return userID, sessionID
}

func seedSandboxRuntimeQualificationAccount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO assets(symbol) VALUES ('BTC'),('USDT') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_exchange_accounts(
 id,exchange,environment,native_account_hash,state,current_epoch,
 credential_generation,revision,created_at,updated_at
) VALUES ($1,'binance','spot_testnet',$2,'ARMED',1,1,1,$3,$3)`,
		accountID, strings.Repeat("c", 64), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_account_epochs(account_id,epoch,reason,opened_at)
VALUES ($1,1,'initial',$2)`, accountID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_credential_generations(
 account_id,generation,key_fingerprint,account_identity_hash,validated_at
) VALUES ($1,1,$2,$3,$4)`,
		accountID, strings.Repeat("d", 32), strings.Repeat("c", 64), now); err != nil {
		t.Fatal(err)
	}
}

func seedSandboxRuntimeQualificationSession(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID sandbox.AccountID,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO configuration_versions(
 id,version,configuration_hash,canonical_payload,actor,recorded_at
) VALUES ('sandbox_runtime-config-qualification',2,$1,'{}','sandbox_runtime-qualification',$2)`,
		stableSandboxRuntimeHash("sandbox_runtime-config-qualification"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_sessions(
 id,state,configuration_id,strategy_set_hash,revision,created_by,created_at,updated_at
) VALUES ('sandbox_runtime-session-qualification','ARMED','sandbox_runtime-config-qualification',$1,1,$2,$3,$3)`,
		strings.Repeat("e", 64), userID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_session_accounts(session_id,account_id,account_epoch)
VALUES ('sandbox_runtime-session-qualification',$1,1)`, accountID); err != nil {
		t.Fatal(err)
	}
	seedSandboxRuntimeQualificationArm(t, ctx, pool, userID, sessionID, now)
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_account_leases(
 account_id,environment,owner,fencing_token,acquired_at,expires_at
) VALUES ($1,'spot_testnet','sandbox_runtime-worker',1,$2,$3)`,
		accountID, now, now.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func seedSandboxRuntimeQualificationArm(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_authorizations(
 id,token_hash,user_id,session_id,purpose,totp_counter,session_revision,source_hash,reason_hash,
 created_at,expires_at
) VALUES (
 'sandbox_runtime-authorization-qualification',$1,$2,$3,'sandbox_arm',1,
 (SELECT revision FROM sessions WHERE id=$3),$4,$5,$6,$7
)`,
		strings.Repeat("f", 64), userID, sessionID, strings.Repeat("1", 64),
		strings.Repeat("2", 64), now, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_arms(
 id,sandbox_session_id,authorization_id,actor_user_id,actor_session_id,
 source_hash,reason_hash,created_at,expires_at,revision
) VALUES (
 'sandbox_runtime-arm-unconsumed','sandbox_runtime-session-qualification',
 'sandbox_runtime-authorization-qualification',$1,$2,$3,$4,$5,$6,1
)`, userID, sessionID, strings.Repeat("1", 64), strings.Repeat("2", 64),
		now, now.Add(15*time.Minute)); err == nil {
		t.Fatal("unconsumed high-risk authorization created an arm")
	}
	if _, err := pool.Exec(ctx, `
UPDATE sandbox_runtime_sandbox_authorizations SET consumed_at=$2 WHERE id=$1`,
		"sandbox_runtime-authorization-qualification", now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_arms(
 id,sandbox_session_id,authorization_id,actor_user_id,actor_session_id,
 source_hash,reason_hash,created_at,expires_at,revision
) VALUES (
 'sandbox_runtime-arm-qualification','sandbox_runtime-session-qualification',
 'sandbox_runtime-authorization-qualification',$1,$2,$3,$4,$5,$6,1
)`, userID, sessionID, strings.Repeat("1", 64), strings.Repeat("2", 64),
		now, now.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func sandboxQualificationSubmission(
	accountID sandbox.AccountID,
	now time.Time,
) (sandbox.Submission, domain.Quantity, domain.Price) {
	planID, _ := domain.NewExecutionPlanID("sandbox_runtime-plan-qualification")
	orderID, _ := domain.NewVirtualOrderID("sandbox_runtime-order-qualification")
	strategyID, _ := domain.NewStrategyID("trend")
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("0.001")
	price, _ := domain.ParsePrice("10000")
	notional, _ := domain.ParseNotional("10")
	submission := sandbox.Submission{
		PlanID: planID, OrderID: orderID, AccountID: accountID, AccountEpoch: 1,
		ClientOrderID: "ax-sandbox-runtime-qualification", StrategyID: strategyID,
		Instrument: instrument, Side: domain.SideBuy, Quantity: quantity, LimitPrice: price,
		Notional: notional, Style: sandbox.OrderStyleLimitGTC, Action: sandbox.IntentEntry,
		RequestHash: strings.Repeat("3", 64), PolicyHash: strings.Repeat("4", 64),
		ApprovedAt: now,
	}
	return submission, quantity, price
}

func sandboxQualificationPlan(
	submission sandbox.Submission,
	userID, sessionID string,
	now time.Time,
	snapshot sandbox.AccountSnapshot,
) sandbox.ApprovedSandboxPlan {
	pipeline := sandboxQualificationPipeline(now)
	plan := sandbox.ApprovedSandboxPlan{
		ID: submission.PlanID.String(), SessionID: "sandbox_runtime-session-qualification",
		Submissions: []sandbox.Submission{submission},
		Reservations: []sandbox.DurableReservation{{
			ID: "sandbox_runtime-reservation-qualification", AccountID: submission.AccountID, AccountEpoch: 1,
			OrderID: submission.OrderID.String(), Asset: "USDT", Quantity: "10",
		}},
		Arm: sandbox.Arm{
			ID: "sandbox_runtime-arm-qualification", SessionID: "sandbox_runtime-session-qualification",
			AccountIDs:        []sandbox.AccountID{submission.AccountID},
			AuthorizationHash: strings.Repeat("5", 64),
			ActorUserID:       userID, ActorSessionID: sessionID,
			ReasonHash: strings.Repeat("2", 64),
			CreatedAt:  now, ExpiresAt: now.Add(15 * time.Minute), Revision: 1,
		},
		Eligibility: map[sandbox.Exchange]sandbox.EligibilitySnapshot{
			sandbox.ExchangeBinance: {
				ObservedAt: now, Exchange: "binance", Instrument: "BTCUSDT",
				BookHealthy: true, BookFresh: true, BookEligible: true,
				ClockEligible: true, Eligible: true,
			},
		},
		EntrySafety: sandboxQualificationEntrySafety(submission, now),
		AccountSnapshots: map[sandbox.AccountID]sandbox.AccountSnapshotReference{
			submission.AccountID: {AccountID: snapshot.AccountID, AccountEpoch: snapshot.Epoch,
				SnapshotHash: snapshot.SnapshotHash, ObservedAt: snapshot.ObservedAt},
		},
		Pipeline: pipeline, ApprovedAt: now,
		ConfigurationID: "sandbox_runtime-config-qualification",
	}
	plan.ApprovalHash = pipeline.HashFor(plan)
	return plan
}

func sandboxQualificationPipeline(now time.Time) sandbox.ApprovalPipelineEvidence {
	return sandbox.ApprovalPipelineEvidence{
		IntentKind:        sandbox.ApprovalStrategyIntent,
		IntentHash:        strings.Repeat("a", 64),
		AllocatorHash:     strings.Repeat("b", 64),
		RiskHash:          strings.Repeat("c", 64),
		PlannerHash:       strings.Repeat("d", 64),
		AssetApprovalHash: strings.Repeat("e", 64),
		RiskApproved:      true, AssetApproved: true, ObservedAt: now,
	}
}

func sandboxQualificationEntrySafety(
	submission sandbox.Submission,
	now time.Time,
) map[sandbox.AccountID]sandbox.EntrySafetySnapshot {
	return map[sandbox.AccountID]sandbox.EntrySafetySnapshot{
		submission.AccountID: {
			AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
			Exchange: sandbox.ExchangeBinance, ObservedAt: now,
			State: sandbox.EngineArmed, ArmActive: true,
			GlobalIntegrationEnabled: true, GlobalSubmissionEnabled: true,
			ExchangeIntegrationEnabled: true, ExchangeSubmissionEnabled: true,
			PublicEligible: true, PrivateStreamHealthy: true, AccountStateFresh: true,
			ReconciliationClean: true, LeaseHeld: true, EvidenceHealthy: true,
			OpenCapacityAvailable: true, DailyCapacityAvailable: true,
		},
	}
}

func approveSandboxRuntimeDispatcherFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture sandboxRuntimeDispatcherFixture,
) (*SandboxRuntimeDispatcherStore, string) {
	t.Helper()
	store, err := NewSandboxRuntimeDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	limits := sandbox.SubmissionLimits{
		MaximumOrderNotional: "10", MaximumDailyNotional: "50",
		MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2,
	}
	if err = store.ApprovePlan(ctx, fixture.plan, limits, sandbox.NoKillPoint{}); err != nil {
		t.Fatal(err)
	}
	assertSandboxRuntimeEntrySafetyPersisted(t, ctx, pool, fixture.plan)
	claimed, err := store.ClaimOutbox(
		ctx, fixture.accountID, 1, "sandbox_runtime-worker", 1, fixture.plan.Arm.ExpiresAt,
		time.Minute, 1, sandbox.NoKillPoint{},
	)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("expired arm outbox claim=%d error=%v", len(claimed), err)
	}
	claimed, err = store.ClaimOutbox(
		ctx, fixture.accountID, 1, "sandbox_runtime-worker", 1, fixture.now,
		time.Minute, 1, sandbox.NoKillPoint{},
	)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("outbox claim=%d error=%v", len(claimed), err)
	}
	if err = store.MarkSubmitting(
		ctx, claimed[0].ID, 1, fixture.plan.Arm.ExpiresAt, sandbox.NoKillPoint{},
	); err == nil {
		t.Fatal("expired arm crossed the submission network boundary")
	}
	if err = store.MarkSubmitting(
		ctx, claimed[0].ID, 1, fixture.now, sandbox.NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	return store, claimed[0].ID
}

func assertSandboxRuntimeEntrySafetyPersisted(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	plan sandbox.ApprovedSandboxPlan,
) {
	t.Helper()
	var count int
	var allGates bool
	if err := pool.QueryRow(ctx, `
SELECT count(*),
       bool_and(
         state='ARMED' AND arm_active
         AND global_integration_enabled AND global_submission_enabled
         AND exchange_integration_enabled AND exchange_submission_enabled
         AND public_eligible AND private_stream_healthy AND account_state_fresh
         AND reconciliation_clean AND lease_held AND evidence_healthy
         AND open_capacity_available AND daily_capacity_available
       )
FROM sandbox_runtime_plan_entry_safety
WHERE plan_id=$1`, plan.ID).Scan(&count, &allGates); err != nil ||
		count != len(plan.EntrySafety) || !allGates {
		t.Fatalf(
			"entry safety rows=%d want=%d gates=%t error=%v",
			count,
			len(plan.EntrySafety),
			allGates,
			err,
		)
	}
	if _, err := pool.Exec(ctx, `
UPDATE sandbox_runtime_plan_entry_safety
SET private_stream_healthy=false
WHERE plan_id=$1`, plan.ID); err == nil {
		t.Fatal("entry safety mutation was accepted")
	}
	if _, err := pool.Exec(ctx, `
DELETE FROM sandbox_runtime_plan_entry_safety WHERE plan_id=$1`, plan.ID); err == nil {
		t.Fatal("entry safety deletion was accepted")
	}
}

func sandboxRuntimeAcknowledgementEvent(fixture sandboxRuntimeDispatcherFixture) sandbox.PrivateEvent {
	zero, _ := domain.ParseQuantity("0")
	acknowledgement := execution.OrderEvent{
		ID: "sandbox_runtime-ack-qualification", OrderID: fixture.orderID,
		ClientOrderID: fixture.submission.ClientOrderID, State: execution.OrderAcknowledged,
		ExchangeStatus: "NEW", CumulativeQuantity: zero, OccurredAt: fixture.now, Ordinal: 6,
	}
	return sandbox.PrivateEvent{
		Identity: "sandbox_runtime-private-ack-qualification", AccountID: fixture.accountID, AccountEpoch: 1,
		Kind: sandbox.PrivateOrderEvent, OrderID: fixture.orderID,
		ClientOrderID: fixture.submission.ClientOrderID, NativeOrderHash: strings.Repeat("7", 64),
		OrderEvent: &acknowledgement, OccurredAt: fixture.now, ReceivedAt: fixture.now,
	}
}

func assertSandboxRuntimeInboxCrashRecovery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	outboxID string,
	event sandbox.PrivateEvent,
) {
	t.Helper()
	err := store.AppendPrivateEvent(
		ctx, outboxID, 1, event,
		&sandboxRuntimePostgresCrashOnce{boundary: sandbox.KillAfterInboxCommit},
	)
	if !errors.Is(err, sandbox.ErrInjectedCrash) {
		t.Fatalf("inbox crash = %v", err)
	}
	var unreduced bool
	if err = pool.QueryRow(ctx, `
SELECT reduced_at IS NULL FROM sandbox_runtime_private_inbox WHERE id=$1`,
		event.Identity).Scan(&unreduced); err != nil || !unreduced {
		t.Fatalf("durable unreduced inbox=%t error=%v", unreduced, err)
	}
	recovered, err := store.RecoverPrivateInbox(
		ctx,
		event.AccountID,
		event.AccountEpoch,
		1,
	)
	if err != nil || recovered != 1 {
		t.Fatalf("inbox recovery count=%d error=%v", recovered, err)
	}
	assertSandboxRuntimeInboxReplay(t, ctx, pool, store, outboxID, event)
}

func assertSandboxRuntimeInboxReplay(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	outboxID string,
	event sandbox.PrivateEvent,
) {
	t.Helper()
	replay := event
	replay.ReceivedAt = event.ReceivedAt.Add(time.Second)
	if err := store.AppendPrivateEvent(
		ctx,
		outboxID,
		1,
		replay,
		sandbox.NoKillPoint{},
	); err != nil {
		t.Fatalf("recovered inbox replay with a later receive time failed: %v", err)
	}
	var retainedReceivedAt time.Time
	if err := pool.QueryRow(ctx, `
SELECT received_at FROM sandbox_runtime_private_inbox WHERE id=$1`,
		event.Identity,
	).Scan(&retainedReceivedAt); err != nil ||
		!retainedReceivedAt.Equal(event.ReceivedAt) {
		t.Fatalf(
			"first inbox receive time was not retained: time=%s error=%v",
			retainedReceivedAt,
			err,
		)
	}
	conflict := replay
	conflict.NativeOrderHash = strings.Repeat("9", 64)
	err := store.AppendPrivateEvent(
		ctx,
		outboxID,
		1,
		conflict,
		sandbox.NoKillPoint{},
	)
	if err == nil || err.Error() != "sandbox_runtime_private_event_identity_conflict" {
		t.Fatalf("conflicting inbox replay was accepted: %v", err)
	}
}

func sandboxRuntimeFillEvent(fixture sandboxRuntimeDispatcherFixture) sandbox.PrivateEvent {
	fillID, _ := domain.NewVirtualFillID("sandbox_runtime-fill-qualification")
	feeAsset, _ := domain.ParseAssetSymbol("USDT")
	fee, _ := domain.ParseFee("0.01")
	fill := execution.FillFact{
		ID: fillID, Quantity: fixture.quantity, Price: fixture.price, Fee: fee,
		FeeAsset: feeAsset, Ordinal: 7,
	}
	filled := execution.OrderEvent{
		ID: "sandbox_runtime-filled-qualification", OrderID: fixture.orderID,
		ClientOrderID: fixture.submission.ClientOrderID, State: execution.OrderFilled,
		ExchangeStatus: "FILLED", CumulativeQuantity: fixture.quantity,
		Fees:  []execution.FeeFact{{Asset: feeAsset, Total: fee}},
		Fills: []execution.FillFact{fill}, OccurredAt: fixture.now.Add(time.Second), Ordinal: 7,
	}
	return sandbox.PrivateEvent{
		Identity: "sandbox_runtime-private-fill-qualification", AccountID: fixture.accountID, AccountEpoch: 1,
		Kind: sandbox.PrivateFillEvent, OrderID: fixture.orderID,
		ClientOrderID: fixture.submission.ClientOrderID, NativeOrderHash: strings.Repeat("7", 64),
		NativeFillHash: strings.Repeat("8", 64), OrderEvent: &filled,
		OccurredAt: fixture.now.Add(time.Second), ReceivedAt: fixture.now.Add(time.Second),
	}
}

func assertSandboxRuntimeFillCrashRecovery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	outboxID string,
	event sandbox.PrivateEvent,
) {
	t.Helper()
	err := store.AppendPrivateEvent(
		ctx, outboxID, 1, event,
		&sandboxRuntimePostgresCrashOnce{boundary: sandbox.KillAfterFillPosting},
	)
	if !errors.Is(err, sandbox.ErrInjectedCrash) {
		t.Fatalf("fill crash = %v", err)
	}
	assertSandboxRuntimeNativeFillCount(t, ctx, pool, event, 0)
	if err = store.AppendPrivateEvent(ctx, outboxID, 1, event, sandbox.NoKillPoint{}); err != nil {
		t.Fatalf("fill recovery failed: %v", err)
	}
	replay := event
	replay.Identity += "-native-replay"
	replay.NativeOrderHash = strings.Repeat("9", 64)
	replay.ReceivedAt = event.ReceivedAt.Add(time.Second)
	if err = store.AppendPrivateEvent(
		ctx, outboxID, 1, replay, sandbox.NoKillPoint{},
	); err != nil {
		t.Fatalf("terminal canonical replay failed: %v", err)
	}
	assertSandboxRuntimePrivateEventReduced(t, ctx, pool, replay.Identity)
	assertSandboxRuntimeNativeFillCount(t, ctx, pool, event, 1)
}

func assertSandboxRuntimeNativeFillCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	event sandbox.PrivateEvent, want int,
) {
	t.Helper()
	var count int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_runtime_exchange_fills
 WHERE account_id=$1 AND account_epoch=$2 AND native_fill_id_hash=$3`,
		event.AccountID, event.AccountEpoch, event.NativeFillHash).Scan(&count)
	if err != nil || count != want {
		t.Fatalf("native fill count=%d want=%d error=%v", count, want, err)
	}
}

func assertSandboxRuntimePrivateEventReduced(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	var reduced bool
	err := pool.QueryRow(ctx, `SELECT reduced_at IS NOT NULL FROM sandbox_runtime_private_inbox WHERE id=$1`, id).Scan(&reduced)
	if err != nil || !reduced {
		t.Fatalf("terminal canonical replay reduced=%t error=%v", reduced, err)
	}
}

func assertSandboxRuntimeRecoveredState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	outboxID string,
) {
	t.Helper()
	state := loadSandboxRuntimeRecoveredState(t, ctx, pool, outboxID)
	if state.outbox != "TERMINAL" || state.order != "FILLED" || state.reservation != "CONSUMED" ||
		state.plan != "COMPLETED" || state.reduced != 3 || state.inbox != 3 || state.fills != 1 {
		t.Fatalf("recovery state=%s/%s reservation=%s plan=%s inbox=%d/%d fills=%d",
			state.outbox, state.order, state.reservation, state.plan,
			state.reduced, state.inbox, state.fills)
	}
}

type sandboxRuntimeRecoveredState struct {
	outbox, order, reservation, plan string
	reduced, inbox, fills            int
}

func loadSandboxRuntimeRecoveredState(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	outboxID string,
) sandboxRuntimeRecoveredState {
	t.Helper()
	var state sandboxRuntimeRecoveredState
	var accountID, orderID string
	var accountEpoch int64
	if err := pool.QueryRow(ctx, `
SELECT state,order_state,account_id,account_epoch,order_id
FROM sandbox_runtime_submission_outbox WHERE id=$1`,
		outboxID).Scan(&state.outbox, &state.order, &accountID, &accountEpoch, &orderID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT state FROM sandbox_runtime_sandbox_reservations WHERE id='sandbox_runtime-reservation-qualification'`,
	).Scan(&state.reservation); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT state FROM sandbox_runtime_submission_plans WHERE id='execution_plan:sandbox_runtime-plan-qualification'`,
	).Scan(&state.plan); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE reduced_at IS NOT NULL),count(*)
FROM sandbox_runtime_private_inbox
WHERE account_id=$1 AND account_epoch=$2 AND order_id=$3`,
		accountID, accountEpoch, orderID).Scan(&state.reduced, &state.inbox); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_exchange_fills
WHERE account_id=$1 AND account_epoch=$2 AND order_id=$3`,
		accountID, accountEpoch, orderID).Scan(&state.fills); err != nil {
		t.Fatal(err)
	}
	return state
}

func assertSandboxRuntimeUnsentAttemptRecovery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	fixture sandboxRuntimeDispatcherFixture,
) {
	t.Helper()
	at, snapshot, plan := sandboxRuntimeUnsentPlan(t, fixture)
	if err := store.RecordAccountSnapshot(ctx, "sandbox_runtime-snapshot-unsent-recovery", snapshot); err != nil {
		t.Fatalf("unsent plan snapshot error=%v", err)
	}
	if err := store.ApprovePlan(
		ctx,
		plan,
		sandbox.SubmissionLimits{
			MaximumOrderNotional: "10", MaximumDailyNotional: "50",
			MaximumOpenPerAccount: 1, MaximumOpenGlobal: 2,
		},
		sandbox.NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	outboxID := claimUnknownSandboxRuntimeUnsent(
		t, ctx, store, fixture.accountID, at,
	)
	reconciliation := recordSandboxRuntimeUnsentReconciliation(
		t, ctx, store, fixture.accountID, at,
	)
	resolved, err := store.ResolveReconciledTerminal(
		ctx, outboxID, 1, reconciliation.ID,
		reconciliation.ReconciledAt, sandbox.NoKillPoint{},
	)
	if err != nil || !resolved {
		t.Fatalf("unsent resolution=%t error=%v", resolved, err)
	}
	assertSandboxRuntimeUnsentRecoveredState(t, ctx, pool, outboxID)
}

func sandboxRuntimeUnsentPlan(
	t *testing.T,
	fixture sandboxRuntimeDispatcherFixture,
) (time.Time, sandbox.AccountSnapshot, sandbox.ApprovedSandboxPlan) {
	t.Helper()
	at := fixture.now.Add(2 * time.Second)
	snapshot := sandboxRuntimeUnsentSnapshot(t, fixture, at)
	planID, _ := domain.NewExecutionPlanID("sandbox_runtime-plan-unsent")
	orderID, _ := domain.NewVirtualOrderID("sandbox_runtime-order-unsent")
	submission := fixture.submission
	submission.PlanID = planID
	submission.OrderID = orderID
	submission.ClientOrderID = "ax-sandbox-runtime-unsent"
	submission.RequestHash = strings.Repeat("6", 64)
	submission.ApprovedAt = at
	plan := fixture.plan
	plan.ID = planID.String()
	plan.Submissions = []sandbox.Submission{submission}
	reservation := fixture.plan.Reservations[0]
	reservation.ID = "sandbox_runtime-reservation-unsent"
	reservation.OrderID = orderID.String()
	plan.Reservations = []sandbox.DurableReservation{reservation}
	plan.Eligibility = map[sandbox.Exchange]sandbox.EligibilitySnapshot{
		sandbox.ExchangeBinance: {ObservedAt: at, Exchange: "binance", Instrument: "BTCUSDT",
			BookHealthy: true, BookFresh: true, BookEligible: true, ClockEligible: true, Eligible: true}}
	plan.EntrySafety = sandboxQualificationEntrySafety(submission, at)
	plan.AccountSnapshots = map[sandbox.AccountID]sandbox.AccountSnapshotReference{
		fixture.accountID: {AccountID: fixture.accountID, AccountEpoch: 1,
			SnapshotHash: snapshot.SnapshotHash, ObservedAt: snapshot.ObservedAt}}
	plan.Pipeline = sandboxQualificationPipeline(at)
	plan.ApprovedAt = at
	plan.ApprovalHash = plan.Pipeline.HashFor(plan)
	return at, snapshot, plan
}

func sandboxRuntimeUnsentSnapshot(t *testing.T, fixture sandboxRuntimeDispatcherFixture, at time.Time) sandbox.AccountSnapshot {
	t.Helper()
	baseAvailable, err := domain.ParseBalance("0.001")
	if err != nil {
		t.Fatal(err)
	}
	quoteAvailable, err := domain.ParseBalance("39.99")
	if err != nil {
		t.Fatal(err)
	}
	zero, err := domain.ParseBalance("0")
	if err != nil {
		t.Fatal(err)
	}
	return sandbox.AccountSnapshot{
		AccountID: fixture.accountID, Epoch: 1,
		Balances: []sandbox.Balance{
			{Asset: "BTC", Available: baseAvailable, Reserved: zero},
			{Asset: "USDT", Available: quoteAvailable, Reserved: zero},
		},
		OrdersHash: strings.Repeat("a", 64), FillsHash: strings.Repeat("b", 64),
		SnapshotHash: strings.Repeat("c", 64), ObservedAt: at,
	}
}

func claimUnknownSandboxRuntimeUnsent(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	accountID sandbox.AccountID,
	at time.Time,
) string {
	t.Helper()
	claimed, err := store.ClaimOutbox(
		ctx,
		accountID,
		1,
		"sandbox_runtime-worker",
		1,
		at,
		time.Minute,
		1,
		sandbox.NoKillPoint{},
	)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("unsent claim count=%d error=%v", len(claimed), err)
	}
	if err = store.MarkSubmitting(
		ctx,
		claimed[0].ID,
		1,
		at,
		sandbox.NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	if err = store.MarkUnknown(
		ctx,
		claimed[0].ID,
		1,
		at,
		sandbox.NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	return claimed[0].ID
}

func recordSandboxRuntimeUnsentReconciliation(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	accountID sandbox.AccountID,
	at time.Time,
) sandbox.ReconciliationResult {
	t.Helper()
	reconciledAt := at.Add(time.Second)
	result := sandbox.ReconciliationResult{
		ID: "sandbox_runtime-reconciliation-unsent", AccountID: accountID,
		AccountEpoch: 1, State: "clean",
		EvidenceHash: strings.Repeat("9", 64), ReconciledAt: reconciledAt,
	}
	if err := store.RecordReconciliation(ctx, result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertSandboxRuntimeUnsentRecoveredState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	outboxID string,
) {
	t.Helper()
	var outboxState, orderState, reservationState, planState string
	if err := pool.QueryRow(ctx, `
SELECT outbox.state,outbox.order_state,reservation.state,plan.state
FROM sandbox_runtime_submission_outbox outbox
JOIN sandbox_runtime_sandbox_reservations reservation ON reservation.order_id=outbox.order_id
JOIN sandbox_runtime_submission_plans plan ON plan.id=outbox.plan_id
WHERE outbox.id=$1`, outboxID).Scan(
		&outboxState,
		&orderState,
		&reservationState,
		&planState,
	); err != nil {
		t.Fatal(err)
	}
	if outboxState != "TERMINAL" || orderState != "REJECTED" ||
		reservationState != "RELEASED" || planState != "FAILED" {
		t.Fatalf(
			"unsent state=%s/%s reservation=%s plan=%s",
			outboxState,
			orderState,
			reservationState,
			planState,
		)
	}
}
