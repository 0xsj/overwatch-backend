package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/0xsj/overwatch-backend/internal/tool/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Store struct {
	mu       sync.Mutex
	tools    map[id.ID]domain.Tool
	mappings map[id.ID]domain.Mapping
	depth    int
}

func New() *Store {
	return &Store{tools: map[id.ID]domain.Tool{}, mappings: map[id.ID]domain.Mapping{}}
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
	tools := make(map[id.ID]domain.Tool, len(s.tools))
	for k, v := range s.tools {
		tools[k] = v
	}
	mappings := make(map[id.ID]domain.Mapping, len(s.mappings))
	for k, v := range s.mappings {
		mappings[k] = v
	}
	s.depth = 1
	s.mu.Unlock()

	restore := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.tools, s.mappings, s.depth = tools, mappings, 0
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

func (s *Store) Create(_ context.Context, t domain.Tool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.tools {
		if existing.OrgID == t.OrgID && !existing.Archived() &&
			strings.EqualFold(existing.Name, t.Name) {
			return fmt.Errorf("tool: insert tool: %w", domain.ErrNameTaken)
		}
	}
	s.tools[t.ID] = t
	return nil
}

// ByID takes the org and checks it, rather than reading by id alone. A store
// that ignores the tenant argument passes every test and leaks in production.
func (s *Store) ByID(_ context.Context, org, want id.ID) (domain.Tool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tools[want]
	if !ok || t.OrgID != org {
		return domain.Tool{}, fmt.Errorf("tool: read tool: %w", domain.ErrNotFound)
	}
	return t, nil
}

func (s *Store) ForOrg(_ context.Context, org id.ID, includeArchived bool) ([]domain.Tool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Tool, 0, len(s.tools))
	for _, t := range s.tools {
		if t.OrgID != org || (t.Archived() && !includeArchived) {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *Store) Save(_ context.Context, t domain.Tool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.tools[t.ID]
	if !ok || existing.OrgID != t.OrgID {
		return fmt.Errorf("tool: save tool: %w", domain.ErrNotFound)
	}
	if existing.Version != t.Version-1 {
		return fmt.Errorf("tool: save tool: %w", domain.ErrStaleWrite)
	}
	if !t.Archived() {
		for _, other := range s.tools {
			if other.ID != t.ID && other.OrgID == t.OrgID && !other.Archived() &&
				strings.EqualFold(other.Name, t.Name) {
				return fmt.Errorf("tool: save tool: %w", domain.ErrNameTaken)
			}
		}
	}
	s.tools[t.ID] = t
	return nil
}

func (s *Store) CreateMapping(_ context.Context, m domain.Mapping) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.mappings {
		if existing.ToolID == m.ToolID && existing.Field == m.Field &&
			existing.Version == m.Version {
			return fmt.Errorf("tool: insert mapping: %w", domain.ErrStaleWrite)
		}
	}
	s.mappings[m.ID] = m
	return nil
}

func (s *Store) MappingByID(_ context.Context, org, want id.ID) (domain.Mapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.mappings[want]
	if !ok || m.OrgID != org {
		return domain.Mapping{}, fmt.Errorf("tool: read mapping: %w", domain.ErrMappingGone)
	}
	return m, nil
}

func (s *Store) MappingsForTool(_ context.Context, org, tool id.ID) ([]domain.Mapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Mapping, 0, len(s.mappings))
	for _, m := range s.mappings {
		if m.OrgID == org && m.ToolID == tool {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Version > out[j].Version
	})
	return out, nil
}

func (s *Store) LiveMapping(_ context.Context, tool id.ID, field string) (domain.Mapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.mappings {
		if m.ToolID == tool && m.Field == field && m.Live() {
			return m, nil
		}
	}
	return domain.Mapping{}, fmt.Errorf("tool: live mapping: %w", domain.ErrMappingGone)
}

func (s *Store) NextVersion(_ context.Context, tool id.ID, field string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, m := range s.mappings {
		if m.ToolID == tool && m.Field == field && m.Version > highest {
			highest = m.Version
		}
	}
	return highest + 1, nil
}

// SaveMapping re-states `mapping_live`. Without it the memory adapter accepts
// two live versions of one field and the promotion order stops mattering — the
// bug the transaction exists to prevent would only ever appear against Postgres.
func (s *Store) SaveMapping(_ context.Context, m domain.Mapping) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.mappings[m.ID]; !ok {
		return fmt.Errorf("tool: save mapping: %w", domain.ErrMappingGone)
	}
	if m.Live() {
		for _, other := range s.mappings {
			if other.ID != m.ID && other.ToolID == m.ToolID &&
				other.Field == m.Field && other.Live() {
				return fmt.Errorf("tool: save mapping: %w", domain.ErrAlreadyLive)
			}
		}
	}
	s.mappings[m.ID] = m
	return nil
}
