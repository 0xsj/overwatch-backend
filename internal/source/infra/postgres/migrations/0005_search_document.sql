create table source.search_document (
    id             uuid primary key,
    workspace_id   uuid not null,
    source_id      uuid not null,
    capture_id     uuid not null references source.capture(id),
    extraction_id  uuid,
    content_sha256 text not null check (content_sha256 ~ '^[a-f0-9]{64}$'),
    content_bytes  bigint not null check (content_bytes between 1 and 8388608),
    indexed_at     timestamptz not null,
    search_vector  tsvector not null,
    foreign key (workspace_id, source_id) references source.source(workspace_id, id),
    check ((extraction_id is null) or (extraction_id = id))
);

create index search_document_workspace_vector
    on source.search_document using gin(search_vector);
create index search_document_workspace_order
    on source.search_document(workspace_id, id desc);
create unique index search_document_capture_artifact
    on source.search_document(capture_id, coalesce(extraction_id, '00000000-0000-0000-0000-000000000000'::uuid));
