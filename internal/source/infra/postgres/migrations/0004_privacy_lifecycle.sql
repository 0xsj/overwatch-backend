-- Privacy controls are source metadata. Purging revokes access to retained
-- bytes while preserving the source, capture metadata, citations and audit
-- history for an explainable record of what was removed.
alter table source.source add column sensitivity text not null default 'internal';
alter table source.source add constraint source_sensitivity_check check (sensitivity in ('public','internal','restricted'));
alter table source.source add column privacy_updated_by uuid;
alter table source.source add column privacy_updated_at timestamptz;
alter table source.source add column legal_hold boolean not null default false;
alter table source.source add column legal_hold_reason text not null default '' check (octet_length(legal_hold_reason) <= 2000);
alter table source.source add column purged_at timestamptz;
alter table source.source add column purged_by uuid;
alter table source.source add column purge_reason text not null default '' check (octet_length(purge_reason) <= 2000);

alter table source.source add constraint source_legal_hold_reason_check
    check ((legal_hold = false and legal_hold_reason = '') or (legal_hold = true and octet_length(legal_hold_reason) between 1 and 2000));
alter table source.source add constraint source_purge_metadata_check
    check ((purged_at is null and purged_by is null and purge_reason = '')
       or (purged_at is not null and purged_by is not null and octet_length(purge_reason) between 1 and 2000));

-- The existing text-only capture limit predates binary imports. Keep the
-- source lifecycle migration independent; capture validation is already owned
-- by the source domain and its later schema changes.
create index source_retention_queue on source.source(workspace_id, retention_until, id)
    where retention_until is not null and purged_at is null;
