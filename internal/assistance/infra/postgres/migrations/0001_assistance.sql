create table assistance.operation (
    id             uuid primary key,
    workspace_id   uuid not null,
    source_id      uuid not null,
    capture_id     uuid not null,
    status         text not null check (status = 'completed'),
    provider       text not null check (octet_length(provider) between 1 and 200),
    method         text not null check (octet_length(method) between 1 and 200),
    created_by     uuid not null,
    created_at     timestamptz not null,
    completed_at   timestamptz not null,
    proposal_count integer not null check (proposal_count between 0 and 12),
    check (completed_at >= created_at)
);

create table assistance.proposal (
    id                    uuid primary key,
    operation_id          uuid not null references assistance.operation(id) on delete cascade,
    workspace_id          uuid not null,
    source_id             uuid not null,
    capture_id            uuid not null,
    generated_statement   text not null check (octet_length(generated_statement) between 1 and 4000),
    generated_quote       bytea not null check (octet_length(generated_quote) between 1 and 8000),
    generated_quote_start integer not null check (generated_quote_start >= 0),
    generated_quote_end   integer not null check (generated_quote_end > generated_quote_start),
    state                 text not null check (state in ('proposed','accepted','rejected')),
    reviewed_statement    text not null default '' check (octet_length(reviewed_statement) <= 4000),
    reviewed_quote        bytea not null default ''::bytea check (octet_length(reviewed_quote) <= 8000),
    reviewed_quote_start  integer,
    reviewed_quote_end    integer,
    reviewed_by           uuid,
    reviewed_at          timestamptz,
    review_note           text not null default '' check (octet_length(review_note) <= 2000),
    created_at            timestamptz not null,
    check ((state = 'proposed' and reviewed_by is null and reviewed_at is null)
       or (state in ('accepted','rejected') and reviewed_by is not null and reviewed_at is not null)),
    check ((reviewed_quote_start is null and reviewed_quote_end is null)
       or (reviewed_quote_start >= 0 and reviewed_quote_end > reviewed_quote_start))
);

create table assistance.proposal_review (
    id           uuid primary key,
    proposal_id  uuid not null references assistance.proposal(id) on delete cascade,
    workspace_id uuid not null,
    decision     text not null check (decision in ('accept','reject')),
    statement    text not null default '' check (octet_length(statement) <= 4000),
    quote        bytea not null default ''::bytea check (octet_length(quote) <= 8000),
    quote_start  integer,
    quote_end    integer,
    note         text not null default '' check (octet_length(note) <= 2000),
    reviewer     uuid not null,
    reviewed_at  timestamptz not null
);

create index operation_for_capture on assistance.operation(workspace_id, source_id, capture_id, created_at desc);
create index proposal_for_operation on assistance.proposal(workspace_id, operation_id, created_at, id);
create index proposal_review_for_proposal on assistance.proposal_review(workspace_id, proposal_id, reviewed_at, id);
