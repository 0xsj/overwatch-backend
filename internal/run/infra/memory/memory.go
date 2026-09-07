package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/0xsj/overwatch-backend/internal/run/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Store struct {
	mu          sync.Mutex
	runs        map[id.ID]domain.Run
	invocations map[id.ID]domain.Invocation
	artifacts   map[id.ID]domain.Artifact
	depth       int
}

func New() *Store {
	return &Store{
		runs:        map[id.ID]domain.Run{},
		invocations: map[id.ID]domain.Invocation{},
		artifacts:   map[id.ID]domain.Artifact{},
	}
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
	runs := make(map[id.ID]domain.Run, len(s.runs))
	for k, v := range s.runs {
		runs[k] = v
	}
	invocations := make(map[id.ID]domain.Invocation, len(s.invocations))
	for k, v := range s.invocations {
		invocations[k] = v
	}
	artifacts := make(map[id.ID]domain.Artifact, len(s.artifacts))
	for k, v := range s.artifacts {
		artifacts[k] = v
	}
	s.depth = 1
	s.mu.Unlock()

	restore := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.runs, s.invocations, s.artifacts, s.depth = runs, invocations, artifacts, 0
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

func (s *Store) Create(_ context.Context, r domain.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return nil
}

func (s *Store) ByID(_ context.Context, workspace, want id.ID) (domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[want]
	if !ok || r.WorkspaceID != workspace {
		return domain.Run{}, fmt.Errorf("run: read run: %w", domain.ErrNotFound)
	}
	return r, nil
}

// Page restates the keyset predicate: strictly before `(started_at, id)`,
// newest first. An adapter that ignored the cursor would page correctly on the
// first request and loop forever on the second.
func (s *Store) Page(_ context.Context, workspace, target id.ID,
	before time.Time, beforeID id.ID, limit int) ([]domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Run, 0, len(s.runs))
	for _, r := range s.runs {
		if r.WorkspaceID != workspace {
			continue
		}
		if !target.IsZero() && r.TargetID != target {
			continue
		}
		if !before.IsZero() {
			if r.StartedAt.After(before) {
				continue
			}
			if r.StartedAt.Equal(before) && !less(r.ID, beforeID) {
				continue
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.After(out[j].StartedAt)
		}
		return less(out[j].ID, out[i].ID)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) Finish(_ context.Context, r domain.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	held, ok := s.runs[r.ID]
	if !ok || held.WorkspaceID != r.WorkspaceID {
		return fmt.Errorf("run: finish run: %w", domain.ErrNotFound)
	}
	if held.Version != r.Version-1 {
		return fmt.Errorf("run: finish run: %w", domain.ErrStaleWrite)
	}
	s.runs[r.ID] = r
	return nil
}

// Claim is NOT a claim. `for update skip locked` has no meaning in a map, so
// this returns matching runs without excluding another worker — the doc says so
// rather than the shape implying a guarantee it cannot keep.
func (s *Store) Claim(_ context.Context, batch int) ([]domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Run, 0, batch)
	for _, r := range s.runs {
		// `running` and nothing else — a run whose every step was refused has
		// nothing pending and still has to be finished.
		if r.State != domain.StateRunning {
			continue
		}
		out = append(out, r)
		if len(out) == batch {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

func (s *Store) PlanInvocation(_ context.Context, i domain.Invocation) error {
	if err := i.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invocations[i.ID] = i
	return nil
}

func (s *Store) SaveInvocation(_ context.Context, i domain.Invocation) error {
	if err := i.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.invocations[i.ID]; !ok {
		return fmt.Errorf("run: save invocation: %w", domain.ErrInvocationNotFound)
	}
	s.invocations[i.ID] = i
	return nil
}

func (s *Store) Invocations(_ context.Context, run id.ID) ([]domain.Invocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Invocation, 0)
	for _, i := range s.invocations {
		if i.RunID == run {
			out = append(out, i)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Sequence < out[b].Sequence })
	return out, nil
}

func (s *Store) InvocationByID(_ context.Context, workspace, want id.ID) (domain.Invocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.invocations[want]
	if !ok || i.WorkspaceID != workspace {
		return domain.Invocation{}, fmt.Errorf("run: read invocation: %w", domain.ErrInvocationNotFound)
	}
	return i, nil
}

func (s *Store) Refusals(_ context.Context, workspace, rule id.ID, limit int) ([]domain.Invocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Invocation, 0)
	for _, i := range s.invocations {
		if i.WorkspaceID == workspace && i.RefusalRule == rule {
			out = append(out, i)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].FinishedAt.After(out[b].FinishedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// AddArtifact restates `unique (invocation_id, stream)` as an idempotent write.
// The bytes are content-addressed, so a retried write is identical and ignoring
// it is correct rather than merely convenient.
func (s *Store) AddArtifact(_ context.Context, a domain.Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, held := range s.artifacts {
		if held.InvocationID == a.InvocationID && held.Stream == a.Stream {
			return nil
		}
	}
	s.artifacts[a.ID] = a
	return nil
}

func (s *Store) Artifacts(_ context.Context, invocation id.ID) ([]domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Artifact, 0)
	for _, a := range s.artifacts {
		if a.InvocationID == invocation {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stream < out[j].Stream })
	return out, nil
}

func (s *Store) ArtifactByID(_ context.Context, workspace, want id.ID) (domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.artifacts[want]
	if !ok || a.WorkspaceID != workspace {
		return domain.Artifact{}, fmt.Errorf("run: read artifact: %w", domain.ErrArtifactNotFound)
	}
	return a, nil
}

// less compares two ids the way Postgres compares uuids: as bytes, big-endian.
// A different order here would page differently from the database, which is the
// kind of divergence that only shows up on the second page.
func less(a, b id.ID) bool {
	for n := range a {
		if a[n] != b[n] {
			return a[n] < b[n]
		}
	}
	return false
}
