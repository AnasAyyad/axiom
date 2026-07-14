INSERT INTO exchanges (id, name, environment)
VALUES ('binance', 'Binance', 'production_public')
ON CONFLICT (id) DO NOTHING;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM exchanges
    WHERE id = 'binance' AND name = 'Binance' AND environment = 'production_public'
  ) THEN
    RAISE EXCEPTION 'binance_public_reference_conflict';
  END IF;
END;
$$;
