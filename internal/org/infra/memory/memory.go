package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Store struct {
	mu      sync.Mutex
	orgs    map[id.ID]domain.Org
	members map[id.ID]domain.Member
	depth   int
}

func New() *Store {
	return &Store{orgs: map[id.ID]domain.Org{}, members: map[id.ID]domain.Member{}}
}

func (s *Store) InTx(ctx context.Context, fn func(context.Context) error) (err error) {
	s.mu.Lock()
	if s.depth > 0 {
		s.depth++
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.depth--
			s.mu.Unlock()
		}()
		return fn(ctx)
	}
	orgs, members := clone(s.orgs), clone(s.members)
	s.depth = 1
	s.mu.Unlock()

	restore := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.orgs, s.members, s.depth = orgs, members, 0
	}
	defer func() {
		switch p := recover(); {
		case p != nil:
			restore()
			panic(p)
		case err != nil:
			restore()
		default:
			s.mu.Lock()
			s.depth = 0
			s.mu.Unlock()
		}
	}()
	return fn(ctx)
}

func clone[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Store) CreateOrg(_ context.Context, o domain.Org) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orgs[o.ID] = o
	return nil
}

func (s *Store) OrgByID(_ context.Context, want id.ID) (domain.Org, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orgs[want]
	if !ok {
		return domain.Org{}, fmt.Errorf("org: read org: %w", domain.ErrOrgNotFound)
	}
	return o, nil
}

// The live-membership index, as a scan. An index here would be a second thing to
// keep consistent with the map, and this adapter exists to be obviously correct.
func (s *Store) AddMember(_ context.Context, m domain.Member) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.members {
		if existing.OrgID == m.OrgID && existing.AccountID == m.AccountID && !existing.Archived() {
			return fmt.Errorf("org: insert member: %w", domain.ErrMemberExists)
		}
	}
	s.members[m.ID] = m
	return nil
}

func (s *Store) LiveMemberFor(_ context.Context, orgID, account id.ID) (domain.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.members {
		if m.OrgID == orgID && m.AccountID == account && !m.Archived() {
			return m, nil
		}
	}
	return domain.Member{}, fmt.Errorf("org: read membership: %w", domain.ErrMemberNotFound)
}

// SaveMember refuses to archive the last live owner, because an org nobody can
// administer is not a state to reach by accident — and there is no second role
// to promote anybody into yet.
func (s *Store) SaveMember(_ context.Context, m domain.Member) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.members[m.ID]
	if !ok {
		return fmt.Errorf("org: update member: %w", domain.ErrMemberNotFound)
	}
	if existing.Version != m.Version-1 {
		return fmt.Errorf("org: update member: %w", domain.ErrStaleWrite)
	}
	if m.Archived() && m.Role == domain.RoleOwner {
		live := 0
		for _, other := range s.members {
			if other.OrgID == m.OrgID && other.Role == domain.RoleOwner && !other.Archived() {
				live++
			}
		}
		if live <= 1 {
			return fmt.Errorf("org: archive member: %w", domain.ErrLastOwner)
		}
	}
	s.members[m.ID] = m
	return nil
}
