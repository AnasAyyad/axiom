package postgres

import (
	"context"

	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
)

// D1Resources returns bounded deterministic projections from authoritative
// tables. Each kind is an explicit allowlist entry; arbitrary table reads are
// impossible through this boundary.
func (store *A11ConsoleStore) D1Resources(
	ctx context.Context,
	kind string,
	query console.D1ListQuery,
) (generated.D1ResourcePage, error) {
	if query.PageSize < 1 || query.PageSize > 200 {
		return generated.D1ResourcePage{}, console.ErrInvalidRequest
	}
	position, err := store.cursor.Decode("v1d-d1:"+kind, query.Cursor)
	if err != nil {
		return generated.D1ResourcePage{}, err
	}
	var items []generated.D1Resource
	switch kind {
	case "assets":
		items, err = store.d1Assets(ctx, position, query)
	case "strategy_versions":
		items, err = store.d1StrategyVersions(ctx, position, query)
	case "risk_controls":
		items, err = store.d1RiskControls(ctx, position, query)
	case "orders":
		items, err = store.d1Orders(ctx, position, query)
	case "fills":
		items, err = store.d1Fills(ctx, position, query)
	case "alerts":
		items, err = store.d1Alerts(ctx, position, query)
	case "reports":
		items, err = store.d1Jobs(ctx, position, query, true)
	case "configuration_revisions":
		items, err = store.d1Configurations(ctx, position, query)
	case "lab_runs":
		items, err = store.d1Jobs(ctx, position, query, false)
	case "qualifications":
		items, err = store.d1Qualifications(ctx, position, query)
	case "users":
		items, err = store.d1Users(ctx, position, query)
	default:
		return generated.D1ResourcePage{}, console.ErrInvalidRequest
	}
	if err != nil {
		return generated.D1ResourcePage{}, err
	}
	return store.d1Page(ctx, kind, items, query.PageSize)
}

func (store *A11ConsoleStore) d1Page(
	ctx context.Context,
	kind string,
	items []generated.D1Resource,
	limit int,
) (generated.D1ResourcePage, error) {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var next *string
	if hasMore && len(items) > 0 {
		value := store.cursor.Encode("v1d-d1:"+kind, items[len(items)-1].Id)
		next = &value
	}
	var snapshot int64
	if err := store.pool.QueryRow(ctx, `SELECT coalesce(max(revision),0) FROM outbox_events`).Scan(&snapshot); err != nil {
		return generated.D1ResourcePage{}, err
	}
	revision := strconv.FormatInt(snapshot, 10)
	return generated.D1ResourcePage{Items: items, HasMore: hasMore, NextCursor: next,
		Revision: revision, SnapshotRevision: revision}, nil
}

