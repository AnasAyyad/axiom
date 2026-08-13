package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"
)

const strategySessionConfigurationSQL = `
SELECT configuration.id,configuration.configuration_hash::text,
       configuration.canonical_payload
FROM sandbox_strategy_sessions strategy
JOIN sandbox_runtime_sandbox_sessions parent
  ON parent.id=strategy.sandbox_session_id
JOIN configuration_versions configuration
  ON configuration.id=parent.configuration_id
JOIN sandbox_runtime_sandbox_arms arm
  ON arm.sandbox_session_id=parent.id
WHERE strategy.id=$1
  AND strategy.state='running'
  AND strategy.revision=$2
  AND parent.state='ARMED'
  AND parent.revision=$3
  AND parent.configuration_id=$4
  AND configuration.configuration_hash=$5
  AND parent.strategy_set_hash=$6
  AND arm.id=$7
  AND arm.revision=$8
  AND arm.revoked_at IS NULL
  AND arm.expires_at>$9`

// StrategySessionConfiguration returns the exact immutable configuration for
// a scheduled session only while its parent, child, and arm revisions still
// match. It performs no market-data, credential, or exchange operation.
func (store *SandboxRuntimeDispatcherStore) StrategySessionConfiguration(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	now time.Time,
) (sandbox.StrategySessionConfiguration, error) {
	if store == nil || work.ValidAt(now) != nil || now.IsZero() || now.Location() != time.UTC {
		return sandbox.StrategySessionConfiguration{}, fmt.Errorf("sandbox_strategy_configuration_invalid")
	}
	var configuration sandbox.StrategySessionConfiguration
	err := store.pool.QueryRow(ctx, strategySessionConfigurationSQL,
		work.SessionID, work.StrategyRevision, work.SessionRevision,
		work.ConfigurationID, work.ConfigurationHash, work.StrategySetHash,
		work.ArmID, work.ArmRevision, now,
	).Scan(&configuration.ID, &configuration.Hash, &configuration.Payload)
	if err != nil || !configuration.ValidFor(work) {
		return sandbox.StrategySessionConfiguration{}, fmt.Errorf("sandbox_strategy_configuration_unavailable")
	}
	return configuration, nil
}

var _ sandbox.StrategySessionConfigurationSource = (*SandboxRuntimeDispatcherStore)(nil)
