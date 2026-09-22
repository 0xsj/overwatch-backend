alter table assistance.comparison
    add column error text not null default ''
        check (octet_length(error) <= 2000);

alter table assistance.comparison
    drop constraint if exists comparison_status_check,
    add constraint comparison_status_check
        check (status in ('completed', 'empty', 'failed', 'unsupported', 'timed_out'));
