-- authz 0001: roles, role permissions, grants, separation-of-duties rules and the seed ledger.
-- Schema "core" is created by the migrator. Creates new objects only and touches no existing
-- table, so it is safe on any database. The eight starter roles are inserted here (keys, scope
-- and the base flag only, no permissions): Service.Seed fills their membership from the
-- declarations during "bms migrate" (authz.md decision 12), once per (role key, permission),
-- so a regional's later edits are never undone and a deleted starter role never returns.
--
-- Nothing here is personal data. Every column is "internal": person IDs, department IDs, role
-- names and permission names. Retention: a row's lifetime. Grants are removed by
-- Service.DeletePerson (people's per-person deletion), which also clears granted_by on grants
-- the person made; there is no retention setting (authz.md, "Data"). Audit rows keep the IDs
-- (audit.md decision 10).
--
-- core.role_grant.person_id and granted_by get their foreign keys to core.person in 0002, once
-- people's set exists (authz.md decision 14). Do not add them here.
--
-- Per authz.md, role_grant, role_permission, sod_rule and role_seed have no updated_at: they
-- are never updated (a grant is deleted and made again; a role's permissions are replaced).

CREATE TABLE core.role (
    id          uuid        NOT NULL,
    -- Starter roles only ("account-admin"); NULL for a regional's own roles. Set by this
    -- migration and never written by the application. Same grammar as module-contract idents.
    key         text,
    -- '' while an unrenamed starter role (the shell shows core.access.role.<key>.title).
    name        text        NOT NULL DEFAULT '',
    description text        NOT NULL DEFAULT '',
    scope       text        NOT NULL,
    -- The one role every signed-in person holds without a grant row.
    base        boolean     NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    CONSTRAINT role_pkey PRIMARY KEY (id),
    CONSTRAINT role_key_key UNIQUE (key),
    CONSTRAINT role_nil_uuid_chk CHECK (id <> '00000000-0000-0000-0000-000000000000'),
    CONSTRAINT role_key_chk CHECK (
        key IS NULL OR (octet_length(key) <= 64 AND key ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*$')),
    -- Bytes, to match the Go side (MaxRoleNameBytes, MaxDescriptionBytes).
    CONSTRAINT role_name_length_chk CHECK (octet_length(name) <= 64),
    CONSTRAINT role_description_length_chk CHECK (octet_length(description) <= 512),
    -- Only a starter role may be unnamed: a regional's own role is found by its name.
    CONSTRAINT role_name_required_chk CHECK (key IS NOT NULL OR name <> ''),
    CONSTRAINT role_scope_chk CHECK (scope IN ('site', 'department')),
    CONSTRAINT role_base_site_chk CHECK (NOT base OR scope = 'site')
);

-- Role names are unique ignoring case; an unnamed starter role does not take part.
-- A violation reports constraint "role_name_key": Conflict, field name, code taken.
CREATE UNIQUE INDEX role_name_key ON core.role (lower(name)) WHERE name <> '';

-- At most one base role: every row with base = true has the same index key.
CREATE UNIQUE INDEX role_base_key ON core.role ((true)) WHERE base;

CREATE TABLE core.role_permission (
    role_id    uuid NOT NULL,
    -- A permission name: <module>.<ident>(.<ident>)*. It need not be declared in this
    -- installation (a disabled module, a later milestone; authz.md decision 13).
    permission text NOT NULL,
    CONSTRAINT role_permission_pkey PRIMARY KEY (role_id, permission),
    CONSTRAINT role_permission_role_id_fkey FOREIGN KEY (role_id)
        REFERENCES core.role (id) ON DELETE CASCADE,
    CONSTRAINT role_permission_permission_chk CHECK (
        octet_length(permission) <= 255
        AND permission ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*(\.[a-z][a-z0-9]*(-[a-z0-9]+)*)+$')
);

-- "Who holds this permission": the grants filter, the last-access-admin count, rules.
CREATE INDEX role_permission_permission_idx ON core.role_permission (permission);

CREATE TABLE core.role_grant (
    id            uuid        NOT NULL,
    -- No foreign key until 0002 (authz.md decision 14).
    person_id     uuid        NOT NULL,
    -- RESTRICT: a role with grants (active or ended) cannot be deleted. The store re-kinds
    -- the 23503 on "role_grant_role_id_fkey" to Conflict in_use.
    role_id       uuid        NOT NULL,
    -- NULL: site-wide. Not checked against core.department (M3); the service asks
    -- DepartmentSource.Active.
    department_id uuid,
    -- NULL: no end. A row past ends_at stays (history) until DeletePerson.
    ends_at       timestamptz,
    -- NULL: the operator, a seed, or the granter was deleted.
    granted_by    uuid,
    created_at    timestamptz NOT NULL,
    CONSTRAINT role_grant_pkey PRIMARY KEY (id),
    CONSTRAINT role_grant_role_id_fkey FOREIGN KEY (role_id)
        REFERENCES core.role (id) ON DELETE RESTRICT,
    -- The all-zero UUID means "none" in Go; it must never be stored.
    CONSTRAINT role_grant_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND person_id <> '00000000-0000-0000-0000-000000000000'
        AND role_id <> '00000000-0000-0000-0000-000000000000'
        AND (department_id IS NULL OR department_id <> '00000000-0000-0000-0000-000000000000')
        AND (granted_by IS NULL OR granted_by <> '00000000-0000-0000-0000-000000000000'))
);

-- One grant per (person, role, department); site-wide is the nil UUID in the key so that two
-- site-wide grants of one role collide (NULLs would not). A violation reports constraint
-- "role_grant_person_role_department_key": Conflict duplicate.
CREATE UNIQUE INDEX role_grant_person_role_department_key ON core.role_grant
    (person_id, role_id, (coalesce(department_id, '00000000-0000-0000-0000-000000000000'::uuid)));

-- The decision queries (every request): a person's grants, read from the index alone.
CREATE INDEX role_grant_person_id_idx ON core.role_grant (person_id)
    INCLUDE (role_id, department_id, ends_at);
-- In-use check, "everyone holding this role", last-access-admin count, rule Affected.
CREATE INDEX role_grant_role_id_idx ON core.role_grant (role_id);
-- A department's grants (a department admin's list). Site-wide rows are not indexed.
CREATE INDEX role_grant_department_id_idx ON core.role_grant (department_id)
    WHERE department_id IS NOT NULL;
-- The grants list: newest first, keyset on (created_at, id).
CREATE INDEX role_grant_created_at_id_idx ON core.role_grant (created_at DESC, id DESC);

CREATE TABLE core.sod_rule (
    id         uuid        NOT NULL,
    -- RESTRICT: a role named by a rule cannot be deleted (Conflict in_use, constraint
    -- "sod_rule_role_id_fkey").
    role_id    uuid        NOT NULL,
    -- Holding role_id forbids this permission...
    permission text,
    -- ...or every declared permission reaching at least this sensitivity level.
    reaches    text,
    note       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    CONSTRAINT sod_rule_pkey PRIMARY KEY (id),
    CONSTRAINT sod_rule_role_id_fkey FOREIGN KEY (role_id)
        REFERENCES core.role (id) ON DELETE RESTRICT,
    CONSTRAINT sod_rule_nil_uuid_chk CHECK (
        id <> '00000000-0000-0000-0000-000000000000'
        AND role_id <> '00000000-0000-0000-0000-000000000000'),
    CONSTRAINT sod_rule_permission_chk CHECK (
        permission IS NULL OR (
            octet_length(permission) <= 255
            AND permission ~ '^[a-z][a-z0-9]*(-[a-z0-9]+)*(\.[a-z][a-z0-9]*(-[a-z0-9]+)*)+$')),
    CONSTRAINT sod_rule_reaches_chk CHECK (
        reaches IS NULL OR reaches IN ('public', 'internal', 'personal', 'sensitive')),
    -- Exactly one of permission and reaches.
    CONSTRAINT sod_rule_one_of_chk CHECK ((permission IS NULL) <> (reaches IS NULL)),
    CONSTRAINT sod_rule_note_length_chk CHECK (octet_length(note) <= 256),
    -- NULLs are distinct, so each constraint only guards its own kind of rule. A violation
    -- is Conflict duplicate.
    CONSTRAINT sod_rule_role_permission_key UNIQUE (role_id, permission),
    CONSTRAINT sod_rule_role_reaches_key UNIQUE (role_id, reaches)
);

-- sod_rule_role_permission_key and sod_rule_role_reaches_key lead with role_id, so they also
-- serve "the rules of the roles a person holds"; no further index.

CREATE TABLE core.role_seed (
    -- A starter role key, not a foreign key: the pair stays recorded after the regional
    -- deletes the role, which is how a deleted starter role stays deleted.
    role_key   text        NOT NULL,
    permission text        NOT NULL,
    seeded_at  timestamptz NOT NULL,
    CONSTRAINT role_seed_pkey PRIMARY KEY (role_key, permission)
);

-- The starter roles (authz.md "Data"): keys, scope and the base flag. No permissions.
INSERT INTO core.role (id, key, name, description, scope, base, created_at, updated_at)
SELECT gen_random_uuid(), v.key, '', '', v.scope, v.base, now(), now()
FROM (VALUES
    ('participant',        'site',       true),
    ('account-admin',      'site',       false),
    ('access-admin',       'site',       false),
    ('department-steward', 'site',       false),
    ('event-admin',        'site',       false),
    ('settings-admin',     'site',       false),
    ('audit-reviewer',     'site',       false),
    ('department-admin',   'department', false)
) AS v (key, scope, base);

COMMENT ON TABLE core.role IS
    'Roles: named sets of permissions, site or department scoped (authz). One base role every signed-in person holds; starter roles carry a key. Sensitivity: internal (nothing personal). Retention: the row''s lifetime.';
COMMENT ON COLUMN core.role.id IS 'sensitivity: none (row identifier)';
COMMENT ON COLUMN core.role.key IS 'sensitivity: internal (starter roles only; set by migration)';
COMMENT ON COLUMN core.role.name IS 'sensitivity: internal (a regional''s role name; empty for an unrenamed starter role)';
COMMENT ON COLUMN core.role.description IS 'sensitivity: internal';
COMMENT ON COLUMN core.role.scope IS 'sensitivity: internal (site or department; immutable)';
COMMENT ON COLUMN core.role.base IS 'sensitivity: internal (the role everyone holds)';
COMMENT ON COLUMN core.role.created_at IS 'sensitivity: internal';
COMMENT ON COLUMN core.role.updated_at IS 'sensitivity: internal';

COMMENT ON TABLE core.role_permission IS
    'The permissions a role holds (authz). A name may be undeclared in this installation. Sensitivity: internal (nothing personal). Retention: the row''s lifetime; deleted with the role.';
COMMENT ON COLUMN core.role_permission.role_id IS 'sensitivity: internal';
COMMENT ON COLUMN core.role_permission.permission IS 'sensitivity: internal (a permission name)';

COMMENT ON TABLE core.role_grant IS
    'A role held by a person, site-wide or for one department, with an optional end date (authz). Sensitivity: internal (reveals that a person holds a role). Retention: until revoked or Service.DeletePerson (people''s per-person deletion), which also nulls granted_by on grants the person made; ended rows stay until then; no retention setting. Export: Service.ExportPerson.';
COMMENT ON COLUMN core.role_grant.id IS 'sensitivity: none (row identifier)';
COMMENT ON COLUMN core.role_grant.person_id IS 'sensitivity: internal (that a person holds a role; deleted by DeletePerson; foreign key to core.person in 0002)';
COMMENT ON COLUMN core.role_grant.role_id IS 'sensitivity: internal';
COMMENT ON COLUMN core.role_grant.department_id IS 'sensitivity: internal (NULL: site-wide)';
COMMENT ON COLUMN core.role_grant.ends_at IS 'sensitivity: internal (NULL: no end; past rows stay until DeletePerson)';
COMMENT ON COLUMN core.role_grant.granted_by IS 'sensitivity: internal (a person ID; set NULL by DeletePerson of the granter)';
COMMENT ON COLUMN core.role_grant.created_at IS 'sensitivity: internal';

COMMENT ON TABLE core.sod_rule IS
    'Separation-of-duties rules: holding a role forbids a permission, or every permission reaching a level (authz). Sensitivity: internal (nothing personal). Retention: the row''s lifetime.';
COMMENT ON COLUMN core.sod_rule.id IS 'sensitivity: none (row identifier)';
COMMENT ON COLUMN core.sod_rule.role_id IS 'sensitivity: internal';
COMMENT ON COLUMN core.sod_rule.permission IS 'sensitivity: internal (exactly one of permission and reaches)';
COMMENT ON COLUMN core.sod_rule.reaches IS 'sensitivity: internal (public, internal, personal or sensitive)';
COMMENT ON COLUMN core.sod_rule.note IS 'sensitivity: internal (the regional''s reason; must not name a person)';
COMMENT ON COLUMN core.sod_rule.created_at IS 'sensitivity: internal';

COMMENT ON TABLE core.role_seed IS
    'Records that a declaration''s starter-role membership was applied, so Service.Seed applies it once (authz). Sensitivity: internal (nothing personal). Retention: the row''s lifetime.';
COMMENT ON COLUMN core.role_seed.role_key IS 'sensitivity: internal (a starter role key; not a foreign key)';
COMMENT ON COLUMN core.role_seed.permission IS 'sensitivity: internal';
COMMENT ON COLUMN core.role_seed.seeded_at IS 'sensitivity: internal';
