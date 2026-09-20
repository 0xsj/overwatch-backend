-- A person's own text — decisions/0043, and the only input this system has that
-- nothing else produces. Every other row here is something a tool said, a rule
-- decided, or a subscriber assembled.

create schema if not exists note;

create table note.note (
    id           uuid        primary key,
    workspace_id uuid        not null,

    -- THE SUBJECT IS THE TUPLE, not a foreign key — 0036 made a fragment IS
    -- (workspace, kind, value), so a note points at the same thing and holds no
    -- fragment_id. That buys three things:
    --
    --   a note SURVIVES re-observation      the fragment row is upserted; the
    --                                       tuple is stable
    --   a note about something NEVER        "watch out for acme-staging.test,
    --   observed is still a note            we have not scanned it"
    --   no polymorphic foreign key          0009 rejected one on the ground the
    --                                       database cannot enforce it
    --
    -- NULL on both is THE ENGAGEMENT SUMMARY — 0042's eighth report section.
    -- One table, two uses, and the difference is whether these are filled.
    subject_kind  text,
    subject_value text,

    -- Optional research navigation context. This is metadata, not a
    -- polymorphic foreign key: a note remains readable if the target changes
    -- or is no longer available.
    context_kind  text,
    context_id    uuid,

    body       text        not null,

    -- Never moves. An edit changes the body and the timestamp; rewriting the
    -- byline is how a statement stops being one.
    author_id  uuid        not null,
    created_at timestamptz not null,
    updated_at timestamptz not null,

    constraint note_body_present check (body <> '' and length(body) <= 20000),

    -- BOTH OR NEITHER. A half-set subject is a note about nothing spelled like
    -- a note about something, and the domain says the same in Go.
    constraint note_subject_paired
        check ((subject_kind is null) = (subject_value is null)),
    -- FOLDED, matching `entity.fragment.value`. 0037 and 0040 both named the
    -- fold mismatch as their own quiet failure; this is the third table to join
    -- on it.
    constraint note_subject_folded
        check (subject_value is null
               or (subject_value <> '' and subject_value = lower(subject_value)
                   and length(subject_value) <= 2000)),
    constraint note_kind_present
        check (subject_kind is null or subject_kind <> ''),
    constraint note_context_paired
        check ((context_kind is null) = (context_id is null)),
    constraint note_context_kind_known
        check (context_kind is null or context_kind in ('question', 'record', 'event', 'connection', 'brief')),
    constraint note_times_ordered check (updated_at >= created_at)
);

-- The LIST, newest first, per engagement. This is what a screen reads and it is
-- the only read that does not filter by subject.
create index note_for_workspace on note.note (workspace_id, created_at desc);

-- Notes ABOUT one thing — the asset drawer's read.
create index note_for_subject on note.note (workspace_id, subject_kind, subject_value)
    where subject_kind is not null;

-- THE ENGAGEMENT SUMMARY, which is what 0042's eighth section carries. Partial,
-- because the section wants exactly the subjectless ones and an index over
-- every note would be paid for on every write to serve a filter that excludes
-- most of them.
create index note_summary on note.note (workspace_id, created_at)
    where subject_kind is null;

create index note_for_context on note.note (workspace_id, context_kind, context_id)
    where context_kind is not null;
