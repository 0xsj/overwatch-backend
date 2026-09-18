create table lead.question (
    id               uuid primary key,
    workspace_id     uuid not null,
    question         text not null check (octet_length(question) between 1 and 400),
    question_context text not null default '' check (octet_length(question_context) <= 4000),
    state            text not null check (state in ('open','answered','dismissed')),
    resolution       text not null default '' check (octet_length(resolution) <= 4000),
    author           uuid not null,
    updated_by       uuid not null,
    created_at       timestamptz not null,
    updated_at       timestamptz not null,
    check (
      (state = 'open' and resolution = '')
      or (state in ('answered','dismissed') and resolution <> '')
    ),
    check (updated_at >= created_at)
);

create table lead.question_observation (
    workspace_id   uuid not null,
    question_id    uuid not null references lead.question(id) on delete cascade,
    observation_id uuid not null,
    primary key (question_id, observation_id)
);

create index question_for_workspace on lead.question(workspace_id,id desc);
create index question_observation_for_question on lead.question_observation(workspace_id,question_id);
