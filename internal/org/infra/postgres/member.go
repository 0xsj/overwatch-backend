package postgres

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/internal/org/infra/postgres/orgdb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

func (s *Store) AddMember(ctx context.Context, m domain.Member) error {
	err := s.q(ctx).InsertMember(ctx, orgdb.InsertMemberParams{
		ID:         uuid(m.ID),
		OrgID:      uuid(m.OrgID),
		AccountID:  uuid(m.AccountID),
		Role:       m.Role.String(),
		Status:     m.Status.String(),
		Version:    int32(m.Version),
		CreatedAt:  stamp(m.CreatedAt),
		UpdatedAt:  stamp(m.UpdatedAt),
		ArchivedAt: stamp(m.ArchivedAt),
	})
	if err == nil {
		return nil
	}
	translated := postgres.Translate(ctx, err, "org: insert member")
	if postgres.IsConstraint(translated, "member_live_account") {
		return fmt.Errorf("org: insert member: %w", domain.ErrMemberExists)
	}
	return translated
}

func (s *Store) MemberByID(ctx context.Context, want id.ID) (domain.Member, error) {
	row, err := s.q(ctx).MemberByID(ctx, uuid(want))
	if err != nil {
		return domain.Member{}, notFound(ctx, err, "org: read member")
	}
	return member(row)
}

func (s *Store) LiveMemberFor(ctx context.Context, orgID, account id.ID) (domain.Member, error) {
	row, err := s.q(ctx).LiveMemberFor(ctx, orgdb.LiveMemberForParams{OrgID: uuid(orgID), AccountID: uuid(account)})
	if err != nil {
		return domain.Member{}, notFound(ctx, err, "org: read membership")
	}
	return member(row)
}

func (s *Store) MembersOf(ctx context.Context, orgID id.ID) ([]domain.Member, error) {
	rows, err := s.q(ctx).MembersOf(ctx, uuid(orgID))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "org: list members")
	}
	out := make([]domain.Member, 0, len(rows))
	for _, row := range rows {
		m, err := member(row)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// SaveMember refuses to archive the last live owner. The check and the write are
// one statement apart, so it is the caller's transaction that makes it hold —
// which is why the count is read inside whatever transaction is on the context
// rather than on a fresh connection.
func (s *Store) SaveMember(ctx context.Context, m domain.Member) error {
	if m.Archived() && m.Role == domain.RoleOwner {
		n, err := s.q(ctx).CountLiveOwners(ctx, uuid(m.OrgID))
		if err != nil {
			return postgres.Translate(ctx, err, "org: count owners")
		}
		if n <= 1 {
			return fmt.Errorf("org: archive member: %w", domain.ErrLastOwner)
		}
	}
	n, err := s.q(ctx).UpdateMember(ctx, orgdb.UpdateMemberParams{
		ID:         uuid(m.ID),
		Role:       m.Role.String(),
		Status:     m.Status.String(),
		Version:    int32(m.Version),
		UpdatedAt:  stamp(m.UpdatedAt),
		ArchivedAt: stamp(m.ArchivedAt),
		Version_2:  int32(m.Version - 1),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "org: update member")
	}
	if n == 0 {
		if _, err := s.MemberByID(ctx, m.ID); err != nil {
			return err
		}
		return fmt.Errorf("org: update member: %w", domain.ErrStaleWrite)
	}
	return nil
}

func notFound(ctx context.Context, err error, op string) error {
	translated := postgres.Translate(ctx, err, op)
	if errors.IsKind(translated, errors.NotFound) {
		return fmt.Errorf("%s: %w", op, domain.ErrMemberNotFound)
	}
	return translated
}
