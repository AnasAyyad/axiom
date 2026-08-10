package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

type projectedSandboxSagaRiskMember struct {
	projection  sandbox.StrategySagaRiskProjectionMember
	valuation   sandbox.StrategyRiskValuation
	observation sandbox.StrategyRiskObservation
	history     sandboxRiskValuationHistory
}

// ProjectStrategySagaRiskInputs derives every account valuation in one
// serializable transaction. The coordinator fence is checked only for its own
// account; every peer lease and full session membership is independently read
// and locked by the store. No peer owner or fence is accepted from the caller.
func (store *SandboxRuntimeDispatcherStore) ProjectStrategySagaRiskInputs(
	ctx context.Context,
	coordinatorLease sandbox.StrategySessionExecutionLease,
	coordinator sandbox.StrategySessionWork,
	members []sandbox.StrategySagaRiskProjectionMember,
	now time.Time,
) (*sandbox.StrategySagaRiskInputs, error) {
	if !validSandboxSagaRiskProjectionRequest(coordinatorLease, coordinator, members, now) ||
		store == nil || store.pool == nil || ctx == nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_risk_projection_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_risk_projection_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateEngineCommandLease(ctx, tx, coordinator.Account.ID,
		coordinatorLease.Owner, coordinatorLease.Fence, now); err != nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_risk_projection_fence_invalid")
	}
	projected, baselineMissing, err := projectSandboxSagaRiskMembers(ctx, tx, members, now)
	if err != nil {
		return nil, err
	}
	if baselineMissing {
		if err = insertSandboxSagaRiskBaselines(ctx, tx, projected, now); err != nil {
			return nil, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("sandbox_strategy_saga_risk_projection_commit_failed")
		}
		return nil, fmt.Errorf("sandbox_strategy_saga_risk_baseline_initialized")
	}
	aggregate, err := buildSandboxSagaRiskInputs(projected, now)
	if err != nil {
		return nil, err
	}
	if err = insertSandboxSagaRiskEvidence(ctx, tx, projected, now); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_risk_projection_commit_failed")
	}
	return aggregate, nil
}

func projectSandboxSagaRiskMembers(ctx context.Context, tx pgx.Tx,
	members []sandbox.StrategySagaRiskProjectionMember, now time.Time,
) ([]projectedSandboxSagaRiskMember, bool, error) {
	projected := make([]projectedSandboxSagaRiskMember, 0, len(members))
	baselineMissing := false
	for _, member := range members {
		if err := validateSandboxSagaRiskMemberCurrent(ctx, tx, member.Admission, now); err != nil {
			return nil, false, err
		}
		valuation, history, err := buildSandboxRiskProjection(ctx, tx, member.Admission,
			member.Snapshot, member.Market, member.Facts, now)
		if err != nil {
			return nil, false, err
		}
		baselineMissing = baselineMissing || !history.Ready
		projected = append(projected, projectedSandboxSagaRiskMember{
			projection: member, valuation: valuation, history: history})
	}
	return projected, baselineMissing, nil
}

