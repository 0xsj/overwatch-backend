package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/0xsj/overwatch-backend/internal/scope/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Store struct {
	mu    sync.Mutex
	rules map[id.ID]domain.Rule
	depth int
}

func New() *Store { return &Store{rules: map[id.ID]domain.Rule{}} }

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
	snapshot := make(map[id.ID]domain.Rule, len(s.rules))
	for k, v := range s.rules {
		snapshot[k] = v
	}
	s.depth = 1
	s.mu.Unlock()

	restore := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.rules, s.depth = snapshot, 0
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

func (s *Store) Create(_ context.Context, r domain.Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules[r.ID] = r
	return nil
}

func (s *Store) ByID(_ context.Context, workspace, want id.ID) (domain.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rules[want]
	if !ok || r.WorkspaceID != workspace {
		return domain.Rule{}, fmt.Errorf("scope: read rule: %w", domain.ErrNotFound)
	}
	return r, nil
}

func (s *Store) Live(_ context.Context, workspace, target id.ID) ([]domain.Rule, error) {
	return s.list(workspace, target, false), nil
}

// All includes superseded rules, newest first. They are the history the
// citations point at — an invocation refusal and a finding's scope proof both
// name a rule id, and both captured it at the time.
func (s *Store) All(_ context.Context, workspace, target id.ID) ([]domain.Rule, error) {
	return s.list(workspace, target, true), nil
}

// Supersede is the ONLY write that touches an existing row, and it moves the
// two lifecycle columns and nothing else — the same restraint the SQL has.
func (s *Store) Supersede(_ context.Context, r domain.Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	held, ok := s.rules[r.ID]
	if !ok || held.WorkspaceID != r.WorkspaceID {
		return fmt.Errorf("scope: supersede rule: %w", domain.ErrNotFound)
	}
	if held.Superseded() {
		return fmt.Errorf("scope: supersede rule: %w", domain.ErrSuperseded)
	}
	held.SupersededAt = r.SupersededAt
	held.SupersededBy = r.SupersededBy
	s.rules[r.ID] = held
	return nil
}

func (s *Store) list(workspace, target id.ID, all bool) []domain.Rule {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Rule, 0, len(s.rules))
	for _, r := range s.rules {
		if r.WorkspaceID != workspace || r.TargetID != target {
			continue
		}
		if r.Superseded() && !all {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
