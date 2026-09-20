create table review.source_link (
    id uuid primary key,
    workspace_id uuid not null,
    downstream_observation_id uuid not null references observation.manual(id),
    upstream_observation_id uuid not null references observation.manual(id),
    rationale text not null check (octet_length(rationale) between 1 and 4000),
    author uuid not null,
    created_at timestamptz not null,
    updated_at timestamptz not null,
    check (downstream_observation_id <> upstream_observation_id),
    unique (workspace_id, downstream_observation_id, upstream_observation_id)
);

create index source_link_for_workspace on review.source_link(workspace_id,id desc);
create index source_link_for_upstream on review.source_link(workspace_id,upstream_observation_id);
