-- Workspace-scoped Logs opens on the newest lines and walks backward by the
-- occurred_at/id keyset. Without this index the first real workspace with a
-- long journal would scan the whole append-only table for every page.
create index line_workspace_recent on journal.line (workspace_id, occurred_at desc, id desc);
