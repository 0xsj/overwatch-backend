alter table assistance.synthesis
    add column status text not null default 'completed'
        check (status in ('completed', 'failed', 'unsupported', 'timed_out')),
    add column error text not null default ''
        check (octet_length(error) <= 2000);
