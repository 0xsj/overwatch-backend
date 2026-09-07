-- The fourth scope — decisions/0024.
--
-- REVERSIBLE while no row uses it: drop the column, narrow the constraints back.

alter table audit.entry add column org_id text not null default '';

alter table audit.entry drop constraint entry_scope_known;
alter table audit.entry add constraint entry_scope_known
    check (scope in ('system', 'account', 'workspace', 'org'));

-- Three ways now, and every clause earns its place.
--
-- An ORG-SCOPE ROW MAY NEVER CARRY A WORKSPACE ID. That is not tidiness: it is
-- the property that makes an org-scope feed safe to read org-wide. The moment
-- such a row can name an engagement, a firm-wide log leaks the client list —
-- which is the read decisions/0024 refused three days earlier and permitted only
-- under this constraint.
alter table audit.entry drop constraint entry_workspace_iff_scoped;
alter table audit.entry add constraint entry_scope_ids check (
    (scope = 'workspace' and workspace_id <> '' and org_id = '') or
    (scope = 'org'       and org_id       <> '' and workspace_id = '') or
    (scope in ('system', 'account')            and workspace_id = '' and org_id = '')
);

-- The org read, with the keyset tiebreaker every read here needs — an
-- append-only ledger is paged by (occurred_at, id) and never by offset.
create index entry_org on audit.entry (org_id, occurred_at desc, id desc)
    where org_id <> '';

-- Rows written before this migration keep the scope they were written with.
-- There is deliberately NO backfill: reclassifying an old `system` row would be
-- inventing a scope its writer never chose, and "written before 0024" is the
-- honest reading.
