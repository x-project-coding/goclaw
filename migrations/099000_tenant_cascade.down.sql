-- Reverse the cascade sweep: rewrite every FK targeting tenants(id) back to
-- the default RESTRICT semantics. Note: a small handful of constraints were
-- explicitly created with ON DELETE CASCADE before migration 000058 (e.g.
-- builtin_tool_tenant_configs, skill_tenant_configs). This down migration
-- treats every tenant FK uniformly — they all become RESTRICT — because (a)
-- down migrations are dev-only paths and (b) leaving heterogeneous cascade
-- state across re-runs would be a worse problem than this minor drift.
DO $$
DECLARE
  rec RECORD;
BEGIN
  FOR rec IN
    SELECT con.conname,
           cls.relname AS table_name,
           pg_get_constraintdef(con.oid) AS def
      FROM pg_constraint con
      JOIN pg_class cls ON cls.oid = con.conrelid
     WHERE con.contype = 'f'
       AND con.confrelid = 'tenants'::regclass
       AND con.confdeltype = 'c'
  LOOP
    EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', rec.table_name, rec.conname);
    -- Strip a trailing `ON DELETE CASCADE` so the re-added constraint defaults
    -- to RESTRICT. pg_get_constraintdef writes it canonically as the suffix.
    EXECUTE format(
      'ALTER TABLE %I ADD CONSTRAINT %I %s',
      rec.table_name,
      rec.conname,
      regexp_replace(rec.def, '\s+ON DELETE CASCADE\s*$', '')
    );
  END LOOP;
END $$;
