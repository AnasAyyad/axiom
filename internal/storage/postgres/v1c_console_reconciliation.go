package postgres

import (
	"context"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

const v1cConsoleReconciliationsSQL = `
SELECT reconciliation.id,reconciliation.account_id,account.exchange,
       reconciliation.account_epoch,reconciliation.state,
       reconciliation.reconciled_at
FROM v1c_reconciliations reconciliation
JOIN v1c_exchange_accounts account
  ON account.id=reconciliation.account_id
WHERE ($1='' OR account.exchange=$1)
  AND ($2::timestamptz IS NULL OR
       (reconciliation.reconciled_at,reconciliation.id)<($2,$3))
ORDER BY reconciliation.reconciled_at DESC,reconciliation.id DESC
LIMIT $4`

// SandboxReconciliations returns exact difference, suspense, quarantine, and
// reset evidence without exposing exchange-private payloads.
func (store *A11ConsoleStore) SandboxReconciliations(
	ctx context.Context,
	cursor string,
	limit int,
	exchangeFilter string,
) (generated.SandboxReconciliationPage, error) {
	if limit < 1 || limit > 200 {
		return generated.SandboxReconciliationPage{}, console.ErrInvalidRequest
	}
	cursorTime, cursorID, _, err := decodeA11TimeCursor(
		store.cursor, v1cConsoleReconciliationScope, cursor,
	)
	if err != nil {
		return generated.SandboxReconciliationPage{}, err
	}
	rows, err := store.pool.Query(
		ctx, v1cConsoleReconciliationsSQL,
		exchangeFilter, nullableTime(cursorTime), cursorID, limit+1,
	)
	if err != nil {
		return generated.SandboxReconciliationPage{}, err
	}
	defer rows.Close()
	items, err := store.scanV1CConsoleReconciliations(ctx, rows, limit+1)
	if err != nil {
		return generated.SandboxReconciliationPage{}, err
	}
	resets, err := store.v1cConsoleResetIncidents(ctx, exchangeFilter)
	if err != nil {
		return generated.SandboxReconciliationPage{}, err
	}
	return store.c6ReconciliationPage(items, resets, limit), nil
}

func (store *A11ConsoleStore) scanV1CConsoleReconciliations(
	ctx context.Context,
	rows pgx.Rows,
	capacity int,
) ([]generated.SandboxReconciliation, error) {
	items := make([]generated.SandboxReconciliation, 0, capacity)
	for rows.Next() {
		var item generated.SandboxReconciliation
		var state string
		if err := rows.Scan(
			&item.Id, &item.AccountId, &item.Exchange, &item.AccountEpoch,
			&state, &item.ReconciledAt,
		); err != nil {
			return nil, err
		}
		item.State = generated.SandboxReconciliationState(state)
		item.ReconciledAt = item.ReconciledAt.UTC()
		item.AuditUrl = "/api/v1/audit-events?event_type=v1c_reconciliation"
		differences, err := store.v1cConsoleDifferences(ctx, item.Id)
		if err != nil {
			return nil, err
		}
		item.Differences = differences
		countC6DifferenceStates(&item)
		items = append(items, item)
	}
	return items, rows.Err()
}

func countC6DifferenceStates(item *generated.SandboxReconciliation) {
	for _, difference := range item.Differences {
		if difference.State == "OPEN" || difference.State == "ADJUSTED" {
			item.SuspenseCount++
		}
		if difference.State == "QUARANTINED" {
			item.QuarantineCount++
		}
	}
}

func (store *A11ConsoleStore) c6ReconciliationPage(
	items []generated.SandboxReconciliation,
	resets []generated.SandboxResetIncident,
	limit int,
) generated.SandboxReconciliationPage {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var next *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		value := encodeA11TimeCursor(
			store.cursor, v1cConsoleReconciliationScope,
			last.ReconciledAt, last.Id,
		)
		next = &value
	}
	revision := "0"
	if len(items) > 0 {
		revision = strconv.FormatInt(items[0].ReconciledAt.UnixNano(), 10)
	}
	return generated.SandboxReconciliationPage{
		Items: items, ResetIncidents: resets, HasMore: hasMore,
		NextCursor: next, Revision: revision,
	}
}

