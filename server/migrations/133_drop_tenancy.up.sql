-- The local edition has one user and no tenants, so the multi-tenant layer
-- added in 114 goes: row-level security, every tenant_id column and every
-- tenant-scoped key, plus the tenants registry and the unused member and
-- owner columns. Irreversible; see the down file.

-- People: nothing assigns a task to a person or scopes an agent or a memory
-- to one any more.
DROP INDEX IF EXISTS idx_board_tasks_assignee_user;
DROP INDEX IF EXISTS idx_agents_owner;
DROP INDEX IF EXISTS idx_agent_memories_owner;
ALTER TABLE board_tasks DROP COLUMN IF EXISTS assignee_user_id;
ALTER TABLE agents DROP COLUMN IF EXISTS owner_user_id;
ALTER TABLE agent_memories DROP COLUMN IF EXISTS owner_user_id;

DROP TABLE IF EXISTS tenant_members;

-- The tenants row carried whether the default board was already seeded. That
-- fact survives here so a board the user has since edited is never re-seeded.
CREATE TABLE install_state (
    id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    board_seeded_at timestamptz
);
INSERT INTO install_state (id, board_seeded_at)
SELECT 1, max(bootstrapped_at) FROM tenants;
DROP TABLE IF EXISTS tenants;

-- gcloud_credentials was keyed by tenant_id alone; it becomes a one-row table
-- the way board_settings already is.
ALTER TABLE gcloud_credentials ADD COLUMN id smallint NOT NULL DEFAULT 1 CHECK (id = 1);

DO $$
DECLARE
    r record;
    n bigint;
    fk_before bigint;
    setnull_before bigint;
