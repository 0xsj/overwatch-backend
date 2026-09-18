-- What a mapping is FOR — decisions/0040.
--
--   subject       what every other reading in this record is ABOUT
--   attribute     a value about that subject.        THE DEFAULT
--   derived_from  the value this record was READ OUT OF. Draws a derivation
--
-- IT IS A DECLARATION AND IT REPLACES A NAME MATCH. `0035` resolved a record's
-- subject by SPELLING — the tool's `produces` kind written as a field name —
-- which works, and works by luck. Doing the same trick a second time for
-- provenance would let somebody create edges in the entity graph by typing a
-- field called `derived_from` for an unrelated reason, and that failure is
-- silent where the spelling one is loud.
--
-- IRREVERSIBLE: adds a column and backfills it.

alter table tool.mapping add column role text not null default 'attribute';

alter table tool.mapping add constraint mapping_role_known
    check (role in ('subject', 'attribute', 'derived_from'));

-- THE BACKFILL IS DERIVABLE AND NOT A GUESS. Today's subject IS the mapping
-- whose field name matches its tool's produces-kind — that is what `Extract`
-- looks for, so reading the same rule out of the same two columns reproduces
-- exactly the behaviour that was running a moment ago.
--
-- A tool with no such mapping already fails extraction with ErrNoSubjectMapping,
-- so nothing here silently acquires a role it did not have: it stays
-- `attribute`, and the tool stays as broken as it already was.
update tool.mapping m
set role = 'subject'
from tool.tool t
where t.id = m.tool_id
  -- NULL is spelled out rather than left to three-valued logic. `null <> ''`
  -- is null and excludes the row anyway, which is correct and is the kind of
  -- correct-by-accident a reader has to re-derive.
  and t.produces is not null and t.produces <> ''
  and m.field = t.produces;

-- EXACTLY ONE LIVE SUBJECT, and one live source, per tool. The predicate is the
-- decision — the same shape `mapping_one_live` uses, and 0011's "partial unique
-- indexes as lifecycle rules" one level over.
--
-- Scoped to `live` because a DRAFT is something somebody is still editing: two
-- competing draft subjects is a person deciding, and refusing it would make the
-- editor unusable. Retired ones are history and are never re-run.
create unique index mapping_one_subject on tool.mapping (tool_id)
    where role = 'subject' and state = 'live';

create unique index mapping_one_source on tool.mapping (tool_id)
    where role = 'derived_from' and state = 'live';
