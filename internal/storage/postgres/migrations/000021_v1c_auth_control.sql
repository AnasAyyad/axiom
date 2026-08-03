SET TIME ZONE 'UTC';

INSERT INTO authorization_permissions(id, description) VALUES
  ('sandbox.read', 'Read redacted sandbox state'),
  ('sandbox.arm', 'Arm bounded sandbox submission'),
  ('sandbox.cancel', 'Cancel sandbox orders in every safety state'),
  ('sandbox.admin', 'Administer sandbox risk, credentials, and sessions')
ON CONFLICT (id) DO NOTHING;

INSERT INTO role_permissions(role_id, permission_id, granted_at)
SELECT 'owner', id, CURRENT_TIMESTAMP
FROM authorization_permissions
WHERE id IN ('sandbox.read','sandbox.arm','sandbox.cancel','sandbox.admin')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions(role_id, permission_id, granted_at)
VALUES ('viewer','sandbox.read',CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING;

CREATE TABLE v1c_totp_replay_state (
  user_id text PRIMARY KEY REFERENCES users(id),
  last_used_counter bigint NOT NULL DEFAULT -1 CHECK (last_used_counter >= -1),
  updated_at timestamptz NOT NULL
);

CREATE TABLE v1c_sandbox_authorizations (
  id text PRIMARY KEY,
  token_hash sha256_hex NOT NULL UNIQUE,
  user_id text NOT NULL REFERENCES users(id),
  session_id text NOT NULL REFERENCES sessions(id),
  purpose text NOT NULL CHECK (purpose IN (
    'sandbox_arm','risk_unlock','credential_rotation','revoke_all_sessions'
  )),
  totp_counter bigint NOT NULL CHECK (totp_counter >= 0),
  session_revision bigint NOT NULL CHECK (session_revision > 0),
  source_hash sha256_hex NOT NULL,
  reason_hash sha256_hex NOT NULL,
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  CHECK (expires_at = created_at + interval '2 minutes'),
  CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);
CREATE INDEX v1c_sandbox_authorizations_session_idx
  ON v1c_sandbox_authorizations(session_id, expires_at)
  WHERE consumed_at IS NULL;

CREATE FUNCTION enforce_v1c_authorization_session() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM sessions session
    JOIN users actor ON actor.id=session.user_id
    WHERE session.id=NEW.session_id
      AND session.user_id=NEW.user_id
      AND session.revision=NEW.session_revision
      AND actor.status='active'
      AND session.revoked_at IS NULL
      AND session.expires_at>NEW.created_at
      AND session.idle_expires_at>NEW.created_at
    FOR SHARE OF session,actor
  ) THEN
    RAISE EXCEPTION 'v1c_authorization_session_invalid';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_authorization_session_active
  BEFORE INSERT ON v1c_sandbox_authorizations
  FOR EACH ROW EXECUTE FUNCTION enforce_v1c_authorization_session();

CREATE TABLE v1c_high_risk_audit_events (
  id text PRIMARY KEY,
  chain_sequence bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
  actor_user_id text NOT NULL REFERENCES users(id),
  session_id text NOT NULL REFERENCES sessions(id),
  purpose text NOT NULL CHECK (purpose IN (
    'sandbox_arm','risk_unlock','credential_rotation','revoke_all_sessions'
  )),
  outcome text NOT NULL,
  source_hash sha256_hex NOT NULL,
  reason_hash sha256_hex NOT NULL,
  revision bigint NOT NULL CHECK (revision > 0),
  before_hash sha256_hex,
  after_hash sha256_hex,
  previous_hash sha256_hex,
  event_hash sha256_hex NOT NULL UNIQUE,
  occurred_at timestamptz NOT NULL
);
CREATE INDEX v1c_high_risk_audit_actor_idx
  ON v1c_high_risk_audit_events(actor_user_id, occurred_at, id);

CREATE TABLE v1c_session_control_events (
  id text PRIMARY KEY,
  actor_user_id text NOT NULL REFERENCES users(id),
  actor_session_id text NOT NULL REFERENCES sessions(id),
  authorization_id text UNIQUE REFERENCES v1c_sandbox_authorizations(id),
  control_kind text NOT NULL CHECK (control_kind IN (
    'revoke_all','privilege_changed','credential_rotation'
  )),
  source_hash sha256_hex,
  reason_hash sha256_hex NOT NULL,
  affected_sessions bigint NOT NULL CHECK (affected_sessions >= 0),
  occurred_at timestamptz NOT NULL,
  CHECK (
    (control_kind='revoke_all' AND authorization_id IS NOT NULL AND source_hash IS NOT NULL) OR
    (control_kind<>'revoke_all' AND authorization_id IS NULL AND source_hash IS NULL)
  )
);

CREATE FUNCTION enforce_v1c_revoke_all_authorization() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
  authorization_user text;
  authorization_session text;
  authorization_purpose text;
  authorization_source sha256_hex;
  authorization_reason sha256_hex;
  authorization_expires timestamptz;
  authorization_consumed timestamptz;
BEGIN
  IF NEW.control_kind<>'revoke_all' THEN
    RETURN NEW;
  END IF;
  SELECT user_id,session_id,purpose,source_hash,reason_hash,expires_at,consumed_at
  INTO authorization_user,authorization_session,authorization_purpose,
       authorization_source,authorization_reason,authorization_expires,
       authorization_consumed
  FROM v1c_sandbox_authorizations
  WHERE id=NEW.authorization_id
  FOR SHARE;

  IF authorization_purpose IS DISTINCT FROM 'revoke_all_sessions' OR
     authorization_user IS DISTINCT FROM NEW.actor_user_id OR
     authorization_session IS DISTINCT FROM NEW.actor_session_id OR
     authorization_source IS DISTINCT FROM NEW.source_hash OR
     authorization_reason IS DISTINCT FROM NEW.reason_hash OR
     authorization_consumed IS NULL OR
     NEW.occurred_at < authorization_consumed OR
     NEW.occurred_at > authorization_expires THEN
    RAISE EXCEPTION 'v1c_revoke_all_authorization_invalid';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER v1c_revoke_all_authorized
  BEFORE INSERT ON v1c_session_control_events
  FOR EACH ROW EXECUTE FUNCTION enforce_v1c_revoke_all_authorization();

CREATE FUNCTION protect_v1c_auth_evidence() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'v1c_auth_evidence_immutable';
  END IF;
  IF TG_TABLE_NAME = 'v1c_sandbox_authorizations' AND
     (to_jsonb(NEW) - 'consumed_at') IS NOT DISTINCT FROM (to_jsonb(OLD) - 'consumed_at') AND
     OLD.consumed_at IS NULL AND NEW.consumed_at IS NOT NULL THEN
    RETURN NEW;
  END IF;
  RAISE EXCEPTION 'v1c_auth_evidence_immutable';
END;
$$;

CREATE TRIGGER v1c_authorizations_protected
  BEFORE UPDATE OR DELETE ON v1c_sandbox_authorizations
  FOR EACH ROW EXECUTE FUNCTION protect_v1c_auth_evidence();
CREATE TRIGGER v1c_high_risk_audit_immutable
  BEFORE UPDATE OR DELETE ON v1c_high_risk_audit_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
CREATE TRIGGER v1c_session_control_immutable
  BEFORE UPDATE OR DELETE ON v1c_session_control_events
  FOR EACH ROW EXECUTE FUNCTION reject_immutable_mutation();
