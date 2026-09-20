alter table assistance.operation
    add column error text not null default '' check (octet_length(error) <= 2000),
    add column retry_of uuid references assistance.operation(id);

alter table assistance.operation
    drop constraint if exists operation_status_check;

alter table assistance.operation
    add constraint operation_status_check check (status in ('completed', 'empty', 'partial', 'failed', 'unsupported'));

create index operation_retry_of on assistance.operation(workspace_id, retry_of) where retry_of is not null;
