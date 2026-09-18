package postgres

import (
	"context"
	"embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/source/extraction/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "source_extraction"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("source extraction: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("source extraction: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

func translate(ctx context.Context, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return postgres.Translate(ctx, err, "source extraction")
}

func scan(row interface{ Scan(...any) error }) (domain.Extraction, error) {
	var out domain.Extraction
	var want, workspace, source, capture, author pgtype.UUID
	var status string
	if err := row.Scan(&want, &workspace, &source, &capture, &out.Method, &status, &out.OutputHash, &out.OutputBytes, &out.Message, &author, &out.CreatedAt); err != nil {
		return domain.Extraction{}, err
	}
	parsed, err := domain.ParseStatus(status)
	if err != nil {
		return domain.Extraction{}, err
	}
	out.ID, out.WorkspaceID, out.SourceID, out.CaptureID, out.CreatedBy = id.ID(want.Bytes), id.ID(workspace.Bytes), id.ID(source.Bytes), id.ID(capture.Bytes), id.ID(author.Bytes)
	out.Status = parsed
	return out, nil
}

const selectColumns = `select id,workspace_id,source_id,capture_id,method,status,output_sha256,output_bytes,message,created_by,created_at from source_extraction.extraction `

func (s *Store) Create(ctx context.Context, in domain.Extraction) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into source_extraction.extraction(id,workspace_id,source_id,capture_id,method,status,output_sha256,output_bytes,message,created_by,created_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.SourceID), uuid(in.CaptureID), in.Method, in.Status.String(), in.OutputHash, in.OutputBytes, in.Message, uuid(in.CreatedBy), in.CreatedAt)
	return translate(ctx, err)
}

func (s *Store) IndexText(ctx context.Context, workspace, source, capture, extraction id.ID, contentHash string, contentBytes int64, content string, indexedAt time.Time) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into source.search_document(id,workspace_id,source_id,capture_id,extraction_id,content_sha256,content_bytes,indexed_at,search_vector)
values($1,$2,$3,$4,$5,$6,$7,$8,to_tsvector('simple',$9))
on conflict (id) do update set content_sha256=excluded.content_sha256,content_bytes=excluded.content_bytes,indexed_at=excluded.indexed_at,search_vector=excluded.search_vector`, uuid(extraction), uuid(workspace), uuid(source), uuid(capture), uuid(extraction), contentHash, contentBytes, indexedAt, content)
	return translate(ctx, err)
}

func (s *Store) ByID(ctx context.Context, workspace, source, capture, want id.ID) (domain.Extraction, error) {
	out, err := scan(s.db.DB(ctx).QueryRow(ctx, selectColumns+`where workspace_id=$1 and source_id=$2 and capture_id=$3 and id=$4`, uuid(workspace), uuid(source), uuid(capture), uuid(want)))
	if err != nil {
		return domain.Extraction{}, translate(ctx, err)
	}
	return out, nil
}

func (s *Store) Page(ctx context.Context, workspace, source, capture, before id.ID, limit int) ([]domain.Extraction, error) {
	rows, err := s.db.DB(ctx).Query(ctx, selectColumns+`where workspace_id=$1 and source_id=$2 and capture_id=$3 and ($4::uuid is null or id < $4) order by id desc limit $5`, uuid(workspace), uuid(source), uuid(capture), pgtype.UUID{Bytes: before, Valid: !before.IsZero()}, limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Extraction, 0)
	for rows.Next() {
		one, err := scan(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}
