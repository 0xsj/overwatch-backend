create table observation.citation_share (
    id uuid primary key,
    workspace_id uuid not null,
    source_id uuid not null,
    observation_id uuid not null,
    created_by uuid not null,
    token_digest text not null unique,
    created_at timestamptz not null,
    revoked_at timestamptz,
    revoked_by uuid,
    check ((revoked_at is null and revoked_by is null) or (revoked_at is not null and revoked_by is not null))
);
create index citation_share_for_observation on observation.citation_share(workspace_id,source_id,observation_id,id desc);
