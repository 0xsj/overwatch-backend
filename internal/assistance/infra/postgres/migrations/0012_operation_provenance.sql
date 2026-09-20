alter table assistance.operation
    add column template_version text not null default '' check (octet_length(template_version) <= 200),
    add column input_bytes bigint not null default 0 check (input_bytes >= 0),
    add column output_bytes bigint not null default 0 check (output_bytes >= 0),
    add column duration_ms bigint not null default 0 check (duration_ms >= 0),
    add column timed_out boolean not null default false;
