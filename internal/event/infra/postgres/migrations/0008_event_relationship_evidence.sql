create table timeline.event_relationship_supporting (
    workspace_id              uuid not null,
    relationship_id           uuid not null references timeline.event_relationship(id) on delete cascade,
    supporting_observation_id uuid not null,
    primary key (relationship_id, supporting_observation_id)
);

create table timeline.event_relationship_opposing (
    workspace_id            uuid not null,
    relationship_id         uuid not null references timeline.event_relationship(id) on delete cascade,
    opposing_observation_id uuid not null,
    primary key (relationship_id, opposing_observation_id)
);

create index event_relationship_supporting_workspace on timeline.event_relationship_supporting(workspace_id,relationship_id);
create index event_relationship_opposing_workspace on timeline.event_relationship_opposing(workspace_id,relationship_id);
