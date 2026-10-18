-- Store.InsertRule: CreateRule. Use Exec; read it back with list_rules.sql ($4 = the id) for
-- Affected. Inside the Write transaction, after lock_invariants.sql.
-- Args: $1 id uuid, $2 role_id uuid, $3 permission text (NULL for a level rule), $4 reaches
-- text (NULL for a permission rule; exactly one of $3 and $4), $5 note text, $6 created_at
-- timestamptz.
-- Constraint errors: sod_rule_role_permission_key, sod_rule_role_reaches_key (Conflict,
-- duplicate); sod_rule_role_id_fkey (unknown role); sod_rule_one_of_chk,
-- sod_rule_permission_chk, sod_rule_reaches_chk, sod_rule_note_length_chk (Invalid).
INSERT INTO core.sod_rule (id, role_id, permission, reaches, note, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