func insertSandboxSagaRiskBaselines(ctx context.Context, tx pgx.Tx,
	members []projectedSandboxSagaRiskMember, now time.Time,
) error {
	for _, member := range members {
		if !member.history.Ready {
			if err := insertSandboxRiskValuation(ctx, tx, member.valuation, "", now); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildSandboxSagaRiskInputs(projected []projectedSandboxSagaRiskMember,
	now time.Time,
) (*sandbox.StrategySagaRiskInputs, error) {
	riskMembers := make([]sandbox.StrategySagaRiskMember, 0, len(projected))
	for index := range projected {
		member := &projected[index]
		observation, err := member.valuation.Observation(member.projection.Admission.Work,
			member.projection.Snapshot, member.projection.Market, member.projection.Facts,
			member.projection.Admission, now)
		if err != nil {
			return nil, fmt.Errorf("sandbox_strategy_saga_risk_projection_invalid")
		}
		member.observation = observation
		riskMembers = append(riskMembers, sandbox.StrategySagaRiskMember{
			Work: member.projection.Admission.Work, Snapshot: member.projection.Snapshot,
			Market: member.projection.Market, Facts: member.projection.Facts, Observation: observation})
	}
	aggregate, err := sandbox.NewStrategySagaRiskInputs(riskMembers, now)
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_saga_risk_projection_invalid")
	}
	return aggregate, nil
}

func insertSandboxSagaRiskEvidence(ctx context.Context, tx pgx.Tx,
	members []projectedSandboxSagaRiskMember, now time.Time,
) error {
	for _, member := range members {
		observationID, err := insertSandboxStrategyRiskObservation(ctx, tx,
			member.projection.Admission.Work, member.projection.Snapshot, member.projection.Facts,
			member.observation, now)
		if err != nil {
			return err
		}
		if err = insertSandboxRiskValuation(ctx, tx, member.valuation, observationID, now); err != nil {
			return err
		}
	}
	return nil
}

func validSandboxSagaRiskProjectionRequest(
	lease sandbox.StrategySessionExecutionLease,
	coordinator sandbox.StrategySessionWork,
	members []sandbox.StrategySagaRiskProjectionMember,
	now time.Time,
) bool {
	want := 1
	if coordinator.Strategy == sandbox.StrategyCrossExchangeArbitrage {
		want = 2
		if coordinator.Account.Exchange != sandbox.ExchangeBinance {
			return false
		}
	} else if coordinator.Strategy != sandbox.StrategyTriangular {
		return false
	}
	if coordinator.ValidAt(now) != nil || lease.ValidFor(coordinator) != nil || len(members) != want {
		return false
	}
	exchanges, valid := validSandboxSagaRiskMembers(coordinator, members, now)
	if !valid {
		return false
	}
	if want == 2 {
		_, binance := exchanges[sandbox.ExchangeBinance]
		_, bybit := exchanges[sandbox.ExchangeBybit]
		return binance && bybit
	}
	return true
}

func validSandboxSagaRiskMembers(coordinator sandbox.StrategySessionWork,
	members []sandbox.StrategySagaRiskProjectionMember, now time.Time,
) (map[sandbox.Exchange]struct{}, bool) {
	accounts := make(map[sandbox.AccountID]struct{}, len(members))
	exchanges := make(map[sandbox.Exchange]struct{}, len(members))
	coordinatorFound := false
	for _, member := range members {
		work := member.Admission.Work
		if member.Admission.Valid() != nil || !member.Admission.ApprovedAt.Equal(now) ||
			work.ValidAt(now) != nil || !sameSandboxSagaRiskTopology(coordinator, work) ||
			member.Snapshot.Validate() != nil || member.Facts.ValidFor(work, member.Snapshot, now) != nil ||
			member.Market.Instrument.Symbol() != work.Instrument ||
			sandbox.StrategyMarketEvidenceHash(member.Market) == "" ||
			string(member.Market.Book.Exchange) != string(work.Account.Exchange) {
			return nil, false
		}
		if _, duplicate := accounts[work.Account.ID]; duplicate {
			return nil, false
		}
		if _, duplicate := exchanges[work.Account.Exchange]; duplicate {
			return nil, false
		}
		accounts[work.Account.ID] = struct{}{}
		exchanges[work.Account.Exchange] = struct{}{}
		coordinatorFound = coordinatorFound || work == coordinator
	}
	return exchanges, coordinatorFound
}

func sameSandboxSagaRiskTopology(expected, actual sandbox.StrategySessionWork) bool {
	return actual.SessionID == expected.SessionID && actual.Strategy == expected.Strategy &&
		actual.Instrument == expected.Instrument && actual.ConfigurationID == expected.ConfigurationID &&
		actual.ConfigurationHash == expected.ConfigurationHash && actual.StrategySetHash == expected.StrategySetHash &&
		actual.SessionRevision == expected.SessionRevision && actual.StrategyRevision == expected.StrategyRevision &&
		actual.ArmID == expected.ArmID && actual.ArmRevision == expected.ArmRevision &&
		actual.StartedAt.Equal(expected.StartedAt) && actual.ArmExpiresAt.Equal(expected.ArmExpiresAt)
}

func validateSandboxSagaRiskMemberCurrent(
	ctx context.Context,
	tx pgx.Tx,
	admission sandbox.StrategySessionAdmission,
	now time.Time,
) error {
	work := admission.Work
	var current bool
	err := tx.QueryRow(ctx, sandboxSagaRiskMemberCurrentSQL,
		work.SessionID, work.Strategy, work.Instrument, work.ConfigurationID,
		work.ConfigurationHash, work.StrategySetHash, work.SessionRevision,
		work.StrategyRevision, work.ArmID, work.ArmRevision, work.Account.ID,
		work.Account.Epoch, work.Account.Exchange, now, work.ArmExpiresAt, work.StartedAt,
	).Scan(&current)
	if err != nil || !current {
		return fmt.Errorf("sandbox_strategy_saga_risk_member_stale")
	}
	return nil
}

const sandboxSagaRiskMemberCurrentSQL = `
SELECT EXISTS(
  SELECT 1
  FROM sandbox_strategy_sessions strategy
  JOIN sandbox_runtime_sandbox_sessions parent
    ON parent.id=strategy.sandbox_session_id
  JOIN configuration_versions configuration
    ON configuration.id=parent.configuration_id
  JOIN sandbox_strategy_session_accounts membership
    ON membership.strategy_session_id=strategy.id
  JOIN sandbox_runtime_exchange_accounts account
    ON account.id=membership.account_id
   AND account.current_epoch=membership.account_epoch
   AND account.exchange=membership.exchange
  JOIN sandbox_runtime_account_leases lease
    ON lease.account_id=account.id
   AND lease.environment=account.environment
  JOIN sandbox_runtime_sandbox_arms arm
    ON arm.sandbox_session_id=parent.id
  WHERE strategy.id=$1
    AND strategy.strategy_id=$2
    AND strategy.instrument=$3
    AND parent.configuration_id=$4
    AND configuration.configuration_hash=$5
    AND parent.strategy_set_hash=$6
    AND parent.revision=$7
    AND strategy.revision=$8
    AND arm.id=$9
    AND arm.revision=$10
    AND membership.account_id=$11
    AND membership.account_epoch=$12
    AND membership.exchange=$13
    AND parent.state='ARMED'
    AND strategy.state='running'
    AND arm.revoked_at IS NULL
    AND arm.created_at<=$14
    AND arm.expires_at=$15
    AND arm.expires_at>$14
    AND strategy.started_at=$16
    AND lease.expires_at>$14
)`

var _ sandbox.StrategySagaRiskObservationProjector = (*SandboxRuntimeDispatcherStore)(nil)
