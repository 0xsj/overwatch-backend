package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Store struct {
	mu           sync.Mutex
	observations map[id.ID]domain.Observation
	unmapped     map[id.ID]domain.Unmapped
}

func New() *Store {
	return &Store{
		observations: map[id.ID]domain.Observation{},
		unmapped:     map[id.ID]domain.Unmapped{},
	}
}

func (s *Store) Create(_ context.Context, o domain.Observation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observations[o.ID] = o
	return nil
}

func (s *Store) ByID(_ context.Context, workspace, want id.ID) (domain.Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.observations[want]
	if !ok || o.WorkspaceID != workspace {
		return domain.Observation{}, fmt.Errorf("observation: read: %w", domain.ErrNotFound)
	}
	return o, nil
}

func (s *Store) ForInvocation(_ context.Context, workspace, invocation id.ID, limit int) ([]domain.Observation, error) {
	return s.list(limit, func(o domain.Observation) bool {
		return o.WorkspaceID == workspace && o.InvocationID == invocation
	}), nil
}

func (s *Store) ForSubject(_ context.Context, workspace id.ID, kind, value string, limit int) ([]domain.Observation, error) {
	return s.list(limit, func(o domain.Observation) bool {
		return o.WorkspaceID == workspace && o.SubjectKind == kind && o.SubjectValue == value
	}), nil
}

// list sorts by field then NEWEST FIRST, matching the index the asset drawer
// reads: "the state of each field" is the first row per field, and an adapter
// that ordered oldest-first would hand a caller the stalest value.
func (s *Store) list(limit int, keep func(domain.Observation) bool) []domain.Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Observation, 0, len(s.observations))
	for _, o := range s.observations {
		if keep(o) {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SubjectValue != out[j].SubjectValue {
			return out[i].SubjectValue < out[j].SubjectValue
		}
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].ObservedAt.After(out[j].ObservedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) Subjects(_ context.Context, workspace id.ID, limit int) ([]domain.Subject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type key struct{ kind, value string }
	held := map[key]*domain.Subject{}
	fields := map[key]map[string]bool{}
	for _, o := range s.observations {
		if o.WorkspaceID != workspace {
			continue
		}
		k := key{o.SubjectKind, o.SubjectValue}
		if held[k] == nil {
			held[k] = &domain.Subject{Kind: o.SubjectKind, Value: o.SubjectValue}
			fields[k] = map[string]bool{}
		}
		held[k].Observations++
		fields[k][o.Field] = true
		if o.ObservedAt.After(held[k].LastSeen) {
			held[k].LastSeen = o.ObservedAt
		}
	}
	out := make([]domain.Subject, 0, len(held))
	for k, subject := range held {
		subject.Fields = len(fields[k])
		out = append(out, *subject)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// RecordUnmapped restates `unique (artifact_id, path)` as an upsert that
// REPLACES the count. `seen` is a property of the artifact, not of how many
// times it was read — adding would make the LEFT ALONE denominator grow every
// time somebody re-extracts.
func (s *Store) RecordUnmapped(_ context.Context, u domain.Unmapped) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for held, existing := range s.unmapped {
		if existing.ArtifactID == u.ArtifactID && existing.Path == u.Path {
			existing.Seen = u.Seen
			existing.Sample = u.Sample
			existing.RecordedAt = u.RecordedAt
			s.unmapped[held] = existing
			return nil
		}
	}
	s.unmapped[u.ID] = u
	return nil
}

func (s *Store) UnmappedFor(_ context.Context, workspace, invocation id.ID) ([]domain.Unmapped, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Unmapped, 0)
	for _, u := range s.unmapped {
		if u.WorkspaceID == workspace && u.InvocationID == invocation {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seen != out[j].Seen {
			return out[i].Seen > out[j].Seen
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func (s *Store) Quality(_ context.Context, workspace, invocation id.ID) (domain.Quality, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := domain.Quality{}
	fields := map[string]bool{}
	// PATHS, not observations — one mapping is one path however many values it
	// flattened into. The identity FIELDS SEEN = MAPPED + LEFT ALONE depends on
	// it, and Postgres counts `distinct mapping_id` for the same reason.
	mappings := map[id.ID]bool{}
	for _, o := range s.observations {
		if o.WorkspaceID == workspace && o.InvocationID == invocation {
			out.Observations++
			mappings[o.MappingID] = true
			fields[o.Field] = true
		}
	}
	out.Mapped = len(mappings)
	for _, u := range s.unmapped {
		if u.WorkspaceID == workspace && u.InvocationID == invocation {
			out.LeftAlone++
		}
	}
	out.Fields = len(fields)
	return out, nil
}
