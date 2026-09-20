alter table source.alert
    add column record_id uuid,
    add column cluster_id uuid;

alter table source.alert
    drop constraint if exists source_alert_kind_shape;

alter table source.alert
    add constraint source_alert_kind_shape check (
        (kind = 'capture_changed' and source_id is not null and capture_id is not null and question_id is null and record_id is null and cluster_id is null)
        or (kind = 'watch_failed' and source_id is not null and capture_id is null and question_id is null and record_id is null and cluster_id is null)
        or (kind = 'question_gap' and source_id is null and capture_id is null and question_id is not null and record_id is null and cluster_id is null)
        or (kind = 'record_gap' and source_id is null and capture_id is null and question_id is null and record_id is not null and cluster_id is null)
        or (kind = 'cluster_gap' and source_id is null and capture_id is null and question_id is null and record_id is null and cluster_id is not null)
    );

create index source_alert_gap_target on source.alert(workspace_id,kind,active,id desc);
