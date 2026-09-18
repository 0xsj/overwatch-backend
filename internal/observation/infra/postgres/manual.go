package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) CreateManual(ctx context.Context, in domain.Manual) error {
	var extraction pgtype.UUID
	if in.ExtractionID != nil {
		extraction = uuid(*in.ExtractionID)
	}
	_, err := s.db.DB(ctx).Exec(ctx, `insert into observation.manual(id,workspace_id,source_id,capture_id,extraction_id,statement,quote,quote_start,quote_end,locator,author,recorded_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.SourceID), uuid(in.CaptureID), extraction, in.Statement, []byte(in.Quote), in.QuoteStart, in.QuoteEnd, in.Locator, uuid(in.Author), in.RecordedAt)
	return postgres.Translate(ctx, err, "observation: record manual citation")
}
func (s *Store) PageManual(ctx context.Context, workspace, source, before id.ID, limit int) ([]domain.Manual, error) {
	cursor := pgtype.UUID{Bytes: before, Valid: !before.IsZero()}
	rows, err := s.db.DB(ctx).Query(ctx, `select id,workspace_id,source_id,capture_id,extraction_id,statement,quote,quote_start,quote_end,locator,author,recorded_at from observation.manual where workspace_id=$1 and source_id=$2 and ($3::uuid is null or id < $3) order by id desc limit $4`, uuid(workspace), uuid(source), cursor, limit)
	if err != nil {
		return nil, postgres.Translate(ctx, err, "observation: page manual citations")
	}
	defer rows.Close()
	out := make([]domain.Manual, 0)
	for rows.Next() {
		one, err := scanManual(rows)
		if err != nil {
			return nil, postgres.Translate(ctx, err, "observation: read manual citation")
		}
		out = append(out, one)
	}
	return out, postgres.Translate(ctx, rows.Err(), "observation: page manual citations")
}

func scanManual(row interface{ Scan(...any) error }) (domain.Manual, error) {
	var one domain.Manual
	var want, workspace, source, capture, extraction, author pgtype.UUID
	var quote []byte
	if err := row.Scan(&want, &workspace, &source, &capture, &extraction, &one.Statement, &quote, &one.QuoteStart, &one.QuoteEnd, &one.Locator, &author, &one.RecordedAt); err != nil {
		return domain.Manual{}, err
	}
	one.ID, one.WorkspaceID, one.SourceID, one.CaptureID, one.Author = ident(want), ident(workspace), ident(source), ident(capture), ident(author)
	if extraction.Valid {
		value := ident(extraction)
		one.ExtractionID = &value
	}
	one.Quote, one.Origin = string(quote), "manual"
	return one, nil
}

func (s *Store) ByIDManual(ctx context.Context, workspace, source, want id.ID) (domain.Manual, error) {
	one, err := scanManual(s.db.DB(ctx).QueryRow(ctx, `select id,workspace_id,source_id,capture_id,extraction_id,statement,quote,quote_start,quote_end,locator,author,recorded_at from observation.manual where workspace_id=$1 and source_id=$2 and id=$3`, uuid(workspace), uuid(source), uuid(want)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Manual{}, domain.ErrNotFound
	}
	return one, postgres.Translate(ctx, err, "observation: read manual citation")
}
