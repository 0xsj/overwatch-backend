alter table assistance.brief_draft
    add column error text not null default ''
        check (octet_length(error) <= 2000);

alter table assistance.brief_draft
    drop constraint if exists brief_draft_status_check,
    add constraint brief_draft_status_check
        check (status in ('completed', 'empty', 'failed', 'unsupported', 'timed_out'));
