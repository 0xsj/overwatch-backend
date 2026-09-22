alter table lead.question add column if not exists context_kind text not null default '';
alter table lead.question add column if not exists context_id uuid;

alter table lead.question drop constraint if exists question_context_check;
alter table lead.question add constraint question_context_check check (
    (context_kind = '' and context_id is null)
    or (context_kind <> '' and context_id is not null)
);
