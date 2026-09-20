alter table source.watch
    add column lease_owner text not null default '' check (octet_length(lease_owner) <= 200),
    add column lease_until timestamptz,
    add constraint source_watch_lease_pair check ((lease_owner = '' and lease_until is null) or (lease_owner <> '' and lease_until is not null));

create index source_watch_lease_expiry on source.watch(enabled, lease_until);
