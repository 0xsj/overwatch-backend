package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/internal/org/infra/postgres/orgdb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) CreateInvite(ctx context.Context, i domain.Invite) error {
	err := s.q(ctx).InsertInvite(ctx, orgdb.InsertInviteParams{
		ID:          uuid(i.ID),
		OrgID:       uuid(i.OrgID),
		Email:       i.Email,
		Role:        i.Role.String(),
		InvitedBy:   uuid(i.InvitedBy),
		WorkspaceID: maybe(i.WorkspaceID),
		Level:       level(i),
		Hash:        i.Hash,
		CreatedAt:   stamp(i.CreatedAt),
		ExpiresAt:   stamp(i.ExpiresAt),
		AcceptedAt:  stamp(i.AcceptedAt),
		AcceptedBy:  maybe(i.AcceptedBy),
		RevokedAt:   stamp(i.RevokedAt),
	})
	if err == nil {
		return nil
	}
	translated := postgres.Translate(ctx, err, "org: insert invite")
	if postgres.IsConstraint(translated, "invite_live") {
		return fmt.Errorf("org: insert invite: %w", domain.ErrInviteExists)
	}
	return translated
}

func (s *Store) InviteByHash(ctx context.Context, hash string) (domain.Invite, error) {
	row, err := s.q(ctx).InviteByHash(ctx, hash)
	if err != nil {
		translated := postgres.Translate(ctx, err, "org: read invite")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Invite{}, fmt.Errorf("org: read invite: %w", domain.ErrInviteGone)
		}
		return domain.Invite{}, translated
	}
	return invite(row)
}

func (s *Store) LiveInviteFor(ctx context.Context, org id.ID, email string) (domain.Invite, error) {
	row, err := s.q(ctx).LiveInviteFor(ctx, orgdb.LiveInviteForParams{
		OrgID: uuid(org), Email: email,
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "org: live invite")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Invite{}, fmt.Errorf("org: live invite: %w", domain.ErrInviteGone)
		}
		return domain.Invite{}, translated
	}
	return invite(row)
}

func (s *Store) InvitesForOrg(ctx context.Context, org id.ID) ([]domain.Invite, error) {
	rows, err := s.q(ctx).InvitesForOrg(ctx, uuid(org))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "org: invites for org")
	}
	out := make([]domain.Invite, 0, len(rows))
	for _, row := range rows {
		i, err := invite(row)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, nil
}

// SaveInvite writes only the three lifecycle columns. Everything else about an
// invitation is decided when it is sent and never changes — an editable role or
// address would be a different invitation wearing the same token.
func (s *Store) SaveInvite(ctx context.Context, i domain.Invite) error {
	n, err := s.q(ctx).SaveInvite(ctx, orgdb.SaveInviteParams{
		ID:         uuid(i.ID),
		AcceptedAt: stamp(i.AcceptedAt),
		AcceptedBy: maybe(i.AcceptedBy),
		RevokedAt:  stamp(i.RevokedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "org: save invite")
	}
	if n == 0 {
		return fmt.Errorf("org: save invite: %w", domain.ErrInviteGone)
	}
	return nil
}

func level(i domain.Invite) pgtype.Text {
	if !i.HasGrant() {
		return pgtype.Text{}
	}
	return pgtype.Text{String: i.Level.String(), Valid: true}
}

func invite(row orgdb.OrgInvite) (domain.Invite, error) {
	role, err := domain.ParseRole(row.Role)
	if err != nil {
		return domain.Invite{}, err
	}
	out := domain.Invite{
		ID:          ident(row.ID),
		OrgID:       ident(row.OrgID),
		Email:       row.Email,
		Role:        role,
		InvitedBy:   ident(row.InvitedBy),
		WorkspaceID: ident(row.WorkspaceID),
		Hash:        row.Hash,
		CreatedAt:   instant(row.CreatedAt),
		ExpiresAt:   instant(row.ExpiresAt),
		AcceptedAt:  instant(row.AcceptedAt),
		AcceptedBy:  ident(row.AcceptedBy),
		RevokedAt:   instant(row.RevokedAt),
	}
	if row.Level.Valid {
		lv, err := domain.ParseLevel(row.Level.String)
		if err != nil {
			return domain.Invite{}, err
		}
		out.Level = lv
	}
	return out, nil
}
