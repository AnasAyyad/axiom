package postgres

import (
	"context"
	"fmt"
)

// Posture returns the durable session command, global risk, and fresh storage posture.
func (store *PublicShadowStore) Posture(ctx context.Context, id string) (PublicShadowPosture, error) {
	var posture PublicShadowPosture
	err := store.pool.QueryRow(ctx, `SELECT ss.state,coalesce(
	  (SELECT next_state FROM risk_state_events ORDER BY entity_revision DESC LIMIT 1),'PAUSED'),coalesce(
	  (SELECT CASE WHEN observed_at>=CURRENT_TIMESTAMP-interval '2 minutes'
	    AND source_instance<>'migration-bootstrap' THEN level ELSE 'CRITICAL' END
	    FROM owner_console_storage_pressure_state WHERE scope_id='market-data'),'CRITICAL')
      FROM shadow_sessions ss WHERE ss.id=$1 AND ss.claim_owner=$2`, id, store.owner).
		Scan(&posture.State, &posture.RiskState, &posture.StoragePressure)
	if err != nil {
		return PublicShadowPosture{}, fmt.Errorf("owner_console_shadow_posture_unavailable")
	}
	return posture, nil
}
