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

func TestV1CPostgresCleanInstallQualification(t *testing.T) {
	ctx, pool := openV1CTestDatabase(t, "AXIOM_V1C_TEST_DSN")
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
	assertV1CSchema(t, ctx, pool)
	assertV1CAuthenticatedRequestEvidence(t, ctx, pool)
	assertV1CCanaryEvidenceRuntimeReadGrant(t, ctx, pool)
	assertV1CFailClosedConstraints(t, ctx, pool)
	assertV1CEngineRuntimePersistence(t, ctx, pool)
	assertV1CLeaseIsolation(t, ctx, pool)
	assertV1CAuditChainSerialization(t, ctx, pool)
	assertV1CAuthorizationSessionBinding(t, ctx, pool)
	assertV1CCanarySessionPersistence(t, ctx, pool)
	assertV1CDispatcherCrashRecovery(t, ctx, pool)
	assertV1CControlRecoveryAndReset(t, ctx, pool)
	assertV1CC6QualificationBoundary(t, ctx, pool)
	assertV1CC6ObserverQueryParameters(t, ctx, pool)
}

func TestV1CPostgresB8ToV1CUpgradeQualification(t *testing.T) {
	ctx, pool := openV1CTestDatabase(t, "AXIOM_V1C_UPGRADE_TEST_DSN")
	defer pool.Close()
	assertPostgres18(t, ctx, pool)
	assertEmptyTestDatabase(t, ctx, pool)
	applyB4MigrationPrefix(t, ctx, pool, 20)
	if _, err := pool.Exec(ctx, `INSERT INTO assets(symbol) VALUES ('V1CUPGRADE')`); err != nil {
		t.Fatal(err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	migrations, err := Migrations()
	if err != nil || len(migrations) != 26 {
		t.Fatalf("migration catalog=%d error=%v", len(migrations), err)
	}
	for _, migration := range migrations[20:] {
		changed, applyErr := applyMigration(ctx, connection, migration)
		if applyErr != nil || !changed {
			t.Fatalf("B8-to-V1C migration %s changed=%t error=%v", migration.Version, changed, applyErr)
		}
	}
	var sentinel int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM assets WHERE symbol='V1CUPGRADE'`).
		Scan(&sentinel); err != nil || sentinel != 1 {
		t.Fatalf("upgrade sentinel=%d error=%v", sentinel, err)
	}
	assertV1CSchema(t, ctx, pool)
	assertV1CC6QualificationBoundary(t, ctx, pool)
	assertV1CC6ObserverQueryParameters(t, ctx, pool)
}

func openV1CTestDatabase(t *testing.T, environment string) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(environment)
	if dsn == "" {
		t.Skip(environment + " is not set")
	}
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil || !strings.HasSuffix(configuration.ConnConfig.Database, "_v1c_test") {
		t.Fatal("V1C integration requires a dedicated database ending _v1c_test")
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

func assertV1CSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	tables := []string{
		"v1c_totp_replay_state", "v1c_sandbox_authorizations",
		"v1c_high_risk_audit_events", "v1c_exchange_accounts",
		"v1c_account_epochs", "v1c_credential_generations",
		"v1c_credential_rotations", "v1c_sandbox_sessions", "v1c_sandbox_arms",
		"v1c_authenticated_request_evidence",
		"v1c_account_snapshots", "v1c_daily_cap_counters",
		"v1c_submission_plans", "v1c_plan_eligibility", "v1c_plan_entry_safety",
		"v1c_sandbox_reservations",
		"v1c_submission_outbox", "v1c_private_inbox", "v1c_exchange_fills",
		"v1c_exchange_metadata", "v1c_reconciliation_differences",
		"v1c_reconciliations", "v1c_reset_incidents", "v1c_external_adjustments",
		"v1c_risk_unlocks", "v1c_account_leases",
		"v1c_engine_startup_evidence",
		"v1c_engine_commands", "v1c_engine_observations", "v1c_canary_evidence",
		"v1c_engine_runtime_events", "v1c_c6_order_observations",
		"v1c_c6_qualification_runs", "v1c_c6_qualification_accounts",
		"v1c_c6_qualification_samples", "v1c_c6_qualification_failures",
		"v1c_c6_chaos_events", "v1c_c6_recovery_events",
	}
	for _, table := range tables {
		var count int
		if err := pool.QueryRow(ctx, `
SELECT count(*) FROM information_schema.tables
WHERE table_schema='public' AND table_name=$1`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("V1C table %s count=%d error=%v", table, count, err)
		}
	}
}

func assertV1CC6QualificationBoundary(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	at := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	runID := fmt.Sprintf("c6-schema-%d", at.UnixNano())
	assertV1CFormalC6ImageRequired(t, ctx, pool, runID, at)
	insertV1CSmokeC6Run(t, ctx, pool, runID, at)
	assertV1CSmokeCannotBecomeFormal(t, ctx, pool, runID, at)
	assertV1CC6RunPending(t, ctx, pool, runID)
}

func assertV1CC6ObserverQueryParameters(
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
		c6ObserveAccountsSQL,
		observedAt,
	).Scan(&total, &fresh, &leases, &cycles)
	if err != nil {
		t.Fatalf("C6 account observer query parameters rejected: %v", err)
	}
	var reconnects, duration int64
	var runtimeHealthy bool
	err = pool.QueryRow(
		ctx,
		c6ObserveRuntimeSQL,
		observedAt.Add(-time.Minute),
		observedAt,
	).Scan(&reconnects, &duration, &runtimeHealthy)
	if err != nil {
		t.Fatalf("C6 runtime observer cutoff parameters rejected: %v", err)
	}
	var details int
	err = pool.QueryRow(
		ctx,
		"SELECT count(*) FROM ("+c6ObserveAccountDetailsSQL+") account_details",
		observedAt,
		observedAt.Add(-time.Minute),
	).Scan(&details)
	if err != nil {
		t.Fatalf("C6 account recovery observer query parameters rejected: %v", err)
	}
}

func assertV1CFormalC6ImageRequired(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runID string,
	at time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_c6_qualification_runs(
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
		t.Fatal("formal C6 run without image identity was accepted")
	}
}

func insertV1CSmokeC6Run(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runID string,
	at time.Time,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO v1c_c6_qualification_runs(
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
		t.Fatalf("C6 pending run insert failed: %v", err)
	}
}

func assertV1CSmokeCannotBecomeFormal(
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
		`SELECT count(*) FROM v1c_c6_order_observations`,
	).Scan(&orderObservations); err != nil {
		t.Fatalf("C6 redacted order observation view failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE v1c_c6_qualification_runs
SET state='PASSED',started_at=$2,ended_at=$2,evidence_hash=$3,
    observed_duration_seconds=259200,qualified=true,revision=2,updated_at=$2
WHERE id=$1`, runID, at, strings.Repeat("d", 64)); err == nil {
		t.Fatal("smoke run fabricated a formal 72-hour pass")
	}
	if _, err := pool.Exec(
		ctx,
		`DELETE FROM v1c_c6_qualification_runs WHERE id=$1`,
		runID,
	); err == nil {
		t.Fatal("C6 qualification run deletion was accepted")
	}
}

func assertV1CC6RunPending(
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
FROM v1c_c6_qualification_runs WHERE id=$1`, runID).Scan(
		&state,
		&profitability,
		&qualified,
	); err != nil || state != "PENDING" || profitability || qualified {
		t.Fatalf(
			"unsafe C6 qualification row state=%s profitability=%t qualified=%t err=%v",
			state,
			profitability,
			qualified,
			err,
		)
	}
}

func assertV1CAuthenticatedRequestEvidence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	store, err := NewV1CDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	record := v1CAuthenticatedRequestEvidence()
	if err = store.RecordAuthenticatedRequest(ctx, record); err != nil {
		t.Fatalf("authenticated request evidence insert failed: %v", err)
	}
	if err = store.RecordAuthenticatedRequest(ctx, record); err == nil {
		t.Fatal("authenticated request hash replay was accepted")
	}
	assertV1CAuthenticatedEvidencePersisted(t, ctx, pool, record)
	assertV1CAuthenticatedEvidenceSchema(t, ctx, pool)
	assertV1CUnsafeAuthenticatedEvidenceRejected(t, ctx, pool, record.RecordedAt)
	assertV1CAuthenticatedEvidenceImmutable(t, ctx, pool, record)
}

func v1CAuthenticatedRequestEvidence() exchangecontracts.AuthenticatedRequestEvidence {
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
		RequestHash:     sha256.Sum256([]byte("v1c-redacted-request")),
		ConfigurationID: "v1c-request-evidence-configuration",
		RecordedAt:      time.Date(2026, 7, 27, 0, 15, 0, 0, time.UTC),
	}
}

func assertV1CCanaryEvidenceRuntimeReadGrant(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	runtimeRole := testRole("AXIOM_V1C_RUNTIME_ROLE", "axiom_app")
	recorderRole := testRole("AXIOM_V1C_RECORDER_ROLE", "axiom_recorder")
	readOnlyRole := testRole("AXIOM_V1C_READONLY_ROLE", "axiom_readonly")
	if err := ApplyRoleGrants(
		ctx,
		pool,
		runtimeRole,
		recorderRole,
		readOnlyRole,
	); err != nil {
		t.Fatalf("V1C role grants failed: %v", err)
	}
	assertV1CCanaryEvidenceRuntimePrivileges(t, ctx, pool, runtimeRole)
	assertV1CCanaryEvidenceRuntimeQuery(t, ctx, pool, runtimeRole)
}

func assertV1CCanaryEvidenceRuntimePrivileges(
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
			"v1c_authenticated_request_evidence",
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

func assertV1CCanaryEvidenceRuntimeQuery(
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
	recordedAt := v1CAuthenticatedRequestEvidence().RecordedAt
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

func assertV1CAuthenticatedEvidencePersisted(
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
FROM v1c_authenticated_request_evidence
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

func assertV1CAuthenticatedEvidenceSchema(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	var columns []string
	rows, err := pool.Query(ctx, `
SELECT column_name FROM information_schema.columns
WHERE table_schema='public' AND table_name='v1c_authenticated_request_evidence'
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

func assertV1CAuthenticatedEvidenceImmutable(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	record exchangecontracts.AuthenticatedRequestEvidence,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE v1c_authenticated_request_evidence
SET configuration_id='mutated'
WHERE exchange='binance' AND request_hash=$1`,
		fmt.Sprintf("%x", record.RequestHash)); err == nil {
		t.Fatal("authenticated request evidence mutation was accepted")
	}
	if _, err := pool.Exec(ctx, `
DELETE FROM v1c_authenticated_request_evidence
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

func assertV1CUnsafeAuthenticatedEvidenceRejected(
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
INSERT INTO v1c_authenticated_request_evidence(
  exchange,host,method,path,field_names,enumerated_fields,
  request_hash,configuration_id,recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,'v1c-unsafe-evidence',$8)`,
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

func assertV1CFailClosedConstraints(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_exchange_accounts(
 id,exchange,environment,native_account_hash,state,current_epoch,
 credential_generation,revision,created_at,updated_at
) VALUES ('production-account','binance','live',$1,'LOCKED',1,1,1,$2,$2)`,
		strings.Repeat("a", 64), now); err == nil {
		t.Fatal("production environment accepted")
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_daily_cap_counters(utc_day,reserved_notional,revision,updated_at)
VALUES ('2026-07-27',20,1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE v1c_daily_cap_counters
SET reserved_notional=10,revision=2,updated_at=$1
WHERE utc_day='2026-07-27'`, now.Add(time.Minute)); err == nil {
		t.Fatal("daily reservation refund accepted")
	}
}

func assertV1CLeaseIsolation(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 27, 0, 30, 0, 0, time.UTC)
	account := sandbox.AccountID("v1c-lease-isolation")
	seedV1CLeaseIsolationAccount(t, ctx, pool, account, now)
	store, err := NewV1CDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	assertV1CLeasePreCommitCrash(t, ctx, pool, store, account, now)
	assertV1CLeaseOwnershipAndFencing(t, ctx, store, account, now)
	assertV1CLeasePostCommitCrash(t, ctx, pool, store, account, now)
}

func seedV1CLeaseIsolationAccount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_exchange_accounts(
 id,exchange,environment,native_account_hash,state,current_epoch,
 credential_generation,revision,created_at,updated_at
) VALUES ($1,'binance','spot_testnet',$2,'LOCKED',1,1,1,$3,$3)`,
		account, strings.Repeat("9", 64), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_account_epochs(account_id,epoch,reason,opened_at)
	VALUES ($1,1,'initial',$2)`, account, now); err != nil {
		t.Fatal(err)
	}
}

func assertV1CLeasePreCommitCrash(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
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
		&v1cPostgresCrashOnce{boundary: sandbox.KillBeforeLeaseTransition},
	); !errors.Is(err, sandbox.ErrInjectedCrash) {
		t.Fatalf("pre-lease crash=%v", err)
	}
	var leaseRows int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM v1c_account_leases WHERE account_id=$1`, account).
		Scan(&leaseRows); err != nil || leaseRows != 0 {
		t.Fatalf("pre-commit lease rows=%d error=%v", leaseRows, err)
	}
}

func assertV1CLeaseOwnershipAndFencing(
	t *testing.T,
	ctx context.Context,
	store *V1CDispatcherStore,
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

func assertV1CLeasePostCommitCrash(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
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
		&v1cPostgresCrashOnce{boundary: sandbox.KillAfterLeaseTransition},
	); !errors.Is(err, sandbox.ErrInjectedCrash) {
		t.Fatalf("post-lease crash=%v", err)
	}
	var persistedFence int64
	if err := pool.QueryRow(ctx, `
SELECT fencing_token FROM v1c_account_leases WHERE account_id=$1`, account).
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

func assertV1CAuditChainSerialization(
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
	_, _, login := a11QualificationAuthentication(t, ctx, pool, clock)
	store, err := NewV1CAuthenticationStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	const events = 8
	appendConcurrentV1CAudits(t, ctx, store, login, now, events)
	assertV1CAuditRows(t, ctx, pool, events)
}

func appendConcurrentV1CAudits(
	t *testing.T,
	ctx context.Context,
	store *V1CAuthenticationStore,
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
				ID:          fmt.Sprintf("v1c-concurrent-audit-%02d", index),
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

func assertV1CAuditRows(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	events int,
) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT previous_hash,event_hash
FROM v1c_high_risk_audit_events
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

func assertV1CAuthorizationSessionBinding(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 28, 0, 5, 0, 0, time.UTC)
	const (
		userID    = "v1c-session-binding-user"
		sessionID = "v1c-session-binding-session"
	)
	seedV1CAuthorizationSession(t, ctx, pool, userID, sessionID, now)
	store, err := NewV1CAuthenticationStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	assertV1CStaleSessionAuthorizationRejected(t, ctx, pool, store, userID, sessionID, now)
	assertV1CActiveSessionAuthorization(t, ctx, store, userID, sessionID, now)
	assertV1CRevokedSessionAuthorization(t, ctx, pool, store, userID, sessionID, now)
}

func seedV1CAuthorizationSession(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO users(
 id,email,password_hash,status,created_at,normalized_email,role_revision,password_changed_at
) VALUES ($1,'v1c-session-binding@example.test','redacted-hash','active',$2,
          'v1c-session-binding@example.test',1,$2)`,
		userID,
		now,
	); err != nil {
		t.Fatal(err)
	}
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

func assertV1CStaleSessionAuthorizationRejected(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CAuthenticationStore,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	stale := v1CAuthorizationWrite(
		"v1c-stale-revision-auth",
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
SELECT count(*) FROM v1c_totp_replay_state WHERE user_id=$1`, userID).
		Scan(&replayRows); err != nil || replayRows != 0 {
		t.Fatalf("stale authorization advanced TOTP state rows=%d error=%v", replayRows, err)
	}
}

func assertV1CActiveSessionAuthorization(
	t *testing.T,
	ctx context.Context,
	store *V1CAuthenticationStore,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	first := v1CAuthorizationWrite(
		"v1c-active-session-auth",
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

func assertV1CRevokedSessionAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CAuthenticationStore,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	second := v1CAuthorizationWrite(
		"v1c-revoked-session-auth",
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
SELECT consumed_at IS NOT NULL FROM v1c_sandbox_authorizations WHERE id=$1`,
		second.ID,
	).Scan(&consumed); err != nil || consumed {
		t.Fatalf("revoked-session consume was not rolled back consumed=%t error=%v", consumed, err)
	}
	assertV1CDirectRevokedSessionAuthorizationRejected(
		t, ctx, pool, userID, sessionID, now.Add(4*time.Second),
	)
}

func assertV1CDirectRevokedSessionAuthorizationRejected(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_sandbox_authorizations(
 id,token_hash,user_id,session_id,purpose,totp_counter,session_revision,
 source_hash,reason_hash,created_at,expires_at
) VALUES (
 'v1c-direct-revoked-session-auth',$1,$2,$3,'sandbox_arm',3,2,$4,$5,$6,$7
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

func v1CAuthorizationWrite(
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

type v1cPostgresCrashOnce struct {
	boundary sandbox.KillBoundary
	hit      bool
}

func (point *v1cPostgresCrashOnce) Hit(
	_ context.Context,
	boundary sandbox.KillBoundary,
) error {
	if boundary == point.boundary && !point.hit {
		point.hit = true
		return sandbox.ErrInjectedCrash
	}
	return nil
}

func assertV1CDispatcherCrashRecovery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	fixture := newV1CDispatcherFixture(t, ctx, pool)
	assertV1CPlanAccountLockUsesRuntimeRole(t, ctx, pool, fixture)
	store, outboxID := approveV1CDispatcherFixture(t, ctx, pool, fixture)
	acknowledgement := v1CAcknowledgementEvent(fixture)
	assertV1CInboxCrashRecovery(t, ctx, pool, store, outboxID, acknowledgement)
	assertV1CCancelPendingBeforeFill(t, ctx, pool, store, fixture, outboxID)
	fill := v1CFillEvent(fixture)
	assertV1CFillCrashRecovery(t, ctx, pool, store, outboxID, fill)
	assertV1CRecoveredState(t, ctx, pool, outboxID)
	assertV1CUnsentAttemptRecovery(t, ctx, pool, store, fixture)
}

func assertV1CPlanAccountLockUsesRuntimeRole(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture v1cDispatcherFixture,
) {
	t.Helper()
	runtimeRole := testRole("AXIOM_V1C_RUNTIME_ROLE", "axiom_app")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	role := pgx.Identifier{runtimeRole}.Sanitize()
	for _, statement := range []string{
		"GRANT SELECT, UPDATE ON v1c_exchange_accounts TO " + role,
		"GRANT SELECT ON v1c_sandbox_session_accounts TO " + role,
		"REVOKE UPDATE ON v1c_sandbox_session_accounts FROM " + role,
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
		validateV1CPlanAccountSQL,
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

func assertV1CCancelPendingBeforeFill(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
	fixture v1cDispatcherFixture,
	outboxID string,
) {
	t.Helper()
	cancelID, err := store.MarkCancelPending(
		ctx,
		fixture.accountID,
		1,
		fixture.submission.ClientOrderID,
		"v1c-worker",
		1,
		fixture.now.Add(500*time.Millisecond),
		sandbox.NoKillPoint{},
	)
	if err != nil || cancelID != outboxID {
		t.Fatalf("cancel pending id=%q want=%q error=%v", cancelID, outboxID, err)
	}
	var orderState, reservationState string
	if err = pool.QueryRow(ctx, `
SELECT order_state FROM v1c_submission_outbox WHERE id=$1`, outboxID).
		Scan(&orderState); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `
SELECT state FROM v1c_sandbox_reservations WHERE order_id=$1`,
		fixture.orderID.String(),
	).Scan(&reservationState); err != nil {
		t.Fatal(err)
	}
	if orderState != "CANCEL_PENDING" || reservationState != "ACTIVE" {
		t.Fatalf("cancel pending order=%s reservation=%s", orderState, reservationState)
	}
}

type v1cDispatcherFixture struct {
	now        time.Time
	accountID  sandbox.AccountID
	orderID    domain.VirtualOrderID
	quantity   domain.Quantity
	price      domain.Price
	submission sandbox.Submission
	plan       sandbox.ApprovedSandboxPlan
}

func newV1CDispatcherFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) v1cDispatcherFixture {
	t.Helper()
	now := time.Date(2026, 7, 28, 0, 10, 0, 0, time.UTC)
	userID, sessionID := v1CQualificationOwner(t, ctx, pool)
	accountID := sandbox.AccountID("binance-testnet-qualification")
	seedV1CQualificationAccount(t, ctx, pool, accountID, now)
	seedV1CQualificationSession(t, ctx, pool, accountID, userID, sessionID, now)
	submission, quantity, price := v1CQualificationSubmission(accountID, now)
	return v1cDispatcherFixture{
		now: now, accountID: accountID, orderID: submission.OrderID,
		quantity: quantity, price: price, submission: submission,
		plan: v1CQualificationPlan(submission, userID, sessionID, now),
	}
}

func v1CQualificationOwner(
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

func seedV1CQualificationAccount(
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
INSERT INTO v1c_exchange_accounts(
 id,exchange,environment,native_account_hash,state,current_epoch,
 credential_generation,revision,created_at,updated_at
) VALUES ($1,'binance','spot_testnet',$2,'ARMED',1,1,1,$3,$3)`,
		accountID, strings.Repeat("c", 64), now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_account_epochs(account_id,epoch,reason,opened_at)
VALUES ($1,1,'initial',$2)`, accountID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_credential_generations(
 account_id,generation,key_fingerprint,account_identity_hash,validated_at
) VALUES ($1,1,$2,$3,$4)`,
		accountID, strings.Repeat("d", 32), strings.Repeat("c", 64), now); err != nil {
		t.Fatal(err)
	}
}

func seedV1CQualificationSession(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	accountID sandbox.AccountID,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_sandbox_sessions(
 id,state,configuration_id,strategy_set_hash,revision,created_by,created_at,updated_at
) VALUES ('v1c-session-qualification','ARMED','v1c-config-qualification',$1,1,$2,$3,$3)`,
		strings.Repeat("e", 64), userID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_sandbox_session_accounts(session_id,account_id,account_epoch)
VALUES ('v1c-session-qualification',$1,1)`, accountID); err != nil {
		t.Fatal(err)
	}
	seedV1CQualificationArm(t, ctx, pool, userID, sessionID, now)
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_account_leases(
 account_id,environment,owner,fencing_token,acquired_at,expires_at
) VALUES ($1,'spot_testnet','v1c-worker',1,$2,$3)`,
		accountID, now, now.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func seedV1CQualificationArm(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, sessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_sandbox_authorizations(
 id,token_hash,user_id,session_id,purpose,totp_counter,session_revision,source_hash,reason_hash,
 created_at,expires_at
) VALUES (
 'v1c-authorization-qualification',$1,$2,$3,'sandbox_arm',1,
 (SELECT revision FROM sessions WHERE id=$3),$4,$5,$6,$7
)`,
		strings.Repeat("f", 64), userID, sessionID, strings.Repeat("1", 64),
		strings.Repeat("2", 64), now, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_sandbox_arms(
 id,sandbox_session_id,authorization_id,actor_user_id,actor_session_id,
 source_hash,reason_hash,created_at,expires_at,revision
) VALUES (
 'v1c-arm-unconsumed','v1c-session-qualification',
 'v1c-authorization-qualification',$1,$2,$3,$4,$5,$6,1
)`, userID, sessionID, strings.Repeat("1", 64), strings.Repeat("2", 64),
		now, now.Add(15*time.Minute)); err == nil {
		t.Fatal("unconsumed high-risk authorization created an arm")
	}
	if _, err := pool.Exec(ctx, `
UPDATE v1c_sandbox_authorizations SET consumed_at=$2 WHERE id=$1`,
		"v1c-authorization-qualification", now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_sandbox_arms(
 id,sandbox_session_id,authorization_id,actor_user_id,actor_session_id,
 source_hash,reason_hash,created_at,expires_at,revision
) VALUES (
 'v1c-arm-qualification','v1c-session-qualification',
 'v1c-authorization-qualification',$1,$2,$3,$4,$5,$6,1
)`, userID, sessionID, strings.Repeat("1", 64), strings.Repeat("2", 64),
		now, now.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func v1CQualificationSubmission(
	accountID sandbox.AccountID,
	now time.Time,
) (sandbox.Submission, domain.Quantity, domain.Price) {
	planID, _ := domain.NewExecutionPlanID("v1c-plan-qualification")
	orderID, _ := domain.NewVirtualOrderID("v1c-order-qualification")
	strategyID, _ := domain.NewStrategyID("trend")
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("0.001")
	price, _ := domain.ParsePrice("10000")
	notional, _ := domain.ParseNotional("10")
	submission := sandbox.Submission{
		PlanID: planID, OrderID: orderID, AccountID: accountID, AccountEpoch: 1,
		ClientOrderID: "ax-v1c-qualification", StrategyID: strategyID,
		Instrument: instrument, Side: domain.SideBuy, Quantity: quantity, LimitPrice: price,
		Notional: notional, Style: sandbox.OrderStyleLimitGTC, Action: sandbox.IntentEntry,
		RequestHash: strings.Repeat("3", 64), PolicyHash: strings.Repeat("4", 64),
		ApprovedAt: now,
	}
	return submission, quantity, price
}

func v1CQualificationPlan(
	submission sandbox.Submission,
	userID, sessionID string,
	now time.Time,
) sandbox.ApprovedSandboxPlan {
	pipeline := v1CQualificationPipeline(now)
	plan := sandbox.ApprovedSandboxPlan{
		ID: submission.PlanID.String(), SessionID: "v1c-session-qualification",
		Submissions: []sandbox.Submission{submission},
		Reservations: []sandbox.DurableReservation{{
			ID: "v1c-reservation-qualification", AccountID: submission.AccountID, AccountEpoch: 1,
			OrderID: submission.OrderID.String(), Asset: "USDT", Quantity: "10",
		}},
		Arm: sandbox.Arm{
			ID: "v1c-arm-qualification", SessionID: "v1c-session-qualification",
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
		EntrySafety: v1CQualificationEntrySafety(submission, now),
		Pipeline:    pipeline, ApprovedAt: now,
		ConfigurationID: "v1c-config-qualification",
	}
	plan.ApprovalHash = pipeline.HashFor(plan)
	return plan
}

func v1CQualificationPipeline(now time.Time) sandbox.ApprovalPipelineEvidence {
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

func v1CQualificationEntrySafety(
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

func approveV1CDispatcherFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture v1cDispatcherFixture,
) (*V1CDispatcherStore, string) {
	t.Helper()
	store, err := NewV1CDispatcherStore(pool)
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
	assertV1CEntrySafetyPersisted(t, ctx, pool, fixture.plan)
	claimed, err := store.ClaimOutbox(
		ctx, fixture.accountID, 1, "v1c-worker", 1, fixture.plan.Arm.ExpiresAt,
		time.Minute, 1, sandbox.NoKillPoint{},
	)
	if err != nil || len(claimed) != 0 {
		t.Fatalf("expired arm outbox claim=%d error=%v", len(claimed), err)
	}
	claimed, err = store.ClaimOutbox(
		ctx, fixture.accountID, 1, "v1c-worker", 1, fixture.now,
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

func assertV1CEntrySafetyPersisted(
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
FROM v1c_plan_entry_safety
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
UPDATE v1c_plan_entry_safety
SET private_stream_healthy=false
WHERE plan_id=$1`, plan.ID); err == nil {
		t.Fatal("entry safety mutation was accepted")
	}
	if _, err := pool.Exec(ctx, `
DELETE FROM v1c_plan_entry_safety WHERE plan_id=$1`, plan.ID); err == nil {
		t.Fatal("entry safety deletion was accepted")
	}
}

func v1CAcknowledgementEvent(fixture v1cDispatcherFixture) sandbox.PrivateEvent {
	zero, _ := domain.ParseQuantity("0")
	acknowledgement := execution.OrderEvent{
		ID: "v1c-ack-qualification", OrderID: fixture.orderID,
		ClientOrderID: fixture.submission.ClientOrderID, State: execution.OrderAcknowledged,
		ExchangeStatus: "NEW", CumulativeQuantity: zero, OccurredAt: fixture.now, Ordinal: 6,
	}
	return sandbox.PrivateEvent{
		Identity: "v1c-private-ack-qualification", AccountID: fixture.accountID, AccountEpoch: 1,
		Kind: sandbox.PrivateOrderEvent, OrderID: fixture.orderID,
		ClientOrderID: fixture.submission.ClientOrderID, NativeOrderHash: strings.Repeat("7", 64),
		OrderEvent: &acknowledgement, OccurredAt: fixture.now, ReceivedAt: fixture.now,
	}
}

func assertV1CInboxCrashRecovery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
	outboxID string,
	event sandbox.PrivateEvent,
) {
	t.Helper()
	err := store.AppendPrivateEvent(
		ctx, outboxID, 1, event,
		&v1cPostgresCrashOnce{boundary: sandbox.KillAfterInboxCommit},
	)
	if !errors.Is(err, sandbox.ErrInjectedCrash) {
		t.Fatalf("inbox crash = %v", err)
	}
	var unreduced bool
	if err = pool.QueryRow(ctx, `
SELECT reduced_at IS NULL FROM v1c_private_inbox WHERE id=$1`,
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
	assertV1CInboxReplay(t, ctx, pool, store, outboxID, event)
}

func assertV1CInboxReplay(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
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
SELECT received_at FROM v1c_private_inbox WHERE id=$1`,
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
	if err == nil || err.Error() != "v1c_private_event_identity_conflict" {
		t.Fatalf("conflicting inbox replay was accepted: %v", err)
	}
}

func v1CFillEvent(fixture v1cDispatcherFixture) sandbox.PrivateEvent {
	fillID, _ := domain.NewVirtualFillID("v1c-fill-qualification")
	feeAsset, _ := domain.ParseAssetSymbol("USDT")
	fee, _ := domain.ParseFee("0.01")
	fill := execution.FillFact{
		ID: fillID, Quantity: fixture.quantity, Price: fixture.price, Fee: fee,
		FeeAsset: feeAsset, Ordinal: 7,
	}
	filled := execution.OrderEvent{
		ID: "v1c-filled-qualification", OrderID: fixture.orderID,
		ClientOrderID: fixture.submission.ClientOrderID, State: execution.OrderFilled,
		ExchangeStatus: "FILLED", CumulativeQuantity: fixture.quantity,
		Fees:  []execution.FeeFact{{Asset: feeAsset, Total: fee}},
		Fills: []execution.FillFact{fill}, OccurredAt: fixture.now.Add(time.Second), Ordinal: 7,
	}
	return sandbox.PrivateEvent{
		Identity: "v1c-private-fill-qualification", AccountID: fixture.accountID, AccountEpoch: 1,
		Kind: sandbox.PrivateFillEvent, OrderID: fixture.orderID,
		ClientOrderID: fixture.submission.ClientOrderID, NativeOrderHash: strings.Repeat("7", 64),
		NativeFillHash: strings.Repeat("8", 64), OrderEvent: &filled,
		OccurredAt: fixture.now.Add(time.Second), ReceivedAt: fixture.now.Add(time.Second),
	}
}

func assertV1CFillCrashRecovery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
	outboxID string,
	event sandbox.PrivateEvent,
) {
	t.Helper()
	err := store.AppendPrivateEvent(
		ctx, outboxID, 1, event,
		&v1cPostgresCrashOnce{boundary: sandbox.KillAfterFillPosting},
	)
	if !errors.Is(err, sandbox.ErrInjectedCrash) {
		t.Fatalf("fill crash = %v", err)
	}
	var fillCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v1c_exchange_fills`).Scan(&fillCount); err != nil ||
		fillCount != 0 {
		t.Fatalf("uncommitted fill count=%d error=%v", fillCount, err)
	}
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
	var replayReduced bool
	if err = pool.QueryRow(ctx, `
SELECT reduced_at IS NOT NULL FROM v1c_private_inbox WHERE id=$1`,
		replay.Identity,
	).Scan(&replayReduced); err != nil || !replayReduced {
		t.Fatalf("terminal canonical replay reduced=%t error=%v", replayReduced, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v1c_exchange_fills`).Scan(&fillCount); err != nil ||
		fillCount != 1 {
		t.Fatalf("terminal replay fill count=%d error=%v", fillCount, err)
	}
}

func assertV1CRecoveredState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	outboxID string,
) {
	t.Helper()
	var outboxState, orderState, reservationState, planState string
	var reducedCount int
	if err := pool.QueryRow(ctx, `
SELECT state,order_state FROM v1c_submission_outbox WHERE id=$1`,
		outboxID).Scan(&outboxState, &orderState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT state FROM v1c_sandbox_reservations WHERE id='v1c-reservation-qualification'`,
	).Scan(&reservationState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT state FROM v1c_submission_plans WHERE id='execution_plan:v1c-plan-qualification'`,
	).Scan(&planState); err != nil {
		t.Fatal(err)
	}
	var inboxCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE reduced_at IS NOT NULL),count(*)
FROM v1c_private_inbox`).Scan(&reducedCount, &inboxCount); err != nil {
		t.Fatal(err)
	}
	var persistedFills int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM v1c_exchange_fills`).Scan(&persistedFills); err != nil {
		t.Fatal(err)
	}
	if outboxState != "TERMINAL" || orderState != "FILLED" ||
		reservationState != "CONSUMED" || planState != "COMPLETED" ||
		reducedCount != 3 || inboxCount != 3 ||
		persistedFills != 1 {
		t.Fatalf("recovery state=%s/%s reservation=%s plan=%s inbox=%d/%d fills=%d",
			outboxState, orderState, reservationState, planState,
			reducedCount, inboxCount, persistedFills)
	}
}

func assertV1CUnsentAttemptRecovery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
	fixture v1cDispatcherFixture,
) {
	t.Helper()
	at, plan := v1CUnsentPlan(fixture)
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
	outboxID := claimUnknownV1CUnsent(
		t, ctx, store, fixture.accountID, at,
	)
	reconciliation := recordV1CUnsentReconciliation(
		t, ctx, store, fixture.accountID, at,
	)
	resolved, err := store.ResolveReconciledTerminal(
		ctx, outboxID, 1, reconciliation.ID,
		reconciliation.ReconciledAt, sandbox.NoKillPoint{},
	)
	if err != nil || !resolved {
		t.Fatalf("unsent resolution=%t error=%v", resolved, err)
	}
	assertV1CUnsentRecoveredState(t, ctx, pool, outboxID)
}

func v1CUnsentPlan(
	fixture v1cDispatcherFixture,
) (time.Time, sandbox.ApprovedSandboxPlan) {
	at := fixture.now.Add(2 * time.Second)
	planID, _ := domain.NewExecutionPlanID("v1c-plan-unsent")
	orderID, _ := domain.NewVirtualOrderID("v1c-order-unsent")
	submission := fixture.submission
	submission.PlanID = planID
	submission.OrderID = orderID
	submission.ClientOrderID = "ax-v1c-unsent"
	submission.RequestHash = strings.Repeat("6", 64)
	submission.ApprovedAt = at
	plan := fixture.plan
	plan.ID = planID.String()
	plan.Submissions = []sandbox.Submission{submission}
	reservation := fixture.plan.Reservations[0]
	reservation.ID = "v1c-reservation-unsent"
	reservation.OrderID = orderID.String()
	plan.Reservations = []sandbox.DurableReservation{reservation}
	plan.Eligibility = map[sandbox.Exchange]sandbox.EligibilitySnapshot{
		sandbox.ExchangeBinance: {
			ObservedAt: at, Exchange: "binance", Instrument: "BTCUSDT",
			BookHealthy: true, BookFresh: true, BookEligible: true,
			ClockEligible: true, Eligible: true,
		},
	}
	plan.EntrySafety = v1CQualificationEntrySafety(submission, at)
	plan.Pipeline = v1CQualificationPipeline(at)
	plan.ApprovedAt = at
	plan.ApprovalHash = plan.Pipeline.HashFor(plan)
	return at, plan
}

func claimUnknownV1CUnsent(
	t *testing.T,
	ctx context.Context,
	store *V1CDispatcherStore,
	accountID sandbox.AccountID,
	at time.Time,
) string {
	t.Helper()
	claimed, err := store.ClaimOutbox(
		ctx,
		accountID,
		1,
		"v1c-worker",
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

func recordV1CUnsentReconciliation(
	t *testing.T,
	ctx context.Context,
	store *V1CDispatcherStore,
	accountID sandbox.AccountID,
	at time.Time,
) sandbox.ReconciliationResult {
	t.Helper()
	reconciledAt := at.Add(time.Second)
	result := sandbox.ReconciliationResult{
		ID: "v1c-reconciliation-unsent", AccountID: accountID,
		AccountEpoch: 1, State: "clean",
		EvidenceHash: strings.Repeat("9", 64), ReconciledAt: reconciledAt,
	}
	if err := store.RecordReconciliation(ctx, result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertV1CUnsentRecoveredState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	outboxID string,
) {
	t.Helper()
	var outboxState, orderState, reservationState, planState string
	if err := pool.QueryRow(ctx, `
SELECT outbox.state,outbox.order_state,reservation.state,plan.state
FROM v1c_submission_outbox outbox
JOIN v1c_sandbox_reservations reservation ON reservation.order_id=outbox.order_id
JOIN v1c_submission_plans plan ON plan.id=outbox.plan_id
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
