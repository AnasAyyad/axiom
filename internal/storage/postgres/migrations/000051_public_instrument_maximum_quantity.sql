SET TIME ZONE 'UTC';

-- Historical metadata rows predate preservation of the venue's exact maximum
-- Spot quantity, so they remain NULL rather than receiving a guessed value.
-- Newly fetched production-public records use a new metadata version with an
-- exact positive maximum quantity.
ALTER TABLE instrument_metadata_versions
  ADD COLUMN maximum_quantity financial_amount,
  ADD CONSTRAINT instrument_metadata_maximum_quantity_valid CHECK (
    maximum_quantity IS NULL OR (
      maximum_quantity > 0 AND maximum_quantity >= minimum_quantity
    )
  );

COMMENT ON COLUMN instrument_metadata_versions.maximum_quantity IS
  'Exact public Spot maximum quantity; NULL only for historical rows recorded before migration 51.';
