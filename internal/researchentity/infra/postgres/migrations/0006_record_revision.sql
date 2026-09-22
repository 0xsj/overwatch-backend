create table research.record_revision (
    id                uuid primary key,
    workspace_id      uuid not null,
    record_id         uuid not null references research.record(id) on delete cascade,
    revision          integer not null check (revision > 0),
    kind              text not null check (kind in ('person','account','organisation','place')),
    name              text not null check (octet_length(name) between 1 and 400),
    description       text not null default '' check (octet_length(description) <= 4000),
    observation_ids   jsonb not null default '[]'::jsonb,
    place_geometry    jsonb,
    archived_at       timestamptz,
    archived_by       uuid,
    changed_by        uuid not null,
    changed_at        timestamptz not null,
    unique (record_id, revision)
);

create index record_revision_for_record on research.record_revision(workspace_id, record_id, revision);
