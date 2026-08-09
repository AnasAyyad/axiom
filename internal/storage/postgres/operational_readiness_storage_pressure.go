package postgres

import (
	"context"
	"fmt"
	"strconv"

	"axiom/internal/storage/pressure"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OperationalReadinessStoragePressureState is the authoritative current disk posture.
type OperationalReadinessStoragePressureState struct {
	pressure.Observation
	Revision       int64
	SourceInstance string
}

// OperationalReadinessStoragePressureStore persists current posture and immutable samples.
type OperationalReadinessStoragePressureStore struct {
	pool   *pgxpool.Pool
	source string
}

// NewOperationalReadinessStoragePressureStore constructs the recorder-owned pressure store.
func NewOperationalReadinessStoragePressureStore(pool *pgxpool.Pool, source string) (*OperationalReadinessStoragePressureStore, error) {
	if pool == nil || !validRuntimeIdentity(source) {
		return nil, fmt.Errorf("operational_readiness_storage_pressure_dependencies_invalid")
	}
	return &OperationalReadinessStoragePressureStore{pool: pool, source: source}, nil
}

// Observe serializes one strictly newer sample. Retrying the exact same sample
// is idempotent; conflicting or older observations fail closed.
func (store *OperationalReadinessStoragePressureStore) Observe(
	ctx context.Context, observation pressure.Observation, policy pressure.Policy,
) (OperationalReadinessStoragePressureState, bool, error) {
	if policy.Validate() != nil || observation.ObservedAt.IsZero() ||
		observation.Level == "" {
		return OperationalReadinessStoragePressureState{}, false, fmt.Errorf("operational_readiness_storage_pressure_observation_invalid")
	}
	expected, err := policy.Classify(observation.AvailableBytes, observation.TotalBytes, observation.ObservedAt)
	if err != nil || expected.Level != observation.Level {
		return OperationalReadinessStoragePressureState{}, false, fmt.Errorf("operational_readiness_storage_pressure_observation_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return OperationalReadinessStoragePressureState{}, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('axiom:operational_readiness:storage-pressure'))`); err != nil {
		return OperationalReadinessStoragePressureState{}, false, err
	}
	prior, err := readOperationalReadinessStoragePressure(ctx, tx)
	if err != nil {
		return OperationalReadinessStoragePressureState{}, false, err
	}
	if observation.ObservedAt.Before(prior.ObservedAt) {
		return OperationalReadinessStoragePressureState{}, false, fmt.Errorf("operational_readiness_storage_pressure_stale")
	}
	if observation.ObservedAt.Equal(prior.ObservedAt) {
		if prior.Level != observation.Level || prior.AvailableBytes != observation.AvailableBytes ||
			prior.TotalBytes != observation.TotalBytes || prior.SourceInstance != store.source {
			return OperationalReadinessStoragePressureState{}, false, fmt.Errorf("operational_readiness_storage_pressure_conflict")
		}
		return prior, false, tx.Commit(ctx)
	}
	next := OperationalReadinessStoragePressureState{Observation: observation,
		Revision: prior.Revision + 1, SourceInstance: store.source}
	if err = writeOperationalReadinessStoragePressure(ctx, tx, prior, next, policy); err != nil {
		return OperationalReadinessStoragePressureState{}, false, err
	}
	return next, prior.Level != next.Level, tx.Commit(ctx)
}

func writeOperationalReadinessStoragePressure(ctx context.Context, tx pgx.Tx, prior, next OperationalReadinessStoragePressureState,
	policy pressure.Policy) error {
	eventID, err := ownerConsoleIdentifier("storage-pressure")
	if err != nil {
		return err
	}
	evidenceHash := ownerConsoleHash([]byte(fmt.Sprintf("%s\x1f%s\x1f%d\x1f%d\x1f%d\x1f%s",
		next.Level, prior.Level, next.AvailableBytes, next.TotalBytes,
		next.Revision, next.ObservedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))))
	if _, err := tx.Exec(ctx, `UPDATE owner_console_storage_pressure_state SET
level=$1,available_bytes=$2,total_bytes=$3,high_free_bytes=$4,critical_free_bytes=$5,
revision=$6,observed_at=$7,source_instance=$8 WHERE scope_id='market-data'`,
		next.Level, next.AvailableBytes, next.TotalBytes,
		policy.HighFreeBytes, policy.CriticalFreeBytes, next.Revision,
		next.ObservedAt, next.SourceInstance); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO owner_console_storage_pressure_events(
id,scope_id,prior_level,level,available_bytes,total_bytes,high_free_bytes,
	critical_free_bytes,revision,observed_at,source_instance,evidence_hash
	) VALUES($1,'market-data',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, eventID,
		prior.Level, next.Level, next.AvailableBytes, next.TotalBytes,
		policy.HighFreeBytes, policy.CriticalFreeBytes, next.Revision,
		next.ObservedAt, next.SourceInstance, evidenceHash); err != nil {
		return err
	}
	if err := upsertOperationalReadinessStorageAlert(ctx, tx, next); err != nil {
		return err
	}
	return nil
}

// Current returns current posture; callers decide freshness for their use case.
func (store *OperationalReadinessStoragePressureStore) Current(ctx context.Context) (OperationalReadinessStoragePressureState, error) {
	return readOperationalReadinessStoragePressure(ctx, store.pool)
}

type operationalReadinessPressureQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readOperationalReadinessStoragePressure(ctx context.Context, query operationalReadinessPressureQuerier) (OperationalReadinessStoragePressureState, error) {
	var state OperationalReadinessStoragePressureState
	err := query.QueryRow(ctx, `SELECT level,available_bytes,total_bytes,revision,observed_at,source_instance
FROM owner_console_storage_pressure_state WHERE scope_id='market-data'`).Scan(&state.Level,
		&state.AvailableBytes, &state.TotalBytes, &state.Revision, &state.ObservedAt,
		&state.SourceInstance)
	if err != nil {
		return OperationalReadinessStoragePressureState{}, fmt.Errorf("operational_readiness_storage_pressure_unavailable")
	}
	state.ObservedAt = state.ObservedAt.UTC()
	return state, nil
}

func upsertOperationalReadinessStorageAlert(ctx context.Context, tx pgx.Tx, state OperationalReadinessStoragePressureState) error {
	dedupe := ownerConsoleHash([]byte("axiom:operational_readiness:storage-pressure:market-data"))
	if state.Level == pressure.LevelNormal {
		_, err := tx.Exec(ctx, `UPDATE alerts SET state='resolved',resolved_at=$1,last_seen_at=$1,
revision=revision+1 WHERE deduplication_key=$2 AND state<>'resolved'`, state.ObservedAt, dedupe)
		return err
	}
	severity := "warning"
	if state.Level == pressure.LevelCritical {
		severity = "critical"
	}
	alertID := "storage-pressure-market-data"
	reason := "storage.pressure." + stringLower(string(state.Level))
	_, err := tx.Exec(ctx, `INSERT INTO alerts(
id,alert_type,state,created_at,severity,reason_code,deduplication_key,correlation_id,
last_seen_at,occurrences,revision
) VALUES($1,'storage_pressure','open',$2,$3,$4,$5,$6,$2,1,1)
ON CONFLICT(deduplication_key) DO UPDATE SET state='open',severity=EXCLUDED.severity,
reason_code=EXCLUDED.reason_code,correlation_id=EXCLUDED.correlation_id,
last_seen_at=EXCLUDED.last_seen_at,occurrences=alerts.occurrences+1,
revision=alerts.revision+1,acknowledged_at=NULL,resolved_at=NULL`, alertID,
		state.ObservedAt, severity, reason, dedupe,
		"storage-pressure:"+strconv.FormatInt(state.Revision, 10))
	return err
}

func stringLower(value string) string {
	if value == "HIGH" {
		return "high"
	}
	if value == "CRITICAL" {
		return "critical"
	}
	return "normal"
}

func validRuntimeIdentity(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_' ||
			character == '.' || character == ':' {
			continue
		}
		return false
	}
	return true
}
