package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

func (store *A11ConsoleStore) d1Configurations(
	ctx context.Context,
	position string,
	query console.D1ListQuery,
) ([]generated.D1Resource, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id,version,recorded_at,configuration_hash,actor,
       EXISTS(SELECT 1 FROM configuration_activations activation WHERE activation.configuration_id=id)
FROM configuration_versions WHERE ($1='' OR id<$1)
  AND ($2::timestamptz IS NULL OR recorded_at >= $2)
  AND ($3::timestamptz IS NULL OR recorded_at <= $3)
ORDER BY id DESC LIMIT $4`, position, query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.D1Resource, 0, query.PageSize+1)
	for rows.Next() {
		var id, hash, actor string
		var revision int64
		var occurred time.Time
		var active bool
		if err = rows.Scan(&id, &revision, &occurred, &hash, &actor, &active); err != nil {
			return nil, err
		}
		state := "recorded"
		if active {
			state = "active"
		}
		items = append(items, d1Resource(id, "configuration_revision", revision, state, &occurred,
			map[string]any{"configuration_hash": hash, "actor": actor}, map[string]string{}))
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d1Qualifications(
	ctx context.Context,
	position string,
	query console.D1ListQuery,
) ([]generated.D1Resource, error) {
	rows, err := store.pool.Query(ctx, `
SELECT catalogue.id,coalesce(run.state,'AVAILABLE'),coalesce(run.revision,catalogue.definition_revision),
       coalesce(run.updated_at,catalogue.recorded_at),catalogue.name,catalogue.kind,
       catalogue.duration_seconds,catalogue.owner_start_required,run.id
FROM v1d_qualification_catalogue catalogue
LEFT JOIN LATERAL (
  SELECT * FROM v1d_qualification_runs WHERE qualification_id=catalogue.id
  ORDER BY created_at DESC,id DESC LIMIT 1
) run ON true
WHERE catalogue.active AND ($1='' OR catalogue.id<$1)
  AND ($2='' OR coalesce(run.state,'AVAILABLE')=$2)
  AND ($3::timestamptz IS NULL OR coalesce(run.updated_at,catalogue.recorded_at) >= $3)
  AND ($4::timestamptz IS NULL OR coalesce(run.updated_at,catalogue.recorded_at) <= $4)
ORDER BY catalogue.id DESC LIMIT $5`, position, query.Filters["state"], query.From,
		query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.D1Resource, 0, query.PageSize+1)
	for rows.Next() {
		var id, state, name, kind string
		var duration *int64
		var runID *string
		var revision int64
		var occurred time.Time
		var owner bool
		if err = rows.Scan(&id, &state, &revision, &occurred, &name, &kind, &duration, &owner, &runID); err != nil {
			return nil, err
		}
		items = append(items, d1Resource(id, "qualification", revision, state, &occurred,
			map[string]any{"name": name, "kind": kind, "duration_seconds": duration,
				"owner_start_required": owner, "latest_run_id": runID},
			map[string]string{"sandbox": d1QualificationLink(id)}))
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d1Users(
	ctx context.Context,
	position string,
	query console.D1ListQuery,
) ([]generated.D1Resource, error) {
	rows, err := store.pool.Query(ctx, `
SELECT user_account.id,user_account.status,user_account.role_revision,user_account.created_at,
       user_account.email,coalesce(array_agg(role.role_id ORDER BY role.role_id)
       FILTER (WHERE role.role_id IS NOT NULL),'{}'::text[])
FROM users user_account LEFT JOIN user_roles role ON role.user_id=user_account.id
WHERE ($1='' OR user_account.id<$1) AND ($2='' OR user_account.status=$2)
  AND ($3::timestamptz IS NULL OR user_account.created_at >= $3)
  AND ($4::timestamptz IS NULL OR user_account.created_at <= $4)
GROUP BY user_account.id ORDER BY user_account.id DESC LIMIT $5`, position, query.Filters["state"], query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.D1Resource, 0, query.PageSize+1)
	for rows.Next() {
		var id, state, email string
		var roles []string
		var revision int64
		var occurred time.Time
		if err = rows.Scan(&id, &state, &revision, &occurred, &email, &roles); err != nil {
			return nil, err
		}
		items = append(items, d1Resource(id, "user", revision, state, &occurred,
			map[string]any{"email": email, "roles": roles}, map[string]string{"roles": "/api/v1/users/" + id + "/roles"}))
	}
	return items, rows.Err()
}

// D1Resource returns one explicitly supported detail projection.
func (store *A11ConsoleStore) D1Resource(
	ctx context.Context,
	kind, id string,
) (generated.D1Resource, error) {
	switch kind {
	case "strategy":
		return store.d1StrategyDetail(ctx, id)
	case "order":
		return store.d1OrderDetail(ctx, id)
	case "command":
		return store.d1CommandDetail(ctx, id)
	}
	return generated.D1Resource{}, console.ErrNotFound
}

func (store *A11ConsoleStore) d1OrderDetail(ctx context.Context, id string) (generated.D1Resource, error) {
	var state, instrument, side, quantity, account string
	var plan *string
	var revision int64
	var occurred time.Time
	err := store.pool.QueryRow(ctx, `
SELECT state,revision,updated_at,instrument_id,side,quantity::text,account_id,plan_id
FROM orders WHERE id=$1`, id).Scan(&state, &revision, &occurred, &instrument,
		&side, &quantity, &account, &plan)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.D1Resource{}, console.ErrNotFound
	}
	if err != nil {
		return generated.D1Resource{}, err
	}
	return d1Resource(id, "order", revision, state, &occurred,
		map[string]any{"instrument": instrument, "side": side, "quantity": quantity,
			"account_id": account, "plan_id": plan},
		map[string]string{"self": "/api/v1/orders/" + id}), nil
}

func (store *A11ConsoleStore) d1StrategyDetail(ctx context.Context, id string) (generated.D1Resource, error) {
	var name, family, configured, runtimeState string
	var version, revision int64
	var blockers []byte
	var occurred time.Time
	err := store.pool.QueryRow(ctx, `
SELECT definition.name,definition.family,coalesce(max(version.version),0),
       control.configured_state,control.runtime_state,control.revision,
       control.blocking_prerequisites,control.updated_at
FROM strategy_definitions definition
JOIN v1d_strategy_controls control ON control.strategy_id=definition.id
LEFT JOIN strategy_versions version ON version.strategy_id=definition.id
WHERE definition.id=$1
GROUP BY definition.id,control.strategy_id`, id).Scan(
		&name, &family, &version, &configured, &runtimeState, &revision, &blockers, &occurred,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.D1Resource{}, console.ErrNotFound
	}
	if err != nil {
		return generated.D1Resource{}, err
	}
	var blockerValues []string
	_ = json.Unmarshal(blockers, &blockerValues)
	return d1Resource(id, "strategy", revision, runtimeState, &occurred,
		map[string]any{"name": name, "family": family, "latest_version": strconv.FormatInt(version, 10),
			"configured_state": configured, "runtime_state": runtimeState,
			"blocking_prerequisites": blockerValues, "real_trading_enabled": false},
		map[string]string{"versions": "/api/v1/strategies/" + id + "/versions"}), nil
}

func (store *A11ConsoleStore) d1CommandDetail(ctx context.Context, id string) (generated.D1Resource, error) {
	var state, kind, target, correlation string
	var revision int64
	var occurred time.Time
	err := store.pool.QueryRow(ctx, `
SELECT state,coalesce(command_kind,''),coalesce(target_id,''),entity_revision,
       coalesce(correlation_id,id),coalesce(updated_at,created_at)
FROM command_requests WHERE id=$1`, id).Scan(&state, &kind, &target, &revision, &correlation, &occurred)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.D1Resource{}, console.ErrNotFound
	}
	if err != nil {
		return generated.D1Resource{}, err
	}
	result := d1Resource(id, "command", revision, state, &occurred,
		map[string]any{"command_kind": kind, "target_id": target}, map[string]string{})
	result.CorrelationId = correlation
	return result, nil
}
