package memory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/0xsj/overwatch-backend/internal/target/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Store struct {
	mu      sync.Mutex
	targets map[id.ID]domain.Target
	depth   int
}

func New() *Store { return &Store{targets: map[id.ID]domain.Target{}} }

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
	snapshot := make(map[id.ID]domain.Target, len(s.targets))
	for k, v := range s.targets {
		snapshot[k] = v
	}
	s.depth = 1
	s.mu.Unlock()

	restore := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.targets, s.depth = snapshot, 0
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

func (s *Store) Create(_ context.Context, t domain.Target) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.nameFree(t); err != nil {
		return fmt.Errorf("target: insert: %w", err)
	}
	s.targets[t.ID] = t
	return nil
}

func (s *Store) ByID(_ context.Context, workspace, want id.ID) (domain.Target, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.targets[want]
	if !ok || t.WorkspaceID != workspace {
		return domain.Target{}, fmt.Errorf("target: read: %w", domain.ErrNotFound)
	}
	return t, nil
}

func (s *Store) Live(_ context.Context, workspace id.ID) ([]domain.Target, error) {
	return s.list(workspace, false), nil
}

// All includes archived targets, newest first — they are what makes one
// reachable and therefore reopenable.
func (s *Store) All(_ context.Context, workspace id.ID) ([]domain.Target, error) {
	return s.list(workspace, true), nil
}

func (s *Store) Save(_ context.Context, t domain.Target) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.targets[t.ID]
	if !ok || existing.WorkspaceID != t.WorkspaceID {
		return fmt.Errorf("target: update: %w", domain.ErrNotFound)
	}
	if existing.Version != t.Version-1 {
		return fmt.Errorf("target: update: %w", domain.ErrStaleWrite)
	}
	if err := s.nameFree(t); err != nil {
		return fmt.Errorf("target: update: %w", err)
	}
	s.targets[t.ID] = t
	return nil
}

// nameFree is the partial unique index as a scan. Archiving RELEASES the name,
// so this checks nothing for an archived row — and a reopen can therefore
// collide, which is the refusal decisions/0027 wants rather than a silent
// rename.
func (s *Store) nameFree(t domain.Target) error {
	if t.Archived() {
		return nil
	}
	for _, other := range s.targets {
		if other.ID != t.ID && other.WorkspaceID == t.WorkspaceID &&
			!other.Archived() && strings.EqualFold(other.Name, t.Name) {
			return domain.ErrNameTaken
		}
	}
	return nil
}

func (s *Store) list(workspace id.ID, all bool) []domain.Target {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Target, 0, len(s.targets))
	for _, t := range s.targets {
		if t.WorkspaceID != workspace || (t.Archived() && !all) {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}
