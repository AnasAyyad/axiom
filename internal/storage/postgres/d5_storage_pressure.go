package postgres

import (
	"context"
	"fmt"
	"strconv"

	"axiom/internal/storage/pressure"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// D5StoragePressureState is the authoritative current disk posture.
type D5StoragePressureState struct {
	pressure.Observation
	Revision       int64
	SourceInstance string
}

// D5StoragePressureStore persists current posture and immutable samples.
type D5StoragePressureStore struct {
	pool   *pgxpool.Pool
	source string
}

// NewD5StoragePressureStore constructs the recorder-owned pressure store.
func NewD5StoragePressureStore(pool *pgxpool.Pool, source string) (*D5StoragePressureStore, error) {
	if pool == nil || !validRuntimeIdentity(source) {
		return nil, fmt.Errorf("d5_storage_pressure_dependencies_invalid")
	}
	return &D5StoragePressureStore{pool: pool, source: source}, nil
}

// Observe serializes one strictly newer sample. Retrying the exact same sample
// is idempotent; conflicting or older observations fail closed.
func (store *D5StoragePressureStore) Observe(
	ctx context.Context, observation pressure.Observation, policy pressure.Policy,
) (D5StoragePressureState, bool, error) {
	if policy.Validate() != nil || observation.ObservedAt.IsZero() ||
		observation.Level == "" {
		return D5StoragePressureState{}, false, fmt.Errorf("d5_storage_pressure_observation_invalid")
	}
	expected, err := policy.Classify(observation.AvailableBytes, observation.TotalBytes, observation.ObservedAt)
	if err != nil || expected.Level != observation.Level {
		return D5StoragePressureState{}, false, fmt.Errorf("d5_storage_pressure_observation_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return D5StoragePressureState{}, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('axiom:d5:storage-pressure'))`); err != nil {
		return D5StoragePressureState{}, false, err
	}
	prior, err := readD5StoragePressure(ctx, tx)
	if err != nil {
		return D5StoragePressureState{}, false, err
	}
	if observation.ObservedAt.Before(prior.ObservedAt) {
		return D5StoragePressureState{}, false, fmt.Errorf("d5_storage_pressure_stale")
	}
	if observation.ObservedAt.Equal(prior.ObservedAt) {
		if prior.Level != observation.Level || prior.AvailableBytes != observation.AvailableBytes ||
			prior.TotalBytes != observation.TotalBytes || prior.SourceInstance != store.source {
			return D5StoragePressureState{}, false, fmt.Errorf("d5_storage_pressure_conflict")
		}
		return prior, false, tx.Commit(ctx)
	}
	next := D5StoragePressureState{Observation: observation,
		Revision: prior.Revision + 1, SourceInstance: store.source}
	if err = writeD5StoragePressure(ctx, tx, prior, next, policy); err != nil {
		return D5StoragePressureState{}, false, err
	}
	return next, prior.Level != next.Level, tx.Commit(ctx)
}

func writeD5StoragePressure(ctx context.Context, tx pgx.Tx, prior, next D5StoragePressureState,
	policy pressure.Policy) error {
	eventID, err := a11Identifier("storage-pressure")
	if err != nil {
		return err
	}
	evidenceHash := a11Hash([]byte(fmt.Sprintf("%s\x1f%s\x1f%d\x1f%d\x1f%d\x1f%s",
		next.Level, prior.Level, next.AvailableBytes, next.TotalBytes,
		next.Revision, next.ObservedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))))
	if _, err := tx.Exec(ctx, `UPDATE v1d_storage_pressure_state SET
level=$1,available_bytes=$2,total_bytes=$3,high_free_bytes=$4,critical_free_bytes=$5,
revision=$6,observed_at=$7,source_instance=$8 WHERE scope_id='market-data'`,
		next.Level, next.AvailableBytes, next.TotalBytes,
		policy.HighFreeBytes, policy.CriticalFreeBytes, next.Revision,
		next.ObservedAt, next.SourceInstance); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v1d_storage_pressure_events(
id,scope_id,prior_level,level,available_bytes,total_bytes,high_free_bytes,
	critical_free_bytes,revision,observed_at,source_instance,evidence_hash
	) VALUES($1,'market-data',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, eventID,
		prior.Level, next.Level, next.AvailableBytes, next.TotalBytes,
		policy.HighFreeBytes, policy.CriticalFreeBytes, next.Revision,
		next.ObservedAt, next.SourceInstance, evidenceHash); err != nil {
		return err
	}
	if err := upsertD5StorageAlert(ctx, tx, next); err != nil {
		return err
	}
	return nil
}

// Current returns current posture; callers decide freshness for their use case.
func (store *D5StoragePressureStore) Current(ctx context.Context) (D5StoragePressureState, error) {
	return readD5StoragePressure(ctx, store.pool)
}

type d5PressureQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readD5StoragePressure(ctx context.Context, query d5PressureQuerier) (D5StoragePressureState, error) {
	var state D5StoragePressureState
	err := query.QueryRow(ctx, `SELECT level,available_bytes,total_bytes,revision,observed_at,source_instance
FROM v1d_storage_pressure_state WHERE scope_id='market-data'`).Scan(&state.Level,
		&state.AvailableBytes, &state.TotalBytes, &state.Revision, &state.ObservedAt,
		&state.SourceInstance)
	if err != nil {
		return D5StoragePressureState{}, fmt.Errorf("d5_storage_pressure_unavailable")
	}
	state.ObservedAt = state.ObservedAt.UTC()
	return state, nil
}

func upsertD5StorageAlert(ctx context.Context, tx pgx.Tx, state D5StoragePressureState) error {
	dedupe := a11Hash([]byte("axiom:d5:storage-pressure:market-data"))
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
