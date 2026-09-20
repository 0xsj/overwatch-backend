create table source.intake_candidate (
    id uuid primary key,
    workspace_id uuid not null,
    title text not null check (octet_length(title) between 1 and 400),
    origin text not null check (origin in ('reference','import')),
    url text not null default '' check (octet_length(url) <= 4000),
    filename text not null default '' check (octet_length(filename) <= 400),
    media_type text not null default '',
    content_bytes bytea,
    note text not null default '' check (octet_length(note) <= 2000),
    created_by uuid not null,
    created_at timestamptz not null,
    status text not null default 'pending' check (status in ('pending','approved','rejected')),
    reviewed_by uuid,
    reviewed_at timestamptz,
    review_note text not null default '' check (octet_length(review_note) <= 2000),
    source_id uuid,
    unique (workspace_id,id),
    foreign key (workspace_id,source_id) references source.source(workspace_id,id),
    check ((status = 'pending' and reviewed_by is null and reviewed_at is null and review_note = '' and source_id is null
            and ((origin = 'reference' and url <> '' and filename = '' and media_type = '' and content_bytes is null)
              or (origin = 'import' and filename <> '' and media_type <> '' and content_bytes is not null and octet_length(content_bytes) between 1 and 8388608)))
        or (status in ('approved','rejected') and reviewed_by is not null and reviewed_at is not null and octet_length(review_note) between 1 and 2000 and content_bytes is null
            and ((status = 'approved' and source_id is not null) or (status = 'rejected' and source_id is null))))
);
create index source_intake_queue on source.intake_candidate(workspace_id,status,id desc);
