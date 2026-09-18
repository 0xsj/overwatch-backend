-- WHICH ROLE READ THIS — decisions/0040.
--
-- A `derived_from` reading is an ordinary observation: *"this URL was read out
-- of the host a.acme.test"* is a thing one source said about a subject, with a
-- mapping and an artifact behind it. It needs the ROLE to be interpreted and
-- nothing else, so it travels through this table, this lineage and this field
-- accounting for free.
--
-- **The role is recorded HERE and not only on the mapping**, because a mapping
-- version is immutable but a tool's NEXT version may declare a different role
-- for the same field. What this observation is, is what the role was when it
-- ran — the same reasoning that puts `mapping_version` on the row rather than
-- resolving it live.
--
-- IRREVERSIBLE: adds a column.

alter table observation.observation
    add column role text not null default 'attribute';

alter table observation.observation add constraint observation_role_known
    check (role in ('subject', 'attribute', 'derived_from'));

-- WHAT THE ENTITY ASSEMBLER READS. One invocation's provenance readings, which
-- is at most one per record and zero for most tools.
--
-- Partial, because `derived_from` is a small minority of a large table and an
-- index over every attribute reading would be paid for on every extraction to
-- serve a query that never wants them.
create index observation_provenance
    on observation.observation (workspace_id, invocation_id)
    where role = 'derived_from';
