alter table assistance.question_suggestions
    add column error text not null default ''
        check (octet_length(error) <= 2000);

alter table assistance.question_suggestions
    drop constraint if exists question_suggestions_status_check,
    add constraint question_suggestions_status_check
        check (status in ('completed', 'empty', 'failed', 'unsupported', 'timed_out'));
