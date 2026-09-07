package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/0xsj/overwatch-backend/internal/check/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Store struct {
	mu     sync.Mutex
	checks map[id.ID]domain.Check
	chains map[id.ID]domain.Chain
	depth  int
}

func New() *Store {
	return &Store{checks: map[id.ID]domain.Check{}, chains: map[id.ID]domain.Chain{}}
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
	checks := make(map[id.ID]domain.Check, len(s.checks))
	for k, v := range s.checks {
		checks[k] = v
	}
	chains := make(map[id.ID]domain.Chain, len(s.chains))
	for k, v := range s.chains {
		chains[k] = v
	}
	s.depth = 1
	s.mu.Unlock()

	restore := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.checks, s.chains, s.depth = checks, chains, 0
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

func (s *Store) Create(_ context.Context, c domain.Check) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.nameFree(c); err != nil {
		return fmt.Errorf("check: insert check: %w", err)
	}
	s.checks[c.ID] = c
	return nil
}

func (s *Store) ByID(_ context.Context, org, want id.ID) (domain.Check, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.checks[want]
	if !ok || c.OrgID != org {
		return domain.Check{}, fmt.Errorf("check: read check: %w", domain.ErrNotFound)
	}
	return c, nil
}

func (s *Store) ForOrg(_ context.Context, org id.ID, includeArchived bool) ([]domain.Check, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Check, 0, len(s.checks))
	for _, c := range s.checks {
		if c.OrgID != org || (c.Archived() && !includeArchived) {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Schedulable restates the SQL's three exclusions. An adapter that returned
// disabled or human checks would make the scheduler spawn things the database
// would not.
func (s *Store) Schedulable(_ context.Context) ([]domain.Check, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Check, 0)
	for _, c := range s.checks {
		if c.Archived() || !c.Enabled || c.Human || c.Interval <= 0 {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OrgID != out[j].OrgID {
			return less(out[i].OrgID, out[j].OrgID)
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// less compares two ids as bytes, the way Postgres orders uuids.
func less(a, b id.ID) bool {
	for n := range a {
		if a[n] != b[n] {
			return a[n] < b[n]
		}
	}
	return false
}

func (s *Store) Save(_ context.Context, c domain.Check) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.checks[c.ID]
	if !ok || existing.OrgID != c.OrgID {
		return fmt.Errorf("check: save check: %w", domain.ErrNotFound)
	}
	if existing.Version != c.Version-1 {
		return fmt.Errorf("check: save check: %w", domain.ErrStaleWrite)
	}
	if err := s.nameFree(c); err != nil {
		return fmt.Errorf("check: save check: %w", err)
	}
	s.checks[c.ID] = c
	return nil
}

func (s *Store) Chain(_ context.Context, want id.ID) (domain.Chain, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.chains[want]
	// Copy both slices out. A caller that appends to what it was handed would
	// otherwise edit the store through a read, which no database can do and
	// which would make the memory adapter permissive in a way nothing catches.
	out := domain.Chain{
		Steps: append([]domain.Step(nil), held.Steps...),
		Flows: append([]domain.Flow(nil), held.Flows...),
	}
	return out, nil
}

func (s *Store) SaveChain(_ context.Context, check id.ID, chain domain.Chain) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.checks[check]; !ok {
		return fmt.Errorf("check: save chain: %w", domain.ErrNotFound)
	}
	s.chains[check] = domain.Chain{
		Steps: append([]domain.Step(nil), chain.Steps...),
		Flows: append([]domain.Flow(nil), chain.Flows...),
	}
	return nil
}

// UsingTool restates the join. Archived checks are excluded, matching the SQL:
// a check nobody can run is not a reason to keep a tool.
func (s *Store) UsingTool(_ context.Context, org, tool id.ID) ([]domain.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Usage, 0)
	for want, chain := range s.chains {
		c, ok := s.checks[want]
		if !ok || c.OrgID != org || c.Archived() {
			continue
		}
		for _, step := range chain.Steps {
			if step.ToolID == tool {
				out = append(out, domain.Usage{CheckID: c.ID, Name: c.Name})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) nameFree(c domain.Check) error {
	if c.Archived() {
		return nil
	}
	for _, other := range s.checks {
		if other.ID != c.ID && other.OrgID == c.OrgID && !other.Archived() &&
			strings.EqualFold(other.Name, c.Name) {
			return domain.ErrNameTaken
		}
	}
	return nil
}
