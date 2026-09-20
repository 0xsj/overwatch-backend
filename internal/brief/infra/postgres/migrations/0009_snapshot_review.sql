-- Review collaboration is a companion to the immutable snapshot. Assignment is
-- current coordination state; decisions are append-only evidence of what a
-- reviewer said about that handoff.

create table brief.snapshot_review (
    workspace_id uuid        not null,
    snapshot_id  uuid        primary key references brief.snapshot(id) on delete cascade,
    state        text        not null default 'pending',
    assignee_id  uuid,
    assigned_by  uuid,
    assigned_at  timestamptz,
    updated_at   timestamptz not null,

    constraint snapshot_review_state_known check (state in ('pending', 'approved', 'changes_requested'))
);

create index snapshot_review_workspace on brief.snapshot_review (workspace_id, updated_at desc);

create table brief.snapshot_review_decision (
    id           uuid        primary key,
    workspace_id uuid        not null,
    snapshot_id  uuid        not null references brief.snapshot(id) on delete cascade,
    reviewer_id  uuid        not null,
    state        text        not null,
    note         text        not null default '',
    created_at   timestamptz not null,

    constraint snapshot_review_decision_state_known check (state in ('approved', 'changes_requested')),
    constraint snapshot_review_decision_note_length check (octet_length(note) <= 4000)
);

create index snapshot_review_decision_snapshot on brief.snapshot_review_decision (workspace_id, snapshot_id, created_at desc);

-- Older frozen handoffs receive the same explicit starting state as new ones.
insert into brief.snapshot_review (workspace_id, snapshot_id, state, updated_at)
select workspace_id, id, 'pending', frozen_at
from brief.snapshot
on conflict (snapshot_id) do nothing;

