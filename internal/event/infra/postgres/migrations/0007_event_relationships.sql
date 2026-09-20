create table timeline.event_relationship (
    id           uuid primary key,
    workspace_id uuid not null,
    from_event_id uuid not null references timeline.event(id) on delete cascade,
    to_event_id   uuid not null references timeline.event(id) on delete cascade,
    kind         text not null check (kind in ('related','precedes','overlaps','same_occurrence_candidate')),
    rationale    text not null check (octet_length(rationale) between 1 and 4000),
    state        text not null check (state in ('proposed','accepted','rejected')),
    review_note  text not null default '' check (octet_length(review_note) <= 4000),
    author       uuid not null,
    updated_by   uuid not null,
    reviewed_by  uuid,
    reviewed_at  timestamptz,
    created_at   timestamptz not null,
    updated_at   timestamptz not null,
    check (from_event_id <> to_event_id),
    check ((state = 'proposed' and reviewed_by is null and reviewed_at is null)
       or (state <> 'proposed' and reviewed_by is not null and reviewed_at is not null and octet_length(review_note) > 0)),
    check (updated_at >= created_at)
);

create unique index event_relationship_unique_kind on timeline.event_relationship(workspace_id,from_event_id,to_event_id,kind);
create index event_relationship_for_workspace on timeline.event_relationship(workspace_id,id desc);
create index event_relationship_for_event on timeline.event_relationship(workspace_id,from_event_id,to_event_id);
