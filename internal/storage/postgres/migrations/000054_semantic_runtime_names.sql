-- Give the live schema responsibility-based names while preserving every
-- historical row and every applied migration record. This migration renames
-- objects only; it never rewrites evidence payloads, hashes, or journal data.

CREATE FUNCTION pg_temp.semantic_object_name(input_name text) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE
  result text := input_name;
BEGIN
  result := replace(result, 'v1c_c6_qualification_', 'sandbox_qualification_');
  result := replace(result, 'v1c_c6_', 'sandbox_qualification_');
  result := replace(result, 'v1c_', 'sandbox_runtime_');
  result := replace(result, 'v1d_', 'owner_console_');
  result := replace(result, 'b4_', 'triangular_arbitrage_');
  result := replace(result, 'b5_', 'cross_exchange_arbitrage_');
  result := replace(result, 'b7_', 'research_promotion_');
  result := replace(result, 'b8_', 'multi_exchange_console_');
  RETURN result;
END;
$$;

-- PostgreSQL truncates identifiers to 63 bytes. Constraint, index, and trigger
-- names derived from longer responsibility names therefore need a stable hash
-- suffix so distinct historical names cannot collapse to the same identifier.
CREATE FUNCTION pg_temp.semantic_identifier_name(input_name text) RETURNS text
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE
  result text := pg_temp.semantic_object_name(input_name);
BEGIN
  IF octet_length(result) <= 63 THEN
    RETURN result;
  END IF;
  RETURN left(result, 54) || '_' || substr(md5(result), 1, 8);
END;
$$;

DO $$
DECLARE
  item record;
  semantic_name text;
BEGIN
  FOR item IN
    SELECT c.oid, c.relname, c.relkind
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public'
      AND c.relkind IN ('r', 'p', 'v', 'm', 'S')
    ORDER BY CASE c.relkind WHEN 'v' THEN 2 WHEN 'm' THEN 2 ELSE 1 END, c.relname
  LOOP
    semantic_name := pg_temp.semantic_object_name(item.relname);
    IF semantic_name = item.relname THEN
      CONTINUE;
    END IF;
    IF EXISTS (
      SELECT 1
      FROM pg_class existing
      JOIN pg_namespace existing_namespace ON existing_namespace.oid = existing.relnamespace
      WHERE existing_namespace.nspname = 'public'
        AND existing.relname = semantic_name
        AND existing.oid <> item.oid
    ) THEN
      RAISE EXCEPTION 'semantic_schema_name_conflict:%', semantic_name;
    END IF;
    EXECUTE format(
      'ALTER %s public.%I RENAME TO %I',
      CASE item.relkind
        WHEN 'v' THEN 'VIEW'
        WHEN 'm' THEN 'MATERIALIZED VIEW'
        WHEN 'S' THEN 'SEQUENCE'
        ELSE 'TABLE'
      END,
      item.relname,
      semantic_name
    );
  END LOOP;
END;
$$;

-- Rename routines before replacing their definitions. Trigger bindings are
-- OID-based, so their safety behavior remains attached throughout the change.
DO $$
DECLARE
  item record;
  semantic_name text;
BEGIN
  FOR item IN
    SELECT p.oid, p.proname, pg_get_function_identity_arguments(p.oid) AS arguments
    FROM pg_proc p
    JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = 'public' AND p.prokind = 'f'
    ORDER BY p.proname, p.oid
  LOOP
    semantic_name := pg_temp.semantic_object_name(item.proname);
    IF semantic_name <> item.proname THEN
      EXECUTE format(
        'ALTER FUNCTION public.%I(%s) RENAME TO %I',
        item.proname,
        item.arguments,
        semantic_name
      );
    END IF;
  END LOOP;
END;
$$;

DO $$
DECLARE
  item record;
  definition text;
  semantic_definition text;
BEGIN
  FOR item IN
    SELECT p.oid
    FROM pg_proc p
    JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = 'public' AND p.prokind = 'f'
    ORDER BY p.oid
  LOOP
    definition := pg_get_functiondef(item.oid);
    semantic_definition := pg_temp.semantic_object_name(definition);
    IF semantic_definition <> definition THEN
      EXECUTE semantic_definition;
    END IF;
  END LOOP;
END;
$$;

DO $$
DECLARE
  item record;
  semantic_name text;
BEGIN
  FOR item IN
    SELECT c.oid AS table_oid, c.relname AS table_name, constraint_record.conname
    FROM pg_constraint constraint_record
    JOIN pg_class c ON c.oid = constraint_record.conrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public'
    ORDER BY c.relname, constraint_record.conname
  LOOP
    semantic_name := pg_temp.semantic_identifier_name(item.conname);
    IF semantic_name <> item.conname THEN
      EXECUTE format(
        'ALTER TABLE public.%I RENAME CONSTRAINT %I TO %I',
        item.table_name,
        item.conname,
        semantic_name
      );
    END IF;
  END LOOP;
END;
$$;

DO $$
DECLARE
  item record;
  semantic_name text;
BEGIN
  FOR item IN
    SELECT c.relname
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public' AND c.relkind = 'i'
    ORDER BY c.relname
  LOOP
    semantic_name := pg_temp.semantic_identifier_name(item.relname);
    IF semantic_name <> item.relname THEN
      EXECUTE format('ALTER INDEX public.%I RENAME TO %I', item.relname, semantic_name);
    END IF;
  END LOOP;
END;
$$;

DO $$
DECLARE
  item record;
  semantic_name text;
BEGIN
  FOR item IN
    SELECT c.relname AS table_name, trigger_record.tgname
    FROM pg_trigger trigger_record
    JOIN pg_class c ON c.oid = trigger_record.tgrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public' AND NOT trigger_record.tgisinternal
    ORDER BY c.relname, trigger_record.tgname
  LOOP
    semantic_name := pg_temp.semantic_identifier_name(item.tgname);
    IF semantic_name <> item.tgname THEN
      EXECUTE format(
        'ALTER TRIGGER %I ON public.%I RENAME TO %I',
        item.tgname,
        item.table_name,
        semantic_name
      );
    END IF;
  END LOOP;
END;
$$;

-- Existing installations may still have the historical qualification login.
-- Renaming a login requires role administration; fail closed rather than
-- silently leaving two identities or selecting one ambiguously.
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'axiom_c6_qualification')
     AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'axiom_sandbox_qualification') THEN
    RAISE EXCEPTION 'semantic_role_name_conflict:axiom_sandbox_qualification';
  ELSIF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'axiom_c6_qualification') THEN
    IF NOT EXISTS (
      SELECT 1 FROM pg_roles
      WHERE rolname = current_user AND (rolsuper OR rolcreaterole)
    ) THEN
      RAISE EXCEPTION 'semantic_role_rename_requires_database_administrator:axiom_c6_qualification';
    END IF;
    ALTER ROLE axiom_c6_qualification RENAME TO axiom_sandbox_qualification;
  END IF;
END;
$$;
