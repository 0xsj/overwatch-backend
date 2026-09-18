create schema if not exists source;

create table source.source (
    id uuid primary key,
    workspace_id uuid not null,
    title text not null check (octet_length(title) between 1 and 400),
    origin text not null check (origin in ('paste','import','reference')),
    url text not null default '' check (octet_length(url) <= 4000),
    filename text not null default '' check (octet_length(filename) <= 400),
    created_by uuid not null,
    created_at timestamptz not null,
    unique (workspace_id,id),
    check (origin <> 'reference' or url <> ''),
    check (origin <> 'import' or filename <> '')
);
create index source_for_workspace on source.source(workspace_id,id desc);

create table source.capture (
    id uuid primary key,
    source_id uuid not null,
    workspace_id uuid not null,
    version integer not null check (version > 0),
    media_type text not null check (media_type in ('text/plain','application/json')),
    sha256 text not null check (sha256 ~ '^[a-f0-9]{64}$'),
    bytes bigint not null check (bytes between 1 and 262144),
    captured_by uuid not null,
    captured_at timestamptz not null,
    foreign key (workspace_id,source_id) references source.source(workspace_id,id),
    unique (source_id,version)
);
