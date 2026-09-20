package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) CreateCitationShare(ctx context.Context, in domain.CitationShare) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into observation.citation_share(id,workspace_id,source_id,observation_id,created_by,token_digest,created_at) values($1,$2,$3,$4,$5,$6,$7)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.SourceID), uuid(in.ObservationID), uuid(in.CreatedBy), in.TokenDigest, in.CreatedAt)
	return postgres.Translate(ctx, err, "observation: create citation share")
}

func (s *Store) CitationShareByDigest(ctx context.Context, workspace id.ID, digest string) (domain.CitationShare, error) {
	share, err := scanCitationShare(s.db.DB(ctx).QueryRow(ctx, `select id,workspace_id,source_id,observation_id,created_by,token_digest,created_at,revoked_at,revoked_by from observation.citation_share where workspace_id=$1 and token_digest=$2 and revoked_at is null`, uuid(workspace), digest))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CitationShare{}, domain.ErrNotFound
	}
	return share, postgres.Translate(ctx, err, "observation: read citation share")
}

func (s *Store) CitationSharesForObservation(ctx context.Context, workspace, source, observation id.ID) ([]domain.CitationShare, error) {
	rows, err := s.db.DB(ctx).Query(ctx, `select id,workspace_id,source_id,observation_id,created_by,token_digest,created_at,revoked_at,revoked_by from observation.citation_share where workspace_id=$1 and source_id=$2 and observation_id=$3 order by id desc`, uuid(workspace), uuid(source), uuid(observation))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "observation: list citation shares")
	}
	defer rows.Close()
	out := make([]domain.CitationShare, 0)
	for rows.Next() {
		one, err := scanCitationShare(rows)
		if err != nil {
			return nil, postgres.Translate(ctx, err, "observation: read citation share")
		}
		out = append(out, one)
	}
	return out, postgres.Translate(ctx, rows.Err(), "observation: list citation shares")
}

func (s *Store) RevokeCitationShare(ctx context.Context, workspace, share, revokedBy id.ID, at time.Time) (domain.CitationShare, error) {
	one, err := scanCitationShare(s.db.DB(ctx).QueryRow(ctx, `update observation.citation_share set revoked_at=$1,revoked_by=$2 where workspace_id=$3 and id=$4 and revoked_at is null returning id,workspace_id,source_id,observation_id,created_by,token_digest,created_at,revoked_at,revoked_by`, at, uuid(revokedBy), uuid(workspace), uuid(share)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CitationShare{}, domain.ErrNotFound
	}
	return one, postgres.Translate(ctx, err, "observation: revoke citation share")
}

func scanCitationShare(row interface{ Scan(...any) error }) (domain.CitationShare, error) {
	var one domain.CitationShare
	var want, workspace, source, observation, createdBy, revokedBy pgtype.UUID
	var createdAt, revokedAt pgtype.Timestamptz
	if err := row.Scan(&want, &workspace, &source, &observation, &createdBy, &one.TokenDigest, &createdAt, &revokedAt, &revokedBy); err != nil {
		return domain.CitationShare{}, err
	}
	one.ID, one.WorkspaceID, one.SourceID, one.ObservationID, one.CreatedBy = ident(want), ident(workspace), ident(source), ident(observation), ident(createdBy)
	one.CreatedAt = instant(createdAt)
	if revokedAt.Valid {
		at := instant(revokedAt)
		one.RevokedAt = &at
	}
	if revokedBy.Valid {
		value := ident(revokedBy)
		one.RevokedBy = &value
	}
	return one, nil
}
