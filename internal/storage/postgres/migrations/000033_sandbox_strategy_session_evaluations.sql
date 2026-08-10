SET TIME ZONE 'UTC';

-- Immutable, redacted scheduler outcomes explain why an armed automatic
-- strategy session was waiting, evaluated, or blocked at a specific instant.
-- They contain only semantic reason tokens and evidence hashes, never market
-- payloads, exchange responses, account balances, or credentials.
CREATE TABLE sandbox_strategy_session_evaluations (
  id text PRIMARY KEY,
  strategy_session_id text NOT NULL REFERENCES sandbox_strategy_sessions(id),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  strategy_revision bigint NOT NULL CHECK (strategy_revision > 0),
  state text NOT NULL CHECK (state IN ('waiting','evaluated','blocked')),
  reason text NOT NULL CHECK (reason ~ '^[a-z0-9_]{1,96}$'),
  evidence_hash sha256_hex NOT NULL,
  occurred_at timestamptz NOT NULL,
  UNIQUE (strategy_session_id,account_id,account_epoch,evidence_hash),
  FOREIGN KEY (strategy_session_id,account_id)
    REFERENCES sandbox_strategy_session_accounts(strategy_session_id,account_id),
  FOREIGN KEY (account_id,account_epoch)
    REFERENCES v1c_account_epochs(account_id,epoch)
);

CREATE INDEX sandbox_strategy_session_evaluations_timeline_idx
  ON sandbox_strategy_session_evaluations(strategy_session_id,occurred_at,id);

CREATE TRIGGER sandbox_strategy_session_evaluations_immutable
  BEFORE UPDATE OR DELETE ON sandbox_strategy_session_evaluations
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
