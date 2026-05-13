-- Rewrite every FK that targets tenants(id) so that ON DELETE CASCADE is set.
-- Up to migration 000057 most tenant_id columns were added via
-- `ALTER TABLE … ADD COLUMN tenant_id UUID … REFERENCES tenants(id)` (no
-- cascade clause), which leaves the FK with the default RESTRICT semantics.
-- Hard-deleting a tenant for trial-cleanup (see spec: trial-cleanup) was
-- therefore impossible without manually purging every child table first.
--
-- This migration sweeps all such constraints in one pg_constraint pass so
-- the change stays correct as new tenant-scoped tables are added without
-- a follow-up migration. Existing CASCADE constraints are rewritten to
-- CASCADE again — that's a no-op semantically and keeps the loop simple.
-- The constraint name is preserved so any downstream introspection that
-- knows it by name keeps working.
--
-- Numbered 099000 (reserved fork-only block, see LOCAL_PATCHES.md) so it
-- never collides with future upstream migrations 000058..000999.
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
       AND con.confdeltype <> 'c'
  LOOP
    EXECUTE format('ALTER TABLE %I DROP CONSTRAINT %I', rec.table_name, rec.conname);
    -- pg_get_constraintdef returns e.g. `FOREIGN KEY (tenant_id) REFERENCES tenants(id)`;
    -- we append ON DELETE CASCADE and re-add under the same name.
    EXECUTE format(
      'ALTER TABLE %I ADD CONSTRAINT %I %s ON DELETE CASCADE',
      rec.table_name, rec.conname, rec.def
    );
  END LOOP;
END $$;
