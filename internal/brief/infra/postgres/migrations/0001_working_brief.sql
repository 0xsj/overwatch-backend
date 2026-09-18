create table brief.working (
    id              uuid primary key,
    workspace_id    uuid not null unique,
    title           text not null check (octet_length(title) between 1 and 400),
    question        text not null check (octet_length(question) between 1 and 4000),
    current_account text not null default '' check (octet_length(current_account) <= 8000),
    alternatives    text not null default '' check (octet_length(alternatives) <= 8000),
    limitations     text not null default '' check (octet_length(limitations) <= 4000),
    next_steps      text not null default '' check (octet_length(next_steps) <= 4000),
    author          uuid not null,
    updated_by      uuid not null,
    created_at      timestamptz not null,
    updated_at      timestamptz not null,
    check (updated_at >= created_at)
);

create table brief.working_observation (
    workspace_id   uuid not null,
    brief_id       uuid not null references brief.working(id) on delete cascade,
    observation_id uuid not null references observation.manual(id),
    primary key (brief_id, observation_id)
);

create table brief.working_question (
    workspace_id uuid not null,
    brief_id     uuid not null references brief.working(id) on delete cascade,
    question_id  uuid not null references lead.question(id),
    primary key (brief_id, question_id)
);

create index working_observation_for_workspace on brief.working_observation(workspace_id, brief_id);
create index working_question_for_workspace on brief.working_question(workspace_id, brief_id);
