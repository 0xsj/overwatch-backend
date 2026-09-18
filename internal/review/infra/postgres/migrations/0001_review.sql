create table review.relation (
    id uuid primary key,
    workspace_id uuid not null,
    left_observation_id uuid not null,
    right_observation_id uuid not null,
    kind text not null check (kind in ('supports','contradicts','repeats','unresolved')),
    rationale text not null check (octet_length(rationale) between 1 and 4000),
    author uuid not null,
    created_at timestamptz not null,
    updated_at timestamptz not null,
    check (left_observation_id <> right_observation_id),
    unique (workspace_id, left_observation_id, right_observation_id)
);
create index relation_for_workspace on review.relation(workspace_id,id desc);
