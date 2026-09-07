package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// targetable is 0034's facet, restated. The SQL view carries the same list and
// `root/vocabulary_test.go` is what stops the two drifting.
var targetable = map[string]bool{
	"host": true, "cidr": true, "ip": true, "asn": true,
	"url": true, "repo": true, "email": true,
}

type Store struct {
	mu           sync.Mutex
	entities     map[id.ID]domain.Entity
	fragments    map[id.ID]domain.Fragment
	attributions map[id.ID]domain.Attribution
}

func New() *Store {
	return &Store{
		entities:     map[id.ID]domain.Entity{},
		fragments:    map[id.ID]domain.Fragment{},
		attributions: map[id.ID]domain.Attribution{},
	}
}

func (s *Store) CreateEntity(_ context.Context, e domain.Entity) error {
	if err := e.Judgement.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// `entity_one_root_per_target`, as a scan. A redelivered `target.added`
	// must be a no-op rather than a second root.
	if !e.TargetID.IsZero() {
		for _, held := range s.entities {
			if held.TargetID == e.TargetID {
				return nil
			}
		}
	}
	s.entities[e.ID] = e
	return nil
}

func (s *Store) EntityByID(_ context.Context, workspace, want id.ID) (domain.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entities[want]
	if !ok || e.WorkspaceID != workspace {
		return domain.Entity{}, fmt.Errorf("entity: read entity: %w", domain.ErrNotFound)
	}
	return e, nil
}

func (s *Store) RootFor(_ context.Context, target id.ID) (domain.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entities {
		if e.TargetID == target && !target.IsZero() {
			return e, nil
		}
	}
	return domain.Entity{}, fmt.Errorf("entity: root for target: %w", domain.ErrNotFound)
}

func (s *Store) Entities(_ context.Context, workspace id.ID, limit int) ([]domain.Entity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Entity, 0, len(s.entities))
	for _, e := range s.entities {
		if e.WorkspaceID == workspace {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return cap(out, limit), nil
}

func (s *Store) JudgeEntity(_ context.Context, e domain.Entity) error {
	if err := e.Judgement.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entities[e.ID]; !ok {
		return fmt.Errorf("entity: judge entity: %w", domain.ErrNotFound)
	}
	s.entities[e.ID] = e
	return nil
}

// Upsert restates `fragment_identity` — the dedup that makes a fragment a tuple
// — including the three merge rules the SQL carries: observations ADD, first_seen
// takes the earliest, last_seen the latest, and origin is promoted and never
// demoted.
func (s *Store) Upsert(_ context.Context, f domain.Fragment) (domain.Fragment, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for held, existing := range s.fragments {
		if existing.WorkspaceID != f.WorkspaceID || existing.Kind != f.Kind || existing.Value != f.Value {
			continue
		}
		merged := existing.Seen(f.LastSeen, f.Observations)
		if !f.FirstSeen.IsZero() && (merged.FirstSeen.IsZero() || f.FirstSeen.Before(merged.FirstSeen)) {
			merged.FirstSeen = f.FirstSeen
		}
		s.fragments[held] = merged
		return merged, false, nil
	}
	s.fragments[f.ID] = f
	return f, true, nil
}

func (s *Store) FragmentByID(_ context.Context, workspace, want id.ID) (domain.Fragment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fragments[want]
	if !ok || f.WorkspaceID != workspace {
		return domain.Fragment{}, fmt.Errorf("entity: read fragment: %w", domain.ErrFragmentNotFound)
	}
	return f, nil
}

func (s *Store) Fragments(_ context.Context, workspace id.ID, kind string, limit int) ([]domain.Fragment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Fragment, 0, len(s.fragments))
	for _, f := range s.fragments {
		if f.WorkspaceID != workspace || (kind != "" && f.Kind != kind) {
			continue
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].Value < out[j].Value
	})
	return cap(out, limit), nil
}

