-- name: InsertNote :exec
insert into note.note (
    id, workspace_id, subject_kind, subject_value, context_kind, context_id, body,
    author_id, created_at, updated_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: NoteByID :one
-- BOTH IDS. A note read without its engagement is another client's.
select id, workspace_id, subject_kind, subject_value, context_kind, context_id, body,
       author_id, created_at, updated_at
from note.note where id = $1 and workspace_id = $2;

-- name: NotesForWorkspace :many
-- The list. `subject` NULL means EVERY note; the two-argument form asks for one
-- subject, and the empty-string form asks for the engagement summary only —
-- three questions one query answers, because they differ by a predicate rather
-- than by shape.
select id, workspace_id, subject_kind, subject_value, context_kind, context_id, body,
       author_id, created_at, updated_at
from note.note
where workspace_id = $1
  and (sqlc.narg(kind)::text is null or subject_kind = sqlc.narg(kind)::text)
  and (sqlc.narg(value)::text is null or subject_value = sqlc.narg(value)::text)
  and (sqlc.narg(context_kind)::text is null or context_kind = sqlc.narg(context_kind)::text)
  and (not sqlc.arg(summary_only)::boolean or subject_kind is null)
order by created_at desc
limit sqlc.arg(page)::int;

-- name: NotesWindow :many
-- Cursor and search are explicit. `position` keeps user punctuation literal
-- rather than treating `%` and `_` as wildcard operators.
select id, workspace_id, subject_kind, subject_value, context_kind, context_id, body,
       author_id, created_at, updated_at
from note.note
where workspace_id = $1
  and ($2::uuid is null or id < $2::uuid)
  and (sqlc.narg(kind)::text is null or subject_kind = sqlc.narg(kind)::text)
  and (sqlc.narg(value)::text is null or subject_value = sqlc.narg(value)::text)
  and (sqlc.narg(context_kind)::text is null or context_kind = sqlc.narg(context_kind)::text)
  and (not sqlc.arg(summary_only)::boolean or subject_kind is null)
  and (sqlc.narg(search)::text is null
       or position(lower(sqlc.narg(search)::text) in lower(body)) > 0
       or position(lower(sqlc.narg(search)::text) in lower(coalesce(subject_kind, ''))) > 0
       or position(lower(sqlc.narg(search)::text) in lower(coalesce(subject_value, ''))) > 0
       or position(lower(sqlc.narg(search)::text) in lower(coalesce(context_kind, ''))) > 0
       or position(lower(sqlc.narg(search)::text) in lower(coalesce(context_id::text, ''))) > 0
       or position(lower(sqlc.narg(search)::text) in lower(id::text)) > 0)
order by id desc
limit sqlc.arg(page)::int;

-- name: SaveNote :execrows
-- The body and the clock, and NOTHING ELSE. The author never moves and neither
-- does created_at — an edit is that person revising their own statement.
update note.note set body = $3, updated_at = $4
where id = $1 and workspace_id = $2;

-- name: DeleteNote :execrows
delete from note.note where id = $1 and workspace_id = $2;

-- name: AllSummaryNotes :many
-- Deliverables include every subjectless note, without the interactive list cap.
select id, workspace_id, subject_kind, subject_value, context_kind, context_id, body,
       author_id, created_at, updated_at
from note.note
where workspace_id = $1 and subject_kind is null
order by created_at desc, id desc;
