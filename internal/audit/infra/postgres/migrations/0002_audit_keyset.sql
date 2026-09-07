-- Exact keyset ordering for the three reads.
--
-- REVERSIBLE: three index swaps. No data changes.
--
-- An append-only ledger grows at the HEAD, which is what rules out offset
-- pagination: rows arrive above page 1 while somebody is reading it, so page 2
-- re-shows rows they have already seen and hides rows they have not. A keyset
-- cursor names the last row a reader saw and is stable under insertion.
--
-- The cursor is (occurred_at, id) and not occurred_at alone, because two entries
-- can share an instant — the registration chain writes several inside one
-- millisecond — and a cursor that cannot break that tie either repeats a row or
-- skips one. `id` is a UUIDv7, so (occurred_at, id) is a total order that
-- matches the order rows were written.
--
-- The indexes therefore need the tiebreaker too, or the keyset predicate sorts
-- after the index rather than inside it.

drop index if exists audit.entry_subject;
drop index if exists audit.entry_actor;
drop index if exists audit.entry_workspace;

create index entry_subject   on audit.entry (subject, occurred_at desc, id desc);
create index entry_actor     on audit.entry (actor, occurred_at desc, id desc);
create index entry_workspace on audit.entry (workspace_id, occurred_at desc, id desc)
    where workspace_id <> '';
