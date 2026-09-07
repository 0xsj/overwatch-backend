-- decisions/0017. See internal/org's 0002 for the reasoning: at-least-once
-- delivery means a redelivered org.created must not produce a second workspace.

alter table workspace.workspace add column if not exists source_event_id uuid;

create unique index if not exists workspace_source_event
    on workspace.workspace (source_event_id) where source_event_id is not null;
