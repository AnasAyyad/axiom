SET TIME ZONE 'UTC';

ALTER TABLE reservations ADD COLUMN remaining_quantity financial_amount;
DROP TRIGGER reservations_follow_state_machine ON reservations;

UPDATE reservations
SET remaining_quantity = CASE WHEN state IN ('active','quarantined') THEN quantity ELSE 0 END;

ALTER TABLE reservations ALTER COLUMN remaining_quantity SET NOT NULL;
ALTER TABLE reservations ADD CONSTRAINT reservations_remaining_quantity_valid CHECK (
  remaining_quantity >= 0 AND remaining_quantity <= quantity AND
  ((state IN ('active','quarantined') AND remaining_quantity > 0) OR
   (state IN ('consumed','released','expired') AND remaining_quantity = 0))
);

CREATE OR REPLACE FUNCTION enforce_reservation_transition() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    IF NEW.state <> 'active' OR NEW.revision <> 1 OR NEW.remaining_quantity <> NEW.quantity OR
       NEW.updated_at < NEW.created_at THEN
      RAISE EXCEPTION 'invalid_reservation_initial_state';
    END IF;
    RETURN NEW;
  END IF;
  IF (to_jsonb(NEW) - 'state' - 'remaining_quantity' - 'revision' - 'updated_at') IS DISTINCT FROM
     (to_jsonb(OLD) - 'state' - 'remaining_quantity' - 'revision' - 'updated_at') OR
     OLD.state <> 'active' OR NEW.revision <> OLD.revision + 1 OR NEW.updated_at < OLD.updated_at THEN
    RAISE EXCEPTION 'invalid_reservation_transition';
  END IF;
  IF NEW.state = 'active' THEN
    IF NEW.remaining_quantity <= 0 OR NEW.remaining_quantity >= OLD.remaining_quantity THEN
      RAISE EXCEPTION 'invalid_reservation_partial_fill';
    END IF;
  ELSIF NEW.state = 'quarantined' THEN
    IF NEW.remaining_quantity <> OLD.remaining_quantity THEN
      RAISE EXCEPTION 'invalid_reservation_quarantine';
    END IF;
  ELSIF NEW.state NOT IN ('consumed','released','expired') OR NEW.remaining_quantity <> 0 THEN
    RAISE EXCEPTION 'invalid_reservation_transition';
  END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER reservations_follow_state_machine
  BEFORE INSERT OR UPDATE ON reservations
  FOR EACH ROW EXECUTE FUNCTION enforce_reservation_transition();