func (store *A11ConsoleStore) v1cConsoleDifferences(
	ctx context.Context,
	reconciliationID string,
) ([]generated.SandboxDifference, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id,category,classification,asset_symbol,quantity::text,
       critical,state,recorded_at
FROM v1c_reconciliation_differences
WHERE reconciliation_id=$1
ORDER BY recorded_at,id`, reconciliationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]generated.SandboxDifference, 0)
	for rows.Next() {
		var item generated.SandboxDifference
		var asset, quantity *string
		var state string
		if err = rows.Scan(
			&item.Id,
			&item.Category,
			&item.Classification,
			&asset,
			&quantity,
			&item.Critical,
			&state,
			&item.RecordedAt,
		); err != nil {
			return nil, err
		}
		if asset != nil {
			value := generated.SandboxDifferenceAsset(*asset)
			item.Asset = &value
		}
		if quantity != nil {
			value := generated.Decimal(*quantity)
			item.Quantity = &value
		}
		item.State = generated.SandboxDifferenceState(state)
		item.RecordedAt = item.RecordedAt.UTC()
		item.AuditUrl = "/api/v1/audit-events?event_type=v1c_difference"
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *A11ConsoleStore) v1cConsoleResetIncidents(
	ctx context.Context,
	exchangeFilter string,
) ([]generated.SandboxResetIncident, error) {
	rows, err := store.pool.Query(ctx, `
SELECT incident.id,incident.account_id,account.exchange,
       incident.prior_epoch,incident.new_epoch,incident.state,
       incident.detected_at,incident.resolved_at
FROM v1c_reset_incidents incident
JOIN v1c_exchange_accounts account ON account.id=incident.account_id
WHERE ($1='' OR account.exchange=$1)
ORDER BY incident.detected_at DESC,incident.id DESC
LIMIT 50`, exchangeFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]generated.SandboxResetIncident, 0)
	for rows.Next() {
		var item generated.SandboxResetIncident
		var state string
		if err = rows.Scan(
			&item.Id,
			&item.AccountId,
			&item.Exchange,
			&item.PriorEpoch,
			&item.NewEpoch,
			&state,
			&item.DetectedAt,
			&item.ResolvedAt,
		); err != nil {
			return nil, err
		}
		item.State = generated.SandboxResetIncidentState(state)
		item.DetectedAt = item.DetectedAt.UTC()
		item.ResolvedAt = utcPointer(item.ResolvedAt)
		item.AuditUrl = "/api/v1/audit-events?event_type=v1c_reset"
		item.Adjustments, err = store.v1cConsoleAdjustments(ctx, item.Id)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *A11ConsoleStore) v1cConsoleAdjustments(
	ctx context.Context,
	incidentID string,
) ([]struct {
	Asset      generated.SandboxResetIncidentAdjustmentsAsset     `json:"asset"`
	PnlEffect  generated.SandboxResetIncidentAdjustmentsPnlEffect `json:"pnl_effect"`
	Quantity   generated.Decimal                                  `json:"quantity"`
	RecordedAt time.Time                                          `json:"recorded_at"`
}, error) {
	rows, err := store.pool.Query(ctx, `
SELECT asset_symbol,quantity::text,pnl_effect,recorded_at
FROM v1c_external_adjustments
WHERE reset_incident_id=$1
ORDER BY recorded_at,id`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]struct {
		Asset      generated.SandboxResetIncidentAdjustmentsAsset     `json:"asset"`
		PnlEffect  generated.SandboxResetIncidentAdjustmentsPnlEffect `json:"pnl_effect"`
		Quantity   generated.Decimal                                  `json:"quantity"`
		RecordedAt time.Time                                          `json:"recorded_at"`
	}, 0)
	for rows.Next() {
		var item struct {
			Asset      generated.SandboxResetIncidentAdjustmentsAsset     `json:"asset"`
			PnlEffect  generated.SandboxResetIncidentAdjustmentsPnlEffect `json:"pnl_effect"`
			Quantity   generated.Decimal                                  `json:"quantity"`
			RecordedAt time.Time                                          `json:"recorded_at"`
		}
		if err = rows.Scan(
			&item.Asset,
			&item.Quantity,
			&item.PnlEffect,
			&item.RecordedAt,
		); err != nil {
			return nil, err
		}
		item.RecordedAt = item.RecordedAt.UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}