BEGIN
    -- Row-level security first: with the policies gone nothing reads
    -- app.tenant_id, so the rest of this file needs no session setting.
    IF EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND policyname <> 'tenant_isolation') THEN
        RAISE EXCEPTION 'unexpected row-level security policy; refusing to drop tenancy';
    END IF;
    FOR r IN SELECT tablename, policyname FROM pg_policies WHERE schemaname = 'public' LOOP
        EXECUTE format('DROP POLICY %I ON public.%I', r.policyname, r.tablename);
    END LOOP;
    FOR r IN
        SELECT c.relname FROM pg_class c
        WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
          AND (c.relrowsecurity OR c.relforcerowsecurity)
    LOOP
        EXECUTE format('ALTER TABLE public.%I NO FORCE ROW LEVEL SECURITY', r.relname);
        EXECUTE format('ALTER TABLE public.%I DISABLE ROW LEVEL SECURITY', r.relname);
    END LOOP;

    -- One tenant or none. Two would have their keys collapse into each other.
    FOR r IN
        SELECT table_name FROM information_schema.columns
        WHERE table_schema = 'public' AND column_name = 'tenant_id'
    LOOP
        EXECUTE format('SELECT count(DISTINCT tenant_id) FROM public.%I', r.table_name) INTO n;
        IF n > 1 THEN
            RAISE EXCEPTION 'table % holds rows for % tenants; refusing to merge them', r.table_name, n;
        END IF;
    END LOOP;

    -- Every rewrite below is "drop the leading tenant_id". A key or index that
    -- uses it any other way would be rebuilt wrong, so it stops the migration.
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE connamespace = 'public'::regnamespace
          AND pg_get_constraintdef(oid) LIKE '%tenant_id%'
          AND pg_get_constraintdef(oid) !~ '\(tenant_id[,)]'
    ) OR EXISTS (
        SELECT 1 FROM pg_index i JOIN pg_class t ON t.oid = i.indrelid
        WHERE t.relnamespace = 'public'::regnamespace
          AND pg_get_indexdef(i.indexrelid) LIKE '%tenant_id%'
          AND pg_get_indexdef(i.indexrelid) !~ '\(tenant_id[,)]'
    ) THEN
        RAISE EXCEPTION 'a key or index uses tenant_id other than as its leading column';
    END IF;

    SELECT count(*) INTO fk_before FROM pg_constraint
    WHERE connamespace = 'public'::regnamespace AND contype = 'f';
    SELECT count(*) INTO setnull_before FROM pg_constraint
    WHERE connamespace = 'public'::regnamespace AND contype = 'f'
      AND pg_get_constraintdef(oid) LIKE '%ON DELETE SET NULL%';

    CREATE TEMP TABLE tenancy_rebuild (
        ord int NOT NULL,
        kind text NOT NULL,
        tbl text NOT NULL,
        name text NOT NULL,
        old_def text NOT NULL,
        new_def text,
        PRIMARY KEY (kind, tbl, name)
    ) ON COMMIT DROP;

    INSERT INTO tenancy_rebuild (ord, kind, tbl, name, old_def, new_def)
    SELECT CASE con.contype WHEN 'p' THEN 1 WHEN 'u' THEN 2 ELSE 4 END,
           con.contype::text, cls.relname, con.conname, pg_get_constraintdef(con.oid),
           CASE
               -- UNIQUE (tenant_id, id) existed only so composite FKs had a
               -- target; the id primary key already is one.
               WHEN con.contype = 'u' AND pg_get_constraintdef(con.oid) = 'UNIQUE (tenant_id, id)' THEN NULL
               WHEN con.conname = 'gcloud_credentials_pkey' THEN NULL
               ELSE replace(pg_get_constraintdef(con.oid), '(tenant_id, ', '(')
           END
    FROM pg_constraint con
    JOIN pg_class cls ON cls.oid = con.conrelid
    WHERE con.connamespace = 'public'::regnamespace
      AND con.contype IN ('p', 'u', 'f')
      AND pg_get_constraintdef(con.oid) LIKE '%tenant_id%';

    INSERT INTO tenancy_rebuild (ord, kind, tbl, name, old_def, new_def)
    SELECT 3, 'i', t.relname, ic.relname, pg_get_indexdef(i.indexrelid),
           CASE
               WHEN ic.relname = 'idx_repository_gcloud_resources_repository' THEN NULL
               WHEN replace(pg_get_indexdef(i.indexrelid), '(tenant_id, ', '(') LIKE '%tenant_id%' THEN NULL
               ELSE replace(pg_get_indexdef(i.indexrelid), '(tenant_id, ', '(')
           END
    FROM pg_index i
    JOIN pg_class t ON t.oid = i.indrelid
    JOIN pg_class ic ON ic.oid = i.indexrelid
    WHERE t.relnamespace = 'public'::regnamespace
      AND pg_get_indexdef(i.indexrelid) LIKE '%tenant_id%'
      AND NOT EXISTS (
          SELECT 1 FROM pg_constraint c
          WHERE c.conindid = i.indexrelid AND c.contype IN ('p', 'u', 'x')
      );

    IF EXISTS (SELECT 1 FROM tenancy_rebuild WHERE kind <> 'i' AND new_def LIKE '%tenant_id%') THEN
        RAISE EXCEPTION 'a constraint still names tenant_id after its rewrite';
    END IF;

    FOR r IN SELECT tbl, name FROM tenancy_rebuild WHERE kind = 'f' LOOP
        EXECUTE format('ALTER TABLE public.%I DROP CONSTRAINT %I', r.tbl, r.name);
    END LOOP;
    FOR r IN SELECT tbl, name FROM tenancy_rebuild WHERE kind IN ('p', 'u') LOOP
        EXECUTE format('ALTER TABLE public.%I DROP CONSTRAINT %I', r.tbl, r.name);
    END LOOP;
    FOR r IN SELECT name FROM tenancy_rebuild WHERE kind = 'i' LOOP
        EXECUTE format('DROP INDEX public.%I', r.name);
    END LOOP;

    -- No CASCADE: a dependency this file did not account for fails the
    -- migration instead of being dropped silently.
    FOR r IN
        SELECT table_name FROM information_schema.columns
        WHERE table_schema = 'public' AND column_name = 'tenant_id'
    LOOP
        EXECUTE format('ALTER TABLE public.%I DROP COLUMN tenant_id', r.table_name);
    END LOOP;

    FOR r IN SELECT kind, tbl, name, new_def FROM tenancy_rebuild WHERE new_def IS NOT NULL ORDER BY ord, tbl, name LOOP
        IF r.kind = 'i' THEN
            EXECUTE r.new_def;
        ELSE
            EXECUTE format('ALTER TABLE public.%I ADD CONSTRAINT %I %s', r.tbl, r.name, r.new_def);
        END IF;
    END LOOP;

    IF EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND column_name = 'tenant_id') THEN
        RAISE EXCEPTION 'a tenant_id column survived';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public')
       OR EXISTS (
           SELECT 1 FROM pg_class c
           WHERE c.relnamespace = 'public'::regnamespace AND (c.relrowsecurity OR c.relforcerowsecurity)
       ) THEN
        RAISE EXCEPTION 'row-level security survived';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE connamespace = 'public'::regnamespace AND pg_get_constraintdef(oid) LIKE '%tenant_id%')
       OR EXISTS (
           SELECT 1 FROM pg_index i JOIN pg_class t ON t.oid = i.indrelid
           WHERE t.relnamespace = 'public'::regnamespace AND pg_get_indexdef(i.indexrelid) LIKE '%tenant_id%'
       )
       OR EXISTS (
           SELECT 1 FROM pg_attrdef d JOIN pg_class t ON t.oid = d.adrelid
           WHERE t.relnamespace = 'public'::regnamespace AND pg_get_expr(d.adbin, d.adrelid) LIKE '%app.tenant_id%'
       ) THEN
        RAISE EXCEPTION 'something still refers to tenant_id or app.tenant_id';
    END IF;
    SELECT count(*) INTO n FROM pg_constraint WHERE connamespace = 'public'::regnamespace AND contype = 'f';
    IF n <> fk_before THEN
        RAISE EXCEPTION 'foreign key count changed from % to %', fk_before, n;
    END IF;
    SELECT count(*) INTO n FROM pg_constraint
    WHERE connamespace = 'public'::regnamespace AND contype = 'f'
      AND pg_get_constraintdef(oid) LIKE '%ON DELETE SET NULL%';
    IF n <> setnull_before THEN
        RAISE EXCEPTION 'ON DELETE SET NULL foreign key count changed from % to %', setnull_before, n;
    END IF;
END $$;

ALTER TABLE gcloud_credentials ADD CONSTRAINT gcloud_credentials_pkey PRIMARY KEY (id);
