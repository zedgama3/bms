-- Open's, the watcher's and Migrate's probe (db.md "Open", step 4). No args.
-- Columns: server_version_num int4, read_only bool.
-- read_only is true when the server is in recovery, when this session's default is
-- read-only, or when the database's own setting (ALTER DATABASE ... SET
-- default_transaction_read_only) or this role's setting in it is on. The catalog term lets
-- pooled sessions opened before the ALTER notice it: a session keeps the value it started
-- with. pg_db_role_setting is readable by PUBLIC.
SELECT current_setting('server_version_num')::int AS server_version_num,
       (pg_is_in_recovery()
        OR current_setting('transaction_read_only')::bool
        OR EXISTS (
            SELECT 1
            FROM pg_db_role_setting s
            CROSS JOIN LATERAL unnest(s.setconfig) AS c
            WHERE s.setdatabase = (SELECT oid FROM pg_database WHERE datname = current_database())
              AND s.setrole IN (0, (SELECT oid FROM pg_roles WHERE rolname = current_user))
              AND lower(c) IN ('default_transaction_read_only=on', 'default_transaction_read_only=true',
                               'default_transaction_read_only=yes', 'default_transaction_read_only=1')
        )) AS read_only;
