drop index if exists research.record_resolution_active_alias;
create unique index record_resolution_active_alias on research.record_resolution(workspace_id, alias_record_id) where state in ('proposed','accepted');
