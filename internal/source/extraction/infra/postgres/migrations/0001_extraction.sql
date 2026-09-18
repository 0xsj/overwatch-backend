create schema if not exists source_extraction;

create table source_extraction.extraction (
    id            uuid primary key,
    workspace_id  uuid not null,
    source_id     uuid not null,
    capture_id    uuid not null references source.capture(id),
    method        text not null check (octet_length(method) between 1 and 200),
    status        text not null check (status in ('succeeded','unsupported','failed')),
    output_sha256 text not null default '' check (output_sha256 = '' or output_sha256 ~ '^[a-f0-9]{64}$'),
    output_bytes  bigint not null default 0 check (output_bytes >= 0 and output_bytes <= 8388608),
    message       text not null default '' check (octet_length(message) <= 4000),
    created_by    uuid not null,
    created_at    timestamptz not null,
    foreign key (workspace_id, source_id) references source.source(workspace_id, id),
    check ((status = 'succeeded') = (output_sha256 <> '' and output_bytes > 0)),
    check ((status <> 'succeeded') = (message <> ''))
);

create index extraction_for_capture on source_extraction.extraction(workspace_id, source_id, capture_id, id desc);