func (s *Store) JudgeFragment(_ context.Context, f domain.Fragment) error {
	if err := f.Judgement.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.fragments[f.ID]; !ok {
		return fmt.Errorf("entity: judge fragment: %w", domain.ErrFragmentNotFound)
	}
	s.fragments[f.ID] = f
	return nil
}

// Attribute restates `unique (entity_id, fragment_id)` as an ignore, so a
// redelivered subscriber event writes no second claim.
// MarkRead writes the read pair and NOTHING else — the same restraint the SQL
// has, and for the same reason: reading is not ruling.
func (s *Store) MarkRead(_ context.Context, f domain.Fragment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	held, ok := s.fragments[f.ID]
	if !ok || held.WorkspaceID != f.WorkspaceID {
		return fmt.Errorf("entity: mark read: %w", domain.ErrFragmentNotFound)
	}
	held.ReadAt, held.ReadBy = f.ReadAt, f.ReadBy
	s.fragments[f.ID] = held
	return nil
}

func (s *Store) Attribute(_ context.Context, a domain.Attribution) error {
	if err := a.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, held := range s.attributions {
		if held.EntityID == a.EntityID && held.FragmentID == a.FragmentID {
			return nil
		}
	}
	s.attributions[a.ID] = a
	return nil
}

func (s *Store) AttributionByID(_ context.Context, workspace, want id.ID) (domain.Attribution, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.attributions[want]
	if !ok || a.WorkspaceID != workspace {
		return domain.Attribution{}, fmt.Errorf("entity: read attribution: %w", domain.ErrAttributionNotFound)
	}
	return a, nil
}

func (s *Store) AttributionsFor(_ context.Context, fragment id.ID) ([]domain.Attribution, error) {
	return s.claims(func(a domain.Attribution) bool { return a.FragmentID == fragment }, 0), nil
}

func (s *Store) AttributionsOn(_ context.Context, entity id.ID, state string, limit int) ([]domain.Attribution, error) {
	return s.claims(func(a domain.Attribution) bool {
		return a.EntityID == entity && (state == "" || a.State.String() == state)
	}, limit), nil
}

func (s *Store) claims(keep func(domain.Attribution) bool, limit int) []domain.Attribution {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Attribution, 0, len(s.attributions))
	for _, a := range s.attributions {
		if keep(a) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return cap(out, limit)
}

// Decide restates the SQL's `state = 'proposed'` predicate, which is what makes
// two people accepting at once produce one winner rather than a lost update.
func (s *Store) Decide(_ context.Context, a domain.Attribution) error {
	if err := a.Valid(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	held, ok := s.attributions[a.ID]
	if !ok || held.State != domain.Proposed {
		return fmt.Errorf("entity: decide: %w", domain.ErrAlreadyDecided)
	}
	s.attributions[a.ID] = a
	return nil
}

// Assets restates the VIEW's predicate, in the order the SQL states it: a
// targetable kind, an ACCEPTED attribution, to an entity that is a target's ROOT.
// An accepted attribution to any other entity does not make a fragment an asset.
func (s *Store) Assets(_ context.Context, workspace, target id.ID, limit int) ([]domain.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Asset, 0)
	for _, a := range s.attributions {
		if a.WorkspaceID != workspace || a.State != domain.Accepted {
			continue
		}
		root, ok := s.entities[a.EntityID]
		if !ok || root.TargetID.IsZero() {
			continue
		}
		if !target.IsZero() && root.TargetID != target {
			continue
		}
		f, ok := s.fragments[a.FragmentID]
		if !ok || !targetable[f.Kind] {
			continue
		}
		out = append(out, domain.Asset{
			Fragment: f, AttributionID: a.ID, Claimant: a.Claimant, Basis: a.Basis,
			RootEntityID: root.ID, TargetID: root.TargetID,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].Value < out[j].Value
	})
	return cap(out, limit), nil
}

func cap[T any](in []T, limit int) []T {
	if limit > 0 && len(in) > limit {
		return in[:limit]
	}
	return in
}
