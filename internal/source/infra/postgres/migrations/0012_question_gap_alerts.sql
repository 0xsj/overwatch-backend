alter table source.alert
    alter column source_id drop not null;

alter table source.alert
    add column question_id uuid,
    add column dedupe_key text,
    add column active boolean not null default true;

update source.alert
set dedupe_key = id::text
where dedupe_key is null;

alter table source.alert
    alter column dedupe_key set not null,
    add constraint source_alert_dedupe unique (workspace_id, dedupe_key);

alter table source.alert
    drop constraint if exists alert_kind_check;

alter table source.alert
    add constraint source_alert_kind_shape check (
        (kind = 'capture_changed' and source_id is not null and capture_id is not null and question_id is null)
        or (kind = 'watch_failed' and source_id is not null and capture_id is null and question_id is null)
        or (kind = 'question_gap' and source_id is null and capture_id is null and question_id is not null)
    );

create index source_alert_active_workspace on source.alert(workspace_id,active,id desc);
