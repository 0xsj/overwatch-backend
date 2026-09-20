package postgres

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/note/domain"
	"github.com/0xsj/overwatch-backend/internal/note/infra/postgres/notedb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "note"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("note: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("note: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *notedb.Queries { return notedb.New(s.db.DB(ctx)) }

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

func optionalUUID(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: !i.IsZero()} }

func ident(u pgtype.UUID) id.ID {
	if !u.Valid {
		return id.ID{}
	}
	return id.ID(u.Bytes)
}

func text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: !t.IsZero()}
}

func instant(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func note(row notedb.NoteNote) domain.Note {
	return domain.Note{
		ID: ident(row.ID), WorkspaceID: ident(row.WorkspaceID),
		SubjectKind: row.SubjectKind.String, SubjectValue: row.SubjectValue.String,
		ContextKind: row.ContextKind.String, ContextID: ident(row.ContextID),
		Body: row.Body, Author: ident(row.AuthorID),
		CreatedAt: instant(row.CreatedAt), UpdatedAt: instant(row.UpdatedAt),
	}
}

func (s *Store) Create(ctx context.Context, n domain.Note) error {
	err := s.q(ctx).InsertNote(ctx, notedb.InsertNoteParams{
		ID: uuid(n.ID), WorkspaceID: uuid(n.WorkspaceID),
		SubjectKind: text(n.SubjectKind), SubjectValue: text(n.SubjectValue),
		ContextKind: text(n.ContextKind), ContextID: optionalUUID(n.ContextID),
		Body: n.Body, AuthorID: uuid(n.Author),
		CreatedAt: stamp(n.CreatedAt), UpdatedAt: stamp(n.UpdatedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "note: insert")
	}
	return nil
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Note, error) {
	row, err := s.q(ctx).NoteByID(ctx, notedb.NoteByIDParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "note: read")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Note{}, fmt.Errorf("note: read: %w", domain.ErrNotFound)
		}
		return domain.Note{}, translated
	}
	return note(row), nil
}

// Page answers three questions with one predicate set — every note, the notes
// about one subject, or the ENGAGEMENT SUMMARY. They differ by a filter rather
// than by shape, so three queries would be three places to get the ordering
// wrong.
func (s *Store) Page(ctx context.Context, workspace id.ID, kind, value, contextKind string,
	summaryOnly bool, limit int) ([]domain.Note, error) {
	rows, err := s.q(ctx).NotesForWorkspace(ctx, notedb.NotesForWorkspaceParams{
		WorkspaceID: uuid(workspace), Kind: text(kind), Value: text(value), ContextKind: text(contextKind),
		SummaryOnly: summaryOnly, Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "note: page")
	}
	out := make([]domain.Note, 0, len(rows))
	for _, row := range rows {
		out = append(out, note(row))
	}
	return out, nil
}

func (s *Store) Window(ctx context.Context, workspace, before id.ID, kind, value, contextKind, search string, summaryOnly bool, limit int) ([]domain.Note, error) {
	rows, err := s.q(ctx).NotesWindow(ctx, notedb.NotesWindowParams{
		WorkspaceID: uuid(workspace), Before: optionalUUID(before), Kind: text(kind), Value: text(value), ContextKind: text(contextKind), Search: text(search), SummaryOnly: summaryOnly, Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "note: window")
	}
	out := make([]domain.Note, 0, len(rows))
	for _, row := range rows {
		out = append(out, note(row))
	}
	return out, nil
}

func (s *Store) Save(ctx context.Context, n domain.Note) error {
	rows, err := s.q(ctx).SaveNote(ctx, notedb.SaveNoteParams{
		ID: uuid(n.ID), WorkspaceID: uuid(n.WorkspaceID),
		Body: n.Body, UpdatedAt: stamp(n.UpdatedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "note: save")
	}
	if rows == 0 {
		return fmt.Errorf("note: save: %w", domain.ErrNotFound)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, workspace, want id.ID) error {
	rows, err := s.q(ctx).DeleteNote(ctx, notedb.DeleteNoteParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "note: delete")
	}
	if rows == 0 {
		return fmt.Errorf("note: delete: %w", domain.ErrNotFound)
	}
	return nil
}

func (s *Store) AllSummary(ctx context.Context, workspace id.ID) ([]domain.Note, error) {
	rows, err := s.q(ctx).AllSummaryNotes(ctx, uuid(workspace))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "note: all summary notes")
	}
	out := make([]domain.Note, 0, len(rows))
	for _, row := range rows {
		out = append(out, note(row))
	}
	return out, nil
}
