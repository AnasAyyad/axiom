SET TIME ZONE 'UTC';

-- A strategy plan may use a fresh account snapshot to prove owned inventory.
-- Preserve its immutable identity with the plan so later reconciliation and
-- review can verify the proof without retaining a second private payload.
CREATE TABLE v1c_plan_account_snapshots (
  plan_id text NOT NULL REFERENCES v1c_submission_plans(id),
  account_id text NOT NULL,
  account_epoch bigint NOT NULL CHECK (account_epoch > 0),
  snapshot_hash sha256_hex NOT NULL,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (plan_id,account_id),
  FOREIGN KEY (account_id,account_epoch,snapshot_hash)
    REFERENCES v1c_account_snapshots(account_id,account_epoch,snapshot_hash)
);

CREATE TRIGGER v1c_plan_account_snapshots_immutable
  BEFORE UPDATE OR DELETE ON v1c_plan_account_snapshots
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
