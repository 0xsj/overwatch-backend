alter table research.record
  add column archived_at timestamptz,
  add column archived_by uuid;

create index record_active_for_workspace
  on research.record(workspace_id,id desc)
  where archived_at is null;
