create table brief.working_cluster (
    workspace_id uuid not null,
    brief_id     uuid not null references brief.working(id) on delete cascade,
    cluster_id   uuid not null,
    primary key (brief_id, cluster_id)
);

create index working_cluster_for_workspace on brief.working_cluster(workspace_id, brief_id);

create table brief.snapshot_cluster (
    workspace_id    uuid not null,
    snapshot_id     uuid not null references brief.snapshot(id) on delete cascade,
    cluster_id      uuid not null,
    kind            text not null check (kind in ('claim','account')),
    title           text not null check (octet_length(title) between 1 and 200),
    description     text not null default '' check (octet_length(description) <= 4000),
    observation_ids jsonb not null default '[]'::jsonb,
    primary key (snapshot_id, cluster_id)
);

create index snapshot_cluster_for_snapshot on brief.snapshot_cluster(workspace_id, snapshot_id);
