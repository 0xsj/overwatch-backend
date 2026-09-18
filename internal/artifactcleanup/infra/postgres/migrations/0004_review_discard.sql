alter table artifact_cleanup.review
    add column discarded_by uuid,
    add column discarded_at timestamptz,
    add column discard_reason text not null default '';

alter table artifact_cleanup.review
    drop constraint review_status_known,
    add constraint review_status_known check (status in ('open', 'completed', 'discarded')),
    drop constraint review_completed_shape,
    add constraint review_completed_shape check ((status = 'completed') = (completed_at is not null)),
    add constraint review_discarded_shape check ((status = 'discarded') = (discarded_at is not null and discarded_by is not null));

create index review_workspace_updated on artifact_cleanup.review(workspace_id, updated_at desc);
