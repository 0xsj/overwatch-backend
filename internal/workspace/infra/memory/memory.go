package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/0xsj/overwatch-backend/internal/workspace/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Store struct {
	mu         sync.Mutex
	workspaces map[id.ID]domain.Workspace
	depth      int
}

func New() *Store { return &Store{workspaces: map[id.ID]domain.Workspace{}} }

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
	snapshot := make(map[id.ID]domain.Workspace, len(s.workspaces))
	for k, v := range s.workspaces {
		snapshot[k] = v
	}
	s.depth = 1
	s.mu.Unlock()

	restore := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.workspaces, s.depth = snapshot, 0
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

// The live-name index, as a scan, case-folded — because the Postgres index is
// on lower(name) and an adapter that accepts what the database refuses is worse
// than none.
func (s *Store) Create(_ context.Context, w domain.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.workspaces {
		if existing.OrgID == w.OrgID && !existing.Archived() &&
			strings.EqualFold(existing.Name, w.Name) {
			return fmt.Errorf("workspace: insert: %w", domain.ErrNameTaken)
		}
	}
	s.workspaces[w.ID] = w
	return nil
}

func (s *Store) ByID(_ context.Context, want id.ID) (domain.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.workspaces[want]
	if !ok {
		return domain.Workspace{}, fmt.Errorf("workspace: read: %w", domain.ErrNotFound)
	}
	return w, nil
}

func (s *Store) ForOrg(_ context.Context, org id.ID) ([]domain.Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Workspace, 0, len(s.workspaces))
	for _, w := range s.workspaces {
		if w.OrgID == org && !w.Archived() {
			out = append(out, w)
		}
	}
	return out, nil
}

func (s *Store) Save(_ context.Context, w domain.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.workspaces[w.ID]
	if !ok {
		return fmt.Errorf("workspace: update: %w", domain.ErrNotFound)
	}
	if existing.Version != w.Version-1 {
		return fmt.Errorf("workspace: update: %w", domain.ErrStaleWrite)
	}
	s.workspaces[w.ID] = w
	return nil
}
