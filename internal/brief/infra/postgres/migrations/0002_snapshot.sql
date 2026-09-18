create table brief.snapshot (
    id                uuid primary key,
    workspace_id      uuid not null,
    brief_id          uuid not null references brief.working(id),
    title             text not null check (octet_length(title) between 1 and 400),
    question          text not null check (octet_length(question) between 1 and 4000),
    current_account   text not null default '' check (octet_length(current_account) <= 8000),
    alternatives      text not null default '' check (octet_length(alternatives) <= 8000),
    limitations       text not null default '' check (octet_length(limitations) <= 4000),
    next_steps        text not null default '' check (octet_length(next_steps) <= 4000),
    author            uuid not null,
    updated_by        uuid not null,
    frozen_by         uuid not null,
    source_updated_at timestamptz not null,
    frozen_at         timestamptz not null
);

create table brief.snapshot_observation (
    workspace_id   uuid not null,
    snapshot_id    uuid not null references brief.snapshot(id) on delete cascade,
    observation_id uuid not null references observation.manual(id),
    primary key (snapshot_id, observation_id)
);

create table brief.snapshot_question (
    workspace_id uuid not null,
    snapshot_id  uuid not null references brief.snapshot(id) on delete cascade,
    question_id  uuid not null,
    question      text not null check (octet_length(question) between 1 and 400),
    state         text not null check (state in ('open','answered','dismissed')),
    resolution    text not null default '' check (octet_length(resolution) <= 4000),
    primary key (snapshot_id, question_id),
    check ((state = 'open' and resolution = '') or (state in ('answered','dismissed') and resolution <> ''))
);

create index snapshot_for_workspace on brief.snapshot(workspace_id, frozen_at desc, id desc);
create index snapshot_observation_for_snapshot on brief.snapshot_observation(workspace_id, snapshot_id);
create index snapshot_question_for_snapshot on brief.snapshot_question(workspace_id, snapshot_id);
