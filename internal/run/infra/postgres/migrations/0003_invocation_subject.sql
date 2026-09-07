-- What an invocation was AIMED AT — decisions/0037.
--
-- Both values exist at plan time: they are what the spawn gate is asked, and
-- they were thrown away. Storing them makes coverage exact rather than inferred
-- — a cell is checked when an invocation of that check was aimed at that asset —
-- and it is what keeps `never checked` and `found nothing` apart, because a
-- check that ran and found nothing produces no observation and would otherwise
-- read as never run.
--
-- REVERSIBLE: two nullable columns.

alter table run.invocation add column subject_kind text;
-- FOLDED, matching `entity.fragment.value`. 0037 names the fold mismatch as its
-- own quiet failure: `ACME.test` here against `acme.test` there produces a grid
-- where every cell reads `never` and every row is otherwise correct.
alter table run.invocation add column subject_value text;

alter table run.invocation add constraint invocation_subject_paired check (
    (subject_kind is null) = (subject_value is null)
);
alter table run.invocation add constraint invocation_subject_folded check (
    subject_value is null or subject_value = lower(subject_value)
);

-- What coverage reads: the newest finished invocation per subject, per check.
-- The check comes from the run, so this index carries the join's other half.
create index invocation_subject on run.invocation (workspace_id, subject_kind, subject_value, finished_at desc)
    where subject_value is not null;