func (store *A11ConsoleStore) d1Assets(
	ctx context.Context,
	position string,
	query console.D1ListQuery,
) ([]generated.D1Resource, error) {
	rows, err := store.pool.Query(ctx, `
SELECT asset.symbol,coalesce(screen.status,'pending_review'),coalesce(screen.version,1),
       screen.recorded_at
FROM assets asset
LEFT JOIN LATERAL (
  SELECT status,version,recorded_at FROM asset_screening_versions
  WHERE asset_symbol=asset.symbol ORDER BY version DESC LIMIT 1
) screen ON true
WHERE ($1='' OR asset.symbol<$1)
  AND ($2='' OR coalesce(screen.status,'pending_review')=$2)
  AND ($3::timestamptz IS NULL OR screen.recorded_at >= $3)
  AND ($4::timestamptz IS NULL OR screen.recorded_at <= $4)
ORDER BY asset.symbol DESC LIMIT $5`, position, query.Filters["state"], query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.D1Resource, 0, query.PageSize+1)
	for rows.Next() {
		var id, state string
		var revision int64
		var occurred *time.Time
		if err = rows.Scan(&id, &state, &revision, &occurred); err != nil {
			return nil, err
		}
		items = append(items, d1Resource(id, "asset", revision, state, occurred,
			map[string]any{"symbol": id, "spot_only": true}, map[string]string{"self": "/api/v1/assets?state=" + state}))
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d1StrategyVersions(
	ctx context.Context,
	position string,
	query console.D1ListQuery,
) ([]generated.D1Resource, error) {
	strategyID := query.Filters["strategy_id"]
	rows, err := store.pool.Query(ctx, `
SELECT version.id,version.promotion_status,version.version,version.created_at,
       version.implementation_hash
FROM strategy_versions version
WHERE version.strategy_id=$1 AND ($2='' OR version.id<$2)
  AND ($3::timestamptz IS NULL OR version.created_at >= $3)
  AND ($4::timestamptz IS NULL OR version.created_at <= $4)
ORDER BY version.id DESC LIMIT $5`, strategyID, position, query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.D1Resource, 0, query.PageSize+1)
	for rows.Next() {
		var id, state, hash string
		var revision int64
		var occurred time.Time
		if err = rows.Scan(&id, &state, &revision, &occurred, &hash); err != nil {
			return nil, err
		}
		items = append(items, d1Resource(id, "strategy_version", revision, state, &occurred,
			map[string]any{"strategy_id": strategyID, "implementation_hash": hash},
			map[string]string{"strategy": "/api/v1/strategies/" + strategyID}))
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d1RiskControls(
	ctx context.Context,
	position string,
	query console.D1ListQuery,
) ([]generated.D1Resource, error) {
	rows, err := store.pool.Query(ctx, `
SELECT scope_type||':'||scope_id,state,revision,updated_at,reason_code,scope_type,scope_id
FROM v1d_risk_controls
WHERE ($1='' OR scope_type||':'||scope_id<$1) AND ($2='' OR state=$2)
  AND ($3::timestamptz IS NULL OR updated_at >= $3)
  AND ($4::timestamptz IS NULL OR updated_at <= $4)
ORDER BY scope_type||':'||scope_id DESC LIMIT $5`, position, query.Filters["state"], query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.D1Resource, 0, query.PageSize+1)
	for rows.Next() {
		var id, state, reason, scope, scopeID string
		var revision int64
		var occurred time.Time
		if err = rows.Scan(&id, &state, &revision, &occurred, &reason, &scope, &scopeID); err != nil {
			return nil, err
		}
		items = append(items, d1Resource(id, "risk_control", revision, state, &occurred,
			map[string]any{"scope": scope, "scope_id": scopeID, "reason_code": reason},
			map[string]string{"control": "/api/v1/risk/controls/" + scope + "/" + scopeID}))
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d1Orders(
	ctx context.Context,
	position string,
	query console.D1ListQuery,
) ([]generated.D1Resource, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id,state,revision,updated_at,instrument_id,side,quantity::text,account_id,plan_id
FROM orders WHERE ($1='' OR id<$1) AND ($2='' OR state=$2)
  AND ($3::timestamptz IS NULL OR updated_at >= $3)
  AND ($4::timestamptz IS NULL OR updated_at <= $4)
ORDER BY id DESC LIMIT $5`, position, query.Filters["state"], query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.D1Resource, 0, query.PageSize+1)
	for rows.Next() {
		var id, state, instrument, side, quantity, account string
		var plan *string
		var revision int64
		var occurred time.Time
		if err = rows.Scan(&id, &state, &revision, &occurred, &instrument, &side, &quantity, &account, &plan); err != nil {
			return nil, err
		}
		items = append(items, d1Resource(id, "order", revision, state, &occurred,
			map[string]any{"instrument": instrument, "side": side, "quantity": quantity,
				"account_id": account, "plan_id": plan}, map[string]string{"self": "/api/v1/orders/" + id}))
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d1Fills(
	ctx context.Context,
	position string,
	query console.D1ListQuery,
) ([]generated.D1Resource, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id,occurred_at,order_id,exchange_id,quantity::text,price::text,fee_quantity::text,fee_asset
FROM fills WHERE ($1='' OR id<$1)
  AND ($2::timestamptz IS NULL OR occurred_at >= $2)
  AND ($3::timestamptz IS NULL OR occurred_at <= $3)
ORDER BY id DESC LIMIT $4`, position, query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.D1Resource, 0, query.PageSize+1)
	for rows.Next() {
		var id, orderID, exchange, quantity, price, fee, feeAsset string
		var occurred time.Time
		if err = rows.Scan(&id, &occurred, &orderID, &exchange, &quantity, &price, &fee, &feeAsset); err != nil {
			return nil, err
		}
		items = append(items, d1Resource(id, "fill", 1, "recorded", &occurred,
			map[string]any{"order_id": orderID, "exchange": exchange, "quantity": quantity,
				"price": price, "fee_quantity": fee, "fee_asset": feeAsset},
			map[string]string{"order": "/api/v1/orders/" + orderID}))
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d1Alerts(
	ctx context.Context,
	position string,
	query console.D1ListQuery,
) ([]generated.D1Resource, error) {
	rows, err := store.pool.Query(ctx, `
SELECT alert.id,alert.state,alert.revision,alert.created_at,
       alert.alert_type,alert.incident_id
FROM alerts alert
WHERE ($1='' OR alert.id<$1) AND ($2='' OR alert.state=$2)
  AND ($3::timestamptz IS NULL OR alert.created_at >= $3)
  AND ($4::timestamptz IS NULL OR alert.created_at <= $4)
ORDER BY alert.id DESC LIMIT $5`, position, query.Filters["state"], query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.D1Resource, 0, query.PageSize+1)
	for rows.Next() {
		var id, state, alertType string
		var incident *string
		var revision int64
		var occurred time.Time
		if err = rows.Scan(&id, &state, &revision, &occurred, &alertType, &incident); err != nil {
			return nil, err
		}
		items = append(items, d1Resource(id, "alert", revision, state, &occurred,
			map[string]any{"alert_type": alertType, "incident_id": incident},
			map[string]string{"incident": d1OptionalLink("/api/v1/incidents/", incident)}))
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d1Jobs(
	ctx context.Context,
	position string,
	query console.D1ListQuery,
	reports bool,
) ([]generated.D1Resource, error) {
	rows, err := store.pool.Query(ctx, `
SELECT job.id,job.state,job.progress_revision,job.updated_at,job.job_type,job.run_id,
job.failure_code,report.id,report.mode,report.confidence_tier,report.valuation_basis,
report.maturity,report.source_identity,report.source_revision,report.content_hash
FROM jobs job LEFT JOIN v1d_reports report ON report.job_id=job.id
WHERE ($1='' OR job.id<$1) AND ($2='' OR job.state=$2)
  AND (($3 AND job.job_type LIKE 'report:%') OR (NOT $3 AND job.job_type NOT LIKE 'report:%'))
  AND ($4::timestamptz IS NULL OR job.updated_at >= $4)
  AND ($5::timestamptz IS NULL OR job.updated_at <= $5)
ORDER BY job.id DESC LIMIT $6`, position, query.Filters["state"], reports, query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.D1Resource, 0, query.PageSize+1)
	for rows.Next() {
		var id, state, jobType string
		var runID, failure *string
		var reportID, mode, confidence, valuation, maturity, source, contentHash *string
		var sourceRevision *int64
		var revision int64
		var occurred time.Time
		if err = rows.Scan(&id, &state, &revision, &occurred, &jobType, &runID, &failure,
			&reportID, &mode, &confidence, &valuation, &maturity, &source, &sourceRevision,
			&contentHash); err != nil {
			return nil, err
		}
		kind := "lab_run"
		if reports {
			kind = "report"
		}
		attributes := map[string]any{"job_type": jobType, "run_id": runID,
			"failure_code": failure, "report_id": reportID, "mode": mode,
			"confidence_tier": confidence, "valuation_basis": valuation,
			"maturity": maturity, "source_identity": source,
			"source_revision": sourceRevision, "content_hash": contentHash}
		links := map[string]string{"command": "/api/v1/commands/" + id}
		if reportID != nil {
			links["report"] = "/api/v1/reports/" + *reportID
		}
		items = append(items, d1Resource(id, kind, revision, state, &occurred, attributes, links))
	}
	return items, rows.Err()
}
