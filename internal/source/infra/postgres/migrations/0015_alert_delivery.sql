-- Email delivery is an explicit per-account preference. The address remains in
-- identity; this table is only workspace-scoped consent and kind filtering.
create table source.alert_delivery_preference (
    workspace_id  uuid        not null,
    account_id    uuid        not null,
    email_enabled boolean     not null default false,
    kinds         text[]      not null default '{}'::text[],
    updated_at    timestamptz not null,
    primary key (workspace_id, account_id)
);

create index source_alert_delivery_account
    on source.alert_delivery_preference(account_id, workspace_id);
