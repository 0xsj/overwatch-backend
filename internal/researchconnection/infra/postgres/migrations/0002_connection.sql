create table research.connection (
    id             uuid primary key,
    workspace_id   uuid not null,
    from_record_id uuid not null,
    to_record_id   uuid not null,
    kind           text not null check (kind in ('associated_with','may_belong_to','mentions','concerns_same_event','located_at')),
    state          text not null check (state in ('proposed','accepted','rejected','deferred')),
    rationale      text not null check (octet_length(rationale) between 1 and 4000),
    author         uuid not null,
    updated_by     uuid not null,
    created_at     timestamptz not null,
    updated_at     timestamptz not null,
    check (from_record_id <> to_record_id),
    check (updated_at >= created_at),
    unique (workspace_id, from_record_id, to_record_id, kind)
);

create table research.connection_evidence (
    connection_id  uuid not null references research.connection(id) on delete cascade,
    workspace_id   uuid not null,
    observation_id uuid not null,
    polarity       text not null check (polarity in ('supporting','opposing')),
    primary key (connection_id, observation_id, polarity)
);

create index connection_for_workspace on research.connection(workspace_id, id desc);
create index connection_evidence_for_connection on research.connection_evidence(connection_id);
