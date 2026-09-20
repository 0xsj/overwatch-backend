alter table note.note
    add column if not exists context_kind text,
    add column if not exists context_id uuid;

alter table note.note drop constraint if exists note_context_paired;
alter table note.note add constraint note_context_paired
    check ((context_kind is null) = (context_id is null));

alter table note.note drop constraint if exists note_context_kind_known;
alter table note.note add constraint note_context_kind_known
    check (context_kind is null or context_kind in ('question', 'record', 'event', 'connection', 'brief'));

create index if not exists note_for_context
    on note.note (workspace_id, context_kind, context_id)
    where context_kind is not null;
