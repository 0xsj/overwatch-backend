-- A reader's watermark is account-scoped and workspace-scoped. The account id
-- names identity's row and is deliberately not a cross-schema foreign key;
-- workspace access remains the root handler's authorization decision.
create schema if not exists seen;

create table seen.marker (
    account_id   uuid        not null,
    workspace_id uuid        not null,
    seen_at      timestamptz not null,
    primary key (account_id, workspace_id)
);

create index seen_marker_workspace on seen.marker (workspace_id, account_id);
