alter table assistance.connection_review
    add column error text not null default ''
        check (octet_length(error) <= 2000);

alter table assistance.connection_review
    drop constraint if exists connection_review_status_check,
    add constraint connection_review_status_check
        check (status in ('completed', 'empty', 'failed', 'unsupported', 'timed_out'));
