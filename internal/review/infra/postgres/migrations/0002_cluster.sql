create table review.cluster (
    id uuid primary key,
    workspace_id uuid not null,
    kind text not null check (kind in ('claim','account')),
    title text not null check (octet_length(title) between 1 and 200),
    description text not null check (octet_length(description) <= 4000),
    author uuid not null,
    updated_by uuid not null,
    created_at timestamptz not null,
    updated_at timestamptz not null
);

create index cluster_for_workspace on review.cluster(workspace_id,id desc);

create table review.cluster_observation (
    cluster_id uuid not null references review.cluster(id) on delete cascade,
    observation_id uuid not null references observation.manual(id),
    ordinal integer not null check (ordinal >= 0 and ordinal < 24),
    primary key (cluster_id, observation_id),
    unique (cluster_id, ordinal)
);

create index cluster_observation_for_observation on review.cluster_observation(observation_id,cluster_id);
