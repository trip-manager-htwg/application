ALTER TABLE newsletters ENABLE ROW LEVEL SECURITY;
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies
        WHERE tablename = 'newsletters' AND policyname = 'tenant_isolation_newsletters'
    ) THEN
        CREATE POLICY tenant_isolation_newsletters ON newsletters
            USING (tenant_id = current_setting('app.tenant_id', true));
    END IF;
END $$;