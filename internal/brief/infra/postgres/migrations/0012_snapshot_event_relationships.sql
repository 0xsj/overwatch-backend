create table brief.snapshot_event_relationship (
    workspace_id              uuid not null,
    snapshot_id               uuid not null references brief.snapshot(id) on delete cascade,
    relationship_id           uuid not null,
    from_event_id             uuid not null,
    to_event_id               uuid not null,
    kind                      text not null,
    rationale                 text not null check (octet_length(rationale) between 1 and 4000),
    state                     text not null,
    review_note               text not null default '' check (octet_length(review_note) <= 4000),
    supporting_observation_ids jsonb not null default '[]'::jsonb,
    opposing_observation_ids   jsonb not null default '[]'::jsonb,
    primary key (snapshot_id, relationship_id),
    check (from_event_id <> to_event_id)
);

create index snapshot_event_relationship_for_snapshot on brief.snapshot_event_relationship(workspace_id, snapshot_id);
