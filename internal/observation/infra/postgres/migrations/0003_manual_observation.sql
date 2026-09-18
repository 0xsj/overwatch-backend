-- Source citations have no fabricated invocation/mapping foreign keys. They
-- remain observation-owned; root validates the peer-owned capture provenance.
create table observation.manual (
    id uuid primary key,
    workspace_id uuid not null,
    source_id uuid not null,
    capture_id uuid not null,
    statement text not null check (octet_length(statement) between 1 and 4000),
    -- Bytea preserves every UTF-8 quotation, including NUL in retained text.
    quote bytea not null check (octet_length(quote) between 1 and 8000),
    quote_start integer not null check (quote_start >= 0),
    quote_end integer not null check (quote_end > quote_start),
    locator text not null default '' check (octet_length(locator) <= 400),
    author uuid not null,
    recorded_at timestamptz not null
);
create index manual_for_source on observation.manual(workspace_id,source_id,id desc);
